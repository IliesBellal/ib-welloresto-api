package ubereats

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/marketplacecatalog"
	"welloresto-api/internal/modules/openinghours"
)

// Publication du menu WelloResto vers Uber Eats (docs/ubereats-menu-sync.md).
//
//  1. Le menu de l'établissement est traduit en catalogue commun
//     (marketplacecatalog), puis en document Uber Eats (menu_document.go) dont
//     les horaires sont les horaires d'ouverture de l'établissement.
//  2. Un document identique au dernier publié avec succès n'est pas renvoyé
//     (l'empreinte ignore les ruptures de stock : elles passent par la mise à
//     jour d'article, menu_availability.go).
//  3. Les tables de correspondance de l'import des commandes sont mises à jour
//     AVANT l'envoi : une commande passée sur le nouveau menu retrouve toujours
//     son produit.
//  4. L'envoi REMPLACE le menu du restaurant chez Uber Eats. Il est synchrone
//     (204) : le journal est clos tout de suite, aucun webhook à attendre.
//  5. Le premier envoi d'un compte, demandé par le restaurateur, exige sa
//     confirmation explicite (ErrConfirmationRequired).

// ErrConfirmationRequired : le compte n'a jamais reçu de menu de WelloResto ; le
// premier envoi remplace le menu actuel d'Uber Eats et doit être confirmé.
var ErrConfirmationRequired = errors.New("ubereats menu: le premier envoi remplace le menu actuel d'Uber Eats, confirmation requise")

// ErrNoAccount : aucun compte Uber Eats actif pour cet établissement.
var ErrNoAccount = errors.New("ubereats menu: aucun compte Uber Eats connecté")

const syncSending = "sending"

// MenuLoader charge le menu d'un établissement (satisfait par
// *menu.MenuRepository ; interface pour éviter un cycle d'import).
type MenuLoader interface {
	GetMenuWithMarketingCategories(ctx context.Context, merchantID string) (*models.MenuResponse, error)
}

// SetMenuPublishing branche le chargement du menu (appelé au démarrage, routes.go).
func (s *UberEatsService) SetMenuPublishing(loader MenuLoader) {
	s.menuLoader = loader
}

// PublishOptions paramètre une publication.
type PublishOptions struct {
	// StoreID : compte Uber Eats ; vide = compte principal.
	StoreID string
	// Origin : manual (bouton du back-office), auto (synchro automatique),
	// uber_request (demande d'Uber Eats)... (marketplace_menu_syncs.origin).
	Origin string
	// Force republie même un menu identique.
	Force bool
	// ConfirmReplace : le restaurateur a confirmé que le menu actuel d'Uber Eats
	// sera remplacé. Exigé pour le premier envoi d'une publication « manual ».
	ConfirmReplace bool
}

// PublishResult résume une publication.
type PublishResult struct {
	SyncID      int64    `json:"sync_id"`
	Status      string   `json:"status"`
	StoreID     string   `json:"store_id"`
	ItemCount   int      `json:"item_count"`
	Warnings    []string `json:"warnings,omitempty"`
	Fingerprint string   `json:"fingerprint"`
}

// accountLocks sérialise publication et alignement d'un même compte.
type accountLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (a *accountLocks) get(key string) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locks == nil {
		a.locks = map[string]*sync.Mutex{}
	}
	m, ok := a.locks[key]
	if !ok {
		m = &sync.Mutex{}
		a.locks[key] = m
	}
	return m
}

// PublishMenu publie le menu WelloResto d'un établissement sur un compte Uber Eats.
func (s *UberEatsService) PublishMenu(ctx context.Context, merchantID string, opts PublishOptions) (*PublishResult, error) {
	log := logger.FromContext(ctx)
	if s.menuLoader == nil {
		return nil, errors.New("ubereats menu: publication non configurée")
	}

	store, err := s.resolveStore(ctx, merchantID, opts.StoreID)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrNoAccount
	}

	lock := s.locks.get(merchantID + "/" + store.StoreID)
	lock.Lock()
	defer lock.Unlock()

	origin := nonEmpty(opts.Origin, "manual")
	rec := SyncRecord{MerchantID: merchantID, StoreID: store.StoreID, Origin: origin, Status: syncSending}

	if origin == "manual" && !opts.ConfirmReplace {
		published, err := s.repo.HasPublished(ctx, merchantID, store.StoreID)
		if err != nil {
			return nil, fmt.Errorf("ubereats menu: lecture du journal (migration 201 jouée ?): %w", err)
		}
		if !published {
			return nil, ErrConfirmationRequired
		}
	}

	built, err := s.build(ctx, merchantID, store)
	if err != nil {
		// Un échec de construction (horaires manquants, rien à publier...) est
		// dit au restaurateur ; la synchro automatique, elle, ne remplit pas le
		// journal à chaque vérification.
		if origin != "auto" {
			rec.Status = syncFailed
			rec.Error = UserMessage(err)
			if _, insErr := s.repo.InsertSync(ctx, rec); insErr != nil {
				log.Warn("[UBER EATS] échec de publication non journalisé", zap.Error(insErr))
			}
		}
		return nil, err
	}

	result := &PublishResult{
		StoreID:     store.StoreID,
		ItemCount:   len(built.ProductItemIDs),
		Warnings:    built.Warnings,
		Fingerprint: built.Fingerprint,
	}
	rec.Fingerprint, rec.ItemCount, rec.ItemIDs = built.Fingerprint, len(built.ProductItemIDs), built.ProductItemIDs

	if !opts.Force {
		prev, err := s.repo.LastSuccessfulFingerprint(ctx, merchantID, store.StoreID)
		if err != nil {
			return nil, err
		}
		if prev == built.Fingerprint {
			result.Status = syncUnchanged
			// La republication automatique vérifie souvent : une ligne par
			// vérification noierait le journal du restaurateur.
			if origin == "auto" {
				return result, nil
			}
			rec.Status = syncUnchanged
			id, err := s.repo.InsertSync(ctx, rec)
			if err != nil {
				return nil, err
			}
			result.SyncID = id
			return result, nil
		}
	}

	id, err := s.repo.InsertSync(ctx, rec)
	if err != nil {
		return nil, err
	}
	result.SyncID = id

	// Le compte entre dans la file de réveils : alignement des disponibilités,
	// republication automatique des changements de menu.
	if err := s.repo.EnsureSyncState(ctx, merchantID, store.StoreID); err != nil {
		log.Warn("[UBER EATS] compte non inscrit dans la file de réveils (migration 202 jouée ?)", zap.Error(err))
	}

	// Correspondances AVANT l'envoi : une commande sur le nouveau menu doit
	// retrouver son produit, jamais en créer un nouveau.
	if err := s.repo.UpsertPublishedMappings(ctx, merchantID, store.StoreID, built); err != nil {
		_ = s.repo.FinishSync(ctx, id, syncFailed, 0, nil, "correspondances produits: "+err.Error())
		return nil, fmt.Errorf("ubereats menu: correspondances produits: %w", err)
	}

	token, err := s.GetValidToken(ctx)
	if err != nil {
		_ = s.repo.FinishSync(ctx, id, syncFailed, 0, nil, "jeton Uber Eats: "+err.Error())
		return nil, err
	}
	httpStatus, err := s.client.UploadMenu(ctx, store.StoreID, token, built.Menu)
	if err != nil {
		_ = s.repo.FinishSync(ctx, id, syncFailed, httpStatus, built.Warnings, err.Error())
		return nil, err
	}

	// Le menu envoyé porte les suspensions du moment : c'est l'état de référence
	// de l'alignement des disponibilités.
	if err := s.repo.ReplaceItemStates(ctx, merchantID, store.StoreID, built.States); err != nil {
		log.Error("[UBER EATS] état envoyé non mémorisé (migration 203 jouée ?)", zap.Error(err))
	}
	if err := s.repo.FinishSync(ctx, id, syncSuccess, httpStatus, built.Warnings, ""); err != nil {
		return nil, err
	}
	if err := s.repo.MarkAccountSynced(ctx, merchantID, store.StoreID, len(built.ProductItemIDs)); err != nil {
		log.Warn("[UBER EATS] last_sync non mis à jour", zap.Error(err))
	}
	result.Status = syncSuccess
	log.Info("[UBER EATS] menu envoyé",
		zap.String("merchant_id", merchantID), zap.String("store_id", store.StoreID),
		zap.String("origin", origin), zap.Int64("sync_id", id), zap.Int("items", len(built.ProductItemIDs)))
	return result, nil
}

// build charge le catalogue, les horaires et construit le document.
func (s *UberEatsService) build(ctx context.Context, merchantID string, store *Store) (*BuiltMenu, error) {
	cat, err := s.loadCatalog(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	hours, err := s.loadOpeningHours(ctx, merchantID, store.Timezone)
	if err != nil {
		return nil, err
	}
	name, err := s.repo.GetMerchantName(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return BuildMenu(cat, name, hours)
}

func (s *UberEatsService) loadCatalog(ctx context.Context, merchantID string) (*marketplacecatalog.Catalog, error) {
	menu, err := s.menuLoader.GetMenuWithMarketingCategories(ctx, merchantID)
	if err != nil {
		return nil, fmt.Errorf("ubereats menu: chargement du menu: %w", err)
	}
	extras, err := s.repo.LoadProductExtras(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return marketplacecatalog.Build(menu, marketplacecatalog.UberEats, extras)
}

// loadOpeningHours lit les horaires d'ouverture actifs de l'établissement (dans
// son fuseau, comme la caisse) et les convertit en plages Uber Eats.
func (s *UberEatsService) loadOpeningHours(ctx context.Context, merchantID, timezone string) ([]ServiceAvailability, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" {
		loc = time.UTC
	}
	slots, err := openinghours.FetchActiveSlots(ctx, s.db, merchantID, time.Now().In(loc))
	if err != nil {
		return nil, fmt.Errorf("ubereats menu: horaires d'ouverture: %w", err)
	}
	return ServiceAvailabilityFromSlots(slots), nil
}

// UserMessage traduit une erreur de publication en phrase pour le restaurateur.
func UserMessage(err error) string {
	switch {
	case errors.Is(err, ErrNoOpeningHours):
		return "Renseignez d'abord vos horaires d'ouverture : Uber Eats en a besoin pour afficher votre menu."
	case errors.Is(err, ErrNothingPublishable), errors.Is(err, marketplacecatalog.ErrNothingToPublish):
		return "Aucun produit à publier : cochez Uber Eats sur au moins un produit visible."
	case errors.Is(err, ErrNoAccount):
		return "Aucun compte Uber Eats n'est connecté."
	}
	return err.Error()
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// ListMenuSyncs : journal des publications de menu de l'établissement
// (écran « Synchronisation » du back-office).
func (s *UberEatsService) ListMenuSyncs(ctx context.Context, merchantID, storeID string, limit int) ([]SyncSummary, error) {
	return s.repo.ListSyncs(ctx, merchantID, storeID, limit)
}
