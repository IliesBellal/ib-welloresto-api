package tasks

import (
	"testing"
)

func lowPriceIDs(t *testing.T, catalog []upsellCatalogProduct, sales map[string]int, limit int) ([]string, float64) {
	t.Helper()
	entries, median := selectUpsellLowPrice(catalog, sales, limit)
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ProductID)
	}
	return ids, median
}

func TestSelectUpsellLowPrice_ThirdOfMedianBestSellersFirst(t *testing.T) {
	// Mains dominate the catalogue, as in real menus: 15 prices above 0,
	// median 990, threshold 330.
	catalog := []upsellCatalogProduct{
		{"orangina", 190}, {"cristalline", 140}, {"coca", 190},
		{"magnum", 290}, {"tiramisu", 300},
		{"sauce", 0}, // free: never in the list
		{"group-no-variant", 0},
		{"panini", 500}, // above the threshold
		{"p1", 990}, {"p2", 990}, {"p3", 990}, {"p4", 990}, {"p5", 990},
		{"p6", 990}, {"p7", 990}, {"p8", 990}, {"p9", 990},
	}
	sales := map[string]int{
		"orangina": 335, "cristalline": 177, "coca": 177, "magnum": 53,
		"tiramisu": 0, // never sold: left out
		"sauce":    42, "panini": 51, "p1": 309,
	}

	ids, median := lowPriceIDs(t, catalog, sales, 30)

	if median != 990 {
		t.Fatalf("median = %v, want 990 (prices at 0 ignored)", median)
	}
	// Threshold 330: orangina, cristalline, coca, magnum. Coca and cristalline
	// tie on orders: the cheaper one comes first.
	want := []string{"orangina", "cristalline", "coca", "magnum"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

func TestSelectUpsellLowPrice_EvenMedianAndLimit(t *testing.T) {
	catalog := []upsellCatalogProduct{{"a", 100}, {"b", 200}, {"c", 900}, {"d", 1100}}
	sales := map[string]int{"a": 5, "b": 9, "c": 1, "d": 1}

	// Median (200+900)/2 = 550 → threshold 183.3: only "a" qualifies.
	ids, median := lowPriceIDs(t, catalog, sales, 30)
	if median != 550 || len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("ids = %v, median = %v; want [a], 550", ids, median)
	}

	cheap := []upsellCatalogProduct{{"a", 100}, {"b", 100}, {"c", 100}, {"d", 900}, {"e", 900}, {"f", 900}, {"g", 900}}
	ids, _ = lowPriceIDs(t, cheap, map[string]int{"a": 1, "b": 2, "c": 3}, 2)
	if len(ids) != 2 || ids[0] != "c" || ids[1] != "b" {
		t.Fatalf("ids = %v, want the 2 best sellers [c b]", ids)
	}
}

func TestSelectUpsellLowPrice_EmptyCatalog(t *testing.T) {
	entries, median := selectUpsellLowPrice(nil, nil, 30)
	if len(entries) != 0 || median != 0 || entries == nil {
		t.Fatalf("got %v (nil=%v), median %v; want an empty non-nil list", entries, entries == nil, median)
	}
}
