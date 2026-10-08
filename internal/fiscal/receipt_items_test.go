package fiscal

import (
	"testing"

	"welloresto-api/internal/models"
)

// checkReceiptItems : la somme des lignes vaut la TVA ventilée, au total et à
// chaque taux (TTC et HT), et TTC = HT + TVA sur chaque ligne.
func checkReceiptItems(t *testing.T, items []models.SnapshotItem, details TaxDetails) {
	t.Helper()
	ttc, ht := map[int64]int64{}, map[int64]int64{}
	var sumTTC, sumHT int64
	for _, it := range items {
		if it.TotalTTC != it.TotalHT+it.TotalTVA {
			t.Errorf("line %q: TTC %d != HT %d + TVA %d", it.Name, it.TotalTTC, it.TotalHT, it.TotalTVA)
		}
		ttc[it.TaxRate] += it.TotalTTC
		ht[it.TaxRate] += it.TotalHT
		sumTTC += it.TotalTTC
		sumHT += it.TotalHT
	}
	if sumTTC != details.TotalTTC() || sumHT != details.TotalHT() {
		t.Errorf("lines TTC %d / HT %d, tax details TTC %d / HT %d", sumTTC, sumHT, details.TotalTTC(), details.TotalHT())
	}
	for _, d := range details.Lines {
		bp := int64(d.Rate*100 + 0.5)
		if ttc[bp] != d.TTC || ht[bp] != d.HT {
			t.Errorf("rate %v: lines TTC %d / HT %d, tax details TTC %d / HT %d", d.Rate, ttc[bp], ht[bp], d.TTC, d.HT)
		}
	}
}

func TestBuildReceiptItems_CompleteSale(t *testing.T) {
	lines := []SaleLine{
		{Kind: models.SnapshotKindArticle, Item: "1", Label: "Pizza", Quantity: 2, UnitTTC: 600, Rate: 10},
		{Kind: models.SnapshotKindOption, Item: "1", Label: "Grande taille", Quantity: 2, UnitTTC: 150, Rate: 10},
		{Kind: models.SnapshotKindSupplement, Item: "1", Label: "Bacon", Quantity: 2, UnitTTC: 100, Rate: 10},
		{Kind: models.SnapshotKindArticle, Item: "2", Label: "Bière", Quantity: 1, UnitTTC: 500, Rate: 20},
		{Kind: models.SnapshotKindDelivery, Label: "Frais de livraison", Quantity: 1, UnitTTC: 300, Rate: 20},
	}
	items, details := BuildReceiptItems(lines, 280)
	checkReceiptItems(t, items, details)

	// 2 × (600 + 150 + 100) = 1 700 à 10 %, 800 à 20 % : 2 500, remise 280.
	if details.TotalTTC() != 2220 || details.Discount != 280 {
		t.Fatalf("details: %+v", details)
	}
	if len(items) != 7 {
		t.Fatalf("expected 5 sale lines + 2 discount lines, got %d: %+v", len(items), items)
	}
	if items[1].Parent == nil || *items[1].Parent != 0 || items[2].Parent == nil || *items[2].Parent != 0 {
		t.Fatalf("option and supplement must point to their article: %+v %+v", items[1], items[2])
	}
	var discount int64
	for _, it := range items[5:] {
		if it.Kind != models.SnapshotKindDiscount || it.TotalTTC >= 0 {
			t.Fatalf("expected negative discount lines, got %+v", it)
		}
		discount += it.TotalTTC
	}
	if discount != -280 || items[5].Name != "Remise (TVA 10 %)" || items[6].Name != "Remise (TVA 20 %)" {
		t.Fatalf("discount lines: %+v %+v", items[5], items[6])
	}
	if items[0].PriceTTC != 600 || items[0].Quantity != 2 || items[0].TaxRate != 1000 || items[0].TotalTTC != 1200 {
		t.Fatalf("article line keeps its unit fields: %+v", items[0])
	}
}

func TestBuildReceiptItems_NoDiscount(t *testing.T) {
	lines := []SaleLine{
		{Kind: models.SnapshotKindArticle, Item: "1", Label: "Café", Quantity: 3, UnitTTC: 1, Rate: 5.5},
		{Kind: models.SnapshotKindArticle, Item: "2", Label: "Thé", Quantity: 1, UnitTTC: 1, Rate: 5.5},
		{Kind: models.SnapshotKindArticle, Item: "3", Label: "Menu", Quantity: 1, UnitTTC: 1299, Rate: 10},
	}
	items, details := BuildReceiptItems(lines, 0)
	checkReceiptItems(t, items, details)
	if len(items) != 3 {
		t.Fatalf("no discount line expected, got %+v", items)
	}
}

func TestBuildReceiptItems_DiscountAbsorbsEverything(t *testing.T) {
	lines := []SaleLine{
		{Kind: models.SnapshotKindArticle, Item: "1", Label: "Offert", Quantity: 1, UnitTTC: 900, Rate: 10},
	}
	items, details := BuildReceiptItems(lines, 1500) // plafonnée au total
	checkReceiptItems(t, items, details)
	if details.TotalTTC() != 0 || len(items) != 2 || items[1].TotalTTC != -900 || items[1].Name != "Remise" {
		t.Fatalf("items %+v, details %+v", items, details)
	}
}

func TestBuildReceiptItems_NegativeLine(t *testing.T) {
	lines := []SaleLine{
		{Kind: models.SnapshotKindArticle, Item: "1", Label: "Plat", Quantity: 1, UnitTTC: 1000, Rate: 10},
		{Kind: models.SnapshotKindArticle, Item: "2", Label: "Avoir consigne", Quantity: 1, UnitTTC: -100, Rate: 10},
	}
	items, details := BuildReceiptItems(lines, 0)
	checkReceiptItems(t, items, details)
}
