package deliveroo_menu

import (
	"context"
	"fmt"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/deliveroo"

	"go.uber.org/zap"
)

type MenuWebhookService struct {
	repo       *Repository
	httpClient *deliveroo.DeliverooService
}

func NewMenuWebhookService(repo *Repository, client *deliveroo.DeliverooService) *MenuWebhookService {
	return &MenuWebhookService{
		repo:       repo,
		httpClient: client,
	}
}

func (s *MenuWebhookService) ProcessMenuUploadResult(ctx context.Context, payload MenuWebhookPayload) error {
	log := logger.FromContext(ctx)

	log.Info(fmt.Sprintf("Menu Webhook received: MenuID=%s, Status=%s", payload.MenuID, payload.Status))

	// Si l'upload a échoué chez Deliveroo, on log les erreurs
	if payload.Status == "FAILURE" {
		log.Error(fmt.Sprintf("Menu upload failed for MenuID %s. Errors: %v", payload.MenuID, payload.Errors))
		// Tu pourrais ici déclencher une alerte, un email au resto, ou mettre à jour un statut en base.
		return nil // On retourne nil car on a "traité" l'info (pas besoin que Deliveroo réessaie d'envoyer le webhook)
	}

	if payload.Status == "SUCCESS" {
		// --- LOGIQUE SCENARIO 13 ---
		// Le menu est maintenant prêt. C'est le moment de mettre à jour les indisponibilités
		// si on avait des items en rupture de stock.

		log.Info("Menu upload SUCCESS. Proceeding with stock sync if needed...")

		// Note : Comme on doit répondre vite au Webhook (souvent < 3 secondes),
		// et que la synchro des stocks peut prendre un peu de temps (appels API supplémentaires),
		// il est recommandé de lancer cela dans une Goroutine.

		go func(menuID string, sites []string, asyncLog *zap.Logger) {
			// On crée un nouveau contexte car le contexte HTTP va mourir à la fin de la requête
			bgCtx := context.Background()

			for _, siteID := range sites {
				// 1. Récupérer le BrandID depuis la base en fonction du SiteID ou MenuID
				brandID, err := s.repo.GetBrandIDBySiteID(bgCtx, siteID) // Méthode à créer dans Repository
				if err != nil {
					asyncLog.Error(fmt.Sprintf("Could not find BrandID for SiteID %s: %v", siteID, err))
					continue
				}

				// 2. Exemple pour le Scénario 13 : Mettre un item indisponible
				// Dans la vraie vie, tu irais chercher les vrais items en rupture dans ta base
				stockPayload := map[string]any{
					"item_unavailabilities": []map[string]any{
						{
							"item_id": "item_1", // L'item du scénario 13
							"status":  "unavailable",
						},
					},
				}

				err = s.httpClient.UpdateIndividualUnavailabilities(bgCtx, brandID, menuID, siteID, stockPayload)
				if err != nil {
					asyncLog.Error(fmt.Sprintf("Failed to update stock after menu upload for site %s: %v", siteID, err))
				} else {
					asyncLog.Info(fmt.Sprintf("Stock successfully updated after menu upload for site %s", siteID))
				}
			}
		}(payload.MenuID, payload.SiteIDs, log)
	}

	return nil
}
