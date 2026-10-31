package deliveroo_orders

import (
	"go.uber.org/zap"

	"welloresto-api/internal/models"
	"welloresto-api/internal/webhook/marketplacepromo"
)

// applyDiscounts pose sur les lignes d'une commande Deliveroo la remise que la
// plateforme a appliquée, pour que la somme des lignes vaille le total payé par
// le client (order.total_price).
//
// Deliveroo envoie, pour chaque article, son prix de catalogue (unit_price) et
// la remise à part (discount_amount, order.offer_discount, promotions) : sans
// cette étape, une promotion laisse des lignes plus chères que le total, et les
// clôtures fiscales, qui additionnent les lignes, surévaluent le chiffre
// d'affaires et la TVA.
//
// Le montant de la remise est l'écart entre les lignes et le total payé, et
// seulement quand la commande porte une remise : un écart dû à autre chose
// (frais, options payantes) n'est jamais pris pour une promotion. La remise va
// aux articles que Deliveroo remise (au prorata de leur discount_amount), sinon à
// toutes les lignes au prorata de leur montant. Le prix de catalogue reste dans
// Price (orderitems.base_price), le prix payé va dans DiscountedPrice
// (orderitems.price).
//
// Hypothèse non vérifiée sur une vraie commande avec promotion : unit_price est
// le prix avant remise (la documentation Deliveroo décrit total_price « avant
// remises » et laisse unit_price muet). Si unit_price était déjà net, l'écart
// vaudrait 0 et rien ne change.
func applyDiscounts(log *zap.Logger, ord DeliverooOrder, products []models.OrderProductPayload) []models.OrderProductPayload {
	if len(products) != len(ord.Items) {
		return products
	}
	hasDiscount := ord.OfferDiscount.Fractional > 0 || len(ord.Promotions) > 0
	var weights []int64
	for _, item := range ord.Items {
		if item.DiscountAmount.Fractional > 0 {
			hasDiscount = true
		}
		weights = append(weights, int64(item.DiscountAmount.Fractional))
	}
	if !hasDiscount {
		return products
	}

	lines := make([]marketplacepromo.Line, len(products))
	linesTotal := 0
	for i, p := range products {
		lines[i] = marketplacepromo.Line{Quantity: p.Quantity, UnitPrice: p.Price}
		linesTotal += p.Quantity * p.Price
	}

	discount := linesTotal - ord.TotalPrice.Fractional
	if discount <= 0 {
		log.Info("[DELIVEROO] commande remisée mais lignes inférieures ou égales au total, lignes inchangées",
			zap.String("display_id", ord.DisplayID), zap.Int("lignes", linesTotal), zap.Int("total", ord.TotalPrice.Fractional))
		return products
	}

	result := marketplacepromo.Prorata(lines, weights, discount)
	if result.Method == marketplacepromo.MethodRejected {
		log.Warn("[DELIVEROO] remise supérieure aux lignes, lignes inchangées",
			zap.String("display_id", ord.DisplayID), zap.Int("remise", discount), zap.Int("lignes", linesTotal))
		return products
	}
	log.Info("[DELIVEROO] remise répartie sur les lignes",
		zap.String("display_id", ord.DisplayID), zap.Int("remise", discount), zap.Int("lignes_apres", len(result.Parts)))
	return result.Apply(products)
}
