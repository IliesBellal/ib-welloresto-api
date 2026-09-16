//go:build postgres_integration

package presets_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/presets"

	"github.com/go-chi/chi/v5"
)

// TestGetSuggestedModules_Postgres — chantier 2's gap for the signup
// tunnel's module-selection screen (chantier 4b): a real seeded preset code
// ("traditional", migrations/todo/131) must return its suggested_modules.
func TestGetSuggestedModules_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	h := presets.NewHandler(presets.NewRepository(db))

	req := httptest.NewRequest(http.MethodGet, "/v1/public/presets/traditional/suggested-modules", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("code", "traditional")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.GetSuggestedModules(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			SuggestedModules []string `json:"suggested_modules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.SuggestedModules) == 0 {
		t.Fatalf("suggested_modules is empty for preset %q, want at least one (migration 131 seeds reservation/planning) — raw body: %s", "traditional", rec.Body.String())
	}
}

// TestGetSuggestedModules_UnknownCode_Postgres — an unknown/inactive preset
// code must be a client error (400, invalid_preset_code), not a 500.
func TestGetSuggestedModules_UnknownCode_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	h := presets.NewHandler(presets.NewRepository(db))

	req := httptest.NewRequest(http.MethodGet, "/v1/public/presets/does-not-exist/suggested-modules", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("code", "does-not-exist")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.GetSuggestedModules(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
