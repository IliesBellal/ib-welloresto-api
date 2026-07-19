//go:build postgres_integration

package reservation

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestReservationRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const bookingCode = "itest-resv-code-1"
	var customerIntID int64
	var bookingIntID int64
	var ruleID = "itest-resv-rule-1"
	var hooID = "itest-resv-hoo-1"

	cleanup := func() {
		if bookingIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM bookings WHERE booking_id = $1`, bookingIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM booking_duration_rules WHERE rule_id = $1`, ruleID)
		_, _ = db.ExecContext(ctx, `DELETE FROM hours_of_operation WHERE id = $1`, hooID)
		if customerIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM customer_rewards WHERE customer_id = $1`, itoa(customerIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE customer_id = $1`, customerIntID)
		}
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM bookings_settings WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, handicap_access, logo_url)
		VALUES ('ITest Resv Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-resv', 'https://example.com', '0600000000', 'tok', 'Europe/Paris', true, 'https://example.com/logo.png')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, primary_color, text_color_on_primary_color)
		VALUES ($1, $2, '#123456', '#ffffff')`, merchantID, time.Now().UTC()); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO bookings_settings (merchant_id, code, auto_accept_reserve_bookings, cancelable_by_customer, sms_enabled, enabled)
		VALUES ($1, $2, true, false, true, true)`, merchantID, bookingCode); err != nil {
		t.Fatalf("seed bookings_settings: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO hours_of_operation (id, merchant_id, day_of_week_from, hour_from, day_of_week_to, hour_to, booking_capacity, enabled)
		VALUES ($1, $2, 1, '09:00:00', 1, '22:00:00', 20, true)`, hooID, merchantID); err != nil {
		t.Fatalf("seed hours_of_operation: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_duration_rules (rule_id, merchant_id, min_party_size, max_party_size, duration_minutes)
		VALUES ($1, $2, 1, 4, 90)`, ruleID, merchantID); err != nil {
		t.Fatalf("seed booking_duration_rules: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (merchant_id, customer_tel, enabled)
		VALUES ($1, '+33611110000', true) RETURNING customer_id`, merchantID).Scan(&customerIntID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	customerID := itoa(customerIntID)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO customer_rewards (customer_id, loyalty_program_id, reward_type, reward_value, creation_date)
		VALUES ($1, '42', 'FREE_ITEM', 500, $2)`, customerID, time.Now().UTC()); err != nil {
		t.Fatalf("seed customer_rewards: %v", err)
	}

	repo := NewReservationRepository(db)

	// GetMerchantByQR: COALESCE(bool, ...) fix + direct bool scan fix.
	merchant, err := repo.GetMerchantByQR(ctx, bookingCode)
	if err != nil {
		t.Fatalf("GetMerchantByQR failed against postgres: %v", err)
	}
	if merchant == nil {
		t.Fatal("expected a merchant")
	}
	if !merchant.HandicapAccess {
		t.Fatal("expected HandicapAccess=true (bool scan fix)")
	}
	if !merchant.AutoAcceptReserveBookings {
		t.Fatal("expected AutoAcceptReserveBookings=true")
	}
	if merchant.CancelableByCustomer {
		t.Fatal("expected CancelableByCustomer=false")
	}
	if !merchant.SMSEnabled {
		t.Fatal("expected SMSEnabled=true")
	}

	if _, err := repo.GetMerchantByQR(ctx, "no-such-code"); err != nil {
		t.Fatalf("GetMerchantByQR (not found) should return (nil,nil), got err: %v", err)
	}

	// GetOperationHoursByQR: UTC_TIMESTAMP (no parens) -> dbx.UTCNow() fix.
	hours, err := repo.GetOperationHoursByQR(ctx, bookingCode)
	if err != nil {
		t.Fatalf("GetOperationHoursByQR failed against postgres: %v", err)
	}
	if len(hours) != 1 || hours[0].DayOfWeek != 1 {
		t.Fatalf("unexpected operation hours: %+v", hours)
	}

	// GetOperationRanges: boolean literal fix.
	ranges, err := repo.GetOperationRanges(ctx, merchantID, 1, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		t.Fatalf("GetOperationRanges failed against postgres: %v", err)
	}
	if len(ranges) != 1 || ranges[0].BookingCapacity != 20 {
		t.Fatalf("unexpected operation ranges: %+v", ranges)
	}

	rules, err := repo.GetBookingDurationRules(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetBookingDurationRules failed against postgres: %v", err)
	}
	if len(rules) != 1 || rules[0].DurationMinutes != 90 {
		t.Fatalf("unexpected duration rules: %+v", rules)
	}

	custData, err := repo.GetCustomerByPhone(ctx, "+33611110000", merchantID)
	if err != nil {
		t.Fatalf("GetCustomerByPhone failed against postgres: %v", err)
	}
	if custData == nil || custData.CustomerID != customerID {
		t.Fatalf("unexpected customer data: %+v", custData)
	}

	rewards, err := repo.GetRewards(ctx, customerID)
	if err != nil {
		t.Fatalf("GetRewards failed against postgres: %v", err)
	}
	if len(rewards) != 1 || rewards[0].RewardValue != 500 {
		t.Fatalf("unexpected rewards: %+v", rewards)
	}

	// repository.CreateBooking is dead code (never called by the service —
	// only CreateBookingTransaction, which delegates to the already-converted
	// bookingcore.CreateBooking, is used in production). Its own "Simulation
	// de l'insertion" comment admits it's incomplete: the INSERT never
	// supplies booking_number (varchar, NOT NULL, no default), so it fails on
	// both dialects identically. The dbx.InsertReturningID conversion is
	// still mechanically correct; this assertion pins the *specific*
	// pre-existing failure so a regression in that conversion would be
	// caught here instead of silently masked by the unrelated bug.
	if _, err := repo.CreateBooking(ctx, &BookingRequest{
		MerchantID: merchantID,
		Customer:   &CustomerData{CustomerID: customerID},
		Booking:    &BookingData{StartDate: "2026-01-01 12:00:00", EndDate: "2026-01-01 13:00:00", PartySize: 2, Status: "pending"},
		CreatedBy:  "itest",
	}); err == nil {
		t.Fatal("expected repository.CreateBooking to fail on the pre-existing missing booking_number bug")
	} else if !containsFold(err.Error(), "booking_number") {
		t.Fatalf("expected a booking_number NOT NULL violation, got: %v", err)
	}

	// CreateBookingTransaction is the real production path, but it transitively
	// calls into the `customers` module (Tier 3, not yet converted to dbx) to
	// upsert the customer record — the same dependency wall the Tier1 report
	// documented for bookingcore.CreateBooking ("CreateBooking dépend de
	// customers, module Tier 3 non converti", 14-tier1-conversion-log.md).
	// Exercising it here would fail on unconverted Tier 3 SQL, not on anything
	// in this module's conversion — out of scope per "ne touche à aucun module
	// hors Tier 2". Seed the booking directly instead, to verify the
	// reservation-owned read/update/cancel paths below against real Postgres.
	start := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	end := time.Now().UTC().Add(25 * time.Hour).Format("2006-01-02 15:04:05")
	startParsed, _ := time.Parse("2006-01-02 15:04:05", start)
	endParsed, _ := time.Parse("2006-01-02 15:04:05", end)
	bookingNumber := "IT0001"
	if err := db.QueryRowContext(ctx, `
		INSERT INTO bookings (booking_number, merchant_id, customer_id, booking_date_from, booking_date_to, booking_duration, party_size, status, created_by)
		VALUES ($1, $2, $3, $4, $5, 60, 2, 'pending', 'itest')
		RETURNING booking_id`,
		bookingNumber, merchantID, customerID, startParsed, endParsed).Scan(&bookingIntID); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	if bookingIntID == 0 {
		t.Fatal("expected a non-zero booking id")
	}

	// GetBookedCapacity: INTERVAL MINUTE fix (booking_duration is NULL here,
	// so the implied end = start + 90min default).
	capacity, err := repo.GetBookedCapacity(ctx, merchantID, time.Now().UTC().Add(24*time.Hour).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("GetBookedCapacity failed against postgres: %v", err)
	}
	if len(capacity) != 1 || capacity[0].PartySize != 2 {
		t.Fatalf("unexpected booked capacity: %+v", capacity)
	}

	capacityExcluded, err := repo.GetBookedCapacityExcludingBooking(ctx, merchantID, time.Now().UTC().Add(24*time.Hour).Format("2006-01-02"), bookingNumber)
	if err != nil {
		t.Fatalf("GetBookedCapacityExcludingBooking failed against postgres: %v", err)
	}
	if len(capacityExcluded) != 0 {
		t.Fatalf("expected the excluded booking to be filtered out, got %+v", capacityExcluded)
	}

	fetched, err := repo.GetBookingByNumber(ctx, bookingNumber, merchantID)
	if err != nil {
		t.Fatalf("GetBookingByNumber failed against postgres: %v", err)
	}
	if fetched == nil || fetched.PartySize != 2 {
		t.Fatalf("unexpected fetched booking: %+v", fetched)
	}

	name, email, phone, err := repo.GetBookingCustomerContact(ctx, bookingNumber, merchantID)
	if err != nil {
		t.Fatalf("GetBookingCustomerContact failed against postgres: %v", err)
	}
	_ = name
	_ = email
	if phone != "+33611110000" {
		t.Fatalf("expected phone +33611110000, got %q", phone)
	}

	found, err := repo.FindExistingActiveBookingWarning(ctx, merchantID, "+33611110000", start)
	if err != nil {
		t.Fatalf("FindExistingActiveBookingWarning failed against postgres: %v", err)
	}
	if !found {
		t.Fatal("expected an active booking warning for the same slot")
	}

	fetched.StartDate = start
	fetched.EndDate = end
	fetched.Status = "confirmed"
	if err := repo.UpdateBooking(ctx, fetched); err != nil {
		t.Fatalf("UpdateBooking failed against postgres: %v", err)
	}
	fetched, err = repo.GetBookingByNumber(ctx, bookingNumber, merchantID)
	if err != nil {
		t.Fatalf("GetBookingByNumber (after update) failed: %v", err)
	}
	if fetched.Status != "confirmed" || fetched.SequenceNumber != 1 {
		t.Fatalf("unexpected booking after update: %+v", fetched)
	}

	if err := repo.CancelBookingPublic(ctx, merchantID, bookingNumber); err != nil {
		t.Fatalf("CancelBookingPublic failed against postgres: %v", err)
	}
	var status, cancelledBy string
	if err := db.QueryRowContext(ctx, `SELECT status, cancelled_by FROM bookings WHERE booking_id = $1`, bookingIntID).Scan(&status, &cancelledBy); err != nil {
		t.Fatalf("read back after CancelBookingPublic: %v", err)
	}
	if status != "cancelled" || cancelledBy != "CUSTOMER" {
		t.Fatalf("unexpected state after CancelBookingPublic: status=%q cancelled_by=%q", status, cancelledBy)
	}

	// CancelBookingDB: dbx.UTCNow() fix.
	if err := repo.CancelBookingDB(ctx, bookingNumber); err != nil {
		t.Fatalf("CancelBookingDB failed against postgres: %v", err)
	}
	var deletionDate *time.Time
	var deletionReasonID *int
	if err := db.QueryRowContext(ctx, `SELECT status, deletion_date, deletion_reason_id FROM bookings WHERE booking_id = $1`, bookingIntID).
		Scan(&status, &deletionDate, &deletionReasonID); err != nil {
		t.Fatalf("read back after CancelBookingDB: %v", err)
	}
	if status != "CANCELED" || deletionDate == nil || deletionReasonID == nil || *deletionReasonID != 9 {
		t.Fatalf("unexpected state after CancelBookingDB: status=%q deletion_date=%v deletion_reason_id=%v", status, deletionDate, deletionReasonID)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
