//go:build postgres_integration

package googleauth

import (
	"context"
	"errors"
	"testing"

	authModule "welloresto-api/internal/modules/auth"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Rattachement Google depuis les paramètres du compte : linkClaims / Unlink,
// exercés avec des Claims fabriqués (la vérification du id_token est couverte
// par verifier_test.go).
func TestGoogleLink_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id LIKE 'itest-glink-%'`)
	})

	seedUser := func(t *testing.T, userID, password, googleSub, authProvider string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users (user_id, name, first_name, last_name, email, tel, password, token, google_sub, auth_provider)
			VALUES ($1, $1, 'ITest', 'GLink', $1 || '@example.com', '+33600000000', $2, 'user-tok-' || $1, NULLIF($3, ''), $4)
		`, userID, password, googleSub, authProvider); err != nil {
			t.Fatalf("seed user %s: %v", userID, err)
		}
	}
	readBack := func(t *testing.T, userID string) (googleSub *string, authProvider string) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `SELECT google_sub, auth_provider FROM users WHERE user_id = $1`, userID).
			Scan(&googleSub, &authProvider); err != nil {
			t.Fatalf("read back %s: %v", userID, err)
		}
		return googleSub, authProvider
	}

	svc := NewService(nil /* verifier unused by linkClaims */, NewRepository(db), authModule.NewAuthRepository(db))

	t.Run("password account links Google and becomes both", func(t *testing.T) {
		seedUser(t, "itest-glink-pwd", "$2a$12$somehash", "", "password")

		if err := svc.linkClaims(ctx, "itest-glink-pwd", &Claims{Sub: "glink-sub-pwd", EmailVerified: true}); err != nil {
			t.Fatalf("linkClaims: %v", err)
		}
		sub, provider := readBack(t, "itest-glink-pwd")
		if sub == nil || *sub != "glink-sub-pwd" || provider != "both" {
			t.Fatalf("got google_sub=%v auth_provider=%q, want glink-sub-pwd / both", sub, provider)
		}
		// Same Google account again: idempotent.
		if err := svc.linkClaims(ctx, "itest-glink-pwd", &Claims{Sub: "glink-sub-pwd", EmailVerified: true}); err != nil {
			t.Fatalf("re-link same sub: %v", err)
		}
	})

	t.Run("email_verified=false is refused", func(t *testing.T) {
		seedUser(t, "itest-glink-unverified", "$2a$12$somehash", "", "password")

		err := svc.linkClaims(ctx, "itest-glink-unverified", &Claims{Sub: "glink-sub-unverified", EmailVerified: false})
		if !errors.Is(err, ErrGoogleEmailNotVerified) {
			t.Fatalf("err = %v, want ErrGoogleEmailNotVerified", err)
		}
	})

	t.Run("Google account already linked to another user is refused", func(t *testing.T) {
		seedUser(t, "itest-glink-owner", "$2a$12$somehash", "glink-sub-taken", "both")
		seedUser(t, "itest-glink-thief", "$2a$12$somehash", "", "password")

		err := svc.linkClaims(ctx, "itest-glink-thief", &Claims{Sub: "glink-sub-taken", EmailVerified: true})
		if !errors.Is(err, ErrGoogleAccountLinkedElsewhere) {
			t.Fatalf("err = %v, want ErrGoogleAccountLinkedElsewhere", err)
		}
		if sub, _ := readBack(t, "itest-glink-thief"); sub != nil {
			t.Fatalf("google_sub = %q, want NULL", *sub)
		}
	})

	t.Run("a different Google account already linked is never replaced", func(t *testing.T) {
		seedUser(t, "itest-glink-other", "$2a$12$somehash", "glink-sub-first", "both")

		err := svc.linkClaims(ctx, "itest-glink-other", &Claims{Sub: "glink-sub-second", EmailVerified: true})
		if !errors.Is(err, ErrGoogleAlreadyLinked) {
			t.Fatalf("err = %v, want ErrGoogleAlreadyLinked", err)
		}
		if sub, _ := readBack(t, "itest-glink-other"); sub == nil || *sub != "glink-sub-first" {
			t.Fatalf("google_sub = %v, want glink-sub-first unchanged", sub)
		}
	})

	t.Run("unlink with a password: back to password", func(t *testing.T) {
		seedUser(t, "itest-glink-unlink", "$2a$12$somehash", "glink-sub-unlink", "both")

		if err := svc.Unlink(ctx, "itest-glink-unlink"); err != nil {
			t.Fatalf("Unlink: %v", err)
		}
		sub, provider := readBack(t, "itest-glink-unlink")
		if sub != nil || provider != "password" {
			t.Fatalf("got google_sub=%v auth_provider=%q, want NULL / password", sub, provider)
		}
		// Not linked any more: no-op.
		if err := svc.Unlink(ctx, "itest-glink-unlink"); err != nil {
			t.Fatalf("second Unlink: %v", err)
		}
	})

	t.Run("unlink without a password is refused", func(t *testing.T) {
		seedUser(t, "itest-glink-googleonly", "", "glink-sub-only", "google")

		if err := svc.Unlink(ctx, "itest-glink-googleonly"); !errors.Is(err, ErrGoogleUnlinkRequiresPassword) {
			t.Fatalf("err = %v, want ErrGoogleUnlinkRequiresPassword", err)
		}
		if sub, _ := readBack(t, "itest-glink-googleonly"); sub == nil {
			t.Fatal("google_sub was cleared, want it kept")
		}
	})
}
