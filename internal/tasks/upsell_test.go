package tasks

import (
	"testing"

	upsellModule "welloresto-api/internal/modules/upsell"
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

func TestUpsellSmoothedConfidence_SmallSamplesPulledTowardsAverage(t *testing.T) {
	// 6 orders out of 6, for a product present in 1 % of 1 000 orders:
	// (6 + 10 × 0.01) / (6 + 10) = 0.38, far from a certainty.
	if got := upsellSmoothedConfidence(6, 6, 10, 1000); got < 0.38 || got > 0.39 {
		t.Fatalf("small sample: got %.3f, want ≈ 0.381", got)
	}
	// On a large sample the smoothing barely matters: 40 / 192 = 0.208.
	if got := upsellSmoothedConfidence(40, 192, 335, 3000); got < 0.20 || got > 0.21 {
		t.Fatalf("large sample: got %.3f, want ≈ 0.203", got)
	}
}

func TestUpsellPairPatterns_Rules(t *testing.T) {
	cases := []struct {
		name                       string
		countAB, countA, countB, n int
		wantAB, wantBA             bool
	}{
		// Pizza Poulet (192 orders) with Orangina (335), 40 together, 3 000
		// orders: lift 1.87, P(Orangina | Pizza) ≈ 20 %, P(Pizza | Orangina) ≈ 12 %.
		{"both directions", 40, 192, 335, 3000, true, true},
		// Same pair ordered together only 7 times: under the minimum of 8.
		{"too few together", 7, 20, 30, 3000, false, false},
		// A drink in almost every order: lift 0.93, no specific link.
		{"no specific link", 180, 200, 2900, 3000, false, false},
		// Frequent source, rare target: P(B | A) ≈ 3 %, P(A | B) ≈ 60 %.
		{"one direction only", 12, 400, 20, 3000, false, true},
	}
	for _, c := range cases {
		ab, ba := upsellPairPatterns("a", "b", c.countAB, c.countA, c.countB, c.n)
		if (ab != nil) != c.wantAB || (ba != nil) != c.wantBA {
			t.Errorf("%s: got A→B=%v B→A=%v, want %v %v", c.name, ab != nil, ba != nil, c.wantAB, c.wantBA)
		}
		if ab != nil && ab.ProductID != "b" {
			t.Errorf("%s: A→B suggests %q, want b", c.name, ab.ProductID)
		}
		if ba != nil && ba.ProductID != "a" {
			t.Errorf("%s: B→A suggests %q, want a", c.name, ba.ProductID)
		}
	}
}

func TestSortUpsellPatterns_ByConfidenceThenID(t *testing.T) {
	entries := []upsellModule.PatternEntry{
		{ProductID: "rare", Lift: 8.6, Confidence: 0.11},
		{ProductID: "b", Lift: 1.4, Confidence: 0.30},
		{ProductID: "a", Lift: 1.3, Confidence: 0.30},
		{ProductID: "c", Lift: 2.0, Confidence: 0.20},
	}
	got := sortUpsellPatterns(entries, 3)
	if len(got) != 3 || got[0].ProductID != "a" || got[1].ProductID != "b" || got[2].ProductID != "c" {
		t.Fatalf("got %+v, want [a b c]: highest lift no longer wins", got)
	}
}
