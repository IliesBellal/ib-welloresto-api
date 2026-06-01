package menu

import (
	"encoding/json"
	"testing"
)

func TestUpdateComponentPayloadUnmarshalAcceptsDecimalPurchaseCostQty(t *testing.T) {
	var payload UpdateComponentPayload

	if err := json.Unmarshal([]byte(`{"purchase_cost_qty":2.85}`), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.PurchaseCostQty == nil {
		t.Fatal("PurchaseCostQty = nil, want 2.85")
	}
	if got, want := *payload.PurchaseCostQty, 2.85; got != want {
		t.Fatalf("PurchaseCostQty = %v, want %v", got, want)
	}
}

func TestUpdateComponentPayloadUnmarshalAcceptsIntegerPurchaseCostQty(t *testing.T) {
	var payload UpdateComponentPayload

	if err := json.Unmarshal([]byte(`{"purchase_cost_qty":3}`), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.PurchaseCostQty == nil {
		t.Fatal("PurchaseCostQty = nil, want 3")
	}
	if got, want := *payload.PurchaseCostQty, 3.0; got != want {
		t.Fatalf("PurchaseCostQty = %v, want %v", got, want)
	}
}
