package kiosk

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

func TestNormalizeKioskOrderType(t *testing.T) {
	cases := map[string]string{
		"":          models.OrderTypeTakeAway, // anciennes versions : pas de mode
		"IN":        models.OrderTypeIn,
		"in":        models.OrderTypeIn,
		"DINE_IN":   models.OrderTypeIn,
		"TAKE_AWAY": models.OrderTypeTakeAway,
		"DELIVERY":  models.OrderTypeTakeAway, // inexistant sur borne
		"garbage":   models.OrderTypeTakeAway,
	}
	for in, want := range cases {
		if got := normalizeKioskOrderType(in); got != want {
			t.Errorf("normalizeKioskOrderType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlattenKioskProducts_FiltersByOrderType(t *testing.T) {
	yes, no := true, false
	isGroup := true
	products := []models.ProductEntry{
		{ProductID: "1", Name: "Sur place seulement", AvailableIn: &yes, AvailableTakeAway: &no},
		{ProductID: "2", Name: "Emporter seulement", AvailableIn: &no, AvailableTakeAway: &yes},
		{ProductID: "3", Name: "Flags NULL"},
		{ProductID: "10", Name: "Groupe", IsProductGroup: &isGroup, SubProducts: []models.ProductEntry{
			{ProductID: "11", Name: "Sous-produit sur place", AvailableIn: &yes, AvailableTakeAway: &no},
			{ProductID: "12", Name: "Sous-produit partout"},
		}},
	}
	kioskAvailable := map[string]bool{"1": true, "2": true, "3": true, "11": true, "12": true}

	cases := []struct {
		orderType string
		want      []string
	}{
		{models.OrderTypeIn, []string{"1", "3", "11", "12"}},
		{models.OrderTypeTakeAway, []string{"2", "3", "12"}},
	}
	for _, tc := range cases {
		got := kioskProductIDs(flattenKioskProducts(products, kioskAvailable, nil, tc.orderType))
		if len(got) != len(tc.want) {
			t.Fatalf("%s: flattenKioskProducts = %v, want %v", tc.orderType, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: flattenKioskProducts = %v, want %v", tc.orderType, got, tc.want)
			}
		}
	}
}

func TestValidateKioskProductAvailability_UsesOrderTypeColumn(t *testing.T) {
	cases := []struct {
		orderType  string
		wantColumn string
	}{
		{"IN", "available_in"},
		{"TAKE_AWAY", "available_take_away"},
		{"", "available_take_away"}, // défaut
	}
	for _, tc := range cases {
		t.Run(tc.orderType, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()
			// Le produit 2 est indisponible pour le mode : absent du résultat.
			mock.ExpectQuery(`is_available_on_kiosk = TRUE AND COALESCE\(` + tc.wantColumn + `, TRUE\) = TRUE`).
				WillReturnRows(sqlmock.NewRows([]string{"product_id"}).AddRow("1"))

			svc := &Service{repo: NewRepository(db), availabilities: fakeScheduleAvailability{}}
			err = svc.validateKioskProductAvailability(context.Background(), "42", tc.orderType, []models.OrderProductPayload{
				{ProductID: "1", Quantity: 1},
				{ProductID: "2", Quantity: 1},
			})
			if !errors.Is(err, models.ErrKioskProductUnavailable) {
				t.Fatalf("err = %v, want ErrKioskProductUnavailable", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("query: %v", err)
			}
		})
	}
}
