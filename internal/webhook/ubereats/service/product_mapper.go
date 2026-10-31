package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/diagnostics"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// productNameMax : longueur de products.name.
const productNameMax = 255

func (s *Service) mapUberItemsToOrderProducts(
	ctx context.Context,
	merchantID string,
	items []ueModels.UberCartItem,
) ([]ordersModels.OrderProductPayload, error) {

	var result []ordersModels.OrderProductPayload

	for _, item := range items {

		productID, fallback, err := s.resolveProduct(ctx, merchantID, item)
		if err != nil {
			return nil, err
		}

		payload := ordersModels.OrderProductPayload{
			ProductID:   productID,
			ProductName: item.Title,
			Quantity:    item.Quantity,
			Price:       item.Price.UnitPrice.Amount,
			OrderedDate: time.Now().Format(time.RFC3339),
		}

		// Commentaire de l'article : le vrai titre si l'article de repli le remplace,
		// puis les allergies et consignes de préparation du client, puis les options
		// qui n'ont pas pu être rattachées (la cuisine doit les voir).
		var notes []string
		if fallback {
			notes = append(notes, "Article Uber Eats : "+item.Title)
		}
		if content := itemComment(item); content != "" {
			notes = append(notes, content)
		}

		if fallback {
			// Pas d'options structurées sur l'article de repli : elles passent en texte.
			notes = append(notes, optionsAsText(item.SelectedModifierGroups)...)
		} else {
			config, degraded, err := s.mapModifiers(ctx, merchantID, productID, item)
			if err != nil {
				return nil, err
			}
			payload.Config = config
			notes = append(notes, degraded...)
		}

		if len(notes) > 0 {
			payload.Comment = &ordersModels.OrderItemCommentPayload{
				Content: strings.Join(notes, " | "),
				UserID:  models.UberEatsWebhookUserID,
			}
		}

		result = append(result, payload)
	}

	return result, nil
}

// resolveProduct renvoie le produit d'un article Uber Eats. Le chemin courant (mapping
// existant) est une simple lecture. Un article inconnu est créé avec son mapping, en
// une transaction qui tient un verrou propre à l'article : deux commandes simultanées
// ne créent pas deux produits, et un échec ne laisse pas de produit sans mapping.
//
// Si la création échoue malgré tout, la commande ne doit pas être perdue (le client a
// payé) : elle prend l'article de repli de l'établissement (fallback = true) et
// l'anomalie est journalisée.
func (s *Service) resolveProduct(ctx context.Context, merchantID string, item ueModels.UberCartItem) (productID string, fallback bool, err error) {
	existing, err := s.productMappingRepo.FindProductIDByUberItemID(ctx, merchantID, item.ID)
	if err != nil {
		return "", false, err
	}
	if existing != nil {
		return *existing, false, nil
	}

	// Uber n'envoie aucun taux de TVA : 10 %, ou 20 % pour une boisson alcoolisée
	// (étiquettes fiscales d'Uber si elles sont activées, sinon le nom du produit).
	alcohol, known := item.IsAlcoholByLabels()
	if !known {
		alcohol = models.LooksAlcoholic(item.Title)
	}

	createErr := s.inLockedTx(ctx, "uber-product:"+merchantID+":"+item.ID, func(txCtx context.Context) error {
		again, err := s.productMappingRepo.FindProductIDByUberItemID(txCtx, merchantID, item.ID)
		if err != nil {
			return err
		}
		if again != nil {
			productID = *again
			return nil
		}

		newID, err := s.menuService.CreateProductFromExternal(
			txCtx,
			merchantID,
			helpers.TruncateRunes(item.Title, productNameMax),
			"UBER IMPORT",
			item.Price.UnitPrice.Amount,
			alcohol,
		)
		if err != nil {
			return err
		}
		if err := s.productMappingRepo.CreateProductMapping(txCtx, merchantID, *newID, item.ID); err != nil {
			return err
		}
		productID = *newID
		return nil
	})
	if createErr == nil {
		return productID, false, nil
	}

	recordDegraded(ctx, merchantID, "product", item.ID, item.Title, createErr)
	fallbackID, fbErr := s.fallbackProduct(ctx, merchantID)
	if fbErr != nil {
		return "", false, fmt.Errorf("uber item %q not created (%v) and fallback product failed: %w", item.Title, createErr, fbErr)
	}
	return fallbackID, true, nil
}

// recordDegraded journalise un article ou une option que la commande a dû
// remplacer par du texte : la commande est créée, l'anomalie reste visible
// (diagnostic_events, kind marketplace_import_degraded).
func recordDegraded(ctx context.Context, merchantID, what, subjectID, title string, cause error) {
	diagnostics.Record(ctx, diagnostics.Event{
		Kind:        "marketplace_import_degraded",
		MerchantID:  merchantID,
		SubjectType: "uber_eats_" + what,
		SubjectID:   subjectID,
		Data:        map[string]any{"brand": models.BrandUberEats, "what": what, "title": title, "error": cause.Error()},
	})
}
