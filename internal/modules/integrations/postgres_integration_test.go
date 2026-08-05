//go:build postgres_integration

package integrations

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestIntegrationsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-integ-m1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_uber_eats_products_mapping WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo_products_mapping WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_uber_eats WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM qrcodes WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM stripe_accounts WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	// --- seed merchant_parameters (joined by GetScanNOrderIntegration / UpdateScanNOrderSettings) ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, primary_color, delivery_distance_limit)
		VALUES ($1, $2, '#000000', 5000)`, merchantID, time.Now().UTC()); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	// --- seed integration_uber_eats + one paid order for KPIs ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO integration_uber_eats (
			merchant_id, store_id, pos_provisionning_refresh_token,
			pos_provisionning_token_expiration_date, delay_duration,
			auto_accept_orders, enabled, commission_rate
		) VALUES ($1, 'store-1', 'refresh-tok', $2, 0, true, true, 12)`,
		merchantID, time.Now().UTC()); err != nil {
		t.Fatalf("seed integration_uber_eats: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, price, tva, ht, created_by, ispaid)
		VALUES ($1, 1, 'UBER_EATS', 'ACCEPTED', 2500, 200, 2300, 'itest', true)`,
		merchantID); err != nil {
		t.Fatalf("seed uber eats order: %v", err)
	}

	repo := NewRepository(db)

	ue, err := repo.GetUberEatsIntegration(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetUberEatsIntegration failed against postgres: %v", err)
	}
	if ue == nil {
		t.Fatal("expected uber eats integration row")
	}
	if !ue.Active || ue.KPIs.Revenue != 2500 || ue.KPIs.Orders != 1 || ue.KPIs.AvgBasket != 2500 {
		t.Fatalf("unexpected uber eats KPIs: %+v", ue)
	}

	newCommission := 18
	newAutoAccept := false
	newPrep := 25
	if err := repo.UpdateUberEatsSettings(ctx, merchantID, &newCommission, &newAutoAccept, &newPrep); err != nil {
		t.Fatalf("UpdateUberEatsSettings failed against postgres: %v", err)
	}
	ue, err = repo.GetUberEatsIntegration(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetUberEatsIntegration (after update) failed: %v", err)
	}
	if ue.CommissionRate != 18 || ue.AutoAcceptOrders || ue.PreparationTimeMinutes != 25 {
		t.Fatalf("UpdateUberEatsSettings did not apply: %+v", ue)
	}

	// --- Deliveroo ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO integration_deliveroo (merchant_id, location_id, brand_id, enabled, auto_accept_orders, commission_rate)
		VALUES ($1, 'loc-1', 'brand-1', true, false, 10)`, merchantID); err != nil {
		t.Fatalf("seed integration_deliveroo: %v", err)
	}
	dl, err := repo.GetDeliverooIntegration(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetDeliverooIntegration failed against postgres: %v", err)
	}
	if dl == nil || !dl.Active || dl.KPIs.Orders != 0 {
		t.Fatalf("unexpected deliveroo integration: %+v", dl)
	}

	dlCommission := 20
	if err := repo.UpdateDeliverooSettings(ctx, merchantID, &dlCommission, nil, nil); err != nil {
		t.Fatalf("UpdateDeliverooSettings failed against postgres: %v", err)
	}
	if err := repo.DisableDeliveroo(ctx, merchantID); err != nil {
		t.Fatalf("DisableDeliveroo failed against postgres: %v", err)
	}
	dl, err = repo.GetDeliverooIntegration(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetDeliverooIntegration (after disable) failed: %v", err)
	}
	if dl.Active || dl.CommissionRate != 20 {
		t.Fatalf("expected deliveroo disabled with commission 20, got %+v", dl)
	}

	// --- ScanNOrder ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, activated)
		VALUES ($1, 'title', 'desc', 'kw', 'french', true)`, merchantID); err != nil {
		t.Fatalf("seed scannorder_settings: %v", err)
	}
	sno, err := repo.GetScanNOrderIntegration(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetScanNOrderIntegration failed against postgres: %v", err)
	}
	if sno == nil || !sno.Active || sno.PrimaryColor != "#000000" || sno.DeliveryDistanceLimit != 5000 {
		t.Fatalf("unexpected scannorder integration: %+v", sno)
	}
	// pas encore de QR code -> slug vide, donc pas d'access_url
	if sno.slug != "" {
		t.Fatalf("expected empty slug without qrcodes row, got %q", sno.slug)
	}

	// --- slug / access_url ---
	// QR serveur et QR supprimé : ignorés, seul le QR principal fait le slug
	if _, err := db.ExecContext(ctx, `
		INSERT INTO qrcodes (merchant_id, code, user_id, deleted) VALUES ($1, 'itest-integ-waiter', 'u-1', false)`,
		merchantID); err != nil {
		t.Fatalf("seed qrcode serveur: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO qrcodes (merchant_id, code, deleted) VALUES ($1, 'itest-integ-deleted', true)`,
		merchantID); err != nil {
		t.Fatalf("seed qrcode supprimé: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO qrcodes (merchant_id, code, deleted) VALUES ($1, 'itest-integ-slug', false)`,
		merchantID); err != nil {
		t.Fatalf("seed qrcode principal: %v", err)
	}
	sno, err = repo.GetScanNOrderIntegration(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetScanNOrderIntegration (slug) failed: %v", err)
	}
	if sno.slug != "itest-integ-slug" {
		t.Fatalf("expected slug itest-integ-slug, got %q", sno.slug)
	}

	// bout en bout : le service assemble access_url à partir de la base URL
	svc := NewService(db, nil, nil, nil, "", "", "https://sno.itest/")
	snoSvc, err := svc.GetScanNOrder(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetScanNOrder (service) failed: %v", err)
	}
	if snoSvc.AccessURL == nil || *snoSvc.AccessURL != "https://sno.itest/restaurant/itest-integ-slug" {
		t.Fatalf("unexpected access_url: %v", snoSvc.AccessURL)
	}

	if err := repo.UpdateScanNOrderImageURL(ctx, merchantID, "logo_url", "https://cdn.example.com/logo.png"); err != nil {
		t.Fatalf("UpdateScanNOrderImageURL failed against postgres: %v", err)
	}
	logoURL, err := repo.GetScanNOrderCurrentImageURL(ctx, merchantID, "logo_url")
	if err != nil {
		t.Fatalf("GetScanNOrderCurrentImageURL failed against postgres: %v", err)
	}
	if logoURL != "https://cdn.example.com/logo.png" {
		t.Fatalf("unexpected logo url: %q", logoURL)
	}

	closedUntil := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	if err := repo.SetScanNOrderClosedUntil(ctx, merchantID, closedUntil); err != nil {
		t.Fatalf("SetScanNOrderClosedUntil failed against postgres: %v", err)
	}

	newColor := "#abcdef"
	newLimit := 8000
	deliveryOff := false
	if err := repo.UpdateScanNOrderSettings(ctx, merchantID, &UpdateScanNOrderRequest{
		PrimaryColor:          &newColor,
		DeliveryDistanceLimit: &newLimit,
		DeliveryEnabled:       &deliveryOff,
	}); err != nil {
		t.Fatalf("UpdateScanNOrderSettings failed against postgres: %v", err)
	}
	sno, err = repo.GetScanNOrderIntegration(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetScanNOrderIntegration (after update) failed: %v", err)
	}
	if sno.PrimaryColor != newColor || sno.DeliveryDistanceLimit != newLimit || sno.DeliveryEnabled {
		t.Fatalf("UpdateScanNOrderSettings did not apply: %+v", sno)
	}
	if sno.ClosedUntil == nil || !sno.ClosedUntil.Equal(closedUntil) {
		t.Fatalf("expected closed_until %v, got %v", closedUntil, sno.ClosedUntil)
	}

	// --- Stripe Connect ---
	if err := repo.UpsertStripeAccountID(ctx, merchantID, "acct_itest_1"); err != nil {
		t.Fatalf("UpsertStripeAccountID (insert branch) failed against postgres: %v", err)
	}
	accountID, err := repo.GetStripeAccountID(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetStripeAccountID failed against postgres: %v", err)
	}
	if accountID != "acct_itest_1" {
		t.Fatalf("expected acct_itest_1, got %q", accountID)
	}

	if err := repo.UpsertStripeAccountID(ctx, merchantID, "acct_itest_2"); err != nil {
		t.Fatalf("UpsertStripeAccountID (update branch) failed against postgres: %v", err)
	}
	accountID, err = repo.GetStripeAccountID(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetStripeAccountID (after update) failed: %v", err)
	}
	if accountID != "acct_itest_2" {
		t.Fatalf("expected acct_itest_2, got %q", accountID)
	}

	if err := repo.UpdateStripeVerificationStatus(ctx, "acct_itest_2", "verified"); err != nil {
		t.Fatalf("UpdateStripeVerificationStatus failed against postgres: %v", err)
	}

	branding, err := repo.GetStripeBrandingData(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetStripeBrandingData failed against postgres: %v", err)
	}
	if branding.AccountID != "acct_itest_2" || branding.PrimaryColor != newColor {
		t.Fatalf("unexpected branding data: %+v", branding)
	}
}
