package ubereats

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/marketplacecatalog"
)

// Réveils des comptes Uber Eats publiés (table marketplace_sync_state). Appelé
// chaque minute par le cron (internal/tasks/ubereats_wakeups.go) ; ne traite que
// les comptes dont le réveil est échu ou dont le menu a changé depuis plus de
// menuQuietPeriod, jamais tous les comptes.
//
// Uber Eats n'a pas d'heure d'ouverture à surveiller (aucune remise à zéro des
// ruptures le matin) : seuls comptent
//   - le menu modifié (catalogue, horaires) → republié s'il a vraiment changé ;
//   - le filet de sécurité toutes les 2 à 3 heures (envoi raté, redémarrage,
//     modification des horaires, produit masqué à retirer) ;
//   - la demande d'Uber Eats (store.menu_refresh_request) → menu renvoyé.

const (
	// menuQuietPeriod : un menu modifié n'est republié qu'après ce délai sans
	// nouveau changement (une modification déclenche souvent plusieurs
	// enregistrements, et republier remplace tout le menu chez Uber Eats).
	menuQuietPeriod = 3 * time.Minute
	// safetyInterval + jitter : filet de sécurité par compte.
	safetyInterval = 2 * time.Hour
	safetyJitter   = 40 * time.Minute
	// retryDelay : nouvelle tentative après un échec de publication.
	retryDelay = 10 * time.Minute
	// wakeupBatch : comptes traités par tour.
	wakeupBatch = 20
	// wakeupTimeout borne le traitement d'un compte.
	wakeupTimeout = 4 * time.Minute
)

func nextSafetyCheck(from time.Time) time.Time {
	return from.Add(safetyInterval + time.Duration(rand.Int63n(int64(safetyJitter))))
}

// RunWakeups traite les comptes à réveiller. Renvoie le nombre de comptes traités.
func (s *UberEatsService) RunWakeups(ctx context.Context) (int, error) {
	if s.menuLoader == nil {
		return 0, nil
	}
	states, err := s.repo.ClaimDue(ctx, menuQuietPeriod, wakeupBatch)
	if err != nil {
		return 0, fmt.Errorf("réveils Uber Eats (migration 202 jouée ?): %w", err)
	}
	for _, st := range states {
		s.processWakeup(ctx, st)
	}
	return len(states), nil
}

func (s *UberEatsService) processWakeup(parent context.Context, st SyncState) {
	ctx, cancel := context.WithTimeout(parent, wakeupTimeout)
	defer cancel()
	log := logger.FromContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("[UBER EATS] panic réveil: %v", r))
			_ = s.repo.ScheduleCheck(context.Background(), st.MerchantID, st.StoreID, time.Now().Add(retryDelay), checkRetry, fmt.Sprint(r))
		}
	}()

	acc := st.PublishedAccount
	fields := []zap.Field{zap.String("merchant_id", acc.MerchantID), zap.String("store_id", acc.StoreID), zap.String("kind", st.CheckKind)}

	enabled, err := s.repo.AccountEnabled(ctx, acc.MerchantID, acc.StoreID)
	if err != nil {
		log.Error("[UBER EATS] réveil: compte illisible", append(fields, zap.Error(err))...)
		_ = s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.StoreID, time.Now().Add(retryDelay), checkRetry, err.Error())
		return
	}
	if !enabled {
		_ = s.repo.DropSyncState(ctx, acc.MerchantID, acc.StoreID)
		return
	}

	// 1. Menu : republié seulement s'il a vraiment changé (ou si Uber le demande).
	_, pubErr := s.PublishMenu(ctx, acc.MerchantID, PublishOptions{
		StoreID: acc.StoreID,
		Origin:  originOf(st.CheckKind),
		Force:   st.CheckKind == checkUber,
	})
	switch {
	case pubErr == nil:
		if st.MenuDirtySince != nil {
			_ = s.repo.ClearMenuDirty(ctx, acc.MerchantID, acc.StoreID, *st.MenuDirtySince)
		}
	case errors.Is(pubErr, ErrNothingPublishable), errors.Is(pubErr, marketplacecatalog.ErrNothingToPublish), errors.Is(pubErr, ErrNoOpeningHours):
		// Rien à (re)publier tant que l'établissement n'a pas corrigé : pas de
		// nouvelle tentative rapprochée, le filet de sécurité repasse.
		log.Warn("[UBER EATS] republication impossible: "+UserMessage(pubErr), fields...)
		if st.MenuDirtySince != nil {
			_ = s.repo.ClearMenuDirty(ctx, acc.MerchantID, acc.StoreID, *st.MenuDirtySince)
		}
	default:
		log.Error("[UBER EATS] republication automatique échouée", append(fields, zap.Error(pubErr))...)
		_ = s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.StoreID, time.Now().Add(retryDelay), checkRetry, pubErr.Error())
		return
	}

	// 2. Disponibilités : alignement sur l'état courant de WelloResto.
	n, err := s.ReconcileAvailability(ctx, acc)
	if err != nil {
		log.Error("[UBER EATS] alignement des disponibilités échoué", append(fields, zap.Error(err))...)
		_ = s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.StoreID, time.Now().Add(availabilityFallback), checkRetry, err.Error())
		return
	}
	if n > 0 {
		log.Info("[UBER EATS] disponibilités alignées", append(fields, zap.Int("articles", n))...)
	}

	_ = s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.StoreID, nextSafetyCheck(time.Now()), checkSafety, "")
}

func originOf(kind string) string {
	if kind == checkUber {
		return "uber_request"
	}
	return "auto"
}

// RequestMenuRefresh traite la demande d'Uber Eats (store.menu_refresh_request) :
// le menu du compte est renvoyé au prochain tour de réveils. Sans effet pour un
// compte dont WelloResto n'a jamais publié le menu.
func (s *UberEatsService) RequestMenuRefresh(ctx context.Context, storeID string) error {
	merchantID, err := s.repo.GetMerchantIDFromStoreID(ctx, storeID)
	if err != nil || merchantID == nil {
		return err
	}
	published, err := s.repo.HasPublished(ctx, *merchantID, storeID)
	if err != nil || !published {
		return err
	}
	return s.repo.ScheduleCheck(ctx, *merchantID, storeID, time.Now(), checkUber, "")
}
