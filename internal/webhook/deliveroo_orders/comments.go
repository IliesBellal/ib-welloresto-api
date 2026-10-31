package deliveroo_orders

import "strings"

// noCutlery : mention des couverts que Deliveroo envoie quand le client n'en veut
// pas ; rien à signaler à la préparation dans ce cas.
const noCutlery = "PAS DE COUVERTS"

// orderComment rassemble ce que le client a écrit pour la commande, tel que la
// caisse et la production l'affichent (order_comments sans ligne) : ses notes
// (order_notes), ses consignes de couverts reproduites telles que reçues
// (cutlery_notes) et ses consignes de livraison (delivery.delivery_notes).
//
// Deliveroo n'envoie aucune note par article : les consignes de préparation d'un
// article arrivent comme options (modifiers), affichées avec l'article.
func orderComment(ord DeliverooOrder) string {
	var parts []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}

	add(ord.OrderNotes)
	if cutlery := strings.TrimSpace(ord.CutleryNotes); cutlery != "" && !strings.EqualFold(cutlery, noCutlery) {
		add("Couverts : " + cutlery)
	}
	if ord.Delivery != nil {
		if notes := strings.TrimSpace(ord.Delivery.DeliveryNotes); notes != "" {
			add("Livraison : " + notes)
		}
	}

	return strings.Join(parts, "\n")
}
