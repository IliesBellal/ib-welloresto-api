//go:build postgres_integration

package auth

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// LOT A Semaine 2, Chantier 8 (docs/decisions.md) : POST /v1/auth/password/set
// exercisé au niveau Service — la logique de garde (éligibilité) vit
// entièrement dans la clause WHERE atomique d'AuthRepository.SetPasswordForGoogleAccount,
// donc c'est elle qui compte ici, pas la mécanique de résolution du jeton
// (déjà couverte par TestAuthRepository_Postgres/GetUserByToken).
func TestSetPasswordForGoogleAccount_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id LIKE 'itest-pwdset-%'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	seed := func(t *testing.T, userID, authProvider, password string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users (user_id, name, first_name, last_name, email, tel, password, token, auth_provider)
			VALUES ($1, $2, 'ITest', 'PwdSet', $2, '+33600000000', $3, $4, $5)
		`, userID, userID+"@example.com", password, "user-tok-"+userID, authProvider); err != nil {
			t.Fatalf("seed user %s: %v", userID, err)
		}
	}

	svc := AuthService{repo: NewAuthRepository(db)}

	t.Run("google account without a password: succeeds, auth_provider becomes both", func(t *testing.T) {
		seed(t, "itest-pwdset-google-nopass", "google", "")

		if err := svc.SetPasswordForGoogleAccount(ctx, "itest-pwdset-google-nopass", "Sup3r$ecretNew!"); err != nil {
			t.Fatalf("SetPasswordForGoogleAccount: %v", err)
		}

		var authProvider, password string
		if err := db.QueryRowContext(ctx, `SELECT auth_provider, password FROM users WHERE user_id = $1`, "itest-pwdset-google-nopass").
			Scan(&authProvider, &password); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if authProvider != "both" {
			t.Fatalf("auth_provider = %q, want both", authProvider)
		}
		if password == "" || !helpers.PasswordMatches("Sup3r$ecretNew!", password) {
			t.Fatalf("password was not set to a valid hash of the new password")
		}
	})

	t.Run("google account that already has a password: rejected, nothing changed", func(t *testing.T) {
		seed(t, "itest-pwdset-google-haspass", "google", "$2a$12$existinghash")

		err := svc.SetPasswordForGoogleAccount(ctx, "itest-pwdset-google-haspass", "Sup3r$ecretNew!")
		if !errors.Is(err, ErrAccountNotEligibleForPasswordSet) {
			t.Fatalf("err = %v, want ErrAccountNotEligibleForPasswordSet", err)
		}

		var authProvider, password string
		db.QueryRowContext(ctx, `SELECT auth_provider, password FROM users WHERE user_id = $1`, "itest-pwdset-google-haspass").Scan(&authProvider, &password)
		if authProvider != "google" || password != "$2a$12$existinghash" {
			t.Fatalf("account was modified: auth_provider=%q password=%q, want unchanged", authProvider, password)
		}
	})

	t.Run("password-provider account: not eligible, this endpoint is Google-only", func(t *testing.T) {
		seed(t, "itest-pwdset-password-acct", "password", "$2a$12$somehash")

		err := svc.SetPasswordForGoogleAccount(ctx, "itest-pwdset-password-acct", "Sup3r$ecretNew!")
		if !errors.Is(err, ErrAccountNotEligibleForPasswordSet) {
			t.Fatalf("err = %v, want ErrAccountNotEligibleForPasswordSet", err)
		}
	})

	t.Run("weak new password: rejected before any write", func(t *testing.T) {
		seed(t, "itest-pwdset-weak", "google", "")

		err := svc.SetPasswordForGoogleAccount(ctx, "itest-pwdset-weak", "short")
		if !errors.Is(err, models.ErrInvalidInputPasswordTooShort) {
			t.Fatalf("err = %v, want models.ErrInvalidInputPasswordTooShort", err)
		}

		var authProvider, password string
		db.QueryRowContext(ctx, `SELECT auth_provider, password FROM users WHERE user_id = $1`, "itest-pwdset-weak").Scan(&authProvider, &password)
		if authProvider != "google" || password != "" {
			t.Fatalf("account was modified despite a rejected weak password: auth_provider=%q password=%q", authProvider, password)
		}
	})
}

// TestNeedsPasswordSet_Postgres covers LOT A Semaine 3, Chantier 14's
// screen-triggering check: true only for a Google-origin account with no
// password yet — the same eligibility condition SetPasswordForGoogleAccount
// itself enforces, just read-only here.
func TestNeedsPasswordSet_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id LIKE 'itest-needsset-%'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	seed := func(t *testing.T, userID, authProvider, password string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users (user_id, name, first_name, last_name, email, tel, password, token, auth_provider)
			VALUES ($1, $2, 'ITest', 'NeedsSet', $2, '+33600000000', $3, $4, $5)
		`, userID, userID+"@example.com", password, "user-tok-"+userID, authProvider); err != nil {
			t.Fatalf("seed user %s: %v", userID, err)
		}
	}

	svc := AuthService{repo: NewAuthRepository(db)}

	tests := []struct {
		name         string
		userIDSuffix string
		authProvider string
		password     string
		want         bool
	}{
		{"google without password", "google-nopass", "google", "", true},
		{"google with password", "google-haspass", "google", "$2a$12$existinghash", false},
		{"password provider", "password-acct", "password", "$2a$12$somehash", false},
		{"both (already set once)", "both-acct", "both", "$2a$12$somehash", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := "itest-needsset-" + tt.userIDSuffix
			seed(t, userID, tt.authProvider, tt.password)

			got, err := svc.NeedsPasswordSet(ctx, userID)
			if err != nil {
				t.Fatalf("NeedsPasswordSet: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NeedsPasswordSet(%s/%q) = %v, want %v", tt.authProvider, tt.password, got, tt.want)
			}
		})
	}

	t.Run("unknown user: false, no error", func(t *testing.T) {
		got, err := svc.NeedsPasswordSet(ctx, "itest-needsset-does-not-exist")
		if err != nil {
			t.Fatalf("NeedsPasswordSet: %v", err)
		}
		if got {
			t.Fatal("NeedsPasswordSet(unknown user) = true, want false")
		}
	})
}
