//go:build postgres_integration

package googleauth

import (
	"context"
	"errors"
	"strconv"
	"testing"

	authModule "welloresto-api/internal/modules/auth"

	"welloresto-api/internal/database/dbx/pgtest"
)

// LOT A Semaine 2, Chantier 7b (docs/decisions.md) : les cinq branches de
// §5.2.3, exercées via authenticateClaims directement (pas Authenticate) —
// la vérification cryptographique du id_token est couverte séparément par
// verifier_test.go ; ici on ne teste que l'arbre de décision base de
// données, avec des Claims fabriqués.
func TestGoogleAuth_FiveBranches_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest GoogleAuth', 'a', '1', 's', '75001', 'Paris', 'siret-googleauth', 'https://x', '06', 'mtok-googleauth', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	// GetUserByToken (reused by sessionFromToken) LEFT JOINs merchant_parameters
	// and scans several of its columns into non-nullable Go ints — a bare
	// merchant row without this satellite makes that scan fail on NULL.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update)
		VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	// scannorder_settings.activated is also scanned into a non-nullable bool.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type)
		VALUES ($1, '', '', '', '')`, merchantID); err != nil {
		t.Fatalf("seed scannorder_settings: %v", err)
	}

	// Teardown only — unlike most other tests in this session, the merchant
	// is already created by the time this is defined (its id is needed for
	// seedUser below), so calling this immediately would delete the row
	// this test just made.
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id LIKE 'itest-gauth-%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id LIKE 'itest-gauth-%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	seedUser := func(t *testing.T, userID, email, password, googleSub string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users (user_id, name, first_name, last_name, email, tel, password, token, google_sub)
			VALUES ($1, $2, 'ITest', 'GAuth', $2, '+33600000000', $3, $4, NULLIF($5, ''))
		`, userID, email, password, "user-tok-"+userID, googleSub); err != nil {
			t.Fatalf("seed user %s: %v", userID, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users_rights (user_id, merchant_id, token, admin, enabled, login_enabled)
			VALUES ($1, $2, $3, true, true, true)
		`, userID, merchantID, "rights-tok-"+userID); err != nil {
			t.Fatalf("seed users_rights for %s: %v", userID, err)
		}
	}

	repo := NewRepository(db)
	authRepo := authModule.NewAuthRepository(db)
	svc := NewService(nil /* verifier unused by authenticateClaims */, repo, authRepo)

	t.Run("email_verified=false is refused regardless of everything else", func(t *testing.T) {
		_, err := svc.authenticateClaims(ctx, &Claims{Sub: "sub-unverified", Email: "itest-gauth-unverified@example.com", EmailVerified: false})
		if !errors.Is(err, ErrGoogleEmailNotVerified) {
			t.Fatalf("err = %v, want ErrGoogleEmailNotVerified", err)
		}
	})

	t.Run("branch 1: google_sub known -> connexion", func(t *testing.T) {
		seedUser(t, "itest-gauth-known", "itest-gauth-known@example.com", "somehash", "google-sub-known")

		resp, err := svc.authenticateClaims(ctx, &Claims{Sub: "google-sub-known", Email: "itest-gauth-known@example.com", EmailVerified: true})
		if err != nil {
			t.Fatalf("authenticateClaims: %v", err)
		}
		if resp.UserID != "itest-gauth-known" {
			t.Fatalf("UserID = %q, want itest-gauth-known", resp.UserID)
		}
		var rightsToken string
		if err := db.QueryRowContext(ctx, `SELECT token FROM users_rights WHERE user_id = $1`, "itest-gauth-known").Scan(&rightsToken); err != nil {
			t.Fatalf("read back users_rights.token: %v", err)
		}
		if resp.Token != rightsToken {
			t.Fatalf("returned token %q != users_rights.token %q", resp.Token, rightsToken)
		}
	})

	t.Run("branch 2: google_sub unknown, email unknown -> not found", func(t *testing.T) {
		_, err := svc.authenticateClaims(ctx, &Claims{Sub: "google-sub-nobody", Email: "itest-gauth-nobody@example.com", EmailVerified: true})
		if !errors.Is(err, ErrGoogleAccountNotFound) {
			t.Fatalf("err = %v, want ErrGoogleAccountNotFound", err)
		}
	})

	t.Run("branch 3: google_sub unknown, email known, no password -> auto-link", func(t *testing.T) {
		seedUser(t, "itest-gauth-nopass", "itest-gauth-nopass@example.com", "", "")

		resp, err := svc.authenticateClaims(ctx, &Claims{Sub: "google-sub-newlink", Email: "itest-gauth-nopass@example.com", EmailVerified: true})
		if err != nil {
			t.Fatalf("authenticateClaims: %v", err)
		}
		if resp.UserID != "itest-gauth-nopass" {
			t.Fatalf("UserID = %q, want itest-gauth-nopass", resp.UserID)
		}
		var linkedSub string
		if err := db.QueryRowContext(ctx, `SELECT google_sub FROM users WHERE user_id = $1`, "itest-gauth-nopass").Scan(&linkedSub); err != nil {
			t.Fatalf("read back google_sub: %v", err)
		}
		if linkedSub != "google-sub-newlink" {
			t.Fatalf("google_sub = %q, want google-sub-newlink (auto-link did not happen)", linkedSub)
		}
	})

	t.Run("branch 4: google_sub unknown, email known, HAS a password -> refused, never auto-linked", func(t *testing.T) {
		seedUser(t, "itest-gauth-haspass", "itest-gauth-haspass@example.com", "$2a$12$somehash", "")

		_, err := svc.authenticateClaims(ctx, &Claims{Sub: "google-sub-shouldnotlink", Email: "itest-gauth-haspass@example.com", EmailVerified: true})
		if !errors.Is(err, ErrGoogleAccountHasPassword) {
			t.Fatalf("err = %v, want ErrGoogleAccountHasPassword", err)
		}
		var linkedSub *string
		if err := db.QueryRowContext(ctx, `SELECT google_sub FROM users WHERE user_id = $1`, "itest-gauth-haspass").Scan(&linkedSub); err != nil {
			t.Fatalf("read back google_sub: %v", err)
		}
		if linkedSub != nil {
			t.Fatalf("google_sub = %q, want NULL — a refused rattachement must never silently link", *linkedSub)
		}
	})
}
