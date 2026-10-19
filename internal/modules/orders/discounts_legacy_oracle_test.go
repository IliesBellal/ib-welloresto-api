package orders

import (
	"math"
	"strings"

	"welloresto-api/internal/models"
)

// Oracle du test différentiel (docs/promotions-moteur-chantier.md § 14.3) :
// copie VERBATIM de l'ancien applyDiscounts, tel qu'il était avant la
// correction du moteur (2026-10-18). Il n'est pas utilisé en production ; il
// sert à vérifier que le nouveau calcul donne exactement le même résultat
// chaque fois que le choix de l'unité remisée n'a pas d'importance. Ne pas
// corriger ce fichier : ses défauts (P1 à P3) sont ce qu'on compare.
func (s *OrdersService) legacyApplyDiscounts(req *models.PricingRequest, products []models.OrderProductPayload, discounts []*models.DBDiscount, dp map[string]map[string]*models.DiscountProductInfo, do map[string]map[string][]models.DiscountOptionInfo, baseTotal int) []string {

	applied := []string{}
	discountAlreadyApplied := false

	for _, d := range discounts {
		// --- 1. Filtres d'éligibilité globaux ---
		if d.DiscountOrderType != "" && !strings.Contains(d.DiscountOrderType, req.Order.OrderType) {
			continue
		}
		// Bug fix: une remise protégée par un code (discount_code non nul) ne doit jamais
		// s'appliquer sans saisie du code exact, sinon elle s'appliquait par défaut quand
		// req.DiscountCode était vide.
		if d.DiscountCode != nil {
			if req.DiscountCode == "" || req.DiscountCode != *d.DiscountCode {
				continue
			}
		}
		if discountAlreadyApplied && !d.IsCumulative {
			continue
		}
		// discounted_quantity <= 0 ne remise aucun article (et ferait
		// paniquer le modulo par palier des promotions cumulables).
		if d.DiscountedQuantity <= 0 {
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

		// --- 2. Comptage des produits éligibles (via Index pour éviter la copie) ---
		countEligible := 0
		for i := range products {
			if isEligible(products[i]) {
				countEligible++
			}
		}

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

		// --- 4. Détermination du nombre d'applications ---
		n := d.DiscountedQuantity
		if d.IsCumulative {
			// Si cumulable, on applique la remise par paliers (ex: tous les 2 produits)
			n = countEligible - (countEligible % d.DiscountedQuantity)
		}

		// --- 5. Application de la remise ---
		appliedInThisLoop := 0
		for j := 0; j < n; j++ {
			found := false
			// On parcourt le slice original par index pour modifier les valeurs réelles
			for i := range products {
				sp := &products[i] // Pointeur sur l'élément du slice

				// On vérifie si le produit n'a pas déjà une remise et s'il correspond aux critères
				if sp.DiscountedPrice == nil && isEligible(*sp) {
					var calculatedPrice int
					switch d.DiscountUnit {
					case "PERCENTAGE":
						// Arithmétique entière (troncature exacte) : l'ancien calcul
						// flottant int(prix * (1 - pct/100)) perdait un centime sur
						// ~0,7 % des couples prix/pourcentage (ex. -7 % sur 1000 ->
						// 929 au lieu de 930), par erreur de représentation binaire.
						calculatedPrice = sp.Price * (100 - d.DiscountValue) / 100

					case "CURRENCY":
						calculatedPrice = sp.Price - d.DiscountValue

					case "NEWPRICE":
						// isEligible garantit un prix saisi pour ce produit.
						calculatedPrice = *relatedProducts[sp.ProductID].NewPrice
					}

					// Bug fix: plafonner le montant de la remise par MaxDiscountValue/MaxDiscountUnit
					// (jusqu'ici uniquement appliqué aux rewards, jamais aux discounts produit).
					if d.MaxDiscountValue != nil && *d.MaxDiscountValue > 0 {
						discountAmount := sp.Price - calculatedPrice
						var maxDiscount int
						if d.MaxDiscountUnit != nil && *d.MaxDiscountUnit == "PERCENTAGE" {
							// Epsilon : même erreur binaire que ci-dessus (plafond
							// sous-estimé d'un centime sans lui).
							maxDiscount = int(math.Floor(float64(sp.Price)*(*d.MaxDiscountValue)/100.0 + 1e-9))
						} else {
							maxDiscount = int(*d.MaxDiscountValue)
						}
						if discountAmount > maxDiscount {
							calculatedPrice = sp.Price - maxDiscount
						}
					}

					// Bug fix: un prix unitaire remisé ne doit jamais être négatif (Stripe rejette unit_amount < 0).
					calculatedPrice = int(math.Max(0, float64(calculatedPrice)))

					// Sécurisation des pointeurs : on crée de nouvelles adresses mémoire
					// pour que chaque produit ait sa propre instance de prix et d'ID
					finalPrice := calculatedPrice
					finalID := d.DiscountID
					finalName := d.DiscountName

					sp.DiscountedPrice = &finalPrice
					sp.DiscountID = &finalID
					sp.DiscountName = finalName
					applyPromoOptionPrices(sp, relatedOptions[sp.ProductID])

					applied = append(applied, d.DiscountID)
					discountAlreadyApplied = true
					appliedInThisLoop++
					found = true
					break // On passe au "j" suivant (prochaine application de la remise)
				}
			}
			// Si on n'a plus de produits éligibles dans cette boucle, on arrête
			if !found {
				break
			}
		}

		// Si une remise non-cumulable a été appliquée, on stoppe le traitement des autres remises
		if discountAlreadyApplied && !d.IsCumulative {
			break
		}
	}

	return applied
}
