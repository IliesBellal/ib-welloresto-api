package order_life_cycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
)

// TestSendInvoiceEmailHandler_BrevoFailure_Returns502WithCustomerLinked vérifie le contrat HTTP :
// si l'envoi Brevo échoue, le staff doit voir status 502 + customer_linked=true (le client a bien
// été enregistré/lié en base malgré l'échec d'envoi), avec le customer_id pour pouvoir réessayer.
func TestSendInvoiceEmailHandler_BrevoFailure_Returns502WithCustomerLinked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mailerSvc := &fakeMailerService{sendErr: errors.New("brevo timeout")}
	svc := newTestService(db, &fakeOrderFetcher{resp: validOrderResponse()}, &fakeAuditService{}, mailerSvc)
	h := NewOrdersLifeCycleHandler(svc, nil, nil)

	mock.ExpectBegin()
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "old@example.fr"))
	mock.ExpectExec("UPDATE customer").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(customerSelectColumns).
		WithArgs("cus_1", testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id", "customer_first_name", "customer_last_name", "customer_email"}).
			AddRow("cus_1", "Jean", "Dupont", "client@example.fr"))
	mock.ExpectExec("UPDATE orders").
		WithArgs("cus_1", testOrderID, testMerchantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	body := bytes.NewBufferString(`{"email":"client@example.fr","customer_id":"cus_1"}`)
	req := httptest.NewRequest(http.MethodPost, "/orders/"+testOrderID+"/invoice/email", body)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("order_id", testOrderID)
	ctx := context.WithValue(testContext(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.SendInvoiceEmail(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d. body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response body: %v, body=%s", err, rec.Body.String())
	}

	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response data is not an object: %+v", payload)
	}

	if linked, _ := data["customer_linked"].(bool); !linked {
		t.Fatalf("expected customer_linked=true in response, got %+v", data)
	}
	if data["customer_id"] != "cus_1" {
		t.Fatalf("expected customer_id=cus_1 in response, got %+v", data)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
