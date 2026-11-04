package deliveroo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/marketplacecatalog"
)

// Réveils par compte (docs/deliveroo-menu-sync.md, « Réveils »).
//
// Un seul cron léger (RunWakeups, chaque minute) ne traite que les comptes
// dont le réveil est échu : le coût suit le besoin, pas le nombre
// d'établissements. Un réveil peut :
//   - lire les horaires d'ouverture (une fois par jour) ;
//   - republier le menu s'il a réellement changé (calme de menuQuietPeriod) ;
//   - attendre l'ouverture réelle du site puis aligner les disponibilités ;
//   - aligner les disponibilités (changement, filet de sécurité).

const (
	// menuQuietPeriod : délai sans modification du catalogue avant de recalculer
	// le menu. Deliveroo limite d'ailleurs l'envoi à un menu par minute et par site.
	menuQuietPeriod = 3 * time.Minute

	// openingRetryDelay / maxOpeningAttempts : attente que le site passe OPEN
	// après l'heure prévue (la tablette peut être en retard).
	openingRetryDelay  = 10 * time.Minute
	maxOpeningAttempts = 6
	// recheckDelay : second contrôle après un réveil d'ouverture.
	recheckDelay = 30 * time.Minute

	// Filet de sécurité : 2 h + jusqu'à 40 min d'étalement, pour ne pas réveiller
	// tous les comptes en même temps.
	safetyBase   = 2 * time.Hour
	safetyJitter = 40 * time.Minute

	openingRefreshAge = 24 * time.Hour

	// pendingResultTimeout : une publication acceptée sans résultat depuis ce
	// délai est interrogée (V3) ou déclarée non confirmée (V1).
	pendingResultTimeout = 10 * time.Minute
	// maxJobWait : au-delà, un job V3 encore en cours est déclaré non confirmé.
	maxJobWait = 30 * time.Minute

	wakeBatch       = 40
	wakeConcurrency = 4
	wakeTimeout     = 2 * time.Minute

	// autoPublishAPIVersion : version d'API de la republication automatique.
	// V1 : son webhook de résultat est documenté. Passer à V3 après validation
	// du webhook V3 (scénario 15).
	autoPublishAPIVersion = APIVersionV1
)

// syncUnconfirmed : menu accepté par Deliveroo (réponse 200 / job créé) sans
// résultat confirmé par webhook dans pendingResultTimeout.
const syncUnconfirmed = "unconfirmed"

// RunWakeups traite les comptes à réveiller. Appelé chaque minute par le cron.
func (s *DeliverooService) RunWakeups(ctx context.Context) {
	log := logger.FromContext(ctx)
	if s.menuLoader == nil {
		return
	}

	s.resolveStaleSyncs(ctx)

	due, err := s.repo.ClaimDue(ctx, menuQuietPeriod, wakeBatch)
	if err != nil {
		log.Error("[DELIVEROO] réveils: lecture de la file (migration 202 jouée ?)", zap.Error(err))
		return
	}
	if len(due) == 0 {
		return
	}

	sem := make(chan struct{}, wakeConcurrency)
	var wg sync.WaitGroup
	for _, st := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(st SyncState) {
			defer wg.Done()
			defer func() { <-sem }()
			wctx, cancel := context.WithTimeout(ctx, wakeTimeout)
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					log.Error(fmt.Sprintf("[DELIVEROO] panic réveil %s/%s: %v", st.MerchantID, st.LocationID, r))
				}
			}()
			s.processWakeup(wctx, st)
		}(st)
	}
	wg.Wait()
}

func (s *DeliverooService) processWakeup(ctx context.Context, st SyncState) {
	log := logger.FromContext(ctx).With(zap.String("merchant_id", st.MerchantID), zap.String("location_id", st.LocationID))

	acc := st.PublishedAccount
	if acc.BrandID == "" {
		// Intégration désactivée ou brand_id perdu : le compte sort de la file.
		log.Warn("[DELIVEROO] réveil: compte sans brand_id, retiré de la file")
		_ = s.repo.DropSyncState(ctx, st.MerchantID, st.LocationID)
		return
	}

	st = s.refreshOpening(ctx, st)

	// 1. Menu modifié : recalculé, republié seulement s'il a vraiment changé.
	if st.MenuDirtySince != nil && time.Since(*st.MenuDirtySince) >= menuQuietPeriod {
		s.autoPublish(ctx, st)
	}

	// 2. Réveil d'ouverture : attendre que le site soit réellement ouvert.
	if st.CheckKind == checkOpening {
		status, err := s.client.GetSiteStatus(ctx, acc.BrandID, acc.LocationID)
		if err == nil && status != SiteStatusOpen && st.Attempts < maxOpeningAttempts {
			log.Info("[DELIVEROO] site pas encore ouvert, nouvel essai", zap.String("status", status), zap.Int("tentative", st.Attempts+1))
			_ = s.repo.ScheduleCheck(ctx, st.MerchantID, st.LocationID, time.Now().Add(openingRetryDelay), checkOpening, st.Attempts+1, "")
			return
		}
		if err != nil {
			log.Warn("[DELIVEROO] statut du site illisible, alignement quand même", zap.Error(err))
		}
	}

	// 3. Alignement des disponibilités.
	if _, err := s.ReconcileAvailability(ctx, acc); err != nil {
		log.Error("[DELIVEROO] réveil: alignement échoué", zap.Error(err))
		_ = s.repo.ScheduleCheck(ctx, st.MerchantID, st.LocationID, time.Now().Add(availabilityFallback), checkRetry, 0, err.Error())
		return
	}
	s.planNext(ctx, acc, st.CheckKind)
}

// refreshOpening relit horaires et fuseau chez Deliveroo, au plus une fois par
// jour. Meilleur effort : en cas d'échec on garde les anciennes valeurs.
func (s *DeliverooService) refreshOpening(ctx context.Context, st SyncState) SyncState {
	if len(st.OpeningHours) > 0 && time.Since(st.OpeningRefreshedAt) < openingRefreshAge {
		return st
	}
	log := logger.FromContext(ctx)
	hours, err := s.client.GetOpeningHours(ctx, st.BrandID, st.LocationID)
	if err != nil {
		log.Warn("[DELIVEROO] horaires d'ouverture illisibles", zap.String("location_id", st.LocationID), zap.Error(err))
		return st
	}
	tz, err := s.client.GetTimeZone(ctx, st.BrandID, st.LocationID)
	if err != nil || tz == "" {
		tz = st.TimeZone
	}
	if err := s.repo.SaveOpening(ctx, st.MerchantID, st.LocationID, hours, tz); err != nil {
		log.Warn("[DELIVEROO] horaires non enregistrés", zap.Error(err))
		return st
	}
	st.OpeningHours, st.TimeZone, st.OpeningRefreshedAt = hours, tz, time.Now()
	return st
}

// planNext programme le prochain réveil : le plus proche entre l'ouverture
// suivante, le second contrôle après une ouverture, le menu à revérifier et le
// filet de sécurité.
func (s *DeliverooService) planNext(ctx context.Context, acc PublishedAccount, justRan string) {
	log := logger.FromContext(ctx)
	st, ok := s.loadStateForPlanning(ctx, acc)
	if !ok {
		return
	}
	now := time.Now()
	at, kind := now.Add(safetyBase+time.Duration(rand.Int63n(int64(safetyJitter)))), checkSafety

	consider := func(t time.Time, k string) {
		if t.Before(at) {
			at, kind = t, k
		}
	}
	if justRan == checkOpening {
		consider(now.Add(recheckDelay), checkRecheck)
	}
	if loc := s.siteLocation(ctx, st); loc != nil {
		if t, ok := NextOpeningWake(now, loc, st.OpeningHours); ok {
			consider(t, checkOpening)
		}
	}
	if st.MenuDirtySince != nil {
		consider(st.MenuDirtySince.Add(menuQuietPeriod), checkChanged)
	}
	if err := s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.LocationID, at, kind, 0, ""); err != nil {
		log.Warn("[DELIVEROO] prochain réveil non enregistré", zap.Error(err))
	}
}

// loadStateForPlanning relit horaires, fuseau et marqueur de menu du compte.
func (s *DeliverooService) loadStateForPlanning(ctx context.Context, acc PublishedAccount) (SyncState, bool) {
	st, err := s.repo.GetSyncState(ctx, acc.MerchantID, acc.LocationID)
	if err != nil {
		logger.FromContext(ctx).Warn("[DELIVEROO] état du compte illisible", zap.Error(err))
		return SyncState{}, false
	}
	return st, true
}

// siteLocation : fuseau du site (Deliveroo), sinon celui de l'établissement.
func (s *DeliverooService) siteLocation(ctx context.Context, st SyncState) *time.Location {
	for _, name := range []string{st.TimeZone, s.merchantTimezone(ctx, st.MerchantID)} {
		if name == "" {
			continue
		}
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return nil
}

func (s *DeliverooService) merchantTimezone(ctx context.Context, merchantID string) string {
	tz, err := s.repo.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		return ""
	}
	return tz
}

// autoPublish recalcule le menu d'un compte déjà publié et le republie s'il a
// changé. Le marqueur est effacé AVANT le calcul : une modification arrivée
// pendant la publication le repose, elle n'est donc jamais perdue.
func (s *DeliverooService) autoPublish(ctx context.Context, st SyncState) {
	log := logger.FromContext(ctx).With(zap.String("merchant_id", st.MerchantID), zap.String("location_id", st.LocationID))

	prev, err := s.repo.LastSuccessfulFingerprint(ctx, st.MerchantID, st.LocationID)
	if err != nil {
		log.Error("[DELIVEROO] republication: journal illisible", zap.Error(err))
		return
	}
	if err := s.repo.ClearMenuDirty(ctx, st.MerchantID, st.LocationID, *st.MenuDirtySince); err != nil {
		log.Error("[DELIVEROO] republication: marqueur non effacé", zap.Error(err))
		return
	}
	if prev == "" {
		return // jamais publié avec succès : la première publication reste manuelle
	}

	res, err := s.PublishMenu(ctx, st.MerchantID, PublishOptions{LocationID: st.LocationID, Origin: "auto", APIVersion: autoPublishAPIVersion})
	switch {
	case err == nil && res.Status == syncUnchanged:
		return
	case err == nil:
		log.Info("[DELIVEROO] menu republié automatiquement", zap.Int64("sync_id", res.SyncID), zap.Int("articles", res.ItemCount))
	case errors.Is(err, marketplacecatalog.ErrNothingToPublish):
		log.Info("[DELIVEROO] republication: plus aucun produit coché, menu en ligne inchangé")
	default:
		// Échec (dont envoi trop rapproché) : on repose le marqueur, nouvel essai
		// après le délai de calme.
		log.Warn("[DELIVEROO] republication automatique échouée", zap.Error(err))
		_ = s.repo.MarkMenuDirty(ctx, st.MerchantID)
		_ = s.repo.ScheduleCheck(ctx, st.MerchantID, st.LocationID, time.Now().Add(menuQuietPeriod), checkRetry, 0, err.Error())
	}
}

// resolveStaleSyncs clôt les publications acceptées dont le webhook de résultat
// n'est jamais arrivé : un job V3 est interrogé, le reste est déclaré non
// confirmé. Dans les deux cas les disponibilités sont alignées ensuite.
func (s *DeliverooService) resolveStaleSyncs(ctx context.Context) {
	log := logger.FromContext(ctx)
	stale, err := s.repo.ListStaleProcessing(ctx, pendingResultTimeout, 10)
	if err != nil {
		log.Error("[DELIVEROO] publications en attente illisibles", zap.Error(err))
		return
	}
	for _, rec := range stale {
		s.resolveStale(ctx, rec)
	}
}

func (s *DeliverooService) resolveStale(ctx context.Context, rec SyncRecord) {
	log := logger.FromContext(ctx).With(zap.Int64("sync_id", rec.ID), zap.String("location_id", rec.LocationID))

	status, final := syncUnconfirmed, true
	var httpStatus int
	var errText string

	if rec.APIVersion == APIVersionV3 && rec.JobID != "" {
		job, err := s.client.GetJobStatus(ctx, rec.BrandID, rec.JobID)
		switch {
		case err != nil:
			log.Warn("[DELIVEROO] statut du job illisible", zap.Error(err))
			final = false
		case job.Status == "success":
			status, httpStatus = syncSuccess, 200
		case job.Status == "failed":
			status, httpStatus = syncFailed, 400
			errText = "Job Deliveroo en échec : " + jobOutcomeText(job)
		default: // new, in_progress
			final = time.Since(rec.CreatedAt) > maxJobWait
		}
	}
	if !final {
		return
	}
	if status == syncUnconfirmed {
		errText = "Menu accepté par Deliveroo ; résultat non confirmé par webhook."
	}
	if err := s.repo.FinishSync(ctx, rec.ID, status, httpStatus, nil, errText); err != nil {
		log.Error("[DELIVEROO] publication non clôturée", zap.Error(err))
		return
	}
	log.Info("[DELIVEROO] publication clôturée sans webhook", zap.String("status", status))
	if status == syncFailed {
		return
	}
	if err := s.repo.MarkAccountSynced(ctx, rec.MerchantID, rec.LocationID, rec.ItemCount); err != nil {
		log.Warn("[DELIVEROO] last_sync non mis à jour", zap.Error(err))
	}
	_ = s.repo.EnsureSyncState(ctx, rec.MerchantID, rec.LocationID)
	s.scheduleAvailability(PublishedAccount{MerchantID: rec.MerchantID, LocationID: rec.LocationID, BrandID: rec.BrandID})
}

func jobOutcomeText(job *JobStatusResponse) string {
	if len(job.Outcome) == 0 || string(job.Outcome) == "null" {
		return "raison non précisée"
	}
	var s string
	if json.Unmarshal(job.Outcome, &s) == nil {
		return s
	}
	return string(job.Outcome)
}
