package kiosk

import (
	"testing"
	"welloresto-api/internal/models"
)

func TestCleanProductPricesForKiosk_TakeAwayUsesAlternatePrice(t *testing.T) {
	takeAwayPrice := int64(900)
	p := &models.ProductEntry{Price: 1000, PriceTakeAway: &takeAwayPrice}

	cleanProductPricesForKiosk(p, models.OrderTypeTakeAway)

	if p.Price != 900 {
		t.Fatalf("Price = %d, want 900 (PriceTakeAway)", p.Price)
	}
}

func TestCleanProductPricesForKiosk_TakeAwayWithoutAlternatePriceKeepsBase(t *testing.T) {
	p := &models.ProductEntry{Price: 1000, PriceTakeAway: nil}

	cleanProductPricesForKiosk(p, models.OrderTypeTakeAway)

	if p.Price != 1000 {
		t.Fatalf("Price = %d, want 1000 (base price kept, no panic on nil PriceTakeAway)", p.Price)
	}
}

func TestCleanProductPricesForKiosk_DineInKeepsBasePrice(t *testing.T) {
	takeAwayPrice := int64(900)
	p := &models.ProductEntry{Price: 1000, PriceTakeAway: &takeAwayPrice}

	cleanProductPricesForKiosk(p, models.OrderTypeIn)

	if p.Price != 1000 {
		t.Fatalf("Price = %d, want 1000 (DINE_IN/IN must not use PriceTakeAway)", p.Price)
	}
}

func TestKioskFulfillmentToOrderType_InvalidRejected(t *testing.T) {
	if _, err := kioskFulfillmentToOrderType("DELIVERY"); err == nil {
		t.Fatal("expected an error for DELIVERY, which does not exist on Kiosk")
	}
	if _, err := kioskFulfillmentToOrderType(""); err == nil {
		t.Fatal("expected an error for an empty fulfillment type")
	}
}
