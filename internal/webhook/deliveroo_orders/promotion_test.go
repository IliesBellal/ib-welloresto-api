package deliveroo_orders

import (
	"testing"

	"go.uber.org/zap"

	"welloresto-api/internal/models"
)

func money(c int) DeliverooMoney { return DeliverooMoney{Fractional: c, CurrencyCode: "EUR"} }

func productsFor(items []DeliverooItem) []models.OrderProductPayload {
	out := make([]models.OrderProductPayload, len(items))
	for i, it := range items {
		out[i] = models.OrderProductPayload{ProductID: it.PosItemID, ProductName: it.Name, Quantity: it.Quantity, Price: it.UnitPrice.Fractional}
	}
	return out
}

func sumLines(ps []models.OrderProductPayload) int {
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

func TestApplyDiscounts_remiseSurUnArticle(t *testing.T) {
	ord := DeliverooOrder{
		DisplayID:  "ABC",
		TotalPrice: money(2370),
		Items: []DeliverooItem{
			{PosItemID: "pizza", Name: "Pizza", Quantity: 2, UnitPrice: money(1000), DiscountAmount: money(630)},
			{PosItemID: "soda", Name: "Soda", Quantity: 1, UnitPrice: money(1000)},
		},
	}

	got := applyDiscounts(zap.NewNop(), ord, productsFor(ord.Items))

	if total := sumLines(got); total != 2370 {
		t.Fatalf("somme des lignes = %d, want 2370 (order.total_price)", total)
	}
	// la remise de 630 va tout entière à la pizza ; la boisson garde son prix
	last := got[len(got)-1]
	if last.ProductID != "soda" || last.DiscountedPrice != nil {
		t.Fatalf("la boisson ne doit pas être remisée : %+v", last)
	}
}

func TestApplyDiscounts_remiseSurLePanier_auProrata(t *testing.T) {
	ord := DeliverooOrder{
		TotalPrice:    money(1600),
		OfferDiscount: money(400),
		Items: []DeliverooItem{
			{PosItemID: "a", Name: "A", Quantity: 1, UnitPrice: money(1000)},
			{PosItemID: "b", Name: "B", Quantity: 2, UnitPrice: money(500)},
		},
	}

	got := applyDiscounts(zap.NewNop(), ord, productsFor(ord.Items))

	if total := sumLines(got); total != 1600 {
		t.Fatalf("somme des lignes = %d, want 1600", total)
	}
	if got[0].DiscountedPrice == nil || *got[0].DiscountedPrice != 800 {
		t.Fatalf("l'article A doit passer à 800 : %+v", got[0])
	}
}

// Sans remise annoncée, un écart entre lignes et total n'est jamais pris pour
// une promotion.
func TestApplyDiscounts_sansRemise_inchange(t *testing.T) {
	ord := DeliverooOrder{
		TotalPrice: money(1500),
		Items:      []DeliverooItem{{PosItemID: "a", Name: "A", Quantity: 1, UnitPrice: money(2000)}},
	}

	got := applyDiscounts(zap.NewNop(), ord, productsFor(ord.Items))

	if len(got) != 1 || got[0].DiscountedPrice != nil {
		t.Fatalf("lignes inchangées attendues : %+v", got)
	}
}

// unit_price déjà net de la remise : l'écart est nul, rien à changer.
func TestApplyDiscounts_prixDejaNets_inchange(t *testing.T) {
	ord := DeliverooOrder{
		TotalPrice: money(1000),
		Items:      []DeliverooItem{{PosItemID: "a", Name: "A", Quantity: 1, UnitPrice: money(1000), DiscountAmount: money(500)}},
	}

	got := applyDiscounts(zap.NewNop(), ord, productsFor(ord.Items))

	if len(got) != 1 || got[0].DiscountedPrice != nil {
		t.Fatalf("lignes inchangées attendues : %+v", got)
	}
}

// Des frais dans le total (lignes inférieures au total) ne font jamais une remise.
func TestApplyDiscounts_totalAvecFrais_inchange(t *testing.T) {
	ord := DeliverooOrder{
		TotalPrice:    money(1300),
		OfferDiscount: money(100),
		Items:         []DeliverooItem{{PosItemID: "a", Name: "A", Quantity: 1, UnitPrice: money(1000)}},
	}

	got := applyDiscounts(zap.NewNop(), ord, productsFor(ord.Items))

	if len(got) != 1 || got[0].DiscountedPrice != nil {
		t.Fatalf("lignes inchangées attendues : %+v", got)
	}
}

func TestApplyDiscounts_quantiteNonDivisible_scindeLaLigne(t *testing.T) {
	ord := DeliverooOrder{
		TotalPrice: money(2900),
		Items:      []DeliverooItem{{PosItemID: "a", Name: "A", Quantity: 3, UnitPrice: money(1000), DiscountAmount: money(100)}},
	}

	got := applyDiscounts(zap.NewNop(), ord, productsFor(ord.Items))

	if len(got) != 2 {
		t.Fatalf("%d lignes, want 2 : %+v", len(got), got)
	}
	if total := sumLines(got); total != 2900 {
		t.Fatalf("somme des lignes = %d, want 2900", total)
	}
}
