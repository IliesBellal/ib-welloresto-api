package service

import (
	"go.uber.org/zap"

	ordersModels "welloresto-api/internal/models"
	"welloresto-api/internal/webhook/marketplacepromo"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// applyPromotions pose sur les lignes d'une commande Uber Eats la remise que
// l'établissement finance, pour que la somme des lignes vaille ce que le client a
// payé (payment.charges.sub_total_promo_applied).
//
// Uber envoie les prix de catalogue ligne par ligne et la promotion à part
// (payment.promotions) : sans cette étape, une offre « 1 acheté = 1 offert »
// laisse au ticket des lignes plus chères que le total, et les clôtures fiscales,
// qui additionnent les lignes, surévaluent le chiffre d'affaires et la TVA.
//
// Le prix de catalogue reste dans Price (orderitems.base_price) ; le prix payé va
// dans DiscountedPrice (orderitems.price). Une ligne dont toutes les unités ne
// sont pas remisées est scindée (Pizza ×1 à 14,50 € et ×1 à 0,00 €).
// products doit être aligné sur order.Cart.Items.
func applyPromotions(log *zap.Logger, order *ueModels.UberOrder, products []ordersModels.OrderProductPayload) []ordersModels.OrderProductPayload {
	charges := order.Payment.Charges
	if charges.SubTotalPromoApplied == nil || len(products) != len(order.Cart.Items) {
		return products
	}

	lines := make([]marketplacepromo.Line, len(order.Cart.Items))
	linesTotal := 0
	for i, item := range order.Cart.Items {
		lines[i] = marketplacepromo.Line{
			Quantity:  item.Quantity,
			UnitPrice: item.Price.UnitPrice.Amount,
			Cap:       baseUnitPrice(item),
		}
		linesTotal += item.Quantity * item.Price.UnitPrice.Amount
	}

	discount := linesTotal - charges.SubTotalPromoApplied.Amount
	if discount == 0 {
		return products
	}
	if discount < 0 {
		log.Warn("[UBER EATS] sous-total après promotion supérieur aux lignes, lignes inchangées",
			zap.String("display_id", order.DisplayID),
			zap.Int("lignes", linesTotal), zap.Int("sous_total_promo", charges.SubTotalPromoApplied.Amount))
		return products
	}

	result := marketplacepromo.Allocate(lines, promotionTargets(order), discount)
	if result.Method == marketplacepromo.MethodRejected {
		log.Warn("[UBER EATS] remise supérieure aux lignes, lignes inchangées",
			zap.String("display_id", order.DisplayID), zap.Int("remise", discount), zap.Int("lignes", linesTotal))
		return products
	}
	log.Info("[UBER EATS] remise répartie sur les lignes",
		zap.String("display_id", order.DisplayID), zap.Int("remise", discount),
		zap.String("methode", string(result.Method)), zap.Int("lignes_apres", len(result.Parts)))

	return result.Apply(products)
}

// baseUnitPrice : prix unitaire d'un article hors options payantes, plafond de
// ce qu'une promotion sur l'article peut retirer (le prix unitaire d'Uber
// comprend ses options). 0 si on ne peut pas le déterminer.
func baseUnitPrice(item ueModels.UberCartItem) int {
	base := item.Price.UnitPrice.Amount
	for _, g := range item.SelectedModifierGroups {
		for _, o := range g.SelectedItems {
			q := o.Quantity
			if q < 1 {
				q = 1
			}
			base -= o.Price.UnitPrice.Amount * q
		}
	}
	if base <= 0 {
		return 0
	}
	return base
}

// promotionTargets liste les unités que les promotions financées par
// l'établissement disent avoir remisées. discount_items désigne l'article par son
// id (cart.items[].id) ; si plusieurs lignes portent le même id, les unités sont
// attribuées dans l'ordre du panier.
func promotionTargets(order *ueModels.UberOrder) []marketplacepromo.Target {
	if order.Payment.Promotions == nil {
		return nil
	}
	used := make([]int, len(order.Cart.Items))
	var targets []marketplacepromo.Target
	for _, promo := range order.Payment.Promotions.Promotions {
		if !promo.MerchantFunded() {
			continue
		}
		for _, di := range promo.DiscountItems {
			left := di.DiscountedQuantity
			for i, item := range order.Cart.Items {
				if left <= 0 {
					break
				}
				if item.ID != di.ExternalID {
					continue
				}
				take := item.Quantity - used[i]
				if take > left {
					take = left
				}
				if take <= 0 {
					continue
				}
				used[i] += take
				left -= take
				targets = append(targets, marketplacepromo.Target{Line: i, Units: take})
			}
		}
	}
	return targets
}
