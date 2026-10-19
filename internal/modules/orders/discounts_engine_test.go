package orders

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

// Tests du moteur de promotions corrigé (phase 1 du chantier,
// docs/promotions-moteur-chantier.md § 3, § 6 et § 14.3) :
//   - règles de la spécification, une par une, sur des exemples chiffrés ;
//   - test différentiel contre l'ancien calcul (discounts_legacy_oracle_test.go)
//     quand le choix de l'unité remisée n'a pas d'importance ;
//   - propriétés (aucune panique, jamais de hausse, indépendance à l'ordre) sur
//     des tirages aléatoires à graine fixe ;
//   - garde-fous : panique, résultat incohérent, chargement dégradé.

func enginePromo(id, unit string, value int) *models.DBDiscount {
	return &models.DBDiscount{DiscountID: id, DiscountUnit: unit, DiscountValue: value, DiscountedQuantity: 1, Available: true}
}

func engineRun(t *testing.T, orderType string, products []models.OrderProductPayload, discounts []*models.DBDiscount,
	dp map[string]map[string]*models.DiscountProductInfo, do map[string]map[string][]models.DiscountOptionInfo) []string {
	t.Helper()
	if dp == nil {
		dp = map[string]map[string]*models.DiscountProductInfo{}
	}
	if do == nil {
		do = map[string]map[string][]models.DiscountOptionInfo{}
	}
	base := 0
	for _, p := range products {
		base += p.Price
	}
	req := &models.PricingRequest{Order: &models.OrderRequest{OrderType: orderType}}
	return (&OrdersService{}).applyDiscounts(req, products, discounts, dp, do, base)
}

// cartOf fabrique un panier d'une unité par produit, dans l'ordre donné.
func cartOf(prices ...int) []models.OrderProductPayload {
	out := make([]models.OrderProductPayload, len(prices))
	for i, p := range prices {
		out[i] = models.OrderProductPayload{ProductID: fmt.Sprintf("P%d", i), Price: p, Quantity: 1}
	}
	return out
}

func discountedIndexes(products []models.OrderProductPayload) []int {
	out := []int{}
	for i, p := range products {
		if p.DiscountedPrice != nil {
			out = append(out, i)
		}
	}
	return out
}

func permutations(n int) [][]int {
	var out [][]int
	var rec func(cur []int, used []bool)
	rec = func(cur []int, used []bool) {
		if len(cur) == n {
			out = append(out, append([]int(nil), cur...))
			return
		}
		for i := 0; i < n; i++ {
			if !used[i] {
				used[i] = true
				rec(append(cur, i), used)
				used[i] = false
			}
		}
	}
	rec(nil, make([]bool, n))
	return out
}

func TestEngine_CheapestUnitIsDiscounted_AnyCartOrder(t *testing.T) {
	// « 1 offerte dès 3 » : l'article offert est le moins cher, quel que soit l'ordre.
	prices := []int{900, 1100, 1400}
	for _, perm := range permutations(len(prices)) {
		ordered := make([]int, len(perm))
		for i, p := range perm {
			ordered[i] = prices[p]
		}
		products := cartOf(ordered...)
		d := enginePromo("d1", "PERCENTAGE", 100)
		d.MinOrderUnit, d.MinOrderValue = "QTY", 3

		engineRun(t, "IN", products, []*models.DBDiscount{d}, nil, nil)

		got := discountedIndexes(products)
		if len(got) != 1 || products[got[0]].Price != 900 || *products[got[0]].DiscountedPrice != 0 {
			t.Fatalf("panier %v : unité offerte = %v, want celle à 9,00 €", ordered, got)
		}
	}
}

func TestEngine_TieKeepsCartOrder(t *testing.T) {
	products := cartOf(1000, 1000, 1000)
	d := enginePromo("d1", "PERCENTAGE", 100)
	d.MinOrderUnit, d.MinOrderValue = "QTY", 3
	engineRun(t, "IN", products, []*models.DBDiscount{d}, nil, nil)
	if got := discountedIndexes(products); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("égalité : unité remisée = %v, want [0] (ordre du panier)", got)
	}
}

func TestEngine_NthFree_Groups(t *testing.T) {
	// « 3 achetées, la 4ᵉ offerte » = minimum 4, remisée 1, 100 %.
	cases := []struct {
		units          int
		cumulable      bool
		wantDiscounted int
	}{
		{3, true, 0}, {4, true, 1}, {5, true, 1}, {7, true, 1}, {8, true, 2}, {9, true, 2}, {12, true, 3},
		{3, false, 0}, {4, false, 1}, {8, false, 1}, {12, false, 1},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d unités, cumulable=%v", tc.units, tc.cumulable), func(t *testing.T) {
			prices := make([]int, tc.units)
			for i := range prices {
				prices[i] = 1500 - 100*i // du plus cher au moins cher
			}
			products := cartOf(prices...)
			d := enginePromo("d1", "PERCENTAGE", 100)
			d.MinOrderUnit, d.MinOrderValue, d.IsCumulative = "QTY", 4, tc.cumulable
			engineRun(t, "IN", products, []*models.DBDiscount{d}, nil, nil)

			got := discountedIndexes(products)
			if len(got) != tc.wantDiscounted {
				t.Fatalf("%d unités remisées, want %d (indices %v)", len(got), tc.wantDiscounted, got)
			}
			// Ce sont les moins chères : les dernières du panier.
			for k, idx := range got {
				if want := tc.units - tc.wantDiscounted + k; idx != want {
					t.Fatalf("indices remisés %v, want les %d dernières", got, tc.wantDiscounted)
				}
			}
		})
	}
}

func TestEngine_PairDiscount_Groups(t *testing.T) {
	// « 2 pizzas à −50 % » : minimum 2, remisée 2.
	prices := []int{1400, 1000, 1300, 1100, 1200}
	cum := enginePromo("d1", "PERCENTAGE", 50)
	cum.MinOrderUnit, cum.MinOrderValue, cum.DiscountedQuantity, cum.IsCumulative = "QTY", 2, 2, true
	products := cartOf(prices...)
	engineRun(t, "IN", products, []*models.DBDiscount{cum}, nil, nil)
	// Les 4 moins chères (1000, 1100, 1200, 1300), pas la plus chère (1400).
	if got := discountedIndexes(products); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("cumulable : indices remisés %v, want [1 2 3 4]", got)
	}

	non := enginePromo("d1", "PERCENTAGE", 50)
	non.MinOrderUnit, non.MinOrderValue, non.DiscountedQuantity = "QTY", 2, 2
	products = cartOf(prices...)
	engineRun(t, "IN", products, []*models.DBDiscount{non}, nil, nil)
	if got := discountedIndexes(products); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("non cumulable : indices remisés %v, want [1 3] (1000 et 1100)", got)
	}
}

func TestEngine_FixedPrice_NeverRaisesAndCountsAsBought(t *testing.T) {
	newPrice := func(price int) (*models.DBDiscount, map[string]map[string]*models.DiscountProductInfo) {
		d := enginePromo("d1", "NEWPRICE", 0)
		d.MinOrderUnit, d.MinOrderValue = "QTY", 4
		return d, map[string]map[string]*models.DiscountProductInfo{"d1": {}}
	}
	run := func(prices ...int) []int {
		products := cartOf(prices...)
		d, dp := newPrice(500)
		for _, p := range products {
			dp["d1"][p.ProductID] = &models.DiscountProductInfo{ProductID: p.ProductID, NewPrice: intPtr(500)}
		}
		engineRun(t, "IN", products, []*models.DBDiscount{d}, dp, nil)
		return unitPrices(products)
	}

	// « 3 achetés, le 4ᵉ à 5 € » : 4, 5, 6, 7 € comptent, seul 6 € passe à 5 €.
	if got := run(400, 500, 600, 700); !reflect.DeepEqual(got, []int{-1, -1, 500, -1}) {
		t.Fatalf("4,5,6,7 € : %v, want [-1 -1 500 -1]", got)
	}
	// Aucune unité à baisser : rien n'est remisé.
	if got := run(400, 500, 500, 500); !reflect.DeepEqual(got, []int{-1, -1, -1, -1}) {
		t.Fatalf("4,5,5,5 € : %v, want aucune remise", got)
	}
	// Le prix saisi supérieur au prix : jamais appliqué.
	products := cartOf(800)
	d := enginePromo("d1", "NEWPRICE", 0)
	dp := map[string]map[string]*models.DiscountProductInfo{"d1": {"P0": {ProductID: "P0", NewPrice: intPtr(900)}}}
	engineRun(t, "IN", products, []*models.DBDiscount{d}, dp, nil)
	if products[0].DiscountedPrice != nil {
		t.Fatalf("9 € sur un article à 8 € : prix remisé %d, want aucun", *products[0].DiscountedPrice)
	}
}

func TestEngine_ZeroPriceUnitCountsButIsNeverDiscounted(t *testing.T) {
	// Formule à 0 € + pizza à 8 €, prix fixe 3 €, minimum 2 : la formule compte.
	products := cartOf(0, 800)
	d := enginePromo("d1", "NEWPRICE", 0)
	d.MinOrderUnit, d.MinOrderValue = "QTY", 2
	dp := map[string]map[string]*models.DiscountProductInfo{"d1": {
		"P0": {ProductID: "P0", NewPrice: intPtr(300)},
		"P1": {ProductID: "P1", NewPrice: intPtr(300)},
	}}
	engineRun(t, "IN", products, []*models.DBDiscount{d}, dp, nil)
	if got := unitPrices(products); !reflect.DeepEqual(got, []int{-1, 300}) {
		t.Fatalf("prix remisés = %v, want [-1 300]", got)
	}
}

func TestEngine_OfferedUnitStillPaysItsSupplements(t *testing.T) {
	// Sans prix d'option promotionnel, l'unité offerte garde ses suppléments.
	products := []models.OrderProductPayload{{ProductID: "A", Price: 1000, Quantity: 1, Config: withOptions(200, "X")}}
	engineRun(t, "IN", products, []*models.DBDiscount{enginePromo("d1", "PERCENTAGE", 100)}, nil, nil)
	if products[0].DiscountedPrice == nil || *products[0].DiscountedPrice != 0 {
		t.Fatalf("prix remisé = %v, want 0", products[0].DiscountedPrice)
	}
	if got := products[0].Config.Attributes[0].Options[0].ExtraPrice; got != 200 {
		t.Fatalf("supplément = %d, want 200 (toujours facturé)", got)
	}
}

func TestEngine_MinimumInEurosCompareTheWholeCart(t *testing.T) {
	products := []models.OrderProductPayload{
		{ProductID: "A", Price: 1000, Quantity: 1},
		{ProductID: "B", Price: 500, Quantity: 1},
	}
	d := enginePromo("d1", "PERCENTAGE", 20)
	d.MinOrderUnit, d.MinOrderValue = "EUR", 1500
	engineRun(t, "IN", products, []*models.DBDiscount{d}, nil, map[string]map[string][]models.DiscountOptionInfo{})
	// Ciblée sur B seulement, le minimum se compare quand même au panier entier.
	products2 := []models.OrderProductPayload{
		{ProductID: "A", Price: 1000, Quantity: 1},
		{ProductID: "B", Price: 500, Quantity: 1},
	}
	engineRun(t, "IN", products2, []*models.DBDiscount{d}, map[string]map[string]*models.DiscountProductInfo{"d1": targeted("B")}, nil)
	if got := unitPrices(products2); !reflect.DeepEqual(got, []int{-1, 400}) {
		t.Fatalf("prix remisés = %v, want [-1 400]", got)
	}
	d.MinOrderValue = 1501
	products3 := []models.OrderProductPayload{
		{ProductID: "A", Price: 1000, Quantity: 1},
		{ProductID: "B", Price: 500, Quantity: 1},
	}
	engineRun(t, "IN", products3, []*models.DBDiscount{d}, map[string]map[string]*models.DiscountProductInfo{"d1": targeted("B")}, nil)
	if got := unitPrices(products3); !reflect.DeepEqual(got, []int{-1, -1}) {
		t.Fatalf("minimum 15,01 € non atteint : %v, want aucune remise", got)
	}
}

func TestEngine_UnitAlreadyDiscountedDoesNotCountForNextPromotion(t *testing.T) {
	// d1 remise les deux A ; d2 (minimum 2, tout le panier) ne voit plus que B.
	products := []models.OrderProductPayload{
		{ProductID: "A", Price: 1000, Quantity: 1},
		{ProductID: "A", Price: 1000, Quantity: 1},
		{ProductID: "B", Price: 1000, Quantity: 1},
	}
	d1 := enginePromo("d1", "PERCENTAGE", 50)
	d1.IsCumulative = true
	d2 := enginePromo("d2", "PERCENTAGE", 100)
	d2.IsCumulative, d2.MinOrderUnit, d2.MinOrderValue = true, "QTY", 2
	dp := map[string]map[string]*models.DiscountProductInfo{"d1": targeted("A")}

	applied := engineRun(t, "IN", products, []*models.DBDiscount{d1, d2}, dp, nil)

	if got := unitPrices(products); !reflect.DeepEqual(got, []int{500, 500, -1}) {
		t.Fatalf("prix remisés = %v, want [500 500 -1]", got)
	}
	if !reflect.DeepEqual(applied, []string{"d1", "d1"}) {
		t.Fatalf("remises appliquées = %v, want [d1 d1]", applied)
	}
}

func TestEngine_UnknownDiscountUnitDiscountsNothing(t *testing.T) {
	products := cartOf(1000)
	applied := engineRun(t, "IN", products, []*models.DBDiscount{enginePromo("d1", "BOGUS", 10)}, nil, nil)
	if products[0].DiscountedPrice != nil || len(applied) != 0 {
		t.Fatalf("unité inconnue : prix remisé %v, appliquées %v, want aucune remise", products[0].DiscountedPrice, applied)
	}
}

func TestEngine_DisjointPromotions_TotalIndependentOfCartOrder(t *testing.T) {
	prices := []int{790, 870, 990, 1000, 1100}
	build := func(order []int) ([]models.OrderProductPayload, []*models.DBDiscount, map[string]map[string]*models.DiscountProductInfo) {
		products := make([]models.OrderProductPayload, len(order))
		for i, o := range order {
			products[i] = models.OrderProductPayload{ProductID: fmt.Sprintf("P%d", o), Price: prices[o], Quantity: 1}
		}
		d1 := enginePromo("d1", "NEWPRICE", 0)
		d1.IsCumulative, d1.MinOrderUnit, d1.MinOrderValue, d1.DiscountedQuantity = true, "QTY", 2, 2
		d2 := enginePromo("d2", "PERCENTAGE", 50)
		d2.IsCumulative = true
		dp := map[string]map[string]*models.DiscountProductInfo{
			"d1": {"P0": {ProductID: "P0", NewPrice: intPtr(600)}, "P1": {ProductID: "P1", NewPrice: intPtr(600)}, "P2": {ProductID: "P2", NewPrice: intPtr(600)}},
			"d2": targeted("P3", "P4"),
		}
		return products, []*models.DBDiscount{d1, d2}, dp
	}
	total := func(ps []models.OrderProductPayload) int {
		sum := 0
		for _, p := range ps {
			if p.DiscountedPrice != nil {
				sum += *p.DiscountedPrice
			} else {
				sum += p.Price
			}
		}
		return sum
	}

	var want int
	for i, perm := range permutations(len(prices)) {
		products, discounts, dp := build(perm)
		engineRun(t, "IN", products, discounts, dp, nil)
		got := total(products)
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("ordre %v : total %d, want %d (indépendance à l'ordre du panier)", perm, got, want)
		}
	}
}

// --- Test différentiel contre l'ancien calcul ---------------------------------

type fuzzScenario struct {
	orderType string
	code      string
	units     []models.OrderProductPayload
	discounts []*models.DBDiscount
	dp        map[string]map[string]*models.DiscountProductInfo
	do        map[string]map[string][]models.DiscountOptionInfo
}

func (f fuzzScenario) describe() string {
	s := fmt.Sprintf("order=%s code=%q units=[", f.orderType, f.code)
	for _, u := range f.units {
		s += fmt.Sprintf("{%s %d cfg=%v} ", u.ProductID, u.Price, u.Config != nil)
	}
	s += "] discounts=["
	for _, d := range f.discounts {
		s += fmt.Sprintf("{id=%s unit=%s val=%d mode=%q min=%d/%s dq=%d cum=%v max=%v/%v} ", d.DiscountID, d.DiscountUnit, d.DiscountValue,
			d.DiscountOrderType, d.MinOrderValue, d.MinOrderUnit, d.DiscountedQuantity, d.IsCumulative, d.MaxDiscountValue, d.MaxDiscountUnit)
	}
	return s + "]"
}

func pick[T any](r *rand.Rand, xs ...T) T { return xs[r.Intn(len(xs))] }

// genScenario tire un panier et des promotions.
//   - uniform : tous les produits ont le même prix et chaque promotion le même
//     prix fixe pour tous ses produits, donc toutes les unités éligibles d'une
//     promotion ont la même remise (le choix de l'unité n'a pas d'importance) ;
//     le minimum en quantité d'une promotion cumulable reste ≤ sa quantité
//     remisée (le modèle de groupes n'a alors rien changé) ;
//   - disjoint : les promotions ciblent des produits distincts (aucune unité
//     n'est éligible à deux promotions) ;
//   - weird : valeurs extrêmes ou incohérentes (négatifs, unité inconnue, grands
//     minimums), promotions qui se chevauchent.
func genScenario(r *rand.Rand, uniform, disjoint, weird bool) fuzzScenario {
	k := 1 + r.Intn(5)
	ids := make([]string, k)
	prices := map[string]int{}
	uniformPrice := 100 + r.Intn(1900)
	for i := range ids {
		ids[i] = fmt.Sprintf("p%d", i)
		switch {
		case uniform:
			prices[ids[i]] = uniformPrice
		case weird:
			prices[ids[i]] = r.Intn(3001)
		default:
			prices[ids[i]] = 100 + r.Intn(2900)
		}
	}

	f := fuzzScenario{
		orderType: pick(r, "IN", "TAKE_AWAY", "DELIVERY"),
		dp:        map[string]map[string]*models.DiscountProductInfo{},
		do:        map[string]map[string][]models.DiscountOptionInfo{},
	}
	for n := 1 + r.Intn(12); n > 0; n-- {
		id := ids[r.Intn(k)]
		u := models.OrderProductPayload{ProductID: id, Price: prices[id], Quantity: 1}
		if r.Intn(10) < 4 {
			u.Config = withOptions(100, "X")
		}
		f.units = append(f.units, u)
	}

	m := 1 + r.Intn(3)
	if disjoint && m > k {
		m = k
	}
	perm := r.Perm(k)

	for j := 0; j < m; j++ {
		d := &models.DBDiscount{DiscountID: fmt.Sprintf("d%d", j), Available: true, PreferredOrder: j}
		d.DiscountOrderType = pick(r, "", "IN", "TAKE_AWAY", "IN TAKE_AWAY", "DELIVERY", "IN TAKE_AWAY DELIVERY")
		d.IsCumulative = r.Intn(2) == 0
		d.DiscountedQuantity = 1 + r.Intn(4)

		// Produits ciblés.
		var targets []string
		switch {
		case disjoint:
			for i, p := range perm {
				if i%m == j {
					targets = append(targets, ids[p])
				}
			}
		case m == 1 && r.Intn(3) == 0:
			// toute la carte
		default:
			for _, id := range ids {
				if r.Intn(2) == 0 {
					targets = append(targets, id)
				}
			}
		}

		// Unité et valeur.
		unit := pick(r, "PERCENTAGE", "CURRENCY", "NEWPRICE")
		d.DiscountUnit = unit
		refPrice := uniformPrice
		switch unit {
		case "PERCENTAGE":
			d.DiscountValue = 1 + r.Intn(99)
		case "CURRENCY":
			d.DiscountValue = 1 + r.Intn(2*refPrice)
		case "NEWPRICE":
			d.DiscountValue = 0
			newPrice := r.Intn(refPrice) // < prix : la promotion abaisse toujours
			if len(targets) == 0 {
				// NEWPRICE exige un prix par produit : on cible tout.
				targets = append(targets, ids...)
			}
			for _, id := range targets {
				np := newPrice
				if !uniform {
					np = r.Intn(2*prices[id] + 1)
				}
				if f.dp[d.DiscountID] == nil {
					f.dp[d.DiscountID] = map[string]*models.DiscountProductInfo{}
				}
				f.dp[d.DiscountID][id] = &models.DiscountProductInfo{ProductID: id, NewPrice: &np}
			}
		}
		if unit != "NEWPRICE" && len(targets) > 0 {
			f.dp[d.DiscountID] = map[string]*models.DiscountProductInfo{}
			for _, id := range targets {
				f.dp[d.DiscountID][id] = &models.DiscountProductInfo{ProductID: id}
			}
		}

		// Minimum.
		switch r.Intn(3) {
		case 0: // aucun
		case 1:
			d.MinOrderUnit = pick(r, "QTY", "QUANTITY")
			if uniform && d.IsCumulative {
				d.MinOrderValue = r.Intn(d.DiscountedQuantity + 1)
			} else {
				d.MinOrderValue = r.Intn(7)
			}
		case 2:
			d.MinOrderUnit = pick(r, "CURRENCY", "EUR")
			d.MinOrderValue = r.Intn(uniformPrice*8 + 1)
		}

		// Plafond.
		switch r.Intn(4) {
		case 1:
			d.MaxDiscountValue = f64Ptr(float64(1 + r.Intn(refPrice/2+1)))
		case 2:
			d.MaxDiscountValue, d.MaxDiscountUnit = f64Ptr(float64(1+r.Intn(50))), strPtr("PERCENTAGE")
		}

		// Code promo.
		if r.Intn(100) < 15 {
			d.DiscountCode = strPtr("C1")
			if r.Intn(2) == 0 {
				f.code = "C1"
			}
		}

		// Options de la promotion.
		for _, id := range targets {
			if r.Intn(10) < 3 {
				var np *int
				switch r.Intn(3) {
				case 1:
					np = intPtr(0)
				case 2:
					np = intPtr(50)
				}
				if f.do[d.DiscountID] == nil {
					f.do[d.DiscountID] = map[string][]models.DiscountOptionInfo{}
				}
				f.do[d.DiscountID][id] = []models.DiscountOptionInfo{{OptionID: "X", IsOptionMandatory: r.Intn(2) == 0, NewPrice: np}}
			}
		}

		if weird {
			switch r.Intn(8) {
			case 0:
				d.DiscountUnit = "BOGUS"
			case 1:
				d.DiscountValue = -r.Intn(50)
			case 2:
				d.MinOrderValue = 1 << 30
				d.MinOrderUnit = "QTY"
			case 3:
				d.DiscountedQuantity = -r.Intn(3)
			case 4:
				d.DiscountValue = 100 + r.Intn(500)
			}
		}
		f.discounts = append(f.discounts, d)
	}
	return f
}

func (f fuzzScenario) request() *models.PricingRequest {
	return &models.PricingRequest{DiscountCode: f.code, Order: &models.OrderRequest{OrderType: f.orderType}}
}

func (f fuzzScenario) baseTotal() int {
	sum := 0
	for _, u := range f.units {
		sum += u.Price
	}
	return sum
}

func unitSignature(p models.OrderProductPayload) string {
	dp, id := "nil", "nil"
	if p.DiscountedPrice != nil {
		dp = fmt.Sprint(*p.DiscountedPrice)
	}
	if p.DiscountID != nil {
		id = *p.DiscountID
	}
	extras := ""
	if p.Config != nil {
		for _, a := range p.Config.Attributes {
			for _, o := range a.Options {
				extras += fmt.Sprintf("%s=%d;", o.ID, o.ExtraPrice)
			}
		}
	}
	return fmt.Sprintf("%d|%s|%s|%s", p.Price, dp, id, extras)
}

// TestEngine_DifferentialVsLegacy : quand toutes les unités éligibles d'une
// promotion ont la même remise, le nouveau calcul doit donner exactement le
// résultat de l'ancien (mêmes unités, mêmes prix, mêmes suppléments d'options,
// mêmes remises appliquées). Toute différence est une régression.
func TestEngine_DifferentialVsLegacy(t *testing.T) {
	const iterations = 40000
	svc := &OrdersService{}
	for i := 0; i < iterations; i++ {
		seed := int64(20261018 + i)
		r := rand.New(rand.NewSource(seed))
		// Un tiers des tirages avec promotions disjointes, le reste avec une
		// seule promotion : les chevauchements relèvent de D13 (voir ci-dessous).
		disjoint := i%3 == 0
		f := genScenario(r, true, disjoint, false)
		if !disjoint && len(f.discounts) > 1 {
			f.discounts = f.discounts[:1]
		}

		newUnits := append([]models.OrderProductPayload(nil), f.units...)
		oldUnits := append([]models.OrderProductPayload(nil), f.units...)
		newApplied := svc.applyDiscounts(f.request(), newUnits, f.discounts, f.dp, f.do, f.baseTotal())
		oldApplied := svc.legacyApplyDiscounts(f.request(), oldUnits, f.discounts, f.dp, f.do, f.baseTotal())

		for j := range newUnits {
			if a, b := unitSignature(newUnits[j]), unitSignature(oldUnits[j]); a != b {
				t.Fatalf("seed %d, unité %d : nouveau %s, ancien %s\n%s", seed, j, a, b, f.describe())
			}
		}
		if !reflect.DeepEqual(newApplied, oldApplied) {
			t.Fatalf("seed %d : remises appliquées nouveau %v, ancien %v\n%s", seed, newApplied, oldApplied, f.describe())
		}
	}
}

// --- Propriétés ----------------------------------------------------------------

func TestEngine_Properties(t *testing.T) {
	const iterations = 40000
	svc := &OrdersService{}
	for i := 0; i < iterations; i++ {
		seed := int64(7000000 + i)
		r := rand.New(rand.NewSource(seed))
		f := genScenario(r, false, false, true)

		units := append([]models.OrderProductPayload(nil), f.units...)
		before := append([]models.OrderProductPayload(nil), f.units...)
		var applied []string
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("seed %d : panique %v\n%s", seed, rec, f.describe())
				}
			}()
			applied = svc.applyDiscounts(f.request(), units, f.discounts, f.dp, f.do, f.baseTotal())
		}()

		if err := checkDiscountResult(before, units); err != nil {
			t.Fatalf("seed %d : résultat incohérent : %v\n%s", seed, err, f.describe())
		}
		if got := len(discountedIndexes(units)); got != len(applied) {
			t.Fatalf("seed %d : %d unités remisées pour %d remises appliquées\n%s", seed, got, len(applied), f.describe())
		}
	}
}

// TestEngine_Properties_OrderIndependence : avec une seule promotion ou des
// promotions disjointes et sans options, le total ne dépend pas de l'ordre du panier.
func TestEngine_Properties_OrderIndependence(t *testing.T) {
	const iterations = 15000
	svc := &OrdersService{}
	effective := func(ps []models.OrderProductPayload) int {
		sum := 0
		for _, p := range ps {
			if p.DiscountedPrice != nil {
				sum += *p.DiscountedPrice
			} else {
				sum += p.Price
			}
		}
		return sum
	}
	for i := 0; i < iterations; i++ {
		seed := int64(9000000 + i)
		r := rand.New(rand.NewSource(seed))
		f := genScenario(r, false, i%2 == 0, false)
		if i%2 != 0 && len(f.discounts) > 1 {
			f.discounts = f.discounts[:1]
		}
		for j := range f.units {
			f.units[j].Config = nil // les suppléments d'options dépendent de l'unité
		}
		f.do = map[string]map[string][]models.DiscountOptionInfo{}

		run := func(units []models.OrderProductPayload) int {
			cp := append([]models.OrderProductPayload(nil), units...)
			svc.applyDiscounts(f.request(), cp, f.discounts, f.dp, f.do, f.baseTotal())
			return effective(cp)
		}
		want := run(f.units)
		for s := 0; s < 4; s++ {
			shuffled := append([]models.OrderProductPayload(nil), f.units...)
			r.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
			if got := run(shuffled); got != want {
				t.Fatalf("seed %d : total %d après mélange, %d avant\n%s", seed, got, want, f.describe())
			}
		}
	}
}

// --- Garde-fous ------------------------------------------------------------------

func TestApplyDiscountsSafe_PanicCancelsEveryDiscount(t *testing.T) {
	// req.Order nil : accès au mode de commande d'une promotion qui en porte un
	// → panique nil dans le calcul. Le pricing doit continuer au prix du catalogue.
	products := cartOf(1000, 2000)
	d := enginePromo("d1", "PERCENTAGE", 50)
	d.DiscountOrderType = "IN"

	var applied []string
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("la panique ne doit jamais sortir d'applyDiscountsSafe : %v", rec)
			}
		}()
		applied = (&OrdersService{}).applyDiscountsSafe(context.Background(), &models.PricingRequest{MerchantID: "42"},
			products, []*models.DBDiscount{d}, map[string]map[string]*models.DiscountProductInfo{}, map[string]map[string][]models.DiscountOptionInfo{}, 3000)
	}()

	if len(applied) != 0 {
		t.Fatalf("remises appliquées = %v, want aucune", applied)
	}
	for i, p := range products {
		if p.DiscountedPrice != nil || p.DiscountID != nil {
			t.Fatalf("unité %d remisée malgré la panique : %+v", i, p)
		}
	}
}

func TestApplyDiscountsSafe_NormalResultIsKept(t *testing.T) {
	products := cartOf(1000)
	applied := (&OrdersService{}).applyDiscountsSafe(context.Background(),
		&models.PricingRequest{MerchantID: "42", Order: &models.OrderRequest{OrderType: "IN"}},
		products, []*models.DBDiscount{enginePromo("d1", "PERCENTAGE", 20)},
		map[string]map[string]*models.DiscountProductInfo{}, map[string]map[string][]models.DiscountOptionInfo{}, 1000)
	if !reflect.DeepEqual(applied, []string{"d1"}) || products[0].DiscountedPrice == nil || *products[0].DiscountedPrice != 800 {
		t.Fatalf("résultat normal modifié : applied=%v prix=%v", applied, products[0].DiscountedPrice)
	}
}

func TestCheckDiscountResult(t *testing.T) {
	ok := func() []models.OrderProductPayload {
		return []models.OrderProductPayload{{ProductID: "A", Price: 1000, Quantity: 1}}
	}
	withDiscount := func(price int, id *string) []models.OrderProductPayload {
		u := ok()
		u[0].DiscountedPrice, u[0].DiscountID = &price, id
		return u
	}
	d1 := strPtr("d1")

	cases := []struct {
		name    string
		after   []models.OrderProductPayload
		wantErr bool
	}{
		{"aucune remise", ok(), false},
		{"remise normale", withDiscount(800, d1), false},
		{"remise à zéro", withDiscount(0, d1), false},
		{"prix égal au prix de base", withDiscount(1000, d1), false},
		{"hausse de prix", withDiscount(1200, d1), true},
		{"prix négatif", withDiscount(-1, d1), true},
		{"prix remisé sans identifiant", withDiscount(800, nil), true},
		{"identifiant sans prix remisé", func() []models.OrderProductPayload {
			u := ok()
			u[0].DiscountID = d1
			return u
		}(), true},
		{"prix de base modifié", func() []models.OrderProductPayload {
			u := ok()
			u[0].Price = 900
			return u
		}(), true},
		{"nombre d'unités modifié", append(ok(), ok()...), true},
		{"supplément d'option négatif", func() []models.OrderProductPayload {
			u := withDiscount(800, d1)
			u[0].Config = withOptions(-50, "X")
			return u
		}(), true},
		{"supplément d'option promotionnel à zéro", func() []models.OrderProductPayload {
			u := withDiscount(800, d1)
			u[0].Config = withOptions(0, "X")
			return u
		}(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkDiscountResult(ok(), tc.after)
			if (err != nil) != tc.wantErr {
				t.Fatalf("erreur = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// pricingMock prépare les requêtes qui précèdent le chargement des promotions.
func pricingMock(t *testing.T) (*OrdersService, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	mock.ExpectQuery(`FROM merchant m`).
		WillReturnRows(sqlmock.NewRows([]string{"timezone", "currency", "delivery_fees", "delivery_fees_limit", "minimum_cart_for_delivery_order"}).
			AddRow("Europe/Paris", "EUR", 300, 2000, 0))
	mock.ExpectQuery(`WHERE status NOT IN`).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "name", "status"}))
	mock.ExpectQuery(`FROM products p\s+INNER JOIN tva_categories`).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "name", "price", "price_take_away", "price_delivery", "tva_rate_in", "tva_rate_delivery", "tva_rate_take_away"}).
			AddRow("A", "Burger", 1000, 1000, 1100, 10.0, 5.5, 5.5))
	return &OrdersService{ordersRepo: NewOrdersRepository(db, nil)}, mock, func() { db.Close() }
}

func degradedRequest(withRewards bool) *models.PricingRequest {
	req := &models.PricingRequest{
		MerchantID: "42",
		Order: &models.OrderRequest{
			OrderType: "IN",
			Products:  []models.OrderProductPayload{{ProductID: "A", Quantity: 2}},
		},
	}
	if withRewards {
		cid := "c1"
		req.Order.Customer = &models.CustomerRequest{CustomerID: &cid, AvailableRewards: []models.DBReward{{RewardID: "r1"}}}
	}
	return req
}

// TestComputePricing_DegradedWhenDiscountsCannotBeLoaded : une panne de lecture
// des promotions ne bloque jamais le pricing (G1) : prix du catalogue, pas d'erreur.
func TestComputePricing_DegradedWhenDiscountsCannotBeLoaded(t *testing.T) {
	stages := []struct {
		name  string
		setup func(mock sqlmock.Sqlmock)
	}{
		{"échec de GetDiscounts", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(`COALESCE\(d\.discount_order_type, ''\)`).WillReturnError(errors.New("boom"))
		}},
		{"échec de GetDiscountProducts", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(`COALESCE\(d\.discount_order_type, ''\)`).WillReturnRows(sqlmock.NewRows(discountColumns).
				AddRow("d1", "", nil, "Promo", "desc", 20, "PERCENTAGE", 0.0, "", nil, nil, 1, false, true, 0))
			mock.ExpectQuery(`FROM discounts_products dp`).WillReturnError(errors.New("boom"))
		}},
		{"échec de GetDiscountProductOptions", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(`COALESCE\(d\.discount_order_type, ''\)`).WillReturnRows(sqlmock.NewRows(discountColumns).
				AddRow("d1", "", nil, "Promo", "desc", 20, "PERCENTAGE", 0.0, "", nil, nil, 1, false, true, 0))
			mock.ExpectQuery(`FROM discounts_products dp`).
				WillReturnRows(sqlmock.NewRows([]string{"discount_id", "product_id", "new_price"}))
			mock.ExpectQuery(`discounts_products_options`).WillReturnError(errors.New("boom"))
		}},
	}
	for _, st := range stages {
		t.Run(st.name, func(t *testing.T) {
			svc, mock, closeDB := pricingMock(t)
			defer closeDB()
			st.setup(mock)
			mock.ExpectQuery(`.+`).WillReturnRows(sqlmock.NewRows([]string{"seconds"}).AddRow(nil))

			resp, err := svc.ComputePricing(context.Background(), degradedRequest(false))
			if err != nil {
				t.Fatalf("ComputePricing ne doit pas échouer sur une panne des promotions : %v", err)
			}
			if got := resp.OrderRequest.Order.TTC; got != 2000 {
				t.Fatalf("TTC = %d, want 2000 (prix du catalogue, sans promotion)", got)
			}
			for _, p := range resp.OrderRequest.Order.Products {
				if p.DiscountedPrice != nil {
					t.Fatalf("aucune remise attendue, got %+v", p)
				}
			}
		})
	}
}

func TestComputePricing_DegradedWhenRewardsCannotBeLoaded(t *testing.T) {
	svc, mock, closeDB := pricingMock(t)
	defer closeDB()
	mock.ExpectQuery(`COALESCE\(d\.discount_order_type, ''\)`).WillReturnRows(sqlmock.NewRows(discountColumns))
	mock.ExpectQuery(`FROM discounts_products dp`).WillReturnRows(sqlmock.NewRows([]string{"discount_id", "product_id", "new_price"}))
	mock.ExpectQuery(`discounts_products_options`).WillReturnRows(sqlmock.NewRows([]string{"discount_id", "product_id", "option_id", "new_price", "is_option_mandatory"}))
	mock.ExpectQuery(`customer_rewards`).WillReturnError(errors.New("boom"))
	mock.ExpectQuery(`.+`).WillReturnRows(sqlmock.NewRows([]string{"seconds"}).AddRow(nil))

	resp, err := svc.ComputePricing(context.Background(), degradedRequest(true))
	if err != nil {
		t.Fatalf("ComputePricing ne doit pas échouer sur une panne des récompenses : %v", err)
	}
	if got := resp.OrderRequest.Order.TTC; got != 2000 {
		t.Fatalf("TTC = %d, want 2000", got)
	}
}
