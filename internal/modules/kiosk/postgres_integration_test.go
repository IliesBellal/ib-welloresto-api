//go:build postgres_integration

package kiosk

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestKioskRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		kioskID   = "itest-kiosk-1"
		codeID    = "itest-kiosk-code-1"
		tokenID   = "itest-kiosk-tok-1"
		tokenID2  = "itest-kiosk-tok-2"
		codeHash  = "itest-kiosk-code-hash"
		tokenHash = "itest-kiosk-token-hash"
	)
	var merchantID string

	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosk_device_tokens WHERE kiosk_id = $1`, kioskID)
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosk_enrollment_codes WHERE id = $1`, codeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosks WHERE id = $1`, kioskID)
		if mid == "" {
			return
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosk_settings WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts_schedules WHERE discount_id = 'itest-kiosk-disc'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_Id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = 'itest-kiosk-attr'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attributes WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM qrcodes WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM stripe_accounts WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM subscriptions WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, mid)
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-kiosk' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest Kiosk Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-kiosk', 'https://x', '06', 'mtok-kiosk', 'Europe/Paris', 1, 2)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	repo := NewRepository(db)

	// --- enrôlement ---
	if err := repo.CreateEnrollmentCode(ctx, codeID, merchantID, codeHash, time.Now().UTC().Add(15*time.Minute), "itest-admin"); err != nil {
		t.Fatalf("CreateEnrollmentCode failed against postgres: %v", err)
	}
	code, err := repo.GetEnrollmentCodeByHash(ctx, codeHash)
	if err != nil || code == nil || code.ID != codeID {
		t.Fatalf("GetEnrollmentCodeByHash = (%+v, %v)", code, err)
	}
	pending, err := repo.ListPendingEnrollmentCodes(ctx, merchantID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPendingEnrollmentCodes = (%d, %v), want 1", len(pending), err)
	}
	byID, err := repo.GetEnrollmentCodeByID(ctx, merchantID, codeID)
	if err != nil || byID == nil {
		t.Fatalf("GetEnrollmentCodeByID = (%+v, %v)", byID, err)
	}

	// --- création borne + token ---
	if _, err := repo.CreateKiosk(ctx, kioskID, merchantID, "Borne 1", "iPad", "17.5", []byte{0x01, 0x02}, nil); err != nil {
		t.Fatalf("CreateKiosk failed against postgres: %v", err)
	}
	if err := repo.MarkEnrollmentCodeUsed(ctx, codeID, kioskID); err != nil {
		t.Fatalf("MarkEnrollmentCodeUsed failed against postgres: %v", err)
	}
	pending, err = repo.ListPendingEnrollmentCodes(ctx, merchantID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected no pending codes after use, got (%d, %v)", len(pending), err)
	}

	if err := repo.CreateDeviceToken(ctx, tokenID, kioskID, tokenHash, time.Now().UTC().Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateDeviceToken failed against postgres: %v", err)
	}
	tok, err := repo.GetDeviceTokenByHash(ctx, tokenHash)
	if err != nil || tok == nil || tok.ID != tokenID {
		t.Fatalf("GetDeviceTokenByHash = (%+v, %v)", tok, err)
	}
	if err := repo.UpdateDeviceTokenLastUsed(ctx, tokenID); err != nil {
		t.Fatalf("UpdateDeviceTokenLastUsed failed against postgres: %v", err)
	}
	if err := repo.RotateDeviceToken(ctx, tokenID, tokenID2, kioskID, tokenHash+"-2", time.Now().UTC().Add(24*time.Hour)); err != nil {
		t.Fatalf("RotateDeviceToken failed against postgres: %v", err)
	}
	if err := repo.RevokeAllDeviceTokens(ctx, kioskID); err != nil {
		t.Fatalf("RevokeAllDeviceTokens failed against postgres: %v", err)
	}

	// --- vie de la borne ---
	if err := repo.UpdateKioskHeartbeat(ctx, kioskID, "1.2.3", "10.0.0.1"); err != nil {
		t.Fatalf("UpdateKioskHeartbeat failed against postgres: %v", err)
	}
	if err := repo.UpdateKioskLastError(ctx, kioskID, "printer_offline"); err != nil {
		t.Fatalf("UpdateKioskLastError failed against postgres: %v", err)
	}
	if err := repo.UpdateKioskName(ctx, kioskID, "Borne entrée"); err != nil {
		t.Fatalf("UpdateKioskName failed against postgres: %v", err)
	}
	if err := repo.UpdateKioskAdminPinEncrypted(ctx, kioskID, []byte{0x03}); err != nil {
		t.Fatalf("UpdateKioskAdminPinEncrypted failed against postgres: %v", err)
	}
	kiosk, err := repo.GetKioskByID(ctx, kioskID)
	if err != nil || kiosk == nil || kiosk.Name != "Borne entrée" {
		t.Fatalf("GetKioskByID = (%+v, %v)", kiosk, err)
	}
	scoped, err := repo.GetKioskByIDForMerchant(ctx, merchantID, kioskID)
	if err != nil || scoped == nil {
		t.Fatalf("GetKioskByIDForMerchant = (%+v, %v)", scoped, err)
	}
	other, err := repo.GetKioskByIDForMerchant(ctx, "0", kioskID)
	if err != nil || other != nil {
		t.Fatalf("expected nil for wrong merchant, got (%+v, %v)", other, err)
	}
	list, err := repo.ListKiosksByMerchant(ctx, merchantID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListKiosksByMerchant = (%d, %v), want 1", len(list), err)
	}
	// borne révoquée avec heartbeat récent : reste listée 24 h (INTERVAL '24' HOUR)
	if err := repo.UpdateKioskStatus(ctx, kioskID, "revoked"); err != nil {
		t.Fatalf("UpdateKioskStatus failed against postgres: %v", err)
	}
	list, err = repo.ListKiosksByMerchant(ctx, merchantID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected recently-revoked kiosk still listed, got (%d, %v)", len(list), err)
	}
	if err := repo.SetKioskStatusEnabled(ctx, kioskID, "active", true); err != nil {
		t.Fatalf("SetKioskStatusEnabled failed against postgres: %v", err)
	}
	count, err := repo.GetActiveKioskCount(ctx, merchantID)
	if err != nil || count != 1 {
		t.Fatalf("GetActiveKioskCount = (%d, %v), want 1", count, err)
	}

	// --- quotas / settings ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (stripe_subscription_id, merchant_id, package_id, max_kiosks)
		VALUES ('itest-kiosk-sub', $1, 1, 4)`, merchantID); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	maxKiosks, err := repo.GetMerchantMaxKiosks(ctx, merchantID)
	if err != nil || maxKiosks != 4 {
		t.Fatalf("GetMerchantMaxKiosks = (%d, %v), want 4", maxKiosks, err)
	}

	settings, err := repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetKioskSettings (defaults) failed against postgres: %v", err)
	}
	if !settings.FulfillmentDineIn || settings.InactivityTimeoutSec != 90 {
		t.Fatalf("unexpected default settings: %+v", settings)
	}
	if settings.BusinessName == nil || *settings.BusinessName != "ITest Kiosk Merchant" {
		t.Fatalf("expected business name attached, got %+v", settings.BusinessName)
	}

	vf, ff, err := repo.GetKioskFees(ctx, merchantID)
	if err != nil || vf != defaultKioskVariableFees || ff != defaultKioskFixedFees {
		t.Fatalf("GetKioskFees (defaults) = (%v, %v, %v)", vf, ff, err)
	}

	// upsert insert puis update (ON CONFLICT)
	settings.PagerNumberRequired = true
	settings.InactivityTimeoutSec = 120
	if err := repo.UpsertSettings(ctx, settings); err != nil {
		t.Fatalf("UpsertSettings (insert) failed against postgres: %v", err)
	}
	settings.InactivityTimeoutSec = 150
	if err := repo.UpsertSettings(ctx, settings); err != nil {
		t.Fatalf("UpsertSettings (update) failed against postgres: %v", err)
	}
	stored, err := repo.GetSettingsByMerchant(ctx, merchantID)
	if err != nil || stored == nil || stored.InactivityTimeoutSec != 150 || !stored.PagerNumberRequired {
		t.Fatalf("GetSettingsByMerchant = (%+v, %v)", stored, err)
	}
	vf, ff, err = repo.GetKioskFees(ctx, merchantID)
	if err != nil || ff != 15 {
		t.Fatalf("GetKioskFees (stored) = (%v, %v, %v)", vf, ff, err)
	}

	// --- Stripe Terminal / slug ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO stripe_accounts (account_id, merchant_id, terminal_location_id)
		VALUES ('acct_itest_kiosk', $1, 'tml_itest')`, merchantID); err != nil {
		t.Fatalf("seed stripe_accounts: %v", err)
	}
	loc, err := repo.GetTerminalLocationID(ctx, merchantID)
	if err != nil || loc == nil || *loc != "tml_itest" {
		t.Fatalf("GetTerminalLocationID = (%v, %v)", loc, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO qrcodes (merchant_id, code, creation_date) VALUES ($1, 'itest-kiosk-slug', NULL)`, merchantID); err != nil {
		t.Fatalf("seed qrcodes: %v", err)
	}
	slugSettings, err := repo.GetKioskSettings(ctx, merchantID)
	if err != nil || slugSettings.Slug == nil || *slugSettings.Slug != "itest-kiosk-slug" {
		t.Fatalf("expected slug attached, got %+v err=%v", slugSettings.Slug, err)
	}

	// --- produits / options ---
	var prodOK, prodHidden int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id, is_available_on_kiosk)
		VALUES ($1, 'itest-kiosk-ok', 700, 'itest', 0, 0, 0, true) RETURNING product_id`, merchantID).Scan(&prodOK); err != nil {
		t.Fatalf("seed product ok: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id, is_available_on_kiosk)
		VALUES ($1, 'itest-kiosk-hidden', 700, 'itest', 0, 0, 0, false) RETURNING product_id`, merchantID).Scan(&prodHidden); err != nil {
		t.Fatalf("seed product hidden: %v", err)
	}
	prodOKStr := strconv.FormatInt(prodOK, 10)
	available, err := repo.GetAvailableKioskProductIDs(ctx, merchantID, []string{prodOKStr, strconv.FormatInt(prodHidden, 10)})
	if err != nil || !available[prodOKStr] || available[strconv.FormatInt(prodHidden, 10)] {
		t.Fatalf("GetAvailableKioskProductIDs = (%+v, %v)", available, err)
	}
	availMap, err := repo.GetKioskProductAvailabilityMap(ctx, merchantID)
	if err != nil || len(availMap) != 2 || !availMap[prodOKStr] {
		t.Fatalf("GetKioskProductAvailabilityMap = (%+v, %v)", availMap, err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, attribute_type, name, title, max_options)
		VALUES ('itest-kiosk-attr', $1, $2, 'CHECK', 'sauce', 'Sauce', 1)`, prodOK, merchantID); err != nil {
		t.Fatalf("seed attribute: %v", err)
	}
	var optionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ('itest-kiosk-attr', 'BBQ', 40) RETURNING id`).Scan(&optionID); err != nil {
		t.Fatalf("seed option: %v", err)
	}
	optStr := strconv.FormatInt(optionID, 10)
	attrIDs, err := repo.GetConfigurationOptionAttributeIDs(ctx, []string{optStr})
	if err != nil || attrIDs[optStr] != "itest-kiosk-attr" {
		t.Fatalf("GetConfigurationOptionAttributeIDs = (%+v, %v)", attrIDs, err)
	}

	// --- commandes ---
	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, state, price, TVA, HT, created_by)
		VALUES ($1, 1, 'PENDING_CARD_PAYMENT', 'OPEN', 700, 70, 630, 'KIOSK') RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)
	if err := repo.SetKioskIDOnOrder(ctx, orderID, kioskID); err != nil {
		t.Fatalf("SetKioskIDOnOrder failed against postgres: %v", err)
	}
	if err := repo.UpdateOrderMerchantApproval(ctx, merchantID, orderID, "ACCEPTED"); err != nil {
		t.Fatalf("UpdateOrderMerchantApproval failed against postgres: %v", err)
	}
	changed, err := repo.ConfirmKioskCardToCounterBrandStatus(ctx, merchantID, orderID)
	if err != nil || !changed {
		t.Fatalf("ConfirmKioskCardToCounterBrandStatus = (%v, %v), want (true, nil)", changed, err)
	}
	changed, err = repo.ConfirmKioskCardToCounterBrandStatus(ctx, merchantID, orderID)
	if err != nil || changed {
		t.Fatalf("expected idempotent no-op on second confirm, got (%v, %v)", changed, err)
	}

	// --- divers ---
	tz, err := repo.GetMerchantTimezone(ctx, merchantID)
	if err != nil || tz != "Europe/Paris" {
		t.Fatalf("GetMerchantTimezone = (%s, %v)", tz, err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_order_type, discount_code, discount_value, discount_unit, min_order_unit, discounted_quantity, is_cumulative, is_time_limited, available, valid_from)
		VALUES ('itest-kiosk-disc', $1, 'Promo kiosk', 'desc', 'IN TAKE_AWAY', 'KSK5', 5, 'PERCENT', 'CURRENCY', 1, false, false, true, now() - interval '1 day')`, merchantID); err != nil {
		t.Fatalf("seed discount: %v", err)
	}
	dow := int(time.Now().UTC().Weekday())
	if dow == 0 {
		dow = 7
	}
	discounts, err := repo.GetDiscounts(ctx, merchantID, "", dow)
	if err != nil || len(discounts) != 1 || discounts[0].IsCumulative {
		t.Fatalf("GetDiscounts = (%+v, %v)", discounts, err)
	}

	// suppression du code d'enrôlement
	if err := repo.DeleteEnrollmentCode(ctx, codeID); err != nil {
		t.Fatalf("DeleteEnrollmentCode failed against postgres: %v", err)
	}
}
