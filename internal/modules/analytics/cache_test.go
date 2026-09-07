package analytics

import (
	"testing"
	"time"

	"welloresto-api/internal/models"
)

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

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

// TestAccessibleMerchantsCacheTTL_MatchesUserCacheTTL is PROMPT 25 Phase 0's
// security guard: this cache answers who can see what, so it must never
// outlive auth's own UserLoginRow cache (models.UserCacheTTL) — a revoked
// right must not stay live on the analytics page any longer than it stays
// live on the rest of the app. accessibleMerchantsCacheTTL is declared as
// `= models.UserCacheTTL` (service.go) specifically so the two cannot drift
// apart by editing one and forgetting the other; this test is what would
// catch a future regression that replaces that reference with a hardcoded
// duplicate value.
func TestAccessibleMerchantsCacheTTL_MatchesUserCacheTTL(t *testing.T) {
	if accessibleMerchantsCacheTTL != models.UserCacheTTL {
		t.Fatalf("accessibleMerchantsCacheTTL (%v) must equal models.UserCacheTTL (%v) — a revoked right must not outlive the user cache it depends on",
			accessibleMerchantsCacheTTL, models.UserCacheTTL)
	}
}

// TestAccessibleMerchantsCacheKey_DifferentTokensDifferentKeys — this cache
// is keyed by the raw bearer token (same convention as
// models.UserCachePrefix+token), so two different tokens must never collide
// on the same key, whatever establishment scope they happen to resolve to.
func TestAccessibleMerchantsCacheKey_DifferentTokensDifferentKeys(t *testing.T) {
	k1 := accessibleMerchantsCacheKey("token-a")
	k2 := accessibleMerchantsCacheKey("token-b")
	if k1 == k2 {
		t.Fatalf("expected different tokens to produce different cache keys, both got %q", k1)
	}
	if accessibleMerchantsCacheKey("token-a") != k1 {
		t.Fatalf("expected the same token to produce a stable cache key")
	}
}

// TestCustomersLifetimeStatsCacheKey_MerchantOrderIndependentButScopeSensitive
// covers the key shared by GetClients/GetClientsTop's now-cached call to
// Repository.GetCustomersLifetimeStats (PROMPT 25 Phase 0) — same property as
// this file's other builders, plus the period bound (startUTC/endUTC), which
// none of the others carry.
func TestCustomersLifetimeStatsCacheKey_MerchantOrderIndependentButScopeSensitive(t *testing.T) {
	start := mustParseRFC3339(t, "2026-01-01T00:00:00Z")
	end := mustParseRFC3339(t, "2026-02-01T00:00:00Z")

	k1 := customersLifetimeStatsCacheKey([]string{"212", "228"}, nil, start, end)
	k2 := customersLifetimeStatsCacheKey([]string{"228", "212"}, nil, start, end)
	if k1 != k2 {
		t.Fatalf("expected [212,228] and [228,212] to produce the same cache key, got %q vs %q", k1, k2)
	}

	k3 := customersLifetimeStatsCacheKey([]string{"212"}, nil, start, end)
	if k1 == k3 {
		t.Fatalf("expected [212] and [212,228] to produce different cache keys, both got %q", k1)
	}

	otherEnd := mustParseRFC3339(t, "2026-02-02T00:00:00Z")
	k4 := customersLifetimeStatsCacheKey([]string{"212", "228"}, nil, start, otherEnd)
	if k1 == k4 {
		t.Fatalf("expected different period bounds to produce different cache keys, both got %q", k1)
	}
}

// TestUpsellInstrumentationActiveCacheKey_MerchantOrderIndependentButScopeSensitive
// covers the key shared by GetUpsell/GetUpsellByStaff's now-cached call to
// Repository.GetUpsellInstrumentationActive (PROMPT 25 Phase 0).
func TestUpsellInstrumentationActiveCacheKey_MerchantOrderIndependentButScopeSensitive(t *testing.T) {
	k1 := upsellInstrumentationActiveCacheKey([]string{"212", "228"})
	k2 := upsellInstrumentationActiveCacheKey([]string{"228", "212"})
	if k1 != k2 {
		t.Fatalf("expected [212,228] and [228,212] to produce the same cache key, got %q vs %q", k1, k2)
	}

	k3 := upsellInstrumentationActiveCacheKey([]string{"212"})
	if k1 == k3 {
		t.Fatalf("expected [212] and [212,228] to produce different cache keys, both got %q", k1)
	}
}
