package deliveroo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/marketplacecatalog"
)

// Publication du menu WelloResto vers Deliveroo (docs/deliveroo-menu-sync.md).
//
//  1. Le menu de l'établissement est traduit en catalogue commun
//     (marketplacecatalog), puis en document Deliveroo.
//  2. Un document identique au dernier publié avec succès n'est pas renvoyé.
//  3. Les tables de correspondance de l'import des commandes sont mises à jour
//     AVANT l'envoi : une commande passée sur le nouveau menu retrouve toujours
//     son produit.
//  4. Envoi en V1 (PUT synchrone, validation immédiate) ou en V3 (URL
//     présignée S3 + job).
//  5. Le webhook menu.upload_result clôt la publication (CompleteMenuUpload),
//     puis les disponibilités de tous les articles publiés sont alignées sur
//     WelloResto : c'est le scénario 13 de la certification. Si Deliveroo
//     répond MATCH_EXISTING_MENU, aucun webhook ne viendra : l'alignement est
//     fait tout de suite, comme le recommande la doc.

// Versions d'API de publication.
const (
	APIVersionV1 = "v1"
	APIVersionV3 = "v3"
)

// minPublishInterval : Deliveroo limite l'envoi de menu à 1 par minute et par site.
const minPublishInterval = time.Minute

// availabilityTimeout borne l'alignement des disponibilités lancé en tâche de fond.
const availabilityTimeout = 60 * time.Second

// ErrPublishTooSoon : un menu a déjà été envoyé pour ce site il y a moins d'une minute.
var ErrPublishTooSoon = errors.New("deliveroo menu: un menu a été envoyé il y a moins d'une minute, réessayez dans un instant")

// MenuLoader charge le menu d'un établissement (satisfait par
// *menu.MenuRepository ; interface pour éviter un cycle d'import).
type MenuLoader interface {
	GetMenuWithMarketingCategories(ctx context.Context, merchantID string) (*models.MenuResponse, error)
}

// PublishOptions paramètre une publication.
type PublishOptions struct {
	// LocationID : compte Deliveroo (site) ; vide = compte principal.
	LocationID string
	// Origin : manual, scenario_13, scenario_15... (marketplace_menu_syncs.origin).
	Origin string
	// APIVersion : APIVersionV1 (défaut) ou APIVersionV3.
	APIVersion string
	// Force republie même un menu identique, en y ajoutant une révision
	// datée (scénarios de certification : ils exigent un webhook).
	Force bool
}

// PublishResult résume une publication.
type PublishResult struct {
	SyncID      int64    `json:"sync_id"`
	Status      string   `json:"status"`
	APIVersion  string   `json:"api_version"`
	LocationID  string   `json:"location_id"`
	ItemCount   int      `json:"item_count"`
	JobID       string   `json:"job_id,omitempty"`
	Result      string   `json:"result,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Fingerprint string   `json:"fingerprint"`
}

// SetMenuPublishing branche le chargement du menu et le stockage des images
// de couverture (appelé au démarrage, routes.go).
func (s *DeliverooService) SetMenuPublishing(loader MenuLoader, images ImageStore) {
	s.menuLoader = loader
	s.images = images
}

// PublishMenu publie le menu WelloResto d'un établissement sur un compte Deliveroo.
func (s *DeliverooService) PublishMenu(ctx context.Context, merchantID string, opts PublishOptions) (*PublishResult, error) {
	log := logger.FromContext(ctx)
	if s.menuLoader == nil {
		return nil, errors.New("deliveroo menu: publication non configurée")
	}
	version := opts.APIVersion
	if version == "" {
		version = APIVersionV1
	}
	if version != APIVersionV1 && version != APIVersionV3 {
		return nil, fmt.Errorf("deliveroo menu: version d'API inconnue %q", version)
	}

	account, err := s.repo.resolveAccount(ctx, merchantID, opts.LocationID)
	if err != nil {
		return nil, err
	}
	if account.BrandID == "" {
		brandID, err := s.ValidateAndSyncBrandID(ctx, merchantID, account.LocationID)
		if err != nil {
			return nil, err
		}
		account.BrandID = brandID
	}

	last, err := s.repo.LastSentAt(ctx, merchantID, account.LocationID)
	if err != nil {
		return nil, fmt.Errorf("deliveroo menu: lecture du journal (migration 201 jouée ?): %w", err)
	}
	if !last.IsZero() && time.Since(last) < minPublishInterval {
		return nil, ErrPublishTooSoon
	}

	revision := ""
	if opts.Force {
		revision = "rév. " + time.Now().UTC().Format("20060102-150405")
	}
	built, err := s.buildDocument(ctx, merchantID, account.LocationID, revision)
	if err != nil {
		return nil, err
	}

	result := &PublishResult{
		APIVersion:  version,
		LocationID:  account.LocationID,
		ItemCount:   len(built.ProductItemIDs),
		Warnings:    built.Warnings,
		Fingerprint: built.Fingerprint,
	}
	record := SyncRecord{
		MerchantID:  merchantID,
		LocationID:  account.LocationID,
		BrandID:     account.BrandID,
		MenuID:      PublishedMenuID,
		Origin:      nonEmpty(opts.Origin, "manual"),
		APIVersion:  version,
		Status:      syncSending,
		Fingerprint: built.Fingerprint,
		ItemCount:   len(built.ProductItemIDs),
		ItemIDs:     built.ProductItemIDs,
	}

	if !opts.Force {
		prev, err := s.repo.LastSuccessfulFingerprint(ctx, merchantID, account.LocationID)
		if err != nil {
			return nil, err
		}
		if prev == built.Fingerprint {
			record.Status = syncUnchanged
			id, err := s.repo.InsertSync(ctx, record)
			if err != nil {
				return nil, err
			}
			result.SyncID, result.Status = id, syncUnchanged
			return result, nil
		}
	}

	id, err := s.repo.InsertSync(ctx, record)
	if err != nil {
		return nil, err
	}
	record.ID, result.SyncID = id, id

	// Correspondances AVANT l'envoi : une commande sur le nouveau menu doit
	// retrouver son produit, jamais en créer un nouveau.
	if err := s.repo.UpsertPublishedMappings(ctx, merchantID, account.LocationID, built.ProductNames, built.OptionItems); err != nil {
		_ = s.repo.MarkSyncSent(ctx, id, syncFailed, 0, "", nil, "correspondances produits: "+err.Error())
		return nil, fmt.Errorf("deliveroo menu: correspondances produits: %w", err)
	}

	switch version {
	case APIVersionV1:
		resp, httpStatus, err := s.client.PutMenuV1(ctx, account.BrandID, PublishedMenuID, built.Document)
		if err != nil {
			_ = s.repo.MarkSyncSent(ctx, id, syncFailed, httpStatus, "", nil, err.Error())
			return nil, err
		}
		result.Warnings = append(result.Warnings, resp.Warnings...)
		result.Result = resp.Result
		if resp.Result == ResultMatchExistingMenu {
			// Pas de webhook à attendre : le menu en ligne est déjà celui-ci.
			result.Status = syncSuccess
			if err := s.repo.MarkSyncSent(ctx, id, syncSuccess, httpStatus, "", resp.Warnings, ""); err != nil {
				return nil, err
			}
			if err := s.repo.MarkAccountSynced(ctx, merchantID, account.LocationID, record.ItemCount); err != nil {
				log.Warn("[DELIVEROO] last_sync non mis à jour", zap.Error(err))
			}
			s.applyAvailabilityAsync(record)
			return result, nil
		}
		result.Status = syncProcessing
		if err := s.repo.MarkSyncSent(ctx, id, syncProcessing, httpStatus, "", resp.Warnings, ""); err != nil {
			return nil, err
		}

	case APIVersionV3:
		up, err := s.client.GenerateUploadURL(ctx, account.BrandID, PublishedMenuID)
		if err != nil {
			_ = s.repo.MarkSyncSent(ctx, id, syncFailed, 0, "", nil, "URL présignée: "+err.Error())
			return nil, err
		}
		// L'URL présignée n'est valable que quelques secondes : dépôt immédiat.
		if err := s.client.UploadToS3(ctx, up.UploadURL, built.Document); err != nil {
			_ = s.repo.MarkSyncSent(ctx, id, syncFailed, 0, "", nil, "dépôt S3: "+err.Error())
			return nil, err
		}
		job, err := s.client.CreateMenuJob(ctx, account.BrandID, JobRequest{
			Action: "publish_menu_to_live",
			Params: JobParams{MenuID: PublishedMenuID, Version: up.Version},
		})
		if err != nil {
			_ = s.repo.MarkSyncSent(ctx, id, syncFailed, 0, "", nil, "création du job: "+err.Error())
			return nil, err
		}
		result.JobID, result.Status = job.JobID, syncProcessing
		if err := s.repo.MarkSyncSent(ctx, id, syncProcessing, 200, job.JobID, nil, ""); err != nil {
			return nil, err
		}
	}

	log.Info("[DELIVEROO] menu envoyé, résultat attendu par webhook",
		zap.String("merchant_id", merchantID), zap.String("location_id", account.LocationID),
		zap.String("api_version", version), zap.Int64("sync_id", id), zap.Int("items", record.ItemCount))
	return result, nil
}

// buildDocument charge le menu et construit le document Deliveroo.
func (s *DeliverooService) buildDocument(ctx context.Context, merchantID, locationID, revision string) (*BuiltDocument, error) {
	cat, err := s.loadCatalog(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	banner, err := s.repo.GetBannerURL(ctx, merchantID)
	if err != nil {
		return nil, fmt.Errorf("deliveroo menu: bannière: %w", err)
	}
	cover, err := coverImageURL(ctx, s.images, merchantID, banner)
	if err != nil {
		return nil, err
	}
	name, err := s.repo.GetMerchantName(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return BuildMenuDocument(cat, DocumentOptions{
		SiteID:        locationID,
		MenuName:      name,
		CoverImageURL: cover,
		Revision:      revision,
	})
}

func (s *DeliverooService) loadCatalog(ctx context.Context, merchantID string) (*marketplacecatalog.Catalog, error) {
	menu, err := s.menuLoader.GetMenuWithMarketingCategories(ctx, merchantID)
	if err != nil {
		return nil, fmt.Errorf("deliveroo menu: chargement du menu: %w", err)
	}
	extras, err := s.repo.LoadProductExtras(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return marketplacecatalog.Build(menu, marketplacecatalog.Deliveroo, extras)
}

// CompleteMenuUpload traite le webhook menu.upload_result : clôt la
// publication en attente de chaque site, puis aligne les disponibilités si le
// menu est en ligne.
func (s *DeliverooService) CompleteMenuUpload(ctx context.Context, res MenuUploadResult) error {
	log := logger.FromContext(ctx)
	menuID := nonEmpty(res.MenuID, PublishedMenuID)

	var firstErr error
	for _, site := range res.SiteIDs {
		rec, err := s.repo.FindPendingSync(ctx, site, menuID)
		if err != nil {
			log.Error("[DELIVEROO] webhook menu: lecture du journal", zap.String("site_id", site), zap.Error(err))
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if rec == nil {
			// Menu envoyé par un autre chemin (anciens scénarios, Hub Deliveroo) :
			// on ne touche pas aux disponibilités d'un menu qu'on ne connaît pas.
			log.Warn("[DELIVEROO] webhook menu: aucune publication en attente",
				zap.String("site_id", site), zap.String("menu_id", menuID), zap.Int("http_status", res.HTTPStatus))
			continue
		}

		var uploadErrors any
		if !res.Errors.Empty() {
			uploadErrors = res.Errors
		}

		if res.HTTPStatus == 200 {
			if err := s.repo.FinishSync(ctx, rec.ID, syncSuccess, res.HTTPStatus, uploadErrors, ""); err != nil {
				return err
			}
			if err := s.repo.MarkAccountSynced(ctx, rec.MerchantID, rec.LocationID, rec.ItemCount); err != nil {
				log.Warn("[DELIVEROO] last_sync non mis à jour", zap.Error(err))
			}
			log.Info("[DELIVEROO] menu en ligne", zap.Int64("sync_id", rec.ID), zap.String("site_id", site))
			s.applyAvailabilityAsync(*rec)
			continue
		}

		msg := fmt.Sprintf("Deliveroo a refusé le menu (http_status %d)", res.HTTPStatus)
		if res.Errors != nil && res.Errors.Processing != "" {
			msg += " : " + res.Errors.Processing
		}
		if err := s.repo.FinishSync(ctx, rec.ID, syncFailed, res.HTTPStatus, uploadErrors, msg); err != nil {
			return err
		}
		log.Error("[DELIVEROO] menu refusé", zap.Int64("sync_id", rec.ID), zap.String("site_id", site), zap.String("error", msg))
	}
	return firstErr
}

// applyAvailabilityAsync aligne en tâche de fond les disponibilités du menu
// publié (le webhook doit répondre vite).
func (s *DeliverooService) applyAvailabilityAsync(rec SyncRecord) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), availabilityTimeout)
		defer cancel()
		log := logger.FromContext(ctx)
		defer func() {
			if r := recover(); r != nil {
				log.Error(fmt.Sprintf("[DELIVEROO] panic alignement disponibilités: %v", r))
			}
		}()
		if err := s.ApplyPublishedAvailability(ctx, rec); err != nil {
			log.Error("[DELIVEROO] alignement des disponibilités échoué",
				zap.Int64("sync_id", rec.ID), zap.String("site_id", rec.LocationID), zap.Error(err))
		}
	}()
}

// ApplyPublishedAvailability envoie l'état de TOUS les articles produits
// publiés (disponible, épuisé, masqué), en une requête : WelloResto fait foi.
// Seuls les articles du menu publié sont cités (un identifiant inconnu ferait
// échouer tout l'envoi).
func (s *DeliverooService) ApplyPublishedAvailability(ctx context.Context, rec SyncRecord) error {
	cat, err := s.loadCatalog(ctx, rec.MerchantID)
	if err != nil {
		_ = s.repo.SetAvailabilityResult(ctx, rec.ID, err.Error())
		return err
	}
	payload := availabilityPayload(cat.Availabilities(rec.ItemIDs))
	if len(payload.ItemUnavailabilities) == 0 {
		return s.repo.SetAvailabilityResult(ctx, rec.ID, "")
	}
	errText := ""
	if err = s.client.UpdateIndividualUnavailabilities(ctx, rec.BrandID, rec.MenuID, rec.LocationID, payload); err != nil {
		errText = err.Error()
	}
	if setErr := s.repo.SetAvailabilityResult(ctx, rec.ID, errText); setErr != nil && err == nil {
		err = setErr
	}
	return err
}

// ItemUnavailabilitiesRequest : corps du POST item_unavailabilities.
type ItemUnavailabilitiesRequest struct {
	ItemUnavailabilities []ItemUnavailability `json:"item_unavailabilities"`
}

type ItemUnavailability struct {
	ItemID string `json:"item_id"`
	Status string `json:"status"`
}

func availabilityPayload(states map[string]marketplacecatalog.Availability) ItemUnavailabilitiesRequest {
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := ItemUnavailabilitiesRequest{ItemUnavailabilities: make([]ItemUnavailability, 0, len(ids))}
	for _, id := range ids {
		out.ItemUnavailabilities = append(out.ItemUnavailabilities, ItemUnavailability{ItemID: id, Status: UnavailabilityStatus(states[id])})
	}
	return out
}

// IsPublishTooSoon : erreur de cadence (à présenter comme « réessayez »).
func IsPublishTooSoon(err error) bool {
	return err != nil && (errors.Is(err, ErrPublishTooSoon) || strings.Contains(err.Error(), "too_many_requests"))
}
