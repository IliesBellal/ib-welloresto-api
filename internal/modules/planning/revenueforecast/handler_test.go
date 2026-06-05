package revenueforecast

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
)

func TestUpsertRevenueForecastsIgnoresPayloadMerchantID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	h := NewHandler(svc)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_revenue_forecasts (id, merchant_id, forecast_date, amount_ht_cents)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE amount_ht_cents = VALUES(amount_ht_cents)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_ctx", "2026-06-05", int64(12345)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	body := []byte(`{"merchant_id":"merchant_payload","forecasts":[{"date":"2026-06-05","amount_cents":12345}]}`)
	req := httptest.NewRequest(http.MethodPut, "/planning/revenue-forecast", bytes.NewReader(body))
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{MerchantID: "merchant_ctx", UserID: "user_1", MerchantRightsID: "mr_1"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.UpsertRevenueForecasts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
