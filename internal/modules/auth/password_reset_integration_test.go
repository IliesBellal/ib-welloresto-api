//go:build postgres_integration

package auth

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// TestPasswordReset_Postgres covers the forgot-password flow end to end against
// a real Postgres: token issuance, single-use consumption, expiry, rate limit,
// and session invalidation. See docs/PASSWORD_RESET.md.
func TestPasswordReset_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const userID = "itest-pwdreset-user-1"
	const userName = "ITest PwdReset"
	const userEmail = "itest-pwdreset@example.com"
	const rightsToken = "itest-pwdreset-rights-token-1"
	const originalPassword = "OriginalPass123"

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	})

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest PwdReset Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-pwdreset', 'https://example.com', '0600000000', 'mtok-pwdreset', 'Europe/Paris', 1.0, 2.0)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	originalHash, err := helpers.HashUserPassword(originalPassword)
	if err != nil {
		t.Fatalf("hash original password: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
		VALUES ($1, $2, 'ITest', 'PwdReset', $3, $4, '+33600000000', 'user-tok-pwdreset', true)`,
		userID, userName, originalHash, userEmail); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, enabled, login_enabled)
		VALUES ($1, $2, $3, true, true)`, userID, merchantID, rightsToken); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}

	repo := NewAuthRepository(db)
	svc := NewAuthService(repo, nil, nil, nil, "itest-pepper", "")

	clearResets := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("clear password_resets: %v", err)
		}
	}

	// --- Lookup: username, email, casse, compte inconnu -------------------
	t.Run("lookup", func(t *testing.T) {
		for _, login := range []string{userName, userEmail, "ITEST-PWDRESET@EXAMPLE.COM"} {
			got, err := repo.GetUserForPasswordReset(ctx, login)
			if err != nil {
				t.Fatalf("GetUserForPasswordReset(%q) error = %v", login, err)
			}
			if got == nil {
				t.Fatalf("GetUserForPasswordReset(%q) = nil, want the seeded user", login)
			}
			if got.UserID != userID || got.Email != userEmail {
				t.Fatalf("GetUserForPasswordReset(%q) = %+v, want user_id %q", login, got, userID)
			}
		}

		got, err := repo.GetUserForPasswordReset(ctx, "nobody@example.com")
		if err != nil {
			t.Fatalf("GetUserForPasswordReset(unknown) error = %v", err)
		}
		if got != nil {
			t.Fatalf("GetUserForPasswordReset(unknown) = %+v, want nil", got)
		}
	})

	// --- Compte désactivé : aucun lien ne doit être émis ------------------
	t.Run("disabled account is not eligible", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE users SET enabled = false WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("disable user: %v", err)
		}
		defer func() {
			if _, err := db.ExecContext(ctx, `UPDATE users SET enabled = true WHERE user_id = $1`, userID); err != nil {
				t.Fatalf("re-enable user: %v", err)
			}
		}()

		issue, err := svc.RequestPasswordReset(ctx, userEmail, "203.0.113.7")
		if err != nil {
			t.Fatalf("RequestPasswordReset(disabled) error = %v", err)
		}
		if issue != nil {
			t.Fatal("RequestPasswordReset(disabled) issued a token, want nil")
		}
	})

	// --- Mot de passe invalide : ne doit PAS consommer le token -----------
	t.Run("weak password does not burn the token", func(t *testing.T) {
		clearResets()

		issue, err := svc.RequestPasswordReset(ctx, userEmail, "203.0.113.7")
		if err != nil || issue == nil {
			t.Fatalf("RequestPasswordReset() = %v, %v; want an issued token", issue, err)
		}

		if err := svc.ConfirmPasswordReset(ctx, issue.ClearToken, "short"); !errors.Is(err, models.ErrInvalidInputPasswordTooShort) {
			t.Fatalf("ConfirmPasswordReset(short password) error = %v, want ErrInvalidInputPasswordTooShort", err)
		}

		var usedAt *time.Time
		if err := db.QueryRowContext(ctx,
			`SELECT used_at FROM password_resets WHERE user_id = $1`, userID).Scan(&usedAt); err != nil {
			t.Fatalf("read used_at: %v", err)
		}
		if usedAt != nil {
			t.Fatal("a rejected password consumed the single-use token")
		}
	})

	// --- Flux nominal + rejeu + rotation de session -----------------------
	t.Run("nominal flow", func(t *testing.T) {
		clearResets()
		const newPassword = "BrandNewPass456"

		issue, err := svc.RequestPasswordReset(ctx, userName, "203.0.113.7")
		if err != nil || issue == nil {
			t.Fatalf("RequestPasswordReset() = %v, %v; want an issued token", issue, err)
		}

		// Le token en clair ne doit jamais être stocké tel quel.
		var storedHash string
		if err := db.QueryRowContext(ctx,
			`SELECT token_hash FROM password_resets WHERE user_id = $1`, userID).Scan(&storedHash); err != nil {
			t.Fatalf("read token_hash: %v", err)
		}
		if storedHash == issue.ClearToken {
			t.Fatal("the clear token was persisted — it must be stored hashed")
		}
		if storedHash != hashResetToken(issue.ClearToken) {
			t.Fatal("stored token_hash does not match sha256(clear token)")
		}

		if err := svc.ConfirmPasswordReset(ctx, issue.ClearToken, newPassword); err != nil {
			t.Fatalf("ConfirmPasswordReset() error = %v", err)
		}

		// Le mot de passe a bien changé.
		var storedPassword string
		if err := db.QueryRowContext(ctx,
			`SELECT password FROM users WHERE user_id = $1`, userID).Scan(&storedPassword); err != nil {
			t.Fatalf("read password: %v", err)
		}
		if bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(newPassword)) != nil {
			t.Fatal("the new password was not applied")
		}
		if bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(originalPassword)) == nil {
			t.Fatal("the old password still works")
		}

		// La session a bien été invalidée EN BASE (pas seulement dans Redis).
		var currentRightsToken string
		if err := db.QueryRowContext(ctx,
			`SELECT token FROM users_rights WHERE user_id = $1`, userID).Scan(&currentRightsToken); err != nil {
			t.Fatalf("read users_rights.token: %v", err)
		}
		if currentRightsToken == rightsToken {
			t.Fatal("users_rights.token was not rotated — existing sessions survive the reset")
		}

		// Rejeu du même lien.
		if err := svc.ConfirmPasswordReset(ctx, issue.ClearToken, "YetAnotherPass789"); !errors.Is(err, ErrInvalidResetToken) {
			t.Fatalf("replayed token error = %v, want ErrInvalidResetToken", err)
		}

		// Restaure le token de droits pour les sous-tests suivants.
		if _, err := db.ExecContext(ctx,
			`UPDATE users_rights SET token = $1 WHERE user_id = $2`, rightsToken, userID); err != nil {
			t.Fatalf("restore rights token: %v", err)
		}
	})

	// --- Token expiré -----------------------------------------------------
	t.Run("expired token", func(t *testing.T) {
		clearResets()

		issue, err := svc.RequestPasswordReset(ctx, userEmail, "203.0.113.7")
		if err != nil || issue == nil {
			t.Fatalf("RequestPasswordReset() = %v, %v; want an issued token", issue, err)
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE password_resets SET expires_at = now() - interval '1 minute' WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("force expiry: %v", err)
		}

		if err := svc.ConfirmPasswordReset(ctx, issue.ClearToken, "SomeValidPass123"); !errors.Is(err, ErrInvalidResetToken) {
			t.Fatalf("expired token error = %v, want ErrInvalidResetToken", err)
		}
	})

	// --- Token inconnu ----------------------------------------------------
	t.Run("unknown token", func(t *testing.T) {
		if err := svc.ConfirmPasswordReset(ctx, "not-a-real-token", "SomeValidPass123"); !errors.Is(err, ErrInvalidResetToken) {
			t.Fatalf("unknown token error = %v, want ErrInvalidResetToken", err)
		}
		if err := svc.ConfirmPasswordReset(ctx, "   ", "SomeValidPass123"); !errors.Is(err, ErrInvalidResetToken) {
			t.Fatalf("blank token error = %v, want ErrInvalidResetToken", err)
		}
	})

	// --- Rate limit par compte -------------------------------------------
	t.Run("per-account rate limit", func(t *testing.T) {
		clearResets()

		for i := 0; i < PasswordResetMaxPerHour; i++ {
			issue, err := svc.RequestPasswordReset(ctx, userEmail, "203.0.113.7")
			if err != nil {
				t.Fatalf("RequestPasswordReset() #%d error = %v", i+1, err)
			}
			if issue == nil {
				t.Fatalf("RequestPasswordReset() #%d was throttled too early", i+1)
			}
		}

		issue, err := svc.RequestPasswordReset(ctx, userEmail, "203.0.113.7")
		if err != nil {
			t.Fatalf("RequestPasswordReset() over limit error = %v", err)
		}
		if issue != nil {
			t.Fatalf("RequestPasswordReset() #%d was allowed, want throttled", PasswordResetMaxPerHour+1)
		}
	})
}

// fakeResetMailer captures the password-reset email instead of sending it.
// Embeds mailer.Service (nil) so only the method under test is implemented.
type fakeResetMailer struct {
	mailer.Service
	sent []mailer.PasswordResetData
}

func (f *fakeResetMailer) SendPasswordReset(data mailer.PasswordResetData) {
	f.sent = append(f.sent, data)
}

// TestSendPasswordResetLink_Postgres covers the HTTP-facing use case: the email
// actually goes out with a usable link, and the per-IP throttle holds.
func TestSendPasswordResetLink_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const userID = "itest-pwdlink-user-1"
	const userEmail = "itest-pwdlink@example.com"

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	})

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest PwdLink Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-pwdlink', 'https://example.com', '0600000000', 'mtok-pwdlink', 'Europe/Paris', 1.0, 2.0)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	hash, err := helpers.HashUserPassword("OriginalPass123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
		VALUES ($1, 'ITest PwdLink', 'Camille', 'PwdLink', $2, $3, '+33600000000', 'user-tok-pwdlink', true)`,
		userID, hash, userEmail); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, enabled, login_enabled)
		VALUES ($1, $2, 'itest-pwdlink-rights-token', true, true)`, userID, merchantID); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}

	newService := func(baseURL string) (*AuthService, *fakeResetMailer) {
		mail := &fakeResetMailer{}
		svc := NewAuthService(NewAuthRepository(db), nil, mail, nil, "itest-pepper", baseURL)
		svc.redis = newMemRedis()
		return &svc, mail
	}

	t.Run("sends a usable link", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("clear password_resets: %v", err)
		}
		svc, mail := newService("https://backoffice.example.com/reset-password")

		if err := svc.SendPasswordResetLink(ctx, userEmail, "203.0.113.9"); err != nil {
			t.Fatalf("SendPasswordResetLink() error = %v", err)
		}
		if len(mail.sent) != 1 {
			t.Fatalf("got %d emails, want 1", len(mail.sent))
		}

		got := mail.sent[0]
		if got.UserEmail != userEmail {
			t.Fatalf("email sent to %q, want %q", got.UserEmail, userEmail)
		}
		if got.FirstName != "Camille" {
			t.Fatalf("FirstName = %q, want %q", got.FirstName, "Camille")
		}
		if got.ExpiresIn != int(PasswordResetTTL.Minutes()) {
			t.Fatalf("ExpiresIn = %d, want %d", got.ExpiresIn, int(PasswordResetTTL.Minutes()))
		}

		// The link must carry a token that actually resets the password.
		parsed, err := url.Parse(got.ResetURL)
		if err != nil {
			t.Fatalf("ResetURL is not a valid URL: %v", err)
		}
		clearToken := parsed.Query().Get("token")
		if clearToken == "" {
			t.Fatalf("ResetURL %q carries no token", got.ResetURL)
		}
		if err := svc.ConfirmPasswordReset(ctx, clearToken, "PassFromEmail789"); err != nil {
			t.Fatalf("token from the email was rejected: %v", err)
		}
	})

	t.Run("unknown login sends nothing but does not error", func(t *testing.T) {
		svc, mail := newService("https://backoffice.example.com/reset-password")

		if err := svc.SendPasswordResetLink(ctx, "nobody@example.com", "203.0.113.9"); err != nil {
			t.Fatalf("SendPasswordResetLink(unknown) error = %v", err)
		}
		if len(mail.sent) != 0 {
			t.Fatalf("got %d emails for an unknown login, want 0", len(mail.sent))
		}
	})

	t.Run("missing base URL sends nothing but does not error", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("clear password_resets: %v", err)
		}
		svc, mail := newService("")

		if err := svc.SendPasswordResetLink(ctx, userEmail, "203.0.113.9"); err != nil {
			t.Fatalf("SendPasswordResetLink(no base URL) error = %v", err)
		}
		if len(mail.sent) != 0 {
			t.Fatalf("got %d emails without a base URL, want 0", len(mail.sent))
		}
	})

	t.Run("per-IP throttle", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("clear password_resets: %v", err)
		}
		svc, _ := newService("https://backoffice.example.com/reset-password")

		// Unknown logins so the per-account limit never interferes: only the
		// per-IP counter is under test here.
		for i := 0; i < models.PasswordResetIPThrottleMax; i++ {
			if err := svc.SendPasswordResetLink(ctx, "nobody@example.com", "198.51.100.4"); err != nil {
				t.Fatalf("SendPasswordResetLink() #%d error = %v", i+1, err)
			}
		}

		svc.email = &fakeResetMailer{}
		mail := svc.email.(*fakeResetMailer)
		if err := svc.SendPasswordResetLink(ctx, userEmail, "198.51.100.4"); err != nil {
			t.Fatalf("SendPasswordResetLink() over IP limit error = %v", err)
		}
		if len(mail.sent) != 0 {
			t.Fatal("a throttled IP still got an email")
		}

		// A different IP is unaffected.
		svc.email = &fakeResetMailer{}
		mail = svc.email.(*fakeResetMailer)
		if err := svc.SendPasswordResetLink(ctx, userEmail, "198.51.100.5"); err != nil {
			t.Fatalf("SendPasswordResetLink() from another IP error = %v", err)
		}
		if len(mail.sent) != 1 {
			t.Fatalf("got %d emails from a non-throttled IP, want 1", len(mail.sent))
		}
	})
}

// TestPasswordResetHandlers_Postgres exercises the wire contract of the two
// public endpoints: status codes, JSON shape, and the invariant that
// forgot-password answers identically whether or not the account exists.
func TestPasswordResetHandlers_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const userID = "itest-pwdhttp-user-1"
	const userEmail = "itest-pwdhttp@example.com"

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	})

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest PwdHTTP Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-pwdhttp', 'https://example.com', '0600000000', 'mtok-pwdhttp', 'Europe/Paris', 1.0, 2.0)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	hash, err := helpers.HashUserPassword("OriginalPass123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
		VALUES ($1, 'ITest PwdHTTP', 'Dominique', 'PwdHTTP', $2, $3, '+33600000000', 'user-tok-pwdhttp', true)`,
		userID, hash, userEmail); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, enabled, login_enabled)
		VALUES ($1, $2, 'itest-pwdhttp-rights-token', true, true)`, userID, merchantID); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}

	mail := &fakeResetMailer{}
	svc := NewAuthService(NewAuthRepository(db), nil, mail, nil, "itest-pepper", "https://backoffice.example.com/reset-password")
	svc.redis = newMemRedis()
	h := NewAuthHandler(svc)

	post := func(handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/whatever", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "198.51.100.77")
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	// forgot-password must look the same for a real and a fake account.
	knownResp := post(h.ForgotPassword, `{"login":"`+userEmail+`"}`)
	unknownResp := post(h.ForgotPassword, `{"login":"nobody@example.com"}`)

	if knownResp.Code != http.StatusOK || unknownResp.Code != http.StatusOK {
		t.Fatalf("status codes = %d (known) / %d (unknown), want 200 / 200",
			knownResp.Code, unknownResp.Code)
	}
	if knownResp.Body.String() != unknownResp.Body.String() {
		t.Fatalf("forgot-password leaks account existence:\n known:   %s\n unknown: %s",
			knownResp.Body.String(), unknownResp.Body.String())
	}
	if len(mail.sent) != 1 {
		t.Fatalf("got %d emails, want exactly 1 (only the known account)", len(mail.sent))
	}

	// A malformed body is still a 400.
	if rec := post(h.ForgotPassword, `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400", rec.Code)
	}

	// An unusable token is a 400 carrying the agreed error code.
	rec := post(h.ResetPassword, `{"token":"nope","new_password":"ValidPass123"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad token status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_or_expired_token") {
		t.Fatalf("bad token body = %s, want invalid_or_expired_token", rec.Body.String())
	}

	// The link from the email completes the round trip over HTTP.
	parsed, err := url.Parse(mail.sent[0].ResetURL)
	if err != nil {
		t.Fatalf("ResetURL is not a valid URL: %v", err)
	}
	clearToken := parsed.Query().Get("token")

	// A too-short password is rejected without burning the link...
	if rec := post(h.ResetPassword, `{"token":"`+clearToken+`","new_password":"short"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password status = %d, want 400", rec.Code)
	}
	// ...so the very same link still works.
	if rec := post(h.ResetPassword, `{"token":"`+clearToken+`","new_password":"HttpPass456"}`); rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	// And it is single-use.
	if rec := post(h.ResetPassword, `{"token":"`+clearToken+`","new_password":"HttpPass789"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("replayed link status = %d, want 400", rec.Code)
	}

	var stored string
	if err := db.QueryRowContext(ctx, `SELECT password FROM users WHERE user_id = $1`, userID).Scan(&stored); err != nil {
		t.Fatalf("read password: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte("HttpPass456")) != nil {
		t.Fatal("the password set over HTTP was not applied")
	}
}
