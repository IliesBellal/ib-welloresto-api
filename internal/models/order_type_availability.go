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
