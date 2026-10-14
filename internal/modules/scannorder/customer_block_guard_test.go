package scannorder

import (
	"strings"
	"testing"
)

func TestOrderTypeChecksBlockedCustomer(t *testing.T) {
	tests := map[string]bool{
		"TAKE_AWAY": true,
		"DELIVERY":  true,
		"IN":        false,
		"":          false,
		"take_away": false,
	}
	for orderType, want := range tests {
		if got := orderTypeChecksBlockedCustomer(orderType); got != want {
			t.Errorf("orderTypeChecksBlockedCustomer(%q) = %v, want %v", orderType, got, want)
		}
	}
}

// Le statut ne doit pas contenir de mot qui trahisse le blocage : le front le
// reçoit tel quel (docs/CUSTOMER_BLOCKING.md).
func TestStatusOrderUnavailable_IsNeutral(t *testing.T) {
	for _, forbidden := range []string{"block", "ban", "customer", "refus"} {
		if strings.Contains(StatusOrderUnavailable, forbidden) {
			t.Errorf("StatusOrderUnavailable %q ne doit pas contenir %q", StatusOrderUnavailable, forbidden)
		}
	}
}
