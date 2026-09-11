//go:build postgres_integration

package signup

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/pricing"
)

// TestSignupContext_CreateAndGet_Postgres covers LOT A Semaine 3, Chantier 11:
// POST /v1/public/signup-context prices the cart server-side and signs it
// into a stateless context_token (no DB row); GET .../{token} restitutes
// exactly that server-computed price by decoding the same token, never
// anything the client could have supplied itself.
func TestSignupContext_CreateAndGet_Postgres(t *testing.T) {
	ctx := context.Background()
	h, _ := newTestHandler(pgtest.Open(t))
	svc := h.svc

	createResp, err := svc.CreateContext(ctx, "203.0.113.10", CreateContextRequest{
		Segment: "fast_food",
		Cart:    pricing.Cart{Modules: []string{"haccp", "marketplaces", "delivery"}, BillingCycle: "monthly"},
	})
	if err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	if createResp.ContextToken == "" {
		t.Fatal("CreateContext: empty context_token")
	}
	if createResp.ResolvedPlan.PlanCode != pricing.PlanPro {
		t.Fatalf("resolved plan = %q, want %q", createResp.ResolvedPlan.PlanCode, pricing.PlanPro)
	}
	if createResp.RecommendedChannel != "self_serve" {
		t.Fatalf("recommended_channel = %q, want self_serve", createResp.RecommendedChannel)
	}

	getResp, err := svc.GetContext(ctx, createResp.ContextToken)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if getResp.ResolvedPlan.PlanCode != createResp.ResolvedPlan.PlanCode || getResp.ResolvedPlan.MonthlyTotalCents != createResp.ResolvedPlan.MonthlyTotalCents {
		t.Fatalf("GetContext resolved_plan = %+v, want it to match CreateContext's %+v", getResp.ResolvedPlan, createResp.ResolvedPlan)
	}
	if getResp.Segment != "fast_food" {
		t.Fatalf("segment = %q, want %q", getResp.Segment, "fast_food")
	}

	if _, err := svc.GetContext(ctx, "not-a-real-token"); err == nil {
		t.Fatal("GetContext(not-a-real-token): expected an error, got nil")
	}
}

// TestSignup_ContextTokenResolvesPackage_Postgres proves the wiring chantier
// 11c asked for end to end: package_id resolved from a signup-context
// replaces packages.id=1 ("Essentiel", DefaultPackageID) once a
// context_token is supplied to POST /v1/signup, and the context's
// attribution reaches merchant.signup_source.
func TestSignup_ContextTokenResolvesPackage_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	h, _ := newTestHandler(db)
	svc := h.svc

	// A cart requesting all five modules (zero employees) resolves to
	// "complet" (see pricing's own TestResolveCheapestPlan_Postgres) — a
	// plan Essentiel (the default) is not.
	createResp, err := svc.CreateContext(ctx, "203.0.113.20", CreateContextRequest{
		Cart:        pricing.Cart{Modules: []string{"reservation", "haccp", "planning", "marketplaces", "delivery"}},
		Attribution: Attribution{UTMSource: "google", Landing: "/tarifs"},
	})
	if err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	if createResp.ResolvedPlan.PlanCode != pricing.PlanComplet {
		t.Fatalf("resolved plan = %q, want %q", createResp.ResolvedPlan.PlanCode, pricing.PlanComplet)
	}

	const email = "itest-signup-ctxpkg@example.com"
	const siret = "50000000000009" // Luhn-valid, distinct from other tests' fixtures
	cleanupSignupArtifacts(t, ctx, db, email, siret)

	req := validSignupPayload(email, siret)
	req.ContextToken = createResp.ContextToken
	rec := doSignup(t, h, "itest-idem-ctxpkg-1", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup with context_token: status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}

	e := decodeEnvelope(t, rec)
	var data struct {
		MerchantID string `json:"merchant_id"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}

	var packageID string
	if err := db.QueryRowContext(ctx, `SELECT CAST(package_id AS TEXT) FROM subscriptions WHERE merchant_id = $1`, data.MerchantID).Scan(&packageID); err != nil {
		t.Fatalf("read back subscriptions.package_id: %v", err)
	}
	if packageID != createResp.ResolvedPlan.PackageID {
		t.Fatalf("merchant's package_id = %q, want %q (the context's resolved Complet package, not the default)", packageID, createResp.ResolvedPlan.PackageID)
	}

	var signupChannel string
	var signupSource []byte
	if err := db.QueryRowContext(ctx, `SELECT signup_channel, signup_source FROM merchant WHERE id = $1`, data.MerchantID).Scan(&signupChannel, &signupSource); err != nil {
		t.Fatalf("read back signup_channel/signup_source: %v", err)
	}
	if signupChannel != "self_signup" {
		t.Fatalf("signup_channel = %q, want self_signup", signupChannel)
	}
	var attribution Attribution
	if err := json.Unmarshal(signupSource, &attribution); err != nil {
		t.Fatalf("decode signup_source: %v", err)
	}
	if attribution.UTMSource != "google" || attribution.Landing != "/tarifs" {
		t.Fatalf("signup_source = %+v, want the context's attribution", attribution)
	}
}
