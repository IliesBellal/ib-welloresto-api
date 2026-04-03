package receipt

import "welloresto-api/internal/models"

func BuildItemsSnapshot(items []models.ProductEntry, orderType string) []models.SnapshotItem {
	var snap []models.SnapshotItem
	for _, item := range items {
		var activeRate float64

		switch orderType {
		case models.OrderTypeIn:
			activeRate = *item.TVAIn
		case models.OrderTypeDelivery:
			activeRate = *item.TVADelivery
		case models.OrderTypeTakeAway:
			activeRate = *item.TVATakeAway
		default:
			activeRate = *item.TVAIn
		}

		// 2. Calculer le montant de la TVA pour cet item
		// Formule : PrixTTC - (PrixTTC / (1 + Taux/100))
		priceTTC := float64(item.Price)
		priceHT := priceTTC / (1 + (activeRate / 100))
		taxAmount := int64(priceTTC - priceHT)

		qty := 0
		if item.Quantity != nil {
			qty = *item.Quantity
		}

		snap = append(snap, models.SnapshotItem{
			Name:     item.Name,  // Le nom du produit à l'instant T
			Quantity: qty,        // La quantité
			PriceTTC: item.Price, // Le prix payé
			TaxRate:  taxAmount,  // Ex: 1000 pour 10%, 550 pour 5.5%
		})
	}
	return snap
}

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
