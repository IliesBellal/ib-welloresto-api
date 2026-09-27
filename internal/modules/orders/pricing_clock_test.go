package orders

import "testing"

func TestPricingLocalTime(t *testing.T) {
	// req.Time est déjà en heure locale du merchant (ComputePricing) : date et
	// heure en sont extraites telles quelles, sans conversion de fuseau.
	got, err := pricingLocalTime("2026-09-27 00:30:05")
	if err != nil {
		t.Fatalf("pricingLocalTime: %v", err)
	}
	if got.Format("2006-01-02") != "2026-09-27" || got.Format("15:04:05") != "00:30:05" {
		t.Fatalf("pricingLocalTime = %s, want 2026-09-27 00:30:05 (pas de décalage UTC)", got)
	}

	if _, err := pricingLocalTime(""); err == nil {
		t.Fatal("empty req.Time must be rejected, not silently evaluated at 00:00")
	}
}
