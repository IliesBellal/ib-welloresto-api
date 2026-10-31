package service

import (
	"encoding/json"
	"testing"

	"go.uber.org/zap"

	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// Commande réelle 0574D (Croq'Ô'Pizzas Metz, 28/10/2026), sans les données du
// client : deux paires de pizzas, une offerte dans chaque paire (BOGO financé à
// 100 % par l'établissement), 74,68 € de lignes pour 46,19 € payés.
const orderBOGO = `{
  "id": "5bb2a1a1-3b44-4ad6-927d-b841e110574d",
  "display_id": "0574D",
  "cart": {"items": [
    {"id": "b198d37a-c1a8-4747-80dc-3ace02c1ae6d", "title": "Pizza 4 Fromage", "quantity": 2,
     "price": {"unit_price": {"amount": 1399}, "total_price": {"amount": 2798}, "base_unit_price": {"amount": 1399}},
     "selected_modifier_groups": [{"id": "g1", "title": "Base", "selected_items": [
        {"id": "o1", "title": "Tomate", "quantity": 1, "price": {"unit_price": {"amount": 0}}}]}]},
    {"id": "24c248b9-ccfe-4403-98b7-e754399ad4e3", "title": "Pizza Reine", "quantity": 2,
     "price": {"unit_price": {"amount": 1450}, "total_price": {"amount": 2900}, "base_unit_price": {"amount": 1450}}},
    {"id": "🍕_Pizza_Chèvre-Miel_", "title": "Pizza Chèvre-Miel", "quantity": 1,
     "price": {"unit_price": {"amount": 1320}, "total_price": {"amount": 1320}, "base_unit_price": {"amount": 1320}}},
    {"id": "732feca8-2472-47d7-ab9c-734923dea5c0", "title": "Oasis Tropical (2L)", "quantity": 1,
     "price": {"unit_price": {"amount": 450}, "total_price": {"amount": 450}, "base_unit_price": {"amount": 450}}}
  ]},
  "payment": {
    "charges": {
      "total": {"amount": 7468}, "sub_total": {"amount": 7468},
      "total_promo_applied": {"amount": 4619}, "sub_total_promo_applied": {"amount": 4619}
    },
    "promotions": {"promotions": [{
      "promo_type": "BOGO",
      "discount_items": [
        {"external_id": "24c248b9-ccfe-4403-98b7-e754399ad4e3", "discounted_quantity": 1},
        {"external_id": "b198d37a-c1a8-4747-80dc-3ace02c1ae6d", "discounted_quantity": 1}],
      "promo_discount_value": -2849,
      "promo_funding_splits": [
        {"amount_paid": {"amount": -2849}, "funding_source": "ORGANIZATION"},
        {"amount_paid": {"amount": 0}, "funding_source": "UBER"}]
    }]}
  }
}`

func parseOrder(t *testing.T, raw string) *ueModels.UberOrder {
	t.Helper()
	var o ueModels.UberOrder
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		t.Fatalf("payload illisible: %v", err)
	}
	return &o
}

// products reproduit ce que mapUberItemsToOrderProducts produit : une ligne par
// article du panier, au prix unitaire d'Uber.
func productsOf(o *ueModels.UberOrder) []ordersModels.OrderProductPayload {
	out := make([]ordersModels.OrderProductPayload, len(o.Cart.Items))
	for i, it := range o.Cart.Items {
		out[i] = ordersModels.OrderProductPayload{
			ProductID: it.ID, ProductName: it.Title, Quantity: it.Quantity, Price: it.Price.UnitPrice.Amount,
		}
	}
	return out
}

func orderTotal(ps []ordersModels.OrderProductPayload) int {
	t := 0
	for _, p := range ps {
		price := p.Price
		if p.DiscountedPrice != nil {
			price = *p.DiscountedPrice
		}
		t += p.Quantity * price
	}
	return t
}

func TestApplyPromotions_BOGOReel(t *testing.T) {
	order := parseOrder(t, orderBOGO)

	got := applyPromotions(zap.NewNop(), order, productsOf(order))

	if total := orderTotal(got); total != 4619 {
		t.Fatalf("somme des lignes = %d, want 4619 (sub_total_promo_applied)", total)
	}
	type line struct {
		name  string
		qty   int
		base  int
		price int
	}
	want := []line{
		{"Pizza 4 Fromage", 1, 1399, 1399},
		{"Pizza 4 Fromage", 1, 1399, 0},
		{"Pizza Reine", 1, 1450, 1450},
		{"Pizza Reine", 1, 1450, 0},
		{"Pizza Chèvre-Miel", 1, 1320, 1320},
		{"Oasis Tropical (2L)", 1, 450, 450},
	}
	if len(got) != len(want) {
		t.Fatalf("%d lignes, want %d : %+v", len(got), len(want), got)
	}
	for i, w := range want {
		p := got[i]
		price := p.Price
		if p.DiscountedPrice != nil {
			price = *p.DiscountedPrice
		}
		if p.ProductName != w.name || p.Quantity != w.qty || p.Price != w.base || price != w.price {
			t.Errorf("ligne %d = {%s ×%d base %d prix %d}, want %+v", i, p.ProductName, p.Quantity, p.Price, price, w)
		}
	}
	// les lignes non remisées gardent DiscountedPrice vide (comportement historique)
	if got[0].DiscountedPrice != nil || got[4].DiscountedPrice != nil || got[5].DiscountedPrice != nil {
		t.Errorf("une ligne non remisée porte un prix remisé")
	}
}

// Commande FABD1 : le prix unitaire (15,89 €) comprend 1,90 € d'option, la
// promotion n'offre que le prix de base (13,99 €).
func TestApplyPromotions_optionPayanteNonOfferte(t *testing.T) {
	order := parseOrder(t, `{
	  "display_id": "FABD1",
	  "cart": {"items": [{"id": "pizza", "title": "Pizza Fromage", "quantity": 2,
	    "price": {"unit_price": {"amount": 1589}},
	    "selected_modifier_groups": [{"id": "g", "title": "Suppl", "selected_items": [
	       {"id": "o", "title": "Chèvre", "quantity": 1, "price": {"unit_price": {"amount": 190}}}]}]}]},
	  "payment": {"charges": {"sub_total": {"amount": 3178}, "sub_total_promo_applied": {"amount": 1779}},
	    "promotions": {"promotions": [{"promo_type": "BOGO",
	       "discount_items": [{"external_id": "pizza", "discounted_quantity": 1}]}]}}
	}`)

	got := applyPromotions(zap.NewNop(), order, productsOf(order))

	if len(got) != 2 {
		t.Fatalf("%d lignes, want 2 : %+v", len(got), got)
	}
	if got[0].DiscountedPrice != nil || got[1].DiscountedPrice == nil || *got[1].DiscountedPrice != 190 {
		t.Fatalf("la pizza offerte doit coûter son option (190), got %+v / %+v", got[0], got[1])
	}
	if total := orderTotal(got); total != 1779 {
		t.Fatalf("somme des lignes = %d, want 1779", total)
	}
}

func TestApplyPromotions_sansPromotion_inchange(t *testing.T) {
	order := parseOrder(t, `{
	  "cart": {"items": [{"id": "a", "title": "Pizza", "quantity": 2, "price": {"unit_price": {"amount": 1000}}}]},
	  "payment": {"charges": {"sub_total": {"amount": 2000}}}
	}`)
	products := productsOf(order)

	got := applyPromotions(zap.NewNop(), order, products)

	if len(got) != 1 || got[0].DiscountedPrice != nil || got[0].Quantity != 2 {
		t.Fatalf("la commande doit rester inchangée : %+v", got)
	}
}

// Promotion financée par Uber seul : sub_total_promo_applied vaut les lignes
// (le restaurant est payé en entier), rien à remiser.
func TestApplyPromotions_promotionFinanceeParUber_inchange(t *testing.T) {
	order := parseOrder(t, `{
	  "cart": {"items": [{"id": "a", "title": "Pizza", "quantity": 2, "price": {"unit_price": {"amount": 1000}}}]},
	  "payment": {"charges": {"sub_total": {"amount": 2000}, "sub_total_promo_applied": {"amount": 2000}},
	    "promotions": {"promotions": [{"promo_type": "BOGO",
	      "discount_items": [{"external_id": "a", "discounted_quantity": 1}],
	      "promo_funding_splits": [{"amount_paid": {"amount": 0}, "funding_source": "ORGANIZATION"},
	                               {"amount_paid": {"amount": -1000}, "funding_source": "UBER"}]}]}}
	}`)

	got := applyPromotions(zap.NewNop(), order, productsOf(order))

	if len(got) != 1 || got[0].DiscountedPrice != nil {
		t.Fatalf("une promotion financée par Uber ne change pas les lignes : %+v", got)
	}
}

// 20 % sur le panier : pas d'article désigné, la remise est répartie au prorata.
func TestApplyPromotions_remiseSurLePanier_auProrata(t *testing.T) {
	order := parseOrder(t, `{
	  "cart": {"items": [
	    {"id": "a", "title": "Pizza", "quantity": 1, "price": {"unit_price": {"amount": 1000}}},
	    {"id": "b", "title": "Boisson", "quantity": 2, "price": {"unit_price": {"amount": 500}}}]},
	  "payment": {"charges": {"sub_total": {"amount": 2000}, "sub_total_promo_applied": {"amount": 1600}},
	    "promotions": {"promotions": [{"promo_type": "PERCENTOFF"}]}}
	}`)

	got := applyPromotions(zap.NewNop(), order, productsOf(order))

	if total := orderTotal(got); total != 1600 {
		t.Fatalf("somme des lignes = %d, want 1600", total)
	}
	if got[0].DiscountedPrice == nil || *got[0].DiscountedPrice != 800 {
		t.Fatalf("la pizza doit passer à 800, got %+v", got[0])
	}
}

func TestApplyPromotions_remiseSuperieureAuxLignes_inchange(t *testing.T) {
	order := parseOrder(t, `{
	  "cart": {"items": [{"id": "a", "title": "Pizza", "quantity": 1, "price": {"unit_price": {"amount": 1000}}}]},
	  "payment": {"charges": {"sub_total_promo_applied": {"amount": -50}}}
	}`)

	got := applyPromotions(zap.NewNop(), order, productsOf(order))

	if len(got) != 1 || got[0].DiscountedPrice != nil {
		t.Fatalf("lignes inchangées attendues : %+v", got)
	}
}

func TestIsAlcoholByLabels(t *testing.T) {
	cases := []struct {
		raw          string
		alcohol, set bool
	}{
		{`{"title": "Heineken"}`, false, false},
		{`{"title": "Heineken", "tax_info": {"labels": ["CAT_BEER", "TEMP_COLD"]}}`, true, true},
		{`{"title": "Rosé", "taxInfo": {"labels": ["CAT_WINE"]}}`, true, true},
		{`{"title": "Rosé", "price": {"taxInfo": {"labels": ["CAT_ALCOHOL"]}}}`, true, true},
		{`{"title": "Coca", "tax_info": {"labels": ["TEMP_COLD"]}}`, false, true},
	}
	for _, c := range cases {
		var item ueModels.UberCartItem
		if err := json.Unmarshal([]byte(c.raw), &item); err != nil {
			t.Fatal(err)
		}
		alcohol, known := item.IsAlcoholByLabels()
		if alcohol != c.alcohol || known != c.set {
			t.Errorf("%s : alcool=%v connu=%v, want %v %v", c.raw, alcohol, known, c.alcohol, c.set)
		}
	}
}
