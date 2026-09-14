//go:build postgres_integration

package subscriptions

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func cleanupOverrides(t *testing.T, db *sql.DB, merchantID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM subscription_overrides WHERE merchant_id = $1`, merchantID)
	})
}

// readSubscriptionFlag reads one boolean/int column of subscriptions for
// merchantID — used to assert an override's side-effect write landed.
func readSubscriptionBool(t *testing.T, db *sql.DB, merchantID, column string) bool {
	t.Helper()
	var v bool
	if err := db.QueryRowContext(context.Background(), `SELECT `+column+` FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&v); err != nil {
		t.Fatalf("read subscriptions.%s: %v", column, err)
	}
	return v
}

func readSubscriptionInt(t *testing.T, db *sql.DB, merchantID, column string) int {
	t.Helper()
	var v int
	if err := db.QueryRowContext(context.Background(), `SELECT `+column+` FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&v); err != nil {
		t.Fatalf("read subscriptions.%s: %v", column, err)
	}
	return v
}

// TestCreateOverride_Module_Postgres — B1d: a 'module' override flips the
// mapped subscriptions.*_enabled column without creating a subscription_items
// row (access without billing, §7.1's two axes diverging on purpose).
func TestCreateOverride_Module_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-ov-module"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupOverrides(t, db, merchantID)

	svc := newTestService(db)
	note := "test LOT B B1d"
	override, err := svc.CreateOverride(ctx, merchantID, OverrideKindModule, CodeHACCP, OverrideReasonTest, &note, "itest-staff-user", nil, nil)
	if err != nil {
		t.Fatalf("CreateOverride(module haccp): %v", err)
	}
	if override.ID == "" || override.Kind != OverrideKindModule || override.Target != CodeHACCP {
		t.Fatalf("CreateOverride = %+v, unexpected shape", override)
	}

	if !readSubscriptionBool(t, db, merchantID, "haccp_enabled") {
		t.Fatal("subscriptions.haccp_enabled = false, want true after module override")
	}

	items, err := svc.repo.ListActive(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("ListActive = %+v, want no subscription_items row — module override grants access without billing", items)
	}
}

// TestCreateOverride_Price_Postgres — B1d: a 'price' override writes
// subscriptions.override_price_cents.
func TestCreateOverride_Price_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-ov-price"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupOverrides(t, db, merchantID)

	svc := newTestService(db)
	if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "5000", OverrideReasonCommercial, nil, "itest-staff-user", nil, nil); err != nil {
		t.Fatalf("CreateOverride(price): %v", err)
	}

	var got sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT override_price_cents FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&got); err != nil {
		t.Fatalf("read override_price_cents: %v", err)
	}
	if !got.Valid || got.Int64 != 5000 {
		t.Fatalf("override_price_cents = %+v, want 5000", got)
	}
}

// TestCreateOverride_KioskQuota_Postgres — B1d: a 'kiosk_quota' override
// writes subscriptions.max_kiosks directly — orthogonal to the P3 guard
// below, since it's the pre-existing operational quota
// (kiosk.Repository.GetMerchantMaxKiosks), not a subscription_items billing
// code.
func TestCreateOverride_KioskQuota_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-ov-kioskquota"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupOverrides(t, db, merchantID)

	svc := newTestService(db)
	if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindKioskQuota, "7", OverrideReasonGeste, nil, "itest-staff-user", nil, nil); err != nil {
		t.Fatalf("CreateOverride(kiosk_quota): %v", err)
	}
	if got := readSubscriptionInt(t, db, merchantID, "max_kiosks"); got != 7 {
		t.Fatalf("max_kiosks = %d, want 7", got)
	}
}

// TestCreateOverride_RejectsNonBillableModule_Postgres — PRÉALABLE P3: no
// subscription_overrides may target kiosk or sms via a 'module' override
// while pricing_catalog can't price them.
func TestCreateOverride_RejectsNonBillableModule_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-ov-p3-guard"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupOverrides(t, db, merchantID)

	svc := newTestService(db)
	for _, code := range []string{CodeKiosk, CodeSMS} {
		if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindModule, code, OverrideReasonCommercial, nil, "itest-staff-user", nil, nil); !errors.Is(err, models.ErrSubscriptionItemPriceUnavailable) {
			t.Fatalf("CreateOverride(module, %s) = %v, want ErrSubscriptionItemPriceUnavailable", code, err)
		}
	}

	if got := readSubscriptionBool(t, db, merchantID, "kiosks_enabled"); got {
		t.Fatal("kiosks_enabled = true, want unchanged (false) — the guarded override must not have applied")
	}
}

// TestCreateOverride_UnsupportedModuleTarget_Postgres — "marketplaces" has no
// subscriptions.*_enabled column yet (see
// migrations/todo/139_subscription_overrides.up.sql's table comment) — must
// be rejected explicitly, not silently no-op.
func TestCreateOverride_UnsupportedModuleTarget_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-ov-unsupported-target"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupOverrides(t, db, merchantID)

	svc := newTestService(db)
	if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindModule, CodeMarketplaces, OverrideReasonCommercial, nil, "itest-staff-user", nil, nil); !errors.Is(err, models.ErrOverrideTargetUnsupported) {
		t.Fatalf("CreateOverride(module, marketplaces) = %v, want ErrOverrideTargetUnsupported", err)
	}
}

// TestCreateOverride_InvalidKindAndReason_Postgres.
func TestCreateOverride_InvalidKindAndReason_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-ov-invalid"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupOverrides(t, db, merchantID)

	svc := newTestService(db)
	if _, err := svc.CreateOverride(ctx, merchantID, "bogus_kind", CodeHACCP, OverrideReasonTest, nil, "itest-staff-user", nil, nil); !errors.Is(err, models.ErrInvalidOverrideKind) {
		t.Fatalf("CreateOverride(bogus kind) = %v, want ErrInvalidOverrideKind", err)
	}
	if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindModule, CodeHACCP, "bogus_reason", nil, "itest-staff-user", nil, nil); !errors.Is(err, models.ErrInvalidOverrideReason) {
		t.Fatalf("CreateOverride(bogus reason) = %v, want ErrInvalidOverrideReason", err)
	}
}

// TestRevokeOverride_Postgres — DELETE .../overrides/{id}: sets revoked_at,
// disappears from ListActiveOverrides, a second revoke of the same id fails.
func TestRevokeOverride_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-ov-revoke"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupOverrides(t, db, merchantID)

	svc := newTestService(db)
	override, err := svc.CreateOverride(ctx, merchantID, OverrideKindModule, CodeDelivery, OverrideReasonMigration, nil, "itest-staff-user", nil, nil)
	if err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}

	active, err := svc.ListActiveOverrides(ctx)
	if err != nil {
		t.Fatalf("ListActiveOverrides: %v", err)
	}
	found := false
	for _, o := range active {
		if o.ID == override.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("override %s not found in ListActiveOverrides before revocation", override.ID)
	}

	if err := svc.RevokeOverride(ctx, override.ID); err != nil {
		t.Fatalf("RevokeOverride: %v", err)
	}

	active, err = svc.ListActiveOverrides(ctx)
	if err != nil {
		t.Fatalf("ListActiveOverrides after revoke: %v", err)
	}
	for _, o := range active {
		if o.ID == override.ID {
			t.Fatalf("override %s still active after revocation", override.ID)
		}
	}

	if err := svc.RevokeOverride(ctx, override.ID); !errors.Is(err, models.ErrOverrideNotFound) {
		t.Fatalf("second RevokeOverride = %v, want ErrOverrideNotFound", err)
	}
	if err := svc.RevokeOverride(ctx, "does-not-exist"); !errors.Is(err, models.ErrOverrideNotFound) {
		t.Fatalf("RevokeOverride(unknown id) = %v, want ErrOverrideNotFound", err)
	}
}
