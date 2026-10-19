package orders

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"go.uber.org/zap"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

// Moteur de promotions produit du pricing (commun à la caisse, à la borne et
// au Scan&Order). Règle de référence : docs/promotions-moteur-chantier.md § 3.
//
// Le panier arrive éclaté en une ligne par unité (buildSelectedProducts). Pour
// chaque promotion, dans l'ordre de prefered_order :
//  1. les unités éligibles sont celles que la promotion cible, dont les options
//     conviennent, qui ont un prix saisi (NEWPRICE) et qu'une promotion
//     précédente n'a pas déjà remisées ;
//  2. le minimum en quantité compte toutes les unités éligibles, remisables ou
//     non (une unité que la promotion n'abaisse pas compte comme « achetée »),
//     le minimum en euros se compare au panier entier ;
//  3. le nombre d'unités remisées n dépend de la cumulabilité : non cumulable,
//     la quantité remisée une seule fois ; cumulable, la quantité remisée par
//     groupe complet de max(minimum en quantité, quantité remisée) unités ;
//  4. les unités remisées sont, parmi celles dont le prix baisse réellement,
//     celles à la plus petite remise (en égalité, l'ordre du panier).

// discountCandidate est une unité que la promotion abaisse réellement.
type discountCandidate struct {
	index    int // position de l'unité dans le panier
	price    int // prix de l'unité après remise (sans les suppléments d'options)
	discount int // remise en centimes, toujours > 0
}

func (s *OrdersService) applyDiscounts(req *models.PricingRequest, products []models.OrderProductPayload, discounts []*models.DBDiscount, dp map[string]map[string]*models.DiscountProductInfo, do map[string]map[string][]models.DiscountOptionInfo, baseTotal int) []string {

	applied := []string{}
	discountAlreadyApplied := false

	for _, d := range discounts {
		// --- 1. Filtres d'éligibilité globaux ---
		if d.DiscountOrderType != "" && !strings.Contains(d.DiscountOrderType, req.Order.OrderType) {
			continue
		}
		// Une remise protégée par un code (discount_code non nul) ne s'applique
		// jamais sans saisie du code exact.
		if d.DiscountCode != nil {
			if req.DiscountCode == "" || req.DiscountCode != *d.DiscountCode {
				continue
			}
		}
		if discountAlreadyApplied && !d.IsCumulative {
			continue
		}
		// discounted_quantity <= 0 ne remise aucun article.
		if d.DiscountedQuantity <= 0 {
			continue
		}
		// Une unité de remise inconnue ne remise rien (l'ancien calcul rendait
		// alors les articles gratuits).
		switch d.DiscountUnit {
		case "PERCENTAGE", "CURRENCY", "NEWPRICE":
		default:
			continue
		}

		relatedProducts := dp[d.DiscountID]
		relatedOptions := do[d.DiscountID]

		// Un produit est éligible s'il est ciblé (ou si la promotion n'en cible
		// aucun), si ses options satisfont la promotion et, pour NEWPRICE, si un
		// prix remisé a été saisi pour lui.
		isEligible := func(sp models.OrderProductPayload) bool {
			info, targeted := relatedProducts[sp.ProductID]
			if len(relatedProducts) > 0 && !targeted {
				return false
			}
			if d.DiscountUnit == "NEWPRICE" && (info == nil || info.NewPrice == nil) {
				return false
			}
			return s.optionsMatch(sp, relatedOptions[sp.ProductID])
		}

		// --- 2. Unités éligibles (une unité déjà remisée par une promotion
		// précédente n'est plus disponible : ni remise, ni comptée) ---
		eligible := make([]int, 0, len(products))
		for i := range products {
			if products[i].DiscountedPrice == nil && isEligible(products[i]) {
				eligible = append(eligible, i)
			}
		}
		countEligible := len(eligible)
		if countEligible == 0 {
			continue
		}

		// --- 3. Vérification des conditions minimales ---
		testPassed := false
		switch normalizeMinOrderUnit(d.MinOrderUnit) {
		case minOrderUnitQuantity:
			if countEligible >= d.MinOrderValue {
				testPassed = true
			}
		case minOrderUnitCurrency:
			if baseTotal >= d.MinOrderValue {
				testPassed = true
			}
		default:
			testPassed = true
		}

		if !testPassed {
			continue
		}

		// --- 4. Nombre d'unités à remiser ---
		n := d.DiscountedQuantity
		if d.IsCumulative {
			// Par groupe complet : max(minimum en quantité, quantité remisée)
			// unités achetées, quantité remisée unités remisées (« 3 achetées,
			// la 4ᵉ offerte » = minimum 4, remisée 1).
			groupSize := d.DiscountedQuantity
			if normalizeMinOrderUnit(d.MinOrderUnit) == minOrderUnitQuantity && d.MinOrderValue > groupSize {
				groupSize = d.MinOrderValue
			}
			n = countEligible / groupSize * d.DiscountedQuantity
		}

		// --- 5. Unités remisables et choix des moins coûteuses ---
		candidates := make([]discountCandidate, 0, countEligible)
		for _, i := range eligible {
			sp := &products[i]
			price := discountedUnitPrice(d, sp.Price, relatedProducts[sp.ProductID])
			if gain := sp.Price - price; gain > 0 {
				candidates = append(candidates, discountCandidate{index: i, price: price, discount: gain})
			}
		}
		// Tri stable : à remise égale, l'ordre du panier (candidates est déjà
		// dans l'ordre du panier).
		sort.SliceStable(candidates, func(a, b int) bool { return candidates[a].discount < candidates[b].discount })

		count := n
		if count > len(candidates) {
			count = len(candidates)
		}

		// --- 6. Application de la remise ---
		for _, c := range candidates[:count] {
			sp := &products[c.index]

			// Nouvelles adresses mémoire : chaque produit a sa propre instance
			// de prix et d'identifiant.
			finalPrice := c.price
			finalID := d.DiscountID
			finalName := d.DiscountName

			sp.DiscountedPrice = &finalPrice
			sp.DiscountID = &finalID
			sp.DiscountName = finalName
			applyPromoOptionPrices(sp, relatedOptions[sp.ProductID])

			applied = append(applied, d.DiscountID)
		}
		if count > 0 {
			discountAlreadyApplied = true
		}

		// Si une remise non-cumulable a été appliquée, on stoppe le traitement des autres remises
		if count > 0 && !d.IsCumulative {
			break
		}
	}

	return applied
}

// discountedUnitPrice calcule le prix remisé d'une unité de prix de base
// basePrice : arithmétique entière tronquée pour le pourcentage (l'ancien
// calcul flottant perdait un centime sur ~0,7 % des couples prix/pourcentage),
// plafond MaxDiscountValue appliqué par unité, jamais négatif (Stripe rejette
// unit_amount < 0). Un prix qui ne baisse pas est renvoyé tel quel ou plus
// haut : l'appelant ne retient que les unités dont le prix baisse (gain > 0).
func discountedUnitPrice(d *models.DBDiscount, basePrice int, info *models.DiscountProductInfo) int {
	var price int
	switch d.DiscountUnit {
	case "PERCENTAGE":
		price = basePrice * (100 - d.DiscountValue) / 100
	case "CURRENCY":
		price = basePrice - d.DiscountValue
	case "NEWPRICE":
		if info == nil || info.NewPrice == nil {
			return basePrice
		}
		price = *info.NewPrice
	default:
		return basePrice
	}

	// Plafond du montant de la remise (jusqu'ici uniquement appliqué aux
	// rewards, jamais aux remises produit).
	if d.MaxDiscountValue != nil && *d.MaxDiscountValue > 0 {
		discountAmount := basePrice - price
		var maxDiscount int
		if d.MaxDiscountUnit != nil && *d.MaxDiscountUnit == "PERCENTAGE" {
			// Epsilon : même erreur binaire que pour l'ancien pourcentage
			// flottant (plafond sous-estimé d'un centime sans lui).
			maxDiscount = int(math.Floor(float64(basePrice)*(*d.MaxDiscountValue)/100.0 + 1e-9))
		} else {
			maxDiscount = int(*d.MaxDiscountValue)
		}
		if discountAmount > maxDiscount {
			price = basePrice - maxDiscount
		}
	}

	if price < 0 {
		price = 0
	}
	return price
}

// applyDiscountsSafe applique les promotions sans jamais empêcher le pricing
// (docs/promotions-moteur-chantier.md § 14, G2 et G3) : une panique ou un
// résultat incohérent annule toutes les remises (prix du catalogue) et
// journalise l'erreur. Mieux vaut rater une promotion que bloquer une commande.
func (s *OrdersService) applyDiscountsSafe(ctx context.Context, req *models.PricingRequest, products []models.OrderProductPayload, discounts []*models.DBDiscount, dp map[string]map[string]*models.DiscountProductInfo, do map[string]map[string][]models.DiscountOptionInfo, baseTotal int) (applied []string) {
	// Les lignes ne sont modifiées que par remplacement de champs (prix remisé,
	// identifiant, nom, configuration copiée) : une copie superficielle suffit.
	before := append([]models.OrderProductPayload(nil), products...)

	cancel := func(reason string, fields ...zap.Field) {
		copy(products, before)
		applied = []string{}
		ids := make([]string, 0, len(discounts))
		for _, d := range discounts {
			ids = append(ids, d.DiscountID)
		}
		fields = append(fields,
			zap.String("reason", reason),
			zap.String("merchant_id", req.MerchantID),
			zap.Strings("discount_ids", ids),
			zap.Int("unit_count", len(products)),
		)
		logger.FromContext(ctx).Error("promotions annulées, prix du catalogue appliqué", fields...)
	}

	defer func() {
		if r := recover(); r != nil {
			cancel("panic", zap.Any("panic", r), zap.Stack("stack"))
		}
	}()

	applied = s.applyDiscounts(req, products, discounts, dp, do, baseTotal)

	if err := checkDiscountResult(before, products); err != nil {
		cancel("incoherent_result", zap.Error(err))
	}
	return applied
}

// checkDiscountResult vérifie qu'un résultat de promotions est exploitable :
// même nombre d'unités, prix de base inchangé, prix remisé compris entre 0 et le
// prix de base (jamais une hausse), identifiant de remise cohérent avec le prix
// remisé, suppléments d'options promotionnels jamais négatifs.
func checkDiscountResult(before, after []models.OrderProductPayload) error {
	if len(before) != len(after) {
		return fmt.Errorf("nombre d'unités modifié (%d → %d)", len(before), len(after))
	}
	for i := range after {
		b, a := before[i], after[i]
		if a.Price != b.Price {
			return fmt.Errorf("unité %d : prix de base modifié (%d → %d)", i, b.Price, a.Price)
		}
		if (a.DiscountedPrice == nil) != (a.DiscountID == nil) {
			return fmt.Errorf("unité %d : prix remisé et identifiant de remise incohérents", i)
		}
		if a.DiscountedPrice != nil && (*a.DiscountedPrice < 0 || *a.DiscountedPrice > a.Price) {
			return fmt.Errorf("unité %d : prix remisé %d hors de [0, %d]", i, *a.DiscountedPrice, a.Price)
		}
		if a.Config != b.Config && a.Config != nil {
			for _, attr := range a.Config.Attributes {
				for _, opt := range attr.Options {
					if opt.ExtraPrice < 0 {
						return fmt.Errorf("unité %d : supplément d'option négatif (%d)", i, opt.ExtraPrice)
					}
				}
			}
		}
	}
	return nil
}
