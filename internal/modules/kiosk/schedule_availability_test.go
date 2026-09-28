package kiosk

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

// fakeScheduleAvailability renvoie un ensemble fixe de produits hors créneau.
type fakeScheduleAvailability struct {
	unavailable map[string]string
}

func (f fakeScheduleAvailability) GetUnavailableProductsAt(context.Context, string, time.Time) (map[string]string, error) {
	return f.unavailable, nil
}

func kioskProductIDs(products []KioskProduct) []string {
	ids := make([]string, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestFlattenKioskProducts_RemovesOutOfScheduleProductsAndGroups(t *testing.T) {
	isGroup := true
	products := []models.ProductEntry{
		{ProductID: "1", Name: "Croissant"},
		{ProductID: "2", Name: "Burger"},
		{ProductID: "10", Name: "Formules matin", IsProductGroup: &isGroup, SubProducts: []models.ProductEntry{
			{ProductID: "11", Name: "Formule café"},
		}},
		{ProductID: "20", Name: "Boissons", IsProductGroup: &isGroup, SubProducts: []models.ProductEntry{
			{ProductID: "21", Name: "Jus"},
			{ProductID: "22", Name: "Smoothie"},
		}},
	}
	kioskAvailable := map[string]bool{"1": true, "2": true, "11": true, "21": true, "22": true}
	unavailable := map[string]string{
		"1":  "Croissant",      // produit simple hors créneau
		"10": "Formules matin", // groupe hors créneau : ses sous-produits aussi
		"22": "Smoothie",       // sous-produit hors créneau
	}

	got := kioskProductIDs(flattenKioskProducts(products, kioskAvailable, unavailable, ""))

	want := []string{"2", "21"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("flattenKioskProducts = %v, want %v", got, want)
	}
}

func TestFlattenKioskProducts_NoScheduleRestrictionKeepsKioskFilter(t *testing.T) {
	products := []models.ProductEntry{{ProductID: "1"}, {ProductID: "2"}}

	got := kioskProductIDs(flattenKioskProducts(products, map[string]bool{"1": true}, nil, ""))

	if len(got) != 1 || got[0] != "1" {
		t.Fatalf("flattenKioskProducts = %v, want [1]", got)
	}
}

func TestValidateKioskProductAvailability_RejectsOutOfScheduleProduct(t *testing.T) {
	cases := []struct {
		name        string
		unavailable map[string]string
		wantErr     error
	}{
		{"dans le créneau", nil, nil},
		{"hors créneau", map[string]string{"2": "Burger"}, models.ErrKioskProductUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()
			mock.ExpectQuery(`is_available_on_kiosk = TRUE`).
				WillReturnRows(sqlmock.NewRows([]string{"product_id"}).AddRow("1").AddRow("2"))

			svc := &Service{repo: NewRepository(db), availabilities: fakeScheduleAvailability{unavailable: tc.unavailable}}
			err = svc.validateKioskProductAvailability(context.Background(), "42", models.OrderTypeIn, []models.OrderProductPayload{
				{ProductID: "1", Quantity: 1},
				{ProductID: "2", Quantity: 2},
			})

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateKioskProductAvailability err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
