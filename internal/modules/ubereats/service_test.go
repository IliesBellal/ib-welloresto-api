package ubereats

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// TestUberEatsBYOCStatusUpdate_UsesBrandOrderID is a regression test for audit
// item #2: UberEatsBYOCStatusUpdate used to call Uber's
// /restaurantdelivery/status endpoint with our *internal* order id instead of
// resolving Uber's own order id (brand_order_id) first, per
// developer.uber.com/docs/eats/references/api/v1/post-eats-orders-orderid-restaurantdelivery-status
// ("order_id" path parameter = Uber's order UUID, not a caller-supplied one).
func TestUberEatsBYOCStatusUpdate_UsesBrandOrderID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	const internalOrderID = "internal-42"
	const brandOrderID = "uber-brand-xyz"

	mock.ExpectQuery(regexp.QuoteMeta("SELECT access_token, expires_at FROM external_tokens WHERE token_type = ?")).
		WithArgs("ubereats").
		WillReturnRows(sqlmock.NewRows([]string{"access_token", "expires_at"}).
			AddRow("test-token", time.Now().UTC().AddDate(0, 0, 30)))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT o.brand_order_id, o.creation_date")).
		WithArgs(internalOrderID).
		WillReturnRows(sqlmock.NewRows([]string{"brand_order_id", "creation_date"}).
			AddRow(brandOrderID, time.Now()))

	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := NewUberEatsService(db, ConfigUberEats{BaseURL: server.URL, TokenType: "ubereats"}, nil)

	if err := svc.UberEatsBYOCStatusUpdate(context.Background(), "merchant-1", internalOrderID, StatusBYOCStarted); err != nil {
		t.Fatalf("UberEatsBYOCStatusUpdate failed: %v", err)
	}

	wantPath := "/v1/eats/orders/" + brandOrderID + "/restaurantdelivery/status"
	if gotPath != wantPath {
		t.Fatalf("expected request to %q (Uber's brand_order_id), got %q — internal order id leaked into the URL?", wantPath, gotPath)
	}
	if string(gotBody) == "" {
		t.Fatal("expected a non-empty request body")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
