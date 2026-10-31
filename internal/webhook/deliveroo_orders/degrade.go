package deliveroo_orders

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"welloresto-api/internal/diagnostics"
	"welloresto-api/internal/models"
)

// resolveProduct renvoie le produit d'un article Deliveroo (SyncProduct : lecture du
// mapping, sinon création sous verrou). Si la création échoue, la commande ne doit pas
// être perdue — le client a payé : elle prend l'article de repli de l'établissement
// (fallback = true) et l'anomalie est journalisée.
func (s *DeliverooService) resolveProduct(ctx context.Context, merchantID string, item DeliverooItem) (productID string, fallback bool, err error) {
	productID, syncErr := s.sync.SyncProduct(ctx, merchantID, item)
	if syncErr == nil {
		return productID, false, nil
	}

	recordDegraded(ctx, merchantID, "product", item.PosItemID, item.Name, syncErr)
	fallbackID, fbErr := s.fallbackProduct(ctx, merchantID)
	if fbErr != nil {
		return "", false, fmt.Errorf("deliveroo item %q not created (%v) and fallback product failed: %w", item.Name, syncErr, fbErr)
	}
	return fallbackID, true, nil
}

// modifiersLine : « Options : Sans oignons, Sauce ×2 ».
func modifiersLine(mods []DeliverooModifier) string {
	labels := make([]string, 0, len(mods))
	for _, m := range mods {
		label := strings.TrimSpace(m.Name)
		if m.Quantity > 1 {
			label += " ×" + strconv.Itoa(m.Quantity)
		}
		labels = append(labels, label)
	}
	return "Options : " + strings.Join(labels, ", ")
}

// recordDegraded journalise un article ou une option que la commande a dû remplacer
// par du texte : la commande est créée, l'anomalie reste visible (diagnostic_events,
// kind marketplace_import_degraded).
func recordDegraded(ctx context.Context, merchantID, what, subjectID, title string, cause error) {
	diagnostics.Record(ctx, diagnostics.Event{
		Kind:        "marketplace_import_degraded",
		MerchantID:  merchantID,
		SubjectType: "deliveroo_" + what,
		SubjectID:   subjectID,
		Data:        map[string]any{"brand": models.BrandDeliveroo, "what": what, "title": title, "error": cause.Error()},
	})
}
