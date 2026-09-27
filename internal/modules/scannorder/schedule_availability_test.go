package scannorder

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/models"
)

// fakeScheduleAvailability renvoie un ensemble fixe de produits hors créneau.
type fakeScheduleAvailability struct {
	unavailable map[string]string
}

func (f fakeScheduleAvailability) GetUnavailableProductsAt(context.Context, string, time.Time) (map[string]string, error) {
	return f.unavailable, nil
}

func TestWithoutOutOfScheduleProducts(t *testing.T) {
	products := []models.ProductEntry{{ProductID: "1"}, {ProductID: "2"}, {ProductID: "3"}}

	got := withoutOutOfScheduleProducts(products, map[string]string{"2": "Burger"})

	if len(got) != 2 || got[0].ProductID != "1" || got[1].ProductID != "3" {
		t.Fatalf("withoutOutOfScheduleProducts = %+v, want products 1 and 3", got)
	}
	if got := withoutOutOfScheduleProducts(products, nil); len(got) != 3 {
		t.Fatalf("no restriction must keep every product, got %+v", got)
	}
}

func TestOutOfScheduleCartProducts(t *testing.T) {
	svc := &Service{availabilities: fakeScheduleAvailability{unavailable: map[string]string{"12": "Croissant"}}}
	order := &models.OrderRequest{Products: []models.OrderProductPayload{
		{ProductID: "12", Quantity: 1},
		{ProductID: "7", Quantity: 1},
		{ProductID: "12", Quantity: 2}, // même produit en deux lignes : un seul signalement
	}}

	got, err := svc.outOfScheduleCartProducts(context.Background(), "42", order)
	if err != nil {
		t.Fatalf("outOfScheduleCartProducts: %v", err)
	}
	if len(got) != 1 || got[0].ProductID != 12 || got[0].Name != "Croissant" || got[0].Status != models.UnavailableStatusOutOfSchedule {
		t.Fatalf("outOfScheduleCartProducts = %+v, want one out_of_schedule Croissant", got)
	}

	if got, err := svc.outOfScheduleCartProducts(context.Background(), "42", nil); err != nil || len(got) != 0 {
		t.Fatalf("nil order must yield nothing, got %+v, %v", got, err)
	}
}
