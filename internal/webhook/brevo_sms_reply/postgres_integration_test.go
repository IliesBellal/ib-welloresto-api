//go:build postgres_integration

package brevo_sms_reply

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestBrevoSMSReplyRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-brevo-m1"
	const phone = "+33699990001"
	const bookingNumber = "IT0001"

	var customerID int64
	var bookingID int64

	cleanup := func() {
		if bookingID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM bookings WHERE booking_id = $1`, bookingID)
		}
		if customerID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE customer_id = $1`, customerID)
		}
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (merchant_id, customer_tel) VALUES ($1, $2) RETURNING customer_id`,
		merchantID, phone).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	future := time.Now().UTC().Add(48 * time.Hour)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO bookings (
			booking_number, merchant_id, party_size, customer_id,
			booking_date_from, booking_date_to, booking_duration,
			created_by, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING booking_id`,
		bookingNumber, merchantID, 2, customerID, future, future.Add(90*time.Minute), 90,
		"itest", "PENDING_APPROVAL").Scan(&bookingID); err != nil {
		t.Fatalf("seed booking: %v", err)
	}

	repo := NewRepository(db)

	// FindActiveBookingByPhone: legacy status normalized, UTC_TIMESTAMP()/now() ordering branch taken.
	found, err := repo.FindActiveBookingByPhone(ctx, phone)
	if err != nil {
		t.Fatalf("FindActiveBookingByPhone failed against postgres: %v", err)
	}
	if found == nil {
		t.Fatal("expected an active booking to be found")
	}
	if found.MerchantID != merchantID || found.Status != "pending" {
		t.Fatalf("unexpected booking: %+v", found)
	}

	// Reconfirm.
	if err := repo.Reconfirm(ctx, merchantID, found.BookingID); err != nil {
		t.Fatalf("Reconfirm failed against postgres: %v", err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE booking_id = $1`, bookingID).Scan(&status); err != nil {
		t.Fatalf("read back status: %v", err)
	}
	if status != "confirmed" {
		t.Fatalf("expected status confirmed after Reconfirm, got %q", status)
	}

	// CancelByCustomer: sets status, cancelled_by, and deletion_date (dbx.UTCNow()).
	if err := repo.CancelByCustomer(ctx, merchantID, found.BookingID); err != nil {
		t.Fatalf("CancelByCustomer failed against postgres: %v", err)
	}
	var cancelledBy string
	var deletionDate *time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT status, cancelled_by, deletion_date FROM bookings WHERE booking_id = $1`,
		bookingID).Scan(&status, &cancelledBy, &deletionDate); err != nil {
		t.Fatalf("read back after cancel: %v", err)
	}
	if status != "cancelled" || cancelledBy != "CUSTOMER" {
		t.Fatalf("unexpected state after CancelByCustomer: status=%q cancelled_by=%q", status, cancelledBy)
	}
	if deletionDate == nil {
		t.Fatal("expected deletion_date to be set by dbx.UTCNow()")
	}
}
