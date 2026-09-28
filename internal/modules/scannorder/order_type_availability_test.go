package scannorder

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

func TestOrderTypeUnavailableCartProducts(t *testing.T) {
	cases := []struct {
		orderType  string
		wantColumn string
	}{
		{"DELIVERY", "available_delivery"},
		{"IN", "available_in"},
		{"TAKE_AWAY", "available_take_away"},
		{"", "available_take_away"}, // défaut (anciennes versions)
	}
	for _, tc := range cases {
		t.Run(tc.orderType, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()
			mock.ExpectQuery(`COALESCE\(p\.` + tc.wantColumn + `, TRUE\) = FALSE`).
				WillReturnRows(sqlmock.NewRows([]string{"product_id", "name"}).AddRow("7", "Glace"))

			svc := &Service{repo: NewRepository(db)}
			order := &models.OrderRequest{OrderType: tc.orderType, Products: []models.OrderProductPayload{
				{ProductID: "12", Quantity: 1}, // déjà signalé hors créneau
				{ProductID: "7", Quantity: 1},
				{ProductID: "7", Quantity: 2}, // même produit en deux lignes : un seul signalement
			}}
			already := []models.UnavailableProductInfo{{ProductID: 12, Status: models.UnavailableStatusOutOfSchedule}}

			got, err := svc.orderTypeUnavailableCartProducts(context.Background(), "42", order, already)
			if err != nil {
				t.Fatalf("orderTypeUnavailableCartProducts: %v", err)
			}
			if len(got) != 1 || got[0].ProductID != 7 || got[0].Name != "Glace" || got[0].Status != models.UnavailableStatusNotAvailableForOrderType {
				t.Fatalf("orderTypeUnavailableCartProducts = %+v, want one not_available_for_order_type Glace", got)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("query: %v", err)
			}
		})
	}
}

func TestOrderTypeUnavailableCartProducts_EmptyOrder(t *testing.T) {
	svc := &Service{}
	if got, err := svc.orderTypeUnavailableCartProducts(context.Background(), "42", nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("nil order must yield nothing, got %+v, %v", got, err)
	}
}

func TestCleanProductPricesForSNO_NilModePriceKeepsBasePrice(t *testing.T) {
	svc := &Service{}
	p := models.ProductEntry{Price: 1000}
	svc.cleanProductPricesForSNO(&p, models.OrderTypeTakeAway)
	if p.Price != 1000 {
		t.Fatalf("TAKE_AWAY with NULL price_take_away: price = %d, want base 1000", p.Price)
	}

	takeAway := int64(900)
	p = models.ProductEntry{Price: 1000, PriceTakeAway: &takeAway}
	svc.cleanProductPricesForSNO(&p, models.OrderTypeTakeAway)
	if p.Price != 900 {
		t.Fatalf("TAKE_AWAY price = %d, want 900", p.Price)
	}
}
