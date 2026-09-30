package scannorder

import "welloresto-api/internal/models"

// StatusOrderTypeNotAvailable est renvoyé par GetPricingSNO / CreateOrderSNO
// quand le mode de commande demandé n'est pas proposé par le marchand.
const StatusOrderTypeNotAvailable = "order_type_not_available"

// isOrderTypeEnabled indique si le marchand propose le mode de commande, selon
// les mêmes flags scannorder_settings.*_enabled que les boutons affichés par
// le front ScanNOrder. Sans cette garde, un client pouvait forcer un mode non
// proposé (ex. ?type=IN dans l'URL) : prix "sur place", auto-acceptation et
// contournement du contrôle d'ouverture pour IN.
//
// Mode vide/inconnu → refusé (pas de repli silencieux sur le prix IN).
//
// IN reste accepté pour un QR rattaché à une table (location_id) : le front
// force IN dans ce cas, indépendamment de in_enabled. Les règles propres à IN
// seront revues dans un chantier dédié.
func isOrderTypeEnabled(m *models.MerchantRow, orderType string) bool {
	if m == nil {
		return false
	}
	switch orderType {
	case models.OrderTypeTakeAway:
		return m.TakeawayEnabled
	case models.OrderTypeDelivery:
		return m.DeliveryEnabled
	case models.OrderTypeIn:
		return m.InEnabled || (m.LocationID != nil && *m.LocationID != "")
	default:
		return false
	}
}
