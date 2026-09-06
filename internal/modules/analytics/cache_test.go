package analytics

import "testing"

// TestBuildCacheKey_MerchantOrderIndependentButScopeSensitive is PROMPT 23
// Phase 4's mandatory test: the cache key must depend on the SET of
// requested establishments, not the order they were requested in, and must
// change whenever that set changes size. Getting this wrong either serves
// one merchant's cached numbers to a caller who selected a different set
// (order-sensitivity: [212,228] and [228,212] hashing differently would
// double the cache's memory for no reason, but is not itself a leak) or —
// the actually dangerous direction — collapses two different scopes onto
// the same key (a caller adding a merchant to their selection getting served
// the smaller scope's stale answer). buildCacheKey already sorts
// merchantIDs before hashing (see its doc comment) — this test is what
// would catch a regression removing that sort.
func TestBuildCacheKey_MerchantOrderIndependentButScopeSensitive(t *testing.T) {
	k1 := buildCacheKey("revenue", []string{"212", "228"}, "2026-01-01", "2026-01-31", GroupByNone, true)
	k2 := buildCacheKey("revenue", []string{"228", "212"}, "2026-01-01", "2026-01-31", GroupByNone, true)
	if k1 != k2 {
		t.Fatalf("expected [212,228] and [228,212] to produce the same cache key, got %q vs %q", k1, k2)
	}

	k3 := buildCacheKey("revenue", []string{"212"}, "2026-01-01", "2026-01-31", GroupByNone, true)
	if k1 == k3 {
		t.Fatalf("expected [212] and [212,228] to produce different cache keys, both got %q", k1)
	}
}

// TestBuildProductsCacheKey_MerchantOrderIndependentButScopeSensitive covers
// the paginated-tab cache key builder (buildProductsCacheKey) — a separate
// function from buildCacheKey, so a fix to one would not automatically cover
// the other.
func TestBuildProductsCacheKey_MerchantOrderIndependentButScopeSensitive(t *testing.T) {
	k1 := buildProductsCacheKey([]string{"212", "228"}, "2026-01-01", "2026-01-31", "", ProductsSortQuantity, "desc", 1, ProductsDefaultPageSize)
	k2 := buildProductsCacheKey([]string{"228", "212"}, "2026-01-01", "2026-01-31", "", ProductsSortQuantity, "desc", 1, ProductsDefaultPageSize)
	if k1 != k2 {
		t.Fatalf("expected [212,228] and [228,212] to produce the same cache key, got %q vs %q", k1, k2)
	}

	k3 := buildProductsCacheKey([]string{"212"}, "2026-01-01", "2026-01-31", "", ProductsSortQuantity, "desc", 1, ProductsDefaultPageSize)
	if k1 == k3 {
		t.Fatalf("expected [212] and [212,228] to produce different cache keys, both got %q", k1)
	}
}

// TestBuildClientsCacheKey_MerchantOrderIndependentButScopeSensitive covers
// the channels-keyed builder shared by Clients/Upsell.
func TestBuildClientsCacheKey_MerchantOrderIndependentButScopeSensitive(t *testing.T) {
	k1 := buildClientsCacheKey("clients", []string{"212", "228"}, "2026-01-01", "2026-01-31", nil)
	k2 := buildClientsCacheKey("clients", []string{"228", "212"}, "2026-01-01", "2026-01-31", nil)
	if k1 != k2 {
		t.Fatalf("expected [212,228] and [228,212] to produce the same cache key, got %q vs %q", k1, k2)
	}

	k3 := buildClientsCacheKey("clients", []string{"212"}, "2026-01-01", "2026-01-31", nil)
	if k1 == k3 {
		t.Fatalf("expected [212] and [212,228] to produce different cache keys, both got %q", k1)
	}
}

// TestBuildOptionsCacheKey_MerchantOrderIndependentButScopeSensitive and
// TestBuildDiscountsCacheKey_MerchantOrderIndependentButScopeSensitive round
// out coverage: every one of this package's 5 cache-key builders is now
// tested for this property, not just buildCacheKey.
func TestBuildOptionsCacheKey_MerchantOrderIndependentButScopeSensitive(t *testing.T) {
	k1 := buildOptionsCacheKey([]string{"212", "228"}, "2026-01-01", "2026-01-31", nil, OptionsSortQuantity, "desc", 1, OptionsDefaultPageSize)
	k2 := buildOptionsCacheKey([]string{"228", "212"}, "2026-01-01", "2026-01-31", nil, OptionsSortQuantity, "desc", 1, OptionsDefaultPageSize)
	if k1 != k2 {
		t.Fatalf("expected [212,228] and [228,212] to produce the same cache key, got %q vs %q", k1, k2)
	}

	k3 := buildOptionsCacheKey([]string{"212"}, "2026-01-01", "2026-01-31", nil, OptionsSortQuantity, "desc", 1, OptionsDefaultPageSize)
	if k1 == k3 {
		t.Fatalf("expected [212] and [212,228] to produce different cache keys, both got %q", k1)
	}
}

func TestBuildDiscountsCacheKey_MerchantOrderIndependentButScopeSensitive(t *testing.T) {
	k1 := buildDiscountsCacheKey([]string{"212", "228"}, "2026-01-01", "2026-01-31", nil, "discount_amount", "desc", 1, 50)
	k2 := buildDiscountsCacheKey([]string{"228", "212"}, "2026-01-01", "2026-01-31", nil, "discount_amount", "desc", 1, 50)
	if k1 != k2 {
		t.Fatalf("expected [212,228] and [228,212] to produce the same cache key, got %q vs %q", k1, k2)
	}

	k3 := buildDiscountsCacheKey([]string{"212"}, "2026-01-01", "2026-01-31", nil, "discount_amount", "desc", 1, 50)
	if k1 == k3 {
		t.Fatalf("expected [212] and [212,228] to produce different cache keys, both got %q", k1)
	}
}
