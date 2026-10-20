package scannorder

import "welloresto-api/internal/models"

// StatusMinimumCartNotReached est renvoyé par CreateOrderSNO quand une commande
// DELIVERY n'atteint pas merchant_parameters.minimum_cart_for_delivery_order.
// Même valeur que le NotOrderableReason posé par le pricing
// (orders.applyDeliveryRules).
const StatusMinimumCartNotReached = "minimum_cart_for_delivery_not_reached"

// minimumCartNotReached dit si la commande doit être refusée faute d'atteindre
// le minimum de commande en livraison.
//
// Le pricing (applyDeliveryRules) calcule ce minimum sur le TTC produits après
// remises et récompenses, hors frais de livraison, et le signale par
// OrderRequest.IsOrderable = false — tout en répondant Status "success".
// CreateOrderSNO ne lisait pas ce drapeau : une commande sous le minimum
// passait jusqu'au paiement Stripe (commande 40494, 12 € pour 15 € minimum).
//
// On lit OrderRequest.IsOrderable et non PricingResponse.IsOrderable : ce
// dernier est combiné par GetPricingSNO avec la zone de livraison, déjà
// contrôlée en amont dans CreateOrderSNO ("address_too_far").
//
// À appeler après le contrôle des produits indisponibles : sur ce chemin le
// pricing sort avant applyDeliveryRules et IsOrderable reste à false.
func minimumCartNotReached(pricing *models.PricingResponse) bool {
	return pricing != nil && pricing.OrderRequest != nil && !pricing.OrderRequest.IsOrderable
}
