//go:build postgres_integration

package messaggio

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestMarketingRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-msg-m1"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_sms_monthly WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_marketing_settings WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM qrcodes WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	_, err := db.ExecContext(ctx, `
		INSERT INTO merchant_marketing_settings (merchant_id, sms_enabled, sms_unit_price, tracking_template)
		VALUES ($1, TRUE, 7, 'template {tracking_url}')`, merchantID)
	if err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO qrcodes (merchant_id, menu_only, enabled, code, creation_date)
		VALUES ($1, FALSE, TRUE, 'itest-qr-code', NULL)`, merchantID)
	if err != nil {
		t.Fatalf("seed qrcodes: %v", err)
	}

	repo := NewMarketingRepository(db)

	settings, err := repo.GetMarketingSettings(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetMarketingSettings failed against postgres: %v", err)
	}
	if !settings.SMSEnabled || settings.QRCode != "itest-qr-code" || settings.SMSUnitPrice != 7 {
		t.Fatalf("unexpected settings: %+v", settings)
	}

	// Premier enregistrement : INSERT, puis second : branche DO UPDATE (upsert)
	if err := repo.RecordSMSCost(ctx, merchantID, 3, 7); err != nil {
		t.Fatalf("RecordSMSCost (insert) failed against postgres: %v", err)
	}
	if err := repo.RecordSMSCost(ctx, merchantID, 2, 7); err != nil {
		t.Fatalf("RecordSMSCost (upsert) failed against postgres: %v", err)
	}

	var smsCount, totalCost int
	err = db.QueryRowContext(ctx, `
		SELECT sms_count, total_cost FROM merchant_sms_monthly
		WHERE merchant_id = $1`, merchantID).Scan(&smsCount, &totalCost)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if smsCount != 5 || totalCost != 35 {
		t.Fatalf("expected 5 sms / cost 35, got %d / %d", smsCount, totalCost)
	}
}
