package analytics

import "testing"

func TestOptionTypesFilter(t *testing.T) {
	t.Run("empty request defaults to all three, in canonical order", func(t *testing.T) {
		got, ok := optionTypesFilter(nil)
		if !ok {
			t.Fatalf("expected ok=true for empty request")
		}
		want := []string{OptionTypePaid, OptionTypeFree, OptionTypeRemoved}
		if len(got) != len(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("expected %v, got %v", want, got)
			}
		}
	})

	t.Run("a valid subset is preserved, deduped, canonical order", func(t *testing.T) {
		got, ok := optionTypesFilter([]string{"removed", "paid", "removed"})
		if !ok {
			t.Fatalf("expected ok=true")
		}
		want := []string{OptionTypePaid, OptionTypeRemoved}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("an unknown value is rejected, not silently dropped", func(t *testing.T) {
		_, ok := optionTypesFilter([]string{"paid", "mobile"})
		if ok {
			t.Fatalf("expected ok=false for an unrecognized option type")
		}
	})
}

func TestBasketImpactCents(t *testing.T) {
	t.Run("normal case: with-average minus without-average", func(t *testing.T) {
		share := OptionBasketShare{OrderCount: 4, OrderPriceSum: 5600} // avg 1400
		scope := RevenueTotals{OrderCount: 10, TotalTTCCents: 11600}   // others: 6 orders, 6000 -> avg 1000
		got := basketImpactCents(share, scope)
		if got == nil || *got != 400 {
			t.Fatalf("expected +400, got %v", got)
		}
	})

	t.Run("entity present in every scope order: nil, never a division by zero", func(t *testing.T) {
		share := OptionBasketShare{OrderCount: 10, OrderPriceSum: 11600}
		scope := RevenueTotals{OrderCount: 10, TotalTTCCents: 11600}
		if got := basketImpactCents(share, scope); got != nil {
			t.Fatalf("expected nil (no 'without' side to compare against), got %v", *got)
		}
	})

	t.Run("entity with zero orders: nil, never a division by zero", func(t *testing.T) {
		share := OptionBasketShare{}
		scope := RevenueTotals{OrderCount: 10, TotalTTCCents: 11600}
		if got := basketImpactCents(share, scope); got != nil {
			t.Fatalf("expected nil, got %v", *got)
		}
	})
}

func TestOptionsSortColumn(t *testing.T) {
	cases := map[string]string{
		OptionsSortRevenue: "revenue_ttc_cents",
		OptionsSortMargin:  "margin_cents",
		OptionsSortQuantity: "quantity_sold",
		"":                  "quantity_sold",
		"garbage":           "quantity_sold",
	}
	for in, want := range cases {
		if got := optionsSortColumn(in); got != want {
			t.Fatalf("optionsSortColumn(%q) = %q, want %q", in, got, want)
		}
	}
}
