package models

import "strings"

// DefaultChannelOrderType est le mode appliqué par les canaux SNO/Kiosk quand
// le client n'envoie pas de type (anciennes versions d'app) ou un type
// inconnu.
const DefaultChannelOrderType = OrderTypeTakeAway

// NormalizeOrderType ramène orderType à l'un des types acceptés (comparaison
// insensible à la casse et aux espaces). Une valeur absente ou hors liste
// retombe sur DefaultChannelOrderType. Sans liste, les trois types sont
// acceptés.
func NormalizeOrderType(orderType string, accepted ...string) string {
	if len(accepted) == 0 {
		accepted = []string{OrderTypeIn, OrderTypeTakeAway, OrderTypeDelivery}
	}
	normalized := strings.ToUpper(strings.TrimSpace(orderType))
	for _, t := range accepted {
		if normalized == t {
			return t
		}
	}
	return DefaultChannelOrderType
}

// OrderTypeAvailabilityColumn retourne la colonne products portant la
// disponibilité du mode orderType (IN / TAKE_AWAY / DELIVERY, déjà
// normalisé). Valeurs fixes : sûre à interpoler dans une requête SQL.
func OrderTypeAvailabilityColumn(orderType string) string {
	switch orderType {
	case OrderTypeIn:
		return "available_in"
	case OrderTypeDelivery:
		return "available_delivery"
	default:
		return "available_take_away"
	}
}

// IsAvailableForOrderType indique si le produit peut être proposé pour le mode
// orderType (available_in / available_take_away / available_delivery, déjà
// normalisé). Un flag NULL en base (pointeur nil) vaut disponible : seul un
// false explicite masque le produit.
func (p *ProductEntry) IsAvailableForOrderType(orderType string) bool {
	var flag *bool
	switch orderType {
	case OrderTypeIn:
		flag = p.AvailableIn
	case OrderTypeDelivery:
		flag = p.AvailableDelivery
	default:
		flag = p.AvailableTakeAway
	}
	return flag == nil || *flag
}

// IsHiddenByStatus indique si le produit est « masqué » (products.status =
// not_available, ou valeurs héritées '0' / 'false' / 'unavailable') : il n'est
// plus proposé aux clients (ScanNOrder, borne) ni publié sur Deliveroo / Uber
// Eats, mais reste listé en caisse pour pouvoir être réactivé. out_of_stock
// n'est PAS masqué : il reste affiché « Épuisé ». Un statut vide ou inconnu
// n'est pas masqué (comportement historique de ScanNOrder et de la borne).
func (p *ProductEntry) IsHiddenByStatus() bool {
	switch strings.ToLower(strings.TrimSpace(p.Status)) {
	case "not_available", "0", "false", "unavailable":
		return true
	}
	return false
}
