package menu

import (
	"encoding/json"
	"testing"

	"welloresto-api/internal/models"
)

func TestListProductIntegrations(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }
	int64Ptr := func(v int64) *int64 { return &v }

	t.Run("publié sur Deliveroo avec prix spécifique, pas sur Uber Eats", func(t *testing.T) {
		got := listProductIntegrations(models.ProductEntry{
			SyncUberEats:   boolPtr(false),
			SyncDeliveroo:  boolPtr(true),
			PriceUberEats:  int64Ptr(1490),
			PriceDeliveroo: int64Ptr(1390),
		})
		if got.UberEats.Enabled {
			t.Errorf("uber_eats.enabled = true, want false")
		}
		if got.UberEats.PriceOverride == nil || *got.UberEats.PriceOverride != 1490 {
			t.Errorf("uber_eats.price_override = %v, want 1490", got.UberEats.PriceOverride)
		}
		if !got.Deliveroo.Enabled {
			t.Errorf("deliveroo.enabled = false, want true")
		}
		if got.Deliveroo.PriceOverride == nil || *got.Deliveroo.PriceOverride != 1390 {
			t.Errorf("deliveroo.price_override = %v, want 1390", got.Deliveroo.PriceOverride)
		}
	})

	t.Run("prix plateforme nul ou à 0 : pas de prix spécifique", func(t *testing.T) {
		got := listProductIntegrations(models.ProductEntry{
			SyncUberEats:   boolPtr(true),
			SyncDeliveroo:  boolPtr(true),
			PriceUberEats:  int64Ptr(0),
			PriceDeliveroo: nil,
		})
		if !got.UberEats.Enabled || !got.Deliveroo.Enabled {
			t.Errorf("enabled = %v/%v, want true/true", got.UberEats.Enabled, got.Deliveroo.Enabled)
		}
		if got.UberEats.PriceOverride != nil {
			t.Errorf("uber_eats.price_override = %v, want nil", *got.UberEats.PriceOverride)
		}
		if got.Deliveroo.PriceOverride != nil {
			t.Errorf("deliveroo.price_override = %v, want nil", *got.Deliveroo.PriceOverride)
		}
	})

	t.Run("cases sync non lues : non publié", func(t *testing.T) {
		got := listProductIntegrations(models.ProductEntry{})
		if got.UberEats.Enabled || got.Deliveroo.Enabled {
			t.Errorf("enabled = %v/%v, want false/false", got.UberEats.Enabled, got.Deliveroo.Enabled)
		}
	})

	t.Run("sérialisation attendue par le back-office", func(t *testing.T) {
		raw, err := json.Marshal(listProductIntegrations(models.ProductEntry{
			SyncDeliveroo:  boolPtr(true),
			PriceDeliveroo: int64Ptr(1290),
		}))
		if err != nil {
			t.Fatal(err)
		}
		want := `{"uber_eats":{"enabled":false},"deliveroo":{"enabled":true,"price_override":1290}}`
		if string(raw) != want {
			t.Errorf("json = %s, want %s", raw, want)
		}
	})
}
