package models

import "testing"

func TestNormalizeOrderType(t *testing.T) {
	cases := map[string]string{
		"":            OrderTypeTakeAway,
		"  delivery ": OrderTypeDelivery,
		"IN":          OrderTypeIn,
		"TAKE_AWAY":   OrderTypeTakeAway,
		"unknown":     OrderTypeTakeAway,
	}
	for in, want := range cases {
		if got := NormalizeOrderType(in); got != want {
			t.Errorf("NormalizeOrderType(%q) = %q, want %q", in, got, want)
		}
	}
	// Liste restreinte : DELIVERY hors liste → défaut.
	if got := NormalizeOrderType("DELIVERY", OrderTypeIn, OrderTypeTakeAway); got != OrderTypeTakeAway {
		t.Errorf("restricted NormalizeOrderType(DELIVERY) = %q, want TAKE_AWAY", got)
	}
}

func TestIsAvailableForOrderType(t *testing.T) {
	yes, no := true, false
	p := ProductEntry{AvailableIn: &yes, AvailableTakeAway: &no}

	if !p.IsAvailableForOrderType(OrderTypeIn) {
		t.Error("available_in=true must be available IN")
	}
	if p.IsAvailableForOrderType(OrderTypeTakeAway) {
		t.Error("available_take_away=false must not be available TAKE_AWAY")
	}
	if !p.IsAvailableForOrderType(OrderTypeDelivery) {
		t.Error("available_delivery NULL must count as available")
	}
}

func TestOrderTypeAvailabilityColumn(t *testing.T) {
	cases := map[string]string{
		OrderTypeIn:       "available_in",
		OrderTypeTakeAway: "available_take_away",
		OrderTypeDelivery: "available_delivery",
		"":                "available_take_away",
	}
	for in, want := range cases {
		if got := OrderTypeAvailabilityColumn(in); got != want {
			t.Errorf("OrderTypeAvailabilityColumn(%q) = %q, want %q", in, got, want)
		}
	}
}
