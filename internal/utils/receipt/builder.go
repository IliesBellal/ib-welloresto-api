package receipt

import (
	"welloresto-api/internal/models"
)

// Les lignes figées d'un ticket ne sont plus construites ici depuis le ticket
// complet (lot D conformité caisse) : fiscal.BuildReceiptItems les
// reconstitue depuis la base, options, suppléments, livraison et remises
// compris. BuildItemsSnapshot ne gardait que le prix de chaque article.

func BuildPaymentsSnapshot(payments []models.Payment) []models.SnapshotPayment {
	var snap []models.SnapshotPayment
	for _, p := range payments {
		snap = append(snap, models.SnapshotPayment{
			Amount: p.Amount,
			MOP:    p.MOP, // Le moyen de paiement (CB, TR, CASH...)
		})
	}
	return snap
}
