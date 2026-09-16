//go:build postgres_integration

package subscriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
)

// TestGetCurrent_Postgres — chantier 2's gap: GET /v1/subscriptions/current
// must reflect the merchant's actually-active subscription_items, with no
// hypothetical add/remove (unlike PreviewChange).
func TestGetCurrent_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-current-composition"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}
	if _, err := repo.AddItem(ctx, merchantID, CodeHACCP, KindModule, 1, 2900); err != nil {
		t.Fatalf("AddItem(haccp): %v", err)
	}

	h := NewHandler(newTestService(db))
	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/current", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "u1", MerchantID: merchantID}))
	rec := httptest.NewRecorder()

	h.GetCurrent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Current Amount `json:"current"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	current := body.Data.Current
	if current.TotalCents != 11400 {
		t.Fatalf("TotalCents = %d, want 11400 (7900 essentiel + 3500 haccp) — raw body: %s", current.TotalCents, rec.Body.String())
	}
	if len(current.Breakdown) != 2 {
		t.Fatalf("Breakdown has %d lines, want 2 (essentiel, haccp): %+v", len(current.Breakdown), current.Breakdown)
	}
}

// TestGetCurrent_Postgres_Unauthorized — no user in context must 401, never
// panic on a nil user.
func TestGetCurrent_Postgres_Unauthorized(t *testing.T) {
	db := pgtest.Open(t)
	h := NewHandler(newTestService(db))
	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/current", nil)
	rec := httptest.NewRecorder()

	h.GetCurrent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
