package analytics

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewOrderFilter_EmptyMeansNoRestriction(t *testing.T) {
	f, ok := NewOrderFilter(nil, nil)
	if !ok {
		t.Fatal("empty filter must be valid")
	}
	if !f.IsZero() {
		t.Fatalf("expected zero filter, got %+v", f)
	}
}

// Every box ticked must be byte-for-byte the unfiltered query — including
// rows whose order_source/order_type is NULL, which `= ANY(...)` would drop.
func TestNewOrderFilter_CompleteSelectionNormalizesToZero(t *testing.T) {
	f, ok := NewOrderFilter(append([]string(nil), OrderSources...), []string{"DELIVERY", "IN", "TAKE_AWAY"})
	if !ok {
		t.Fatal("complete selection must be valid")
	}
	if !f.IsZero() {
		t.Fatalf("expected zero filter, got %+v", f)
	}
	if pred, args := f.predicate(); pred != "" || args != nil {
		t.Fatalf("expected no predicate, got %q %v", pred, args)
	}
}

func TestNewOrderFilter_UnknownValueRejected(t *testing.T) {
	if _, ok := NewOrderFilter([]string{"UBER"}, nil); ok {
		t.Fatal("unknown source must be rejected")
	}
	if _, ok := NewOrderFilter(nil, []string{"dine_in"}); ok {
		t.Fatal("unknown order type must be rejected")
	}
}

func TestNewOrderFilter_PartialSelectionIsDedupedAndOrdered(t *testing.T) {
	f, ok := NewOrderFilter([]string{"DELIVEROO", "KIOSK", "KIOSK"}, []string{"DELIVERY"})
	if !ok {
		t.Fatal("partial selection must be valid")
	}
	if want := []string{OrderSourceKiosk, OrderSourceDeliveroo}; !reflect.DeepEqual(f.Sources, want) {
		t.Fatalf("sources: got %v want %v", f.Sources, want)
	}
	if want := []string{"DELIVERY"}; !reflect.DeepEqual(f.OrderTypes, want) {
		t.Fatalf("order types: got %v want %v", f.OrderTypes, want)
	}
}

func TestOrderFilter_PredicateArgsMatchPlaceholders(t *testing.T) {
	cases := []OrderFilter{
		{Sources: []string{OrderSourcePOS}},
		{OrderTypes: []string{"IN"}},
		{Sources: []string{OrderSourcePOS}, OrderTypes: []string{"IN"}},
	}
	for _, f := range cases {
		pred, args := f.predicate()
		if got := strings.Count(pred, "?"); got != len(args) {
			t.Fatalf("%+v: %d placeholders for %d args (%q)", f, got, len(args), pred)
		}
	}
}

func TestRepository_ApplyOrderFilter(t *testing.T) {
	where, args := AnalyticsOrdersScope([]string{"212"}, time.Time{}, time.Time{})

	base := &Repository{}
	gotWhere, gotArgs := base.applyOrderFilter(where, args)
	if gotWhere != where || len(gotArgs) != len(args) {
		t.Fatal("zero filter must leave the scope untouched")
	}

	filtered := base.WithOrderFilter(OrderFilter{Sources: []string{OrderSourceKiosk}, OrderTypes: []string{"TAKE_AWAY"}})
	if !base.orderFilter.IsZero() {
		t.Fatal("WithOrderFilter must not mutate the receiver")
	}
	gotWhere, gotArgs = filtered.applyOrderFilter(where, args)
	if !strings.HasPrefix(gotWhere, where) || !strings.Contains(gotWhere, "o.order_source = ANY(?)") || !strings.Contains(gotWhere, "o.order_type = ANY(?)") {
		t.Fatalf("unexpected where: %q", gotWhere)
	}
	if strings.Count(gotWhere, "?") != len(gotArgs) {
		t.Fatalf("%d placeholders for %d args", strings.Count(gotWhere, "?"), len(gotArgs))
	}
	if len(args) != 3 {
		t.Fatal("applyOrderFilter must not grow the caller's args slice")
	}
}

func TestOrderFilter_CacheKeySuffix(t *testing.T) {
	if s := (OrderFilter{}).cacheKeySuffix(); s != "" {
		t.Fatalf("zero filter must keep existing cache keys, got %q", s)
	}
	a := OrderFilter{Sources: []string{OrderSourceKiosk, OrderSourcePOS}}.cacheKeySuffix()
	b := OrderFilter{Sources: []string{OrderSourcePOS, OrderSourceKiosk}}.cacheKeySuffix()
	if a != b {
		t.Fatalf("suffix must not depend on order: %q vs %q", a, b)
	}
	c := OrderFilter{OrderTypes: []string{OrderSourceKiosk, OrderSourcePOS}}.cacheKeySuffix()
	if a == c {
		t.Fatal("sources and order types must not collide in the cache key")
	}
}

func TestOrderFilter_UpsellSuggestionChannels(t *testing.T) {
	if got := (OrderFilter{}).upsellSuggestionChannels(); got != nil {
		t.Fatalf("unrestricted sources must not filter suggestions, got %v", got)
	}
	got := OrderFilter{Sources: []string{OrderSourcePOS, OrderSourceScanNOrder, OrderSourceKiosk, OrderSourceUberEats}}.upsellSuggestionChannels()
	if want := []string{"POS", "SNO", "KIOSK"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// Marketplaces never show our upsell UI: an explicit, empty (non-nil)
	// list, i.e. zero suggestions — not "no filter".
	got = OrderFilter{Sources: []string{OrderSourceDeliveroo}}.upsellSuggestionChannels()
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil list, got %#v", got)
	}
}
