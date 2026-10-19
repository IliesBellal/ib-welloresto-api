package orders

import (
	"reflect"
	"strconv"
	"testing"

	"welloresto-api/internal/models"
)

// Tests « golden » du moteur de promotions, construits sur les promotions
// réellement configurées en production (export du 2026-10-17, voir
// docs/promotions-moteur-chantier.md § 10 et docs/diagnostic-promotions-prod.sql).
//
// Phase 0 du chantier : chaque cas fige le résultat du moteur ACTUEL
// (legacy / legacyApplied). Quand un cas change avec la correction, target /
// targetApplied portent le résultat attendu d'après la règle cible
// (docs/promotions-moteur-chantier.md § 3), calculé à la main et non exécuté
// tant que le moteur n'est pas corrigé. Le jour où le moteur est corrigé,
// goldenTargetEngine passe à true : les cas sans target doivent rester
// identiques, ceux avec target doivent donner exactement le target. Toute autre
// différence est un bug.
//
// Les prix sont ceux du catalogue de l'export (centimes, prix du canal de la
// commande). Les prix de supplément d'option sont fictifs (150) : l'export ne
// les contient pas. Les créneaux horaires et les dates de validité se testent
// en SQL (GetDiscounts), pas ici : chaque cas reçoit la liste de promotions que
// la requête renverrait au jour indiqué.
//
// État : true depuis la correction du moteur (2026-10-18). Les champs legacy /
// legacyApplied restent comme trace du comportement d'avant.
const goldenTargetEngine = true

type goldenUnit struct {
	productID string
	price     int
	optExtra  int      // prix catalogue du supplément des options choisies
	options   []string // options sélectionnées
}

func gu(productID string, price, optExtra int, options ...string) goldenUnit {
	return goldenUnit{productID: productID, price: price, optExtra: optExtra, options: options}
}

type goldenEnv struct {
	discounts []*models.DBDiscount
	dp        map[string]map[string]*models.DiscountProductInfo
	do        map[string]map[string][]models.DiscountOptionInfo
}

type goldenCase struct {
	name          string
	orderType     string
	env           goldenEnv
	units         []goldenUnit
	legacy        []int // prix de chaque unité (remise + suppléments), moteur actuel
	legacyApplied []string
	target        []int // nil = identique au moteur actuel
	targetApplied []string
}

func goldenIDs(from, to int) []string {
	out := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, strconv.Itoa(i))
	}
	return out
}

func goldenTargets(newPrice *int, ids ...string) map[string]*models.DiscountProductInfo {
	out := map[string]*models.DiscountProductInfo{}
	for _, id := range ids {
		info := &models.DiscountProductInfo{ProductID: id}
		if newPrice != nil {
			p := *newPrice
			info.NewPrice = &p
		}
		out[id] = info
	}
	return out
}

// Commerçant 776f8f8e — promotion 3 : prix fixe 6,00 €, cumulable, minimum 2,
// remisée 2, sur place et à emporter, 30 produits dont la formule 587 à 0 €.
func goldenMerchantA() goldenEnv {
	ids := append(goldenIDs(531, 553), "587", "589", "590", "726", "727", "728", "1705")
	return goldenEnv{
		discounts: []*models.DBDiscount{{
			DiscountID: "3", DiscountOrderType: "IN TAKE_AWAY", DiscountValue: 6, DiscountUnit: "NEWPRICE",
			MinOrderValue: 2, MinOrderUnit: "QTY", DiscountedQuantity: 2, IsCumulative: true, Available: true,
		}},
		dp: map[string]map[string]*models.DiscountProductInfo{"3": goldenTargets(intPtr(600), ids...)},
		do: map[string]map[string][]models.DiscountOptionInfo{},
	}
}

// Commerçant 24a40264 — promotion 4 : prix fixe 6,00 €, cumulable, minimum 2,
// remisée 2, tous modes, produits 67, 68 et 69.
func goldenMerchantB() goldenEnv {
	return goldenEnv{
		discounts: []*models.DBDiscount{{
			DiscountID: "4", DiscountOrderType: "IN TAKE_AWAY DELIVERY", DiscountValue: 6, DiscountUnit: "NEWPRICE",
			MinOrderValue: 2, MinOrderUnit: "QTY", DiscountedQuantity: 2, IsCumulative: true, Available: true,
		}},
		dp: map[string]map[string]*models.DiscountProductInfo{"4": goldenTargets(intPtr(600), "67", "68", "69")},
		do: map[string]map[string][]models.DiscountOptionInfo{},
	}
}

// Commerçant d6a6e3ef — promotions 5 à 10 : 34 produits (1399 à 1431 et la
// formule 1541 à 0 €), toutes non cumulables, remisée 1, avec contraintes
// d'options. active donne l'ordre dans lequel GetDiscounts les renverrait le
// jour concerné : lundi à emporter [8 10 9], mercredi ou vendredi [5 6 7 9].
func goldenMerchantC(active ...string) goldenEnv {
	all := append(goldenIDs(1399, 1431), "1541")
	optionsFor := func(optionID string, mandatory bool, newPrice *int, ids []string) map[string][]models.DiscountOptionInfo {
		out := map[string][]models.DiscountOptionInfo{}
		for _, id := range ids {
			out[id] = []models.DiscountOptionInfo{{OptionID: optionID, IsOptionMandatory: mandatory, NewPrice: newPrice}}
		}
		return out
	}
	// Promotion 8 : trois options facultatives à 0 € sur 1399 à 1429 seulement.
	opt8 := map[string][]models.DiscountOptionInfo{}
	for _, id := range goldenIDs(1399, 1429) {
		for _, o := range []string{"2174", "2176", "2178"} {
			opt8[id] = append(opt8[id], models.DiscountOptionInfo{OptionID: o, IsOptionMandatory: false, NewPrice: intPtr(0)})
		}
	}

	byID := map[string]*models.DBDiscount{
		"5":  {DiscountID: "5", DiscountOrderType: "IN TAKE_AWAY DELIVERY", DiscountValue: 200, DiscountUnit: "NEWPRICE", MinOrderValue: 1, MinOrderUnit: "QTY", DiscountedQuantity: 1, Available: true, PreferredOrder: 1},
		"6":  {DiscountID: "6", DiscountOrderType: "IN TAKE_AWAY DELIVERY", DiscountValue: 500, DiscountUnit: "NEWPRICE", MinOrderValue: 1, MinOrderUnit: "QTY", DiscountedQuantity: 1, Available: true, PreferredOrder: 2},
		"7":  {DiscountID: "7", DiscountOrderType: "IN TAKE_AWAY DELIVERY", DiscountValue: 300, DiscountUnit: "NEWPRICE", MinOrderValue: 1, MinOrderUnit: "QTY", DiscountedQuantity: 1, Available: true, PreferredOrder: 3},
		"8":  {DiscountID: "8", DiscountOrderType: "TAKE_AWAY", DiscountValue: 0, DiscountUnit: "NEWPRICE", MinOrderValue: 4, MinOrderUnit: "QTY", DiscountedQuantity: 1, Available: true, PreferredOrder: 4},
		"10": {DiscountID: "10", DiscountOrderType: "TAKE_AWAY", DiscountValue: 300, DiscountUnit: "NEWPRICE", MinOrderValue: 3, MinOrderUnit: "QTY", DiscountedQuantity: 1, Available: true, PreferredOrder: 5},
		"9":  {DiscountID: "9", DiscountOrderType: "TAKE_AWAY", DiscountValue: 50, DiscountUnit: "PERCENTAGE", MinOrderValue: 2, MinOrderUnit: "QTY", DiscountedQuantity: 1, Available: true, PreferredOrder: 6},
	}
	dp := map[string]map[string]*models.DiscountProductInfo{
		"5":  goldenTargets(intPtr(600), all...),
		"6":  goldenTargets(intPtr(500), all...),
		"7":  goldenTargets(intPtr(500), all...),
		"8":  goldenTargets(intPtr(0), all...),
		"10": goldenTargets(intPtr(300), all...),
		"9":  goldenTargets(intPtr(50), all...), // prix ignoré en pourcentage
	}
	do := map[string]map[string][]models.DiscountOptionInfo{
		"5":  optionsFor("2174", true, nil, all),
		"6":  optionsFor("2176", true, nil, all),
		"7":  optionsFor("2178", true, nil, all),
		"8":  opt8,
		"9":  optionsFor("2176", true, intPtr(200), all),
		"10": optionsFor("2176", true, intPtr(0), all),
	}

	env := goldenEnv{dp: dp, do: do}
	for _, id := range active {
		env.discounts = append(env.discounts, byID[id])
	}
	return env
}

func goldenCases() []goldenCase {
	a, b := goldenMerchantA, goldenMerchantB
	monday := func() goldenEnv { return goldenMerchantC("8", "10", "9") }
	wednesday := func() goldenEnv { return goldenMerchantC("5", "6", "7", "9") }
	rep := func(id string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = id
		}
		return out
	}
	none := []string{}

	return []goldenCase{
		// --- Promotion 3 (776f8f8e), la plus utilisée : 511 utilisations en 90 jours ---
		{name: "P3 un seul article : minimum 2 non atteint", orderType: "IN", env: a(),
			units: []goldenUnit{gu("531", 790, 0)}, legacy: []int{790}, legacyApplied: none},
		{name: "P3 deux articles : les deux à 6 €", orderType: "IN", env: a(),
			units: []goldenUnit{gu("532", 870, 0), gu("532", 870, 0)}, legacy: []int{600, 600}, legacyApplied: rep("3", 2)},
		{name: "P3 trois articles du moins cher au plus cher : les deux premiers", orderType: "IN", env: a(),
			units:  []goldenUnit{gu("531", 790, 0), gu("532", 870, 0), gu("537", 990, 0)},
			legacy: []int{600, 600, 990}, legacyApplied: rep("3", 2)},
		{name: "P3 trois articles du plus cher au moins cher : l'article laissé change selon l'ordre", orderType: "IN", env: a(),
			units:  []goldenUnit{gu("537", 990, 0), gu("532", 870, 0), gu("531", 790, 0)},
			legacy: []int{600, 600, 790}, legacyApplied: rep("3", 2),
			target: []int{990, 600, 600}, targetApplied: rep("3", 2)},
		{name: "P3 quatre articles : les quatre à 6 €", orderType: "IN", env: a(),
			units:  []goldenUnit{gu("532", 870, 0), gu("532", 870, 0), gu("532", 870, 0), gu("532", 870, 0)},
			legacy: []int{600, 600, 600, 600}, legacyApplied: rep("3", 4)},
		{name: "P3 cinq articles identiques : quatre à 6 €", orderType: "IN", env: a(),
			units:  []goldenUnit{gu("532", 870, 0), gu("532", 870, 0), gu("532", 870, 0), gu("532", 870, 0), gu("532", 870, 0)},
			legacy: []int{600, 600, 600, 600, 870}, legacyApplied: rep("3", 4)},
		{name: "P3 à emporter, deux articles", orderType: "TAKE_AWAY", env: a(),
			units: []goldenUnit{gu("532", 870, 0), gu("532", 870, 0)}, legacy: []int{600, 600}, legacyApplied: rep("3", 2)},
		{name: "P3 livraison : mode non couvert", orderType: "DELIVERY", env: a(),
			units: []goldenUnit{gu("532", 970, 0), gu("532", 970, 0)}, legacy: []int{970, 970}, legacyApplied: none},
		{name: "P3 produit non ciblé : ne compte pas pour le minimum", orderType: "IN", env: a(),
			units: []goldenUnit{gu("9999", 500, 0), gu("532", 870, 0)}, legacy: []int{500, 870}, legacyApplied: none},
		{name: "P3 formule à 0 € et pizza : la formule passe de 0 € à 6 € (hausse de prix)", orderType: "IN", env: a(),
			units:  []goldenUnit{gu("587", 0, 0), gu("532", 870, 0)},
			legacy: []int{600, 600}, legacyApplied: rep("3", 2),
			target: []int{0, 600}, targetApplied: rep("3", 1)},
		{name: "P3 formule à 0 € seule : minimum 2 non atteint", orderType: "IN", env: a(),
			units: []goldenUnit{gu("587", 0, 0)}, legacy: []int{0}, legacyApplied: none},

		// --- Promotion 4 (24a40264) ---
		{name: "P4 deux articles à emporter", orderType: "TAKE_AWAY", env: b(),
			units: []goldenUnit{gu("67", 1290, 0), gu("68", 1380, 0)}, legacy: []int{600, 600}, legacyApplied: rep("4", 2)},
		{name: "P4 trois articles à emporter : l'article laissé dépend de l'ordre", orderType: "TAKE_AWAY", env: b(),
			units:  []goldenUnit{gu("68", 1380, 0), gu("67", 1290, 0), gu("67", 1290, 0)},
			legacy: []int{600, 600, 1290}, legacyApplied: rep("4", 2),
			target: []int{1380, 600, 600}, targetApplied: rep("4", 2)},
		{name: "P4 livraison, deux articles", orderType: "DELIVERY", env: b(),
			units: []goldenUnit{gu("67", 1320, 0), gu("67", 1320, 0)}, legacy: []int{600, 600}, legacyApplied: rep("4", 2)},
		{name: "P4 sur place, deux articles", orderType: "IN", env: b(),
			units: []goldenUnit{gu("67", 1200, 0), gu("67", 1200, 0)}, legacy: []int{600, 600}, legacyApplied: rep("4", 2)},

		// --- Échelle du lundi à emporter (d6a6e3ef) : promotions 8, 10, 9 ---
		{name: "Lundi, 2 articles avec l'option 2176 : promotion 9, -50 % et option à 2 €", orderType: "TAKE_AWAY", env: monday(),
			units:  []goldenUnit{gu("1399", 800, 150, "2176"), gu("1400", 800, 150, "2176")},
			legacy: []int{600, 950}, legacyApplied: []string{"9"}},
		{name: "Lundi, 2 articles de prix différents : le -50 % va au premier du panier", orderType: "TAKE_AWAY", env: monday(),
			units:  []goldenUnit{gu("1430", 1000, 150, "2176"), gu("1399", 800, 150, "2176")},
			legacy: []int{700, 950}, legacyApplied: []string{"9"},
			target: []int{1150, 600}, targetApplied: []string{"9"}},
		{name: "Lundi, 3 articles : promotion 10 à 3 €, option offerte, bloque la 9", orderType: "TAKE_AWAY", env: monday(),
			units:  []goldenUnit{gu("1399", 800, 150, "2176"), gu("1400", 800, 150, "2176"), gu("1401", 800, 150, "2176")},
			legacy: []int{300, 950, 950}, legacyApplied: []string{"10"}},
		{name: "Lundi, 4 articles : promotion 8, le premier offert avec son option à 0 €", orderType: "TAKE_AWAY", env: monday(),
			units:  []goldenUnit{gu("1399", 800, 150, "2176"), gu("1400", 800, 150, "2176"), gu("1401", 800, 150, "2176"), gu("1402", 800, 150, "2176")},
			legacy: []int{0, 950, 950, 950}, legacyApplied: []string{"8"}},
		{name: "Lundi, 5 articles : un seul offert (non cumulable)", orderType: "TAKE_AWAY", env: monday(),
			units: []goldenUnit{gu("1399", 800, 150, "2176"), gu("1400", 800, 150, "2176"), gu("1401", 800, 150, "2176"),
				gu("1402", 800, 150, "2176"), gu("1403", 800, 150, "2176")},
			legacy: []int{0, 950, 950, 950, 950}, legacyApplied: []string{"8"}},
		{name: "Lundi, 4 articles sans option : promotion 8 (options facultatives)", orderType: "TAKE_AWAY", env: monday(),
			units:  []goldenUnit{gu("1399", 800, 0), gu("1400", 800, 0), gu("1401", 800, 0), gu("1402", 800, 0)},
			legacy: []int{0, 800, 800, 800}, legacyApplied: []string{"8"}},
		{name: "Lundi, 3 articles sans option : 9 et 10 exigent l'option 2176", orderType: "TAKE_AWAY", env: monday(),
			units:  []goldenUnit{gu("1399", 800, 0), gu("1400", 800, 0), gu("1401", 800, 0)},
			legacy: []int{800, 800, 800}, legacyApplied: none},
		{name: "Lundi, 4 articles dont un à 11 € en premier : il est offert, pas le moins cher", orderType: "TAKE_AWAY", env: monday(),
			units: []goldenUnit{gu("1431", 1100, 150, "2176"), gu("1399", 800, 150, "2176"), gu("1400", 800, 150, "2176"),
				gu("1401", 800, 150, "2176")},
			legacy: []int{150, 950, 950, 950}, legacyApplied: []string{"8"},
			target: []int{1250, 0, 950, 950}, targetApplied: []string{"8"}},
		{name: "Lundi, 4 articles dont la formule à 0 € en premier : l'offert est gaspillé sur la formule", orderType: "TAKE_AWAY", env: monday(),
			units: []goldenUnit{gu("1541", 0, 0, "2176"), gu("1399", 800, 150, "2176"), gu("1400", 800, 150, "2176"),
				gu("1401", 800, 150, "2176")},
			legacy: []int{0, 950, 950, 950}, legacyApplied: []string{"8"},
			target: []int{0, 0, 950, 950}, targetApplied: []string{"8"}},

		// --- Mercredi et vendredi (d6a6e3ef) : promotions 5, 6, 7 ---
		{name: "Mercredi, option 2174 : promotion 5 à 6 € ; l'autre article n'est pas remisé (non cumulable)", orderType: "IN", env: wednesday(),
			units:  []goldenUnit{gu("1399", 800, 150, "2174"), gu("1400", 800, 150, "2176")},
			legacy: []int{750, 950}, legacyApplied: []string{"5"}},
		{name: "Mercredi, deux articles avec l'option 2174 : le premier du panier est remisé", orderType: "IN", env: wednesday(),
			units:  []goldenUnit{gu("1430", 1000, 150, "2174"), gu("1399", 800, 150, "2174")},
			legacy: []int{750, 950}, legacyApplied: []string{"5"},
			target: []int{1150, 750}, targetApplied: []string{"5"}},
		{name: "Mercredi, formule à 0 € avec l'option 2174 : passe de 0 € à 6 €", orderType: "IN", env: wednesday(),
			units:  []goldenUnit{gu("1541", 0, 0, "2174")},
			legacy: []int{600}, legacyApplied: []string{"5"},
			target: []int{0}, targetApplied: none},
	}
}

// goldenUnitPrices applique le moteur actuel et renvoie, pour chaque unité, le
// prix après remise plus le supplément des options choisies (avec le prix
// d'option promotionnel quand la promotion en porte un).
func goldenRun(tc goldenCase) ([]int, []string) {
	products := make([]models.OrderProductPayload, 0, len(tc.units))
	baseTotal := 0
	for _, u := range tc.units {
		var cfg *models.ProductConfiguration
		if len(u.options) > 0 {
			cfg = withOptions(u.optExtra, u.options...)
		}
		products = append(products, models.OrderProductPayload{ProductID: u.productID, Price: u.price, Quantity: 1, Config: cfg})
		baseTotal += u.price
	}

	req := &models.PricingRequest{Order: &models.OrderRequest{OrderType: tc.orderType}}
	applied := (&OrdersService{}).applyDiscounts(req, products, tc.env.discounts, tc.env.dp, tc.env.do, baseTotal)

	totals := make([]int, 0, len(products))
	for _, p := range products {
		total := p.Price
		if p.DiscountedPrice != nil {
			total = *p.DiscountedPrice
		}
		if p.Config != nil {
			for _, attr := range p.Config.Attributes {
				for _, opt := range attr.Options {
					if opt.Selected || opt.Quantity > 0 {
						total += opt.ExtraPrice
					}
				}
			}
		}
		totals = append(totals, total)
	}
	return totals, applied
}

func TestProdPromotionsGolden(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			want, wantApplied := tc.legacy, tc.legacyApplied
			if goldenTargetEngine && tc.target != nil {
				want, wantApplied = tc.target, tc.targetApplied
			}

			got, applied := goldenRun(tc)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("prix par unité = %v, want %v", got, want)
			}
			if !reflect.DeepEqual(applied, wantApplied) {
				t.Fatalf("remises appliquées = %v, want %v", applied, wantApplied)
			}
		})
	}
}

// TestProdPromotionsGolden_TargetDefinedForEveryChange garantit qu'aucun cas
// ne change sans que sa valeur cible soit renseignée avec ses remises
// appliquées : le jour de la correction, la liste des écarts est exactement la
// liste des cas qui portent un target.
func TestProdPromotionsGolden_TargetDefinedForEveryChange(t *testing.T) {
	changes := 0
	for _, tc := range goldenCases() {
		if tc.target == nil {
			if tc.targetApplied != nil {
				t.Errorf("%q : targetApplied renseigné sans target", tc.name)
			}
			continue
		}
		changes++
		if tc.targetApplied == nil {
			t.Errorf("%q : target renseigné sans targetApplied", tc.name)
		}
		if len(tc.target) != len(tc.legacy) {
			t.Errorf("%q : target a %d unités, legacy %d", tc.name, len(tc.target), len(tc.legacy))
		}
		if reflect.DeepEqual(tc.target, tc.legacy) && reflect.DeepEqual(tc.targetApplied, tc.legacyApplied) {
			t.Errorf("%q : target identique à legacy, retirer le target", tc.name)
		}
	}
	t.Logf("%d cas sur %d changent avec la correction du moteur", changes, len(goldenCases()))
}
