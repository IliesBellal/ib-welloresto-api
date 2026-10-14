package scannorder

// StatusOrderUnavailable est renvoyé par GetPricingSNO / CreateOrderSNO quand le
// client est bloqué par l'établissement (docs/CUSTOMER_BLOCKING.md). Le nom et
// l'absence de message sont voulus : rien dans la réponse ne doit révéler que
// le client est bloqué, ni pourquoi — le front affiche un refus générique.
const StatusOrderUnavailable = "order_unavailable"

// orderTypeChecksBlockedCustomer dit si le mode de commande passe par le
// contrôle des clients bloqués. Seules les commandes à emporter et en livraison
// identifient le client par son téléphone ; une commande IN (QR en salle) vient
// d'une table, pas d'un client saisi.
func orderTypeChecksBlockedCustomer(orderType string) bool {
	return orderType == "TAKE_AWAY" || orderType == "DELIVERY"
}
