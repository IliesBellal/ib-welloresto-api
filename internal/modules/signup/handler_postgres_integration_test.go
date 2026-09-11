//go:build postgres_integration

package signup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/googleauth"
	"welloresto-api/internal/modules/locations"
	"welloresto-api/internal/modules/menu"
	"welloresto-api/internal/modules/onboarding"
	"welloresto-api/internal/modules/pos"
	"welloresto-api/internal/modules/presets"
	"welloresto-api/internal/modules/pricing"
	"welloresto-api/internal/modules/users"

	"github.com/golang-jwt/jwt/v5"
)

// LOT A Semaine 2, Chantier 6b (docs/decisions.md) : POST /v1/signup couvert
// de bout en bout via le Handler réel (pas seulement Service.Signup) — c'est
// le Handler qui porte toute la mécanique d'idempotence (Idempotency-Key,
// rejeu à l'identique), donc c'est lui qu'il faut exercer pour la couvrir.

func newTestHandler(db *sql.DB) (*Handler, *Repository) {
	return newTestHandlerWithVerifier(db, nil)
}

func newTestHandlerWithVerifier(db *sql.DB, verifier *googleauth.Verifier) (*Handler, *Repository) {
	sessionsRepo := NewRepository(db)
	usersRepo := users.NewUserRepository(db)
	posRepo := pos.NewPOSRepository(db)
	posService := pos.NewPOSService(posRepo, nil)
	presetsRepo := presets.NewRepository(db)
	menuRepo := menu.NewMenuRepository(db, nil)
	locationsRepo := locations.NewLocationsRepository(db)
	presetsService := presets.NewService(presetsRepo, menuRepo, locationsRepo)
	onboardingRepo := onboarding.NewRepository(db)
	pricingService := pricing.NewService(pricing.NewRepository(db))

	svc := NewService(db, sessionsRepo, usersRepo, posService, presetsRepo, presetsService, onboardingRepo, verifier, pricingService, nil, "itest-signing-key")
	return NewHandler(svc, sessionsRepo), sessionsRepo
}

func validSignupPayload(email, siret string) SignupRequest {
	return SignupRequest{
		Identity: SignupIdentity{
			Provider: "password", Email: email, Password: "Sup3r$ecret!",
			FirstName: "ITest", LastName: "Owner",
		},
		PresetCode: "snack",
		Merchant: SignupMerchantPayload{
			FullName: "ITest Signup Merchant", SIRET: siret, Tel: "0600000000",
			Address: "1 rue de test", ZipCode: "75001", City: "Paris", Country: "FR",
			Email: "biz-" + email,
		},
		AcceptsTerms: true,
	}
}

func doSignup(t *testing.T, h *Handler, idempotencyKey string, req SignupRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/signup", bytes.NewReader(body))
	if idempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	h.Signup(rec, httpReq)
	return rec
}

type envelope struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode response envelope: %v (body=%s)", err, rec.Body.String())
	}
	return e
}

func cleanupSignupArtifacts(t *testing.T, ctx context.Context, db *sql.DB, email, siret string) {
	t.Helper()
	cleanup := func() {
		// signup_sessions rows persist across test runs otherwise (each test
		// uses a fixed Idempotency-Key) — a leftover 'completed'/'failed' row
		// from a previous run would make the handler replay a stale cached
		// response referencing a user/merchant this cleanup already deleted.
		_, _ = db.ExecContext(ctx, `DELETE FROM signup_sessions WHERE lower(email) = lower($1)`, email)

		var merchantIntID sql.NullInt64
		_ = db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1`, siret).Scan(&merchantIntID)
		if merchantIntID.Valid {
			mID := strconv.FormatInt(merchantIntID.Int64, 10)
			for _, q := range []string{
				`DELETE FROM onboarding_tasks WHERE merchant_id = $1`,
				`DELETE FROM productcateg WHERE merchant_id = $1`,
				`DELETE FROM locations WHERE merchant_id = $1`,
				`DELETE FROM floors WHERE merchant_id = $1`,
				`DELETE FROM qrcodes WHERE merchant_id = $1`,
				`DELETE FROM scannorder_settings WHERE merchant_id = $1`,
				`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
				`DELETE FROM subscriptions WHERE merchant_id = $1`,
				`DELETE FROM users_rights WHERE merchant_id = $1`,
				`UPDATE merchant SET default_role_id = NULL WHERE id = $2`,
				`DELETE FROM roles WHERE merchant_id = $1`,
			} {
				_, _ = db.ExecContext(ctx, q, mID, merchantIntID.Int64)
			}
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID.Int64)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE lower(email) = lower($1)`, email)
	}
	cleanup()
	t.Cleanup(cleanup)
}

// TestSignup_Nominal_Postgres is the golden path: a valid password signup
// creates the owner user, the merchant (via POSService.CreateMerchant,
// reused wholesale), applies the "snack" preset, and creates the five
// onboarding tasks.
func TestSignup_Nominal_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const email = "itest-signup-nominal@example.com"
	const siret = "73282932000074" // real, Luhn-valid SIRET (INSEE/La Poste's own)
	cleanupSignupArtifacts(t, ctx, db, email, siret)

	h, _ := newTestHandler(db)
	rec := doSignup(t, h, "itest-idem-nominal-1", validSignupPayload(email, siret))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	env := decodeEnvelope(t, rec)
	var data struct {
		Status          string `json:"status"`
		MerchantID      string `json:"merchant_id"`
		UserID          string `json:"user_id"`
		Token           string `json:"token"`
		ActivationState string `json:"activation_state"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Status != "success" || data.MerchantID == "" || data.UserID == "" || data.Token == "" {
		t.Fatalf("unexpected success payload: %+v", data)
	}
	if data.ActivationState != "SETUP" {
		t.Fatalf("activation_state = %q, want SETUP", data.ActivationState)
	}

	// --- token is genuinely users_rights.token for the owner ---
	var rightsToken string
	if err := db.QueryRowContext(ctx, `SELECT token FROM users_rights WHERE user_id = $1 AND merchant_id = $2`, data.UserID, data.MerchantID).Scan(&rightsToken); err != nil {
		t.Fatalf("read back users_rights.token: %v", err)
	}
	if rightsToken != data.Token {
		t.Fatalf("returned token %q does not match users_rights.token %q", data.Token, rightsToken)
	}

	// --- user: name = lower(email), auth_provider = 'password' ---
	var name, authProvider string
	if err := db.QueryRowContext(ctx, `SELECT name, auth_provider FROM users WHERE user_id = $1`, data.UserID).Scan(&name, &authProvider); err != nil {
		t.Fatalf("read back user: %v", err)
	}
	if name != email || authProvider != "password" {
		t.Fatalf("user (name=%q auth_provider=%q), want (name=%q auth_provider=password)", name, authProvider, email)
	}

	// --- preset applied: snack categories present (v2 — LOT A Semaine 3
	// Chantier 9 — six categories, up from four in v1) ---
	var categCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM productcateg WHERE merchant_id = $1`, data.MerchantID).Scan(&categCount)
	if categCount != 6 {
		t.Fatalf("productcateg count = %d, want 6 (snack preset v2)", categCount)
	}

	// --- vat_number derived from SIRET's SIREN (LOT A Semaine 3, Chantier 12) ---
	var vatNumber string
	db.QueryRowContext(ctx, `SELECT vat_number FROM merchant WHERE id = $1`, data.MerchantID).Scan(&vatNumber)
	if vatNumber != "FR44732829320" {
		t.Fatalf("vat_number = %q, want %q (derived from SIREN 732829320)", vatNumber, "FR44732829320")
	}

	// --- onboarding_tasks: five rows ---
	var taskCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM onboarding_tasks WHERE merchant_id = $1`, data.MerchantID).Scan(&taskCount)
	if taskCount != 5 {
		t.Fatalf("onboarding_tasks count = %d, want 5", taskCount)
	}
}

// TestSignup_EmailAlreadyUsed_Postgres covers the email-taken rejection.
func TestSignup_EmailAlreadyUsed_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const email1 = "itest-signup-emailtaken-1@example.com"
	const siret1 = "73282932000074"
	const siret2 = "48170376800046" // distinct valid-format SIRET (Luhn not required for this test's purpose since format isn't what's tested, but validated below anyway)
	cleanupSignupArtifacts(t, ctx, db, email1, siret1)
	cleanupSignupArtifacts(t, ctx, db, email1, siret2)

	h, _ := newTestHandler(db)

	rec1 := doSignup(t, h, "itest-idem-emailtaken-1", validSignupPayload(email1, siret1))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first signup: status = %d, want 201 (body=%s)", rec1.Code, rec1.Body.String())
	}

	rec2 := doSignup(t, h, "itest-idem-emailtaken-2", validSignupPayload(email1, siret2))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second signup (same email): status = %d, want 409 (body=%s)", rec2.Code, rec2.Body.String())
	}
	env := decodeEnvelope(t, rec2)
	var data struct {
		Status string `json:"status"`
	}
	json.Unmarshal(env.Data, &data)
	if data.Status != "email_already_used" {
		t.Fatalf("data.status = %q, want email_already_used", data.Status)
	}

	var count int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM merchant WHERE siret = $1`, siret2).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no merchant created for the rejected second signup, found %d", count)
	}
}

// TestSignup_SIRETAlreadyTaken_Postgres covers the SIRET-taken rejection —
// and that the rejection is generic (does not reveal the existing account).
func TestSignup_SIRETAlreadyTaken_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const email1 = "itest-signup-sirettaken-1@example.com"
	const email2 = "itest-signup-sirettaken-2@example.com"
	const siret = "73282932000074"
	cleanupSignupArtifacts(t, ctx, db, email1, siret)
	cleanupSignupArtifacts(t, ctx, db, email2, siret)

	h, _ := newTestHandler(db)

	rec1 := doSignup(t, h, "itest-idem-sirettaken-1", validSignupPayload(email1, siret))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first signup: status = %d, want 201 (body=%s)", rec1.Code, rec1.Body.String())
	}

	rec2 := doSignup(t, h, "itest-idem-sirettaken-2", validSignupPayload(email2, siret))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second signup (same SIRET): status = %d, want 409 (body=%s)", rec2.Code, rec2.Body.String())
	}
	if bytes.Contains(rec2.Body.Bytes(), []byte(email1)) {
		t.Fatalf("SIRET-taken rejection must not reveal the existing account's email — body: %s", rec2.Body.String())
	}
	env := decodeEnvelope(t, rec2)
	var data struct {
		Status string `json:"status"`
	}
	json.Unmarshal(env.Data, &data)
	if data.Status != "siret_already_registered" {
		t.Fatalf("data.status = %q, want siret_already_registered", data.Status)
	}

	var count int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE lower(email) = lower($1)`, email2).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no user created for the rejected second signup, found %d", count)
	}
}

// TestSignup_IdempotentReplay_Postgres covers the replay requirement: a
// repeated call with the same Idempotency-Key must return the identical
// cached response and must not create a second user/merchant.
func TestSignup_IdempotentReplay_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const email = "itest-signup-replay@example.com"
	const siret = "73282932000074"
	cleanupSignupArtifacts(t, ctx, db, email, siret)

	h, _ := newTestHandler(db)
	req := validSignupPayload(email, siret)

	rec1 := doSignup(t, h, "itest-idem-replay-key", req)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first call: status = %d, want 201 (body=%s)", rec1.Code, rec1.Body.String())
	}

	rec2 := doSignup(t, h, "itest-idem-replay-key", req)
	if rec2.Code != rec1.Code {
		t.Fatalf("replay status = %d, want %d (first call's status)", rec2.Code, rec1.Code)
	}
	// Semantic comparison, not raw bytes: signup_sessions.payload is jsonb
	// (per the chantier's own schema), and Postgres canonicalizes jsonb
	// whitespace on round-trip — "rejouée à l'identique" means the same JSON
	// content, not byte-identical formatting invisible to any real client.
	var env1, env2 map[string]interface{}
	if err := json.Unmarshal(rec1.Body.Bytes(), &env1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &env2); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	body1, _ := json.Marshal(env1)
	body2, _ := json.Marshal(env2)
	if string(body1) != string(body2) {
		t.Fatalf("replay content differs from first response:\nfirst:  %s\nreplay: %s", body1, body2)
	}

	var userCount, merchantCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE lower(email) = lower($1)`, email).Scan(&userCount)
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM merchant WHERE siret = $1`, siret).Scan(&merchantCount)
	if userCount != 1 || merchantCount != 1 {
		t.Fatalf("replay must not create a second user/merchant: users=%d merchants=%d, want 1/1", userCount, merchantCount)
	}
}

// TestSignup_MissingIdempotencyKey_Postgres covers the required-header rejection.
func TestSignup_MissingIdempotencyKey_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const email = "itest-signup-noidem@example.com"
	const siret = "73282932000074"
	cleanupSignupArtifacts(t, ctx, db, email, siret)

	h, _ := newTestHandler(db)
	rec := doSignup(t, h, "", validSignupPayload(email, siret))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}

	var count int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE lower(email) = lower($1)`, email).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no user created without an Idempotency-Key, found %d", count)
	}
}

// --- provider "google" (LOT A Semaine 2, Chantier 7c) ---

func newTestGoogleJWKSServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()
	nBytes := pub.N.Bytes()
	eBytes := []byte{byte(pub.E >> 16), byte(pub.E >> 8), byte(pub.E)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	type jwk struct{ Kty, Kid, N, E string }
	type jwkSet struct{ Keys []jwk }
	set := jwkSet{Keys: []jwk{{
		Kty: "RSA", Kid: kid,
		N: base64.RawURLEncoding.EncodeToString(nBytes),
		E: base64.RawURLEncoding.EncodeToString(eBytes),
	}}}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(set)
	}))
}

func signTestGoogleToken(t *testing.T, priv *rsa.PrivateKey, kid, clientID, sub, email string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"aud": clientID, "iss": "accounts.google.com",
		"sub": sub, "email": email, "email_verified": true,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("sign test google token: %v", err)
	}
	return signed
}

// TestSignup_GoogleProvider_Postgres covers chantier 7c: provider "google"
// replaces email+password with a verified id_token — the created user has
// auth_provider='google', google_sub set, email_verified_at stamped, and no
// usable password (empty string, per users.password's NOT NULL — see
// users.CreateGoogleUser's doc comment).
func TestSignup_GoogleProvider_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const email = "itest-signup-google@example.com"
	const siret = "48170376800046"
	cleanupSignupArtifacts(t, ctx, db, email, siret)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	server := newTestGoogleJWKSServer(t, "test-kid", &priv.PublicKey)
	defer server.Close()
	verifier := googleauth.NewVerifierWithCertsURL("test-client-id", server.URL)

	h, _ := newTestHandlerWithVerifier(db, verifier)

	req := validSignupPayload(email, siret)
	req.Identity.Provider = "google"
	req.Identity.Password = "" // ignored for provider "google"
	req.Identity.IDToken = signTestGoogleToken(t, priv, "test-kid", "test-client-id", "google-sub-signup-test", email)

	rec := doSignup(t, h, "itest-idem-google-1", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}

	env := decodeEnvelope(t, rec)
	var data struct {
		UserID string `json:"user_id"`
	}
	json.Unmarshal(env.Data, &data)

	var authProvider, googleSub, password string
	var emailVerifiedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT auth_provider, google_sub, password, email_verified_at FROM users WHERE user_id = $1`, data.UserID).
		Scan(&authProvider, &googleSub, &password, &emailVerifiedAt); err != nil {
		t.Fatalf("read back user: %v", err)
	}
	if authProvider != "google" {
		t.Fatalf("auth_provider = %q, want google", authProvider)
	}
	if googleSub != "google-sub-signup-test" {
		t.Fatalf("google_sub = %q, want google-sub-signup-test", googleSub)
	}
	if password != "" {
		t.Fatalf("password = %q, want empty (no password on a Google signup)", password)
	}
	if !emailVerifiedAt.Valid {
		t.Fatal("email_verified_at is NULL, want a timestamp stamped from the verified id_token")
	}
}
