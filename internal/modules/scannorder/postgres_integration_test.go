//go:build postgres_integration

package scannorder

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// NOTE : GetMerchantStatus n'est pas exercée ici — elle dépend transitivement
// de planning/settings (ResolvePlanningHoliday, module Tier 4 non converti,
// placeholders `?` non rebindés sous Postgres). Même précédent que la
// dépendance reservation→customers du Tier 2. GetCustomerByPhone dépend de
// customers.FindCustomerByPhone (converti dans le module customers, testé une
// fois customers converti — voir TestScannorderCustomerByPhone_Postgres).
func TestScannorderRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const qrCode = "itest-sno-qr"
	const tableQRCode = "itest-sno-qr-table"
	const brandSlug = "itest-sno-brand"
	var merchantID string

	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM stripe_payments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM delivery_session_order WHERE delivery_session_id IN (SELECT id FROM delivery_session WHERE merchant_id = $1)`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM delivery_session WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM booked_location WHERE booking_id IN (SELECT booking_id FROM bookings WHERE merchant_id = $1)`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM bookings WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_rewards WHERE loyalty_program_id = 'itest-sno-lp'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_loyalty_programs WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts_schedules WHERE discount_id = 'itest-sno-disc'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM availabilities_schedules WHERE availability_id IN (SELECT availability_id FROM availabilities WHERE merchant_id = $1)`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM availabilities_products WHERE availability_id IN (SELECT availability_id FROM availabilities WHERE merchant_id = $1)`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM availabilities WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = 'itest-sno-attr'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM configurable_attributes WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_Id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM hours_of_operation WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM stripe_accounts WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM locations WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM qrcodes WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM brands WHERE brand_id = 'itest-sno-brand-id'`)
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-sno' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		_, _ = db.ExecContext(ctx, `DELETE FROM brands WHERE brand_id = 'itest-sno-brand-id'`)
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	mustExec := func(desc, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}

	mustExec("brands", `
		INSERT INTO brands (brand_id, name, slug, logo_url, banner_url, description) VALUES ('itest-sno-brand-id', 'ITest Brand', $1, 'https://cdn/logo.png', 'https://cdn/banner.png', 'desc')`, brandSlug)

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng, brand_id)
		VALUES ('ITest SNO Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-sno', 'https://x', '06', 'mtok-sno', 'Europe/Paris', 48.85, 2.35, 'itest-sno-brand-id')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	mustExec("merchant_parameters", `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open, delivery_fees, delivery_fees_limit, minimum_cart_for_delivery_order, enable_advance_orders, automatically_add_customer_rewards)
		VALUES ($1, now(), 'EUR', true, 250, 2000, 1500, true, true)`, merchantID)
	mustExec("scannorder_settings", `
		INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, activated, take_away_enabled, take_away_available, delivery_enabled, delivery_available, in_enabled, in_available)
		VALUES ($1, 't', 'd', 'k', 'fr', true, true, true, true, true, true, true)`, merchantID)
	mustExec("stripe_accounts", `
		INSERT INTO stripe_accounts (account_id, merchant_id) VALUES ('acct_itest_sno', $1)`, merchantID)
	mustExec("qrcodes (principal)", `
		INSERT INTO qrcodes (merchant_id, code, creation_date) VALUES ($1, $2, NULL)`, merchantID, qrCode)

	var locationID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO locations (merchant_id, location_name, seats) VALUES ($1, 'T7', 2)
		RETURNING location_id`, merchantID).Scan(&locationID); err != nil {
		t.Fatalf("seed location: %v", err)
	}
	mustExec("qrcodes (table)", `
		INSERT INTO qrcodes (merchant_id, code, location_id, creation_date) VALUES ($1, $2, $3, NULL)`, merchantID, tableQRCode, locationID)

	repo := NewRepository(db)

	// --- GetMerchantByQR (jointures m.id castées ×5) ---
	row, err := repo.GetMerchantByQR(ctx, qrCode)
	if err != nil {
		t.Fatalf("GetMerchantByQR failed against postgres: %v", err)
	}
	if row.MerchantID != merchantID {
		t.Fatalf("unexpected merchant row: %+v", row)
	}

	// --- lookups simples ---
	mid, tz, err := repo.GetMerchantIDAndTZFromQR(ctx, qrCode)
	if err != nil || mid != merchantID || tz != "Europe/Paris" {
		t.Fatalf("GetMerchantIDAndTZFromQR = (%s, %s, %v)", mid, tz, err)
	}
	mid2, tz2, err := repo.GetMerchantIDAndTZFromMerchantID(ctx, merchantID)
	if err != nil || mid2 != merchantID || tz2 != "Europe/Paris" {
		t.Fatalf("GetMerchantIDAndTZFromMerchantID = (%s, %s, %v)", mid2, tz2, err)
	}
	midPtr, err := repo.GetMerchantIDByQR(ctx, qrCode)
	if err != nil || midPtr == nil || *midPtr != merchantID {
		t.Fatalf("GetMerchantIDByQR = (%v, %v)", midPtr, err)
	}

	// --- GetAvailableSlots (CTE récursif traduit) ---
	mustExec("hours_of_operation", `
		INSERT INTO hours_of_operation (id, merchant_id, day_of_week_from, hour_from, day_of_week_to, hour_to, enabled)
		VALUES ('itest-sno-hoo-1', $1, 1, '09:00:00', 7, '22:00:00', true)`, merchantID)

	today := time.Now().UTC().Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	slots, err := repo.GetAvailableSlots(ctx, merchantID, 30, today, "10:00:00")
	if err != nil {
		t.Fatalf("GetAvailableSlots failed against postgres: %v", err)
	}
	// jour même : le délai de préparation décale les créneaux après 10h30
	if len(slots[today]) == 0 {
		t.Fatalf("expected same-day slots, got %+v", slots)
	}
	for _, s := range slots[today] {
		if s.Time < "10:30" {
			t.Fatalf("expected same-day slots >= 10:30 (prep delay), got %s", s.Time)
		}
	}
	// lendemain : tous les créneaux d'ouverture (premier > hour_from)
	if len(slots[tomorrow]) == 0 || slots[tomorrow][0].Time != "09:30" {
		t.Fatalf("expected tomorrow first slot 09:30, got %+v", slots[tomorrow])
	}

	// --- GetLoyaltyPrograms ---
	mustExec("loyalty program", `
		INSERT INTO customer_loyalty_programs (id, merchant_id, name, description, type, target_value, reward_type, reward_value)
		VALUES ('itest-sno-lp', $1, 'Fidelite SNO', 'desc', 'AMOUNT', 5000, 'DISCOUNT', 500)`, merchantID)
	programs, err := repo.GetLoyaltyPrograms(ctx, merchantID, "IN")
	if err != nil || len(programs) != 1 || programs[0].ID != "itest-sno-lp" {
		t.Fatalf("GetLoyaltyPrograms = (%+v, %v)", programs, err)
	}

	// --- GetDiscounts (fenêtre horaire + scans booléens CASE 1/0) ---
	mustExec("discount", `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_order_type, discount_code, discount_value, discount_unit, min_order_unit, discounted_quantity, is_cumulative, is_time_limited, available, valid_from)
		VALUES ('itest-sno-disc', $1, 'Promo SNO', 'desc', 'IN TAKE_AWAY', 'SNO10', 10, 'PERCENT', 'CURRENCY', 1, true, true, true, now() - interval '1 day')`, merchantID)
	dow := int(time.Now().UTC().Weekday())
	if dow == 0 {
		dow = 7
	}
	mustExec("discount schedule", `
		INSERT INTO discounts_schedules (discount_id, day_of_week, available_from, available_to, enabled)
		VALUES ('itest-sno-disc', $1, '00:00:00', '23:59:59', true)`, dow)
	discounts, err := repo.GetDiscounts(ctx, merchantID, "IN", dow)
	if err != nil {
		t.Fatalf("GetDiscounts failed against postgres: %v", err)
	}
	if len(discounts) != 1 || !discounts[0].IsCumulative || !discounts[0].Available {
		t.Fatalf("unexpected discounts: %+v", discounts)
	}

	// --- GetMerchantOpenHours (heures castées CHAR(8)) ---
	openHours, err := repo.GetMerchantOpenHours(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetMerchantOpenHours failed against postgres: %v", err)
	}
	if len(openHours) != 7 || len(openHours[0].Hours) != 1 || openHours[0].Hours[0].From != "09:00" {
		t.Fatalf("unexpected open hours: %+v", openHours)
	}

	// --- GetUnavailableProducts ---
	var prodID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id, is_popular)
		VALUES ($1, 'itest-sno-prod', 900, 'itest', 0, 0, 0, true) RETURNING product_id`, merchantID).Scan(&prodID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	const availabilityID = "itest-sno-avail"
	mustExec("availability", `
		INSERT INTO availabilities (availability_id, merchant_id, availability_name, unavailable_message, available, enabled)
		VALUES ($1, $2, 'Petit dej', 'Dispo le matin', true, true)`, availabilityID, merchantID)
	mustExec("availabilities_products", `
		INSERT INTO availabilities_products (availability_product_id, availability_id, product_id, enabled) VALUES ('itest-sno-ap', $1, $2, true)`, availabilityID, prodID)
	mustExec("availabilities_schedules", `
		INSERT INTO availabilities_schedules (schedule_id, availability_id, day_of_week, available_from, available_to, enabled)
		VALUES ('itest-sno-as', $1, $2, '06:00:00', '11:00:00', true)`, availabilityID, dow)
	unavailable, err := repo.GetUnavailableProducts(ctx, merchantID, dow, "14:00:00")
	if err != nil {
		t.Fatalf("GetUnavailableProducts failed against postgres: %v", err)
	}
	if _, found := unavailable[prodID]; !found {
		t.Fatalf("expected product %d unavailable at 14:00 (dispo 6h-11h), got %+v", prodID, unavailable)
	}

	// --- commandes / paiements Stripe / session de livraison ---
	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, state, price, TVA, HT, created_by)
		VALUES ($1, 1, 'ACCEPTED', 'OPEN', 900, 90, 810, 'SCANNORDER') RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)

	var paymentID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
		VALUES ($1, 'SCANNORDER', $2, 900, 'STRIPE', true) RETURNING payment_id`, merchantID, orderIntID).Scan(&paymentID); err != nil {
		t.Fatalf("seed payment: %v", err)
	}
	mustExec("stripe_payments", `
		INSERT INTO stripe_payments (order_id, payment_id, payment_intent_id, success_key)
		VALUES ($1, $2, 'pi_itest_sno', 'sk-itest')`, orderIntID, paymentID)

	intentID, accountID, err := repo.GetStripePaymentForOrder(ctx, orderID)
	if err != nil || intentID != "pi_itest_sno" || accountID != "acct_itest_sno" {
		t.Fatalf("GetStripePaymentForOrder = (%s, %s, %v)", intentID, accountID, err)
	}

	var sessionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO delivery_session (user_id, merchant_id, start_date, status)
		VALUES ('itest-sno-user', $1, now(), 'active') RETURNING id`, merchantID).Scan(&sessionID); err != nil {
		t.Fatalf("seed delivery_session: %v", err)
	}
	mustExec("delivery_session_order", `
		INSERT INTO delivery_session_order (delivery_session_id, order_id, priority) VALUES ($1, $2, 1)`, sessionID, orderIntID)
	dsID, err := repo.GetDeliverySessionByOrderID(ctx, orderID)
	if err != nil || dsID == nil || *dsID != strconv.FormatInt(sessionID, 10) {
		t.Fatalf("GetDeliverySessionByOrderID = (%v, %v)", dsID, err)
	}

	// --- GetCustomerFromQR (réservation en cours sur la table) ---
	var customerIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (customer_name, merchant_id, customer_tel) VALUES ('ITest SNO Client', $1, '+33698765432')
		RETURNING customer_id`, merchantID).Scan(&customerIntID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	var bookingID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO bookings (booking_number, merchant_id, party_size, customer_id, booking_date_from, booking_date_to, booking_duration, created_by, status)
		VALUES ('ITS001', $1, 2, $2, now() - interval '1 hour', now() + interval '1 hour', 120, 'itest', 'CONFIRMED')
		RETURNING booking_id`, merchantID, customerIntID).Scan(&bookingID); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	mustExec("booked_location", `
		INSERT INTO booked_location (booking_id, location_id) VALUES ($1, $2)`, bookingID, locationID)

	fromQR, err := repo.GetCustomerFromQR(ctx, tableQRCode)
	if err != nil {
		t.Fatalf("GetCustomerFromQR failed against postgres: %v", err)
	}
	if fromQR == nil || fromQR.CustomerID == nil || *fromQR.CustomerID != strconv.FormatInt(customerIntID, 10) {
		t.Fatalf("unexpected customer from QR: %+v", fromQR)
	}

	// --- GetMerchantsByBrandSlug (avec et sans filtre distance/HAVING) ---
	brand, merchants, err := repo.GetMerchantsByBrandSlug(ctx, brandSlug, nil, nil)
	if err != nil {
		t.Fatalf("GetMerchantsByBrandSlug failed against postgres: %v", err)
	}
	if brand == nil || brand.Slug != brandSlug || len(merchants) != 1 || merchants[0].MerchantID != merchantID {
		t.Fatalf("unexpected brand result: brand=%+v merchants=%+v", brand, merchants)
	}
	lat, lng := 48.86, 2.35 // ~1 km
	_, near, err := repo.GetMerchantsByBrandSlug(ctx, brandSlug, &lat, &lng)
	if err != nil {
		t.Fatalf("GetMerchantsByBrandSlug (geo) failed against postgres: %v", err)
	}
	if len(near) != 1 || near[0].DistanceKm == nil || *near[0].DistanceKm > 50 {
		t.Fatalf("unexpected geo result: %+v", near)
	}
	farLat, farLng := 43.30, 5.37 // Marseille, > 50 km
	_, far, err := repo.GetMerchantsByBrandSlug(ctx, brandSlug, &farLat, &farLng)
	if err != nil {
		t.Fatalf("GetMerchantsByBrandSlug (far) failed against postgres: %v", err)
	}
	if len(far) != 0 {
		t.Fatalf("expected no merchant within 50 km of Marseille, got %+v", far)
	}

	// --- upsell + prix ---
	upsell, err := repo.GetUpsellProducts(ctx, merchantID)
	if err != nil || len(upsell) != 1 {
		t.Fatalf("GetUpsellProducts = (%v, %v), want 1 popular product", upsell, err)
	}
	prices, err := repo.GetProductPricesForSNO(ctx, merchantID, []string{strconv.FormatInt(prodID, 10)})
	if err != nil || prices[strconv.FormatInt(prodID, 10)]["price"] != 900 {
		t.Fatalf("GetProductPricesForSNO = (%+v, %v)", prices, err)
	}
	mustExec("configurable_attributes", `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, attribute_type, name, title, max_options)
		VALUES ('itest-sno-attr', $1, $2, 'CHECK', 'sauce', 'Sauce', 1)`, prodID, merchantID)
	var optionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES ('itest-sno-attr', 'Mayo', 60) RETURNING id`).Scan(&optionID); err != nil {
		t.Fatalf("seed option: %v", err)
	}
	optPrices, err := repo.GetConfigurationOptionPricesForSNO(ctx, []string{strconv.FormatInt(optionID, 10)})
	if err != nil || optPrices[strconv.FormatInt(optionID, 10)] != 60 {
		t.Fatalf("GetConfigurationOptionPricesForSNO = (%+v, %v)", optPrices, err)
	}
}

// GetCustomerByPhone depend de customers.FindCustomerByPhone (module Tier 3
// converti apres scannorder dans ce chantier) — teste separement une fois la
// dependance convertie : lookup par telephone normalise + recuperation des
// recompenses disponibles quand automatically_add_customer_rewards est actif.
func TestScannorderCustomerByPhone_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "999929"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_rewards WHERE loyalty_program_id = 'itest-snocp-lp'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer_loyalty_programs WHERE id = 'itest-snocp-lp'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open, automatically_add_customer_rewards)
		VALUES ($1, now(), 'EUR', true, true)`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	var customerIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (customer_name, merchant_id, customer_tel, customer_first_name)
		VALUES ('ITest SNOCP', $1, '+33677788899', 'Paul') RETURNING customer_id`, merchantID).Scan(&customerIntID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO customer_loyalty_programs (id, merchant_id, name, description, type, target_value, reward_type, reward_value)
		VALUES ('itest-snocp-lp', $1, 'LP', 'd', 'AMOUNT', 100, 'DISCOUNT', 300)`, merchantID); err != nil {
		t.Fatalf("seed program: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO customer_rewards (customer_id, loyalty_program_id, reward_type, reward_value, creation_date)
		VALUES ($1, 'itest-snocp-lp', 'DISCOUNT', 300, now())`, strconv.FormatInt(customerIntID, 10)); err != nil {
		t.Fatalf("seed reward: %v", err)
	}

	repo := NewRepository(db)
	mid := merchantID
	tel := "06 77 78 88 99"
	out, err := repo.GetCustomerByPhone(ctx, models.CustomerRequest{Tel: &tel, MerchantID: &mid})
	if err != nil {
		t.Fatalf("GetCustomerByPhone failed against postgres: %v", err)
	}
	if out.CustomerID == nil || *out.CustomerID != strconv.FormatInt(customerIntID, 10) {
		t.Fatalf("expected customer resolved by phone, got %+v", out)
	}
	if len(out.AvailableRewards) != 1 || out.AvailableRewards[0].RewardValue == nil || *out.AvailableRewards[0].RewardValue != 300 {
		t.Fatalf("expected 1 available reward of 300, got %+v", out.AvailableRewards)
	}
}
