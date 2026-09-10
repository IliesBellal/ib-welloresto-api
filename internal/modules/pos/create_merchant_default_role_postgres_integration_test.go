//go:build postgres_integration

package pos

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// LOT A Semaine 1, Chantier 4 (docs/decisions.md) : merchant.default_role_id
// must point at the "staff" system role after CreateMerchant — every member
// created afterwards without an explicit role_id used to land as
// Administrateur by default. The owner (req.UserID, req.Admin: true) must
// still receive the "admin" role explicitly on their own users_rights row;
// only the merchant-wide default changes.
func TestPOSService_CreateMerchant_DefaultRoleIsStaff_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const ownerUserID = "itest-pos-defrole-owner"
	var merchantIntID int64
	var packageIntID int64

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, ownerUserID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, ownerUserID)
		if merchantIntID != 0 {
			merchantID := strconv.FormatInt(merchantIntID, 10)
			_, _ = db.ExecContext(ctx, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `UPDATE merchant SET default_role_id = NULL WHERE id = $1`, merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM qrcodes WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if packageIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM packages WHERE id = $1`, packageIntID)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO packages (package_name, stripe_price_id, kiosks_enabled)
		VALUES ('ITest POS DefRole Package', 'price_itest_pos_defrole', true) RETURNING id`).Scan(&packageIntID); err != nil {
		t.Fatalf("seed package: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
		VALUES ($1, 'ITest POS DefRole Owner', 'ITest', 'Owner', 'hash', 'itest-pos-defrole-owner@example.com', '+33600000099', 'user-tok-pos-defrole', true)`,
		ownerUserID); err != nil {
		t.Fatalf("seed owner user: %v", err)
	}

	repo := NewPOSRepository(db)
	svc := NewPOSService(repo, nil)

	resp, err := svc.CreateMerchant(ctx, CreateMerchantRequest{
		FullName: "ITest POS DefRole Merchant", Address: "a", StreetNumber: "1", Street: "s",
		ZipCode: "75001", City: "Paris", SIRET: "siret-pos-defrole", Tel: "06",
		WebSite: "https://x", Email: "itest-pos-defrole@example.com",
		PackageID: strconv.FormatInt(packageIntID, 10),
		UserID:    ownerUserID, Admin: true,
	})
	if err != nil {
		t.Fatalf("CreateMerchant: %v", err)
	}
	merchantIntID, _ = strconv.ParseInt(resp.MerchantID, 10, 64)
	if merchantIntID == 0 {
		t.Fatalf("expected a non-zero merchant id, got %q", resp.MerchantID)
	}

	var defaultRoleID, defaultRoleSystemKey string
	if err := db.QueryRowContext(ctx, `
		SELECT m.default_role_id, r.system_key
		FROM merchant m
		JOIN roles r ON r.id = m.default_role_id
		WHERE m.id = $1`, merchantIntID).Scan(&defaultRoleID, &defaultRoleSystemKey); err != nil {
		t.Fatalf("read back merchant.default_role_id: %v", err)
	}
	if defaultRoleSystemKey != "staff" {
		t.Fatalf("merchant.default_role_id system_key = %q, want %q", defaultRoleSystemKey, "staff")
	}

	var ownerRoleSystemKey string
	var ownerAdminFlag bool
	if err := db.QueryRowContext(ctx, `
		SELECT r.system_key, ur.admin
		FROM users_rights ur
		JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = $1 AND ur.merchant_id = $2`, ownerUserID, resp.MerchantID).Scan(&ownerRoleSystemKey, &ownerAdminFlag); err != nil {
		t.Fatalf("read back owner's users_rights: %v", err)
	}
	if ownerRoleSystemKey != "admin" {
		t.Fatalf("owner's role system_key = %q, want %q (owner must stay Administrateur despite the new staff default)", ownerRoleSystemKey, "admin")
	}
	if !ownerAdminFlag {
		t.Fatal("owner's users_rights.admin = false, want true")
	}
}
