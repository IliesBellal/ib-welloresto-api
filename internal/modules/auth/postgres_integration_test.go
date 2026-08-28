//go:build postgres_integration

package auth

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
)

func TestAuthRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const userID = "itest-auth-user-1"
	const rightsToken = "itest-rights-token-1"
	const deviceID = "itest-device-1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_devices WHERE device_id = $1`, deviceID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if merchantIntID != 0 {
			merchantID := strconv.FormatInt(merchantIntID, 10)
			_, _ = db.ExecContext(ctx, `DELETE FROM app_version_merchant WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM app_version WHERE app_id = 'itest-app'`)
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest Auth Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-auth', 'https://example.com', '0600000000', 'mtok', 'Europe/Paris', 1.0, 2.0)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open)
		VALUES ($1, $2, 'EUR', true)`, merchantID, time.Now().UTC()); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, activated)
		VALUES ($1, 't', 'd', 'k', 'french', true)`, merchantID); err != nil {
		t.Fatalf("seed scannorder_settings: %v", err)
	}

	// A merchant always has a package/subscription in practice — seeded here so
	// the LEFT JOIN p/s columns that aren't wrapped in COALESCE (
	// resolve to real values instead of NULL.
	var packageIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO packages (package_name, stripe_price_id, kiosks_enabled)
		VALUES ('ITest Package', 'price_itest', true) RETURNING id`).Scan(&packageIntID); err != nil {
		t.Fatalf("seed packages: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM packages WHERE id = $1`, packageIntID) })
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (stripe_subscription_id, merchant_id, package_id, kiosks_enabled)
		VALUES ('itest-sub-1', $1, $2, true)`, merchantID, packageIntID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID) })

	passwordHash, err := helpers.HashPassword("itest-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
		VALUES ($1, 'ITest User', 'ITest', 'User', $2, 'itest-auth@example.com', '+33600000000', 'user-tok', true)`, userID, passwordHash); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, enabled, login_enabled, pin_hash)
		VALUES ($1, $2, $3, true, true, 'itest-pin-hash')`, userID, merchantID, rightsToken); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}

	repo := NewAuthRepository(db)

	// GetUserByToken: 7-way cross-type CAST join fix + COALESCE(bool, FALSE) fix.
	got, err := repo.GetUserByToken(ctx, rightsToken)
	if err != nil {
		t.Fatalf("GetUserByToken failed against postgres: %v", err)
	}
	if got == nil {
		t.Fatal("expected a user for the seeded token")
	}
	if got.UserID != userID || got.MerchantID != merchantID || got.Currency != "EUR" || !got.IsOpen {
		t.Fatalf("unexpected user row: %+v", got)
	}
	if got.RoleID != nil {
		t.Fatalf("expected nil RoleID before any role is attached, got %v", *got.RoleID)
	}
	if len(got.Permissions) != 0 {
		t.Fatalf("expected no permissions before any role is attached, got %v", got.Permissions)
	}

	// RBAC lot 2: attach a role carrying two permissions to this user's
	// users_rights link, and confirm all three constructors (GetUserByToken,
	// Login, GetUserByPIN) load Permissions the same way — via the second
	// query in attachRolePermissions, never aggregated in SQL.
	roleID := "itest-role-" + userID
	if _, err := db.ExecContext(ctx, `
		INSERT INTO roles (id, merchant_id, name) VALUES ($1, $2, 'ITest Role')`, roleID, merchantID); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE id = $1`, roleID) })
	for _, key := range []string{"pos.access", "catalog.manage"} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO role_permissions (role_id, permission_key) VALUES ($1, $2)`, roleID, key); err != nil {
			t.Fatalf("seed role_permission %s: %v", key, err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE users_rights SET role_id = $1 WHERE user_id = $2`, roleID, userID); err != nil {
		t.Fatalf("attach role to users_rights: %v", err)
	}

	gotWithRole, err := repo.GetUserByToken(ctx, rightsToken)
	if err != nil {
		t.Fatalf("GetUserByToken (with role) failed: %v", err)
	}
	if gotWithRole.RoleID == nil || *gotWithRole.RoleID != roleID {
		t.Fatalf("expected RoleID %q, got %v", roleID, gotWithRole.RoleID)
	}
	if len(gotWithRole.Permissions) != 2 {
		t.Fatalf("expected 2 permissions, got %v", gotWithRole.Permissions)
	}

	loginWithRole, err := repo.Login(ctx, "", "", rightsToken)
	if err != nil {
		t.Fatalf("Login (with role) failed: %v", err)
	}
	if len(loginWithRole.Permissions) != 2 {
		t.Fatalf("expected Login to load 2 permissions too, got %v", loginWithRole.Permissions)
	}

	pinWithRole, err := repo.GetUserByPIN(ctx, merchantID, "itest-pin-hash")
	if err != nil {
		t.Fatalf("GetUserByPIN (with role) failed: %v", err)
	}
	if pinWithRole == nil || len(pinWithRole.Permissions) != 2 {
		t.Fatalf("expected GetUserByPIN to load 2 permissions too, got %+v", pinWithRole)
	}

	// Detach the role: Permissions must go back to empty, not stay stale from
	// a previous role (there is no caching inside the repository itself).
	if _, err := db.ExecContext(ctx, `UPDATE users_rights SET role_id = NULL WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("detach role: %v", err)
	}
	gotWithoutRole, err := repo.GetUserByToken(ctx, rightsToken)
	if err != nil {
		t.Fatalf("GetUserByToken (role detached) failed: %v", err)
	}
	if gotWithoutRole.RoleID != nil || len(gotWithoutRole.Permissions) != 0 {
		t.Fatalf("expected no role and no permissions after detaching, got RoleID=%v Permissions=%v", gotWithoutRole.RoleID, gotWithoutRole.Permissions)
	}

	// Login by token (same CAST join + boolean fixes, different column set).
	loginRow, err := repo.Login(ctx, "", "", rightsToken)
	if err != nil {
		t.Fatalf("Login (by token) failed against postgres: %v", err)
	}
	if loginRow.UserID != userID {
		t.Fatalf("unexpected login row: %+v", loginRow)
	}

	// Login by email (exercises the UPPER(...)=UPPER(?) branch, not just token).
	loginRow, err = repo.Login(ctx, "itest-auth@example.com", "itest-password", "")
	if err != nil {
		t.Fatalf("Login (by email) failed against postgres: %v", err)
	}
	if loginRow.UserID != userID {
		t.Fatalf("unexpected login-by-email row: %+v", loginRow)
	}

	// GetUserByPIN: enabled/login_enabled boolean literal fix.
	pinRow, err := repo.GetUserByPIN(ctx, merchantID, "itest-pin-hash")
	if err != nil {
		t.Fatalf("GetUserByPIN failed against postgres: %v", err)
	}
	if pinRow == nil || pinRow.UserID != userID {
		t.Fatalf("unexpected PIN row: %+v", pinRow)
	}

	// Disable the link and confirm GetUserByPIN's boolean filter still excludes it.
	if _, err := db.ExecContext(ctx, `UPDATE users_rights SET enabled = false WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("disable link: %v", err)
	}
	pinRow, err = repo.GetUserByPIN(ctx, merchantID, "itest-pin-hash")
	if err != nil {
		t.Fatalf("GetUserByPIN (disabled) failed against postgres: %v", err)
	}
	if pinRow != nil {
		t.Fatalf("expected nil for disabled link, got %+v", pinRow)
	}
	if _, err := db.ExecContext(ctx, `UPDATE users_rights SET enabled = true WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("re-enable link: %v", err)
	}

	if err := repo.UpdatePassword(ctx, userID, "new-hash"); err != nil {
		t.Fatalf("UpdatePassword failed against postgres: %v", err)
	}

	if err := repo.SetPINHash(ctx, merchantID, userID, ptr("itest-pin-hash-2")); err != nil {
		t.Fatalf("SetPINHash failed against postgres: %v", err)
	}
	conflict, err := repo.CheckPINConflict(ctx, merchantID, "itest-pin-hash-2", "someone-else")
	if err != nil {
		t.Fatalf("CheckPINConflict failed against postgres: %v", err)
	}
	if !conflict {
		t.Fatal("expected a PIN conflict")
	}

	// GetMerchants: cross-type CAST join fix.
	merchants, err := repo.GetMerchants(ctx, userID)
	if err != nil {
		t.Fatalf("GetMerchants failed against postgres: %v", err)
	}
	if len(merchants) != 1 || merchants[0].MerchantID != merchantID {
		t.Fatalf("unexpected merchants: %+v", merchants)
	}

	// CheckAppVersion: dbx.UTCNow() fix (release_date < now()).
	if _, err := db.ExecContext(ctx, `
		INSERT INTO app_version (app_id, version_code, last_functional_version_code, download_url, release_date)
		VALUES ('itest-app', 5, 5, 'https://example.com/app.apk', $1)`, time.Now().UTC().Add(-1*time.Hour)); err != nil {
		t.Fatalf("seed app_version: %v", err)
	}
	result, err := repo.CheckAppVersion(ctx, 1, "itest-app", merchantID)
	if err != nil {
		t.Fatalf("CheckAppVersion failed against postgres: %v", err)
	}
	if result["status"] != "update_available" {
		t.Fatalf("expected update_available, got %+v", result)
	}

	// SaveDevice: ON DUPLICATE KEY UPDATE -> ON CONFLICT rewrite (insert then update branch).
	if err := repo.SaveDevice(ctx, userID, merchantID, "pos", deviceID, "fcm-token-1"); err != nil {
		t.Fatalf("SaveDevice (insert) failed against postgres: %v", err)
	}
	if err := repo.SaveDevice(ctx, userID, merchantID, "pos", deviceID, "fcm-token-2"); err != nil {
		t.Fatalf("SaveDevice (update) failed against postgres: %v", err)
	}
	var fcmToken string
	if err := db.QueryRowContext(ctx, `SELECT fcm_token FROM users_devices WHERE device_id = $1`, deviceID).Scan(&fcmToken); err != nil {
		t.Fatalf("read back device: %v", err)
	}
	if fcmToken != "fcm-token-2" {
		t.Fatalf("expected fcm-token-2 after upsert, got %q", fcmToken)
	}

	// UpdateMFAStatus / MarkAsOTPSent / MarkAsMFAVerified / MarkLastLoginAt: dbx.UTCNow() fixes.
	if err := repo.UpdateMFAStatus(ctx, userID, "PENDING"); err != nil {
		t.Fatalf("UpdateMFAStatus failed against postgres: %v", err)
	}
	if err := repo.MarkAsOTPSent(ctx, userID); err != nil {
		t.Fatalf("MarkAsOTPSent failed against postgres: %v", err)
	}
	if err := repo.MarkAsMFAVerified(ctx, userID); err != nil {
		t.Fatalf("MarkAsMFAVerified failed against postgres: %v", err)
	}
	if err := repo.MarkLastLoginAt(ctx, userID); err != nil {
		t.Fatalf("MarkLastLoginAt failed against postgres: %v", err)
	}
	var mfaStatus string
	var mfaOTPSentAt, lastLoginAt *time.Time
	if err := db.QueryRowContext(ctx, `SELECT mfa_status, mfa_otp_sent_at, last_login_at FROM users WHERE user_id = $1`, userID).
		Scan(&mfaStatus, &mfaOTPSentAt, &lastLoginAt); err != nil {
		t.Fatalf("read back user MFA/login state: %v", err)
	}
	if mfaOTPSentAt == nil || lastLoginAt == nil {
		t.Fatalf("expected mfa_otp_sent_at and last_login_at to be set, got %v / %v", mfaOTPSentAt, lastLoginAt)
	}

	// MarkAsVerified: UPDATE...JOIN -> EXISTS rewrite.
	if err := repo.MarkAsVerified(ctx, rightsToken, "EMAIL"); err != nil {
		t.Fatalf("MarkAsVerified (EMAIL) failed against postgres: %v", err)
	}
	var emailVerifiedAt *time.Time
	if err := db.QueryRowContext(ctx, `SELECT email_verified_at FROM users WHERE user_id = $1`, userID).Scan(&emailVerifiedAt); err != nil {
		t.Fatalf("read back email_verified_at: %v", err)
	}
	if emailVerifiedAt == nil {
		t.Fatal("expected email_verified_at to be set after MarkAsVerified")
	}

	if err := repo.MarkAsVerified(ctx, "no-such-token", "EMAIL"); err == nil {
		t.Fatal("expected an error for an unknown token")
	}
}

func ptr(s string) *string { return &s }
