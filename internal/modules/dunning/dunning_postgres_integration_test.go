//go:build postgres_integration

package dunning

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
)

// fakeMailer/fakeSMS embed the real interfaces (see
// internal/modules/bookings/reminders_test.go for the same pattern already
// used elsewhere in this repo) so only the methods dunning.Service actually
// calls need overriding.
type fakeMailer struct {
	mailer.Service
	sentTemplates []string
}

func (f *fakeMailer) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	f.sentTemplates = append(f.sentTemplates, templateName)
}

type fakeSMS struct {
	sms.Service
	sentCount int
}

func (f *fakeSMS) SendSMSAsync(senderID, phoneNumber, message string) {
	f.sentCount++
}

type fakeStripeRetry struct {
	retriedCustomerIDs []string
}

func (f *fakeStripeRetry) RetryLatestOpenInvoice(customerID string) error {
	f.retriedCustomerIDs = append(f.retriedCustomerIDs, customerID)
	return nil
}

func newTestService(db *sql.DB, mail *fakeMailer, txt *fakeSMS, stripe *fakeStripeRetry) *Service {
	return NewService(NewRepository(db), db, mail, txt, stripe)
}

// seedDunningMerchant inserts a merchant + subscriptions + an admin
// user/users_rights row (so GetMerchantOwnerContact resolves a real tel for
// the SMS assertions) and returns the merchant's text id.
func seedDunningMerchant(t *testing.T, db *sql.DB, label string) string {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email)
		VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $3, 'UTC', 'https://example.com/logo.png', $4)
		RETURNING id`,
		"ITest Dunning "+label, "siret-dun-"+label, "t"+strconv.FormatInt(time.Now().UnixNano(), 36), "itest-dunning-"+label+"@example.com",
	).Scan(&id); err != nil {
		t.Fatalf("seed merchant (%s): %v", label, err)
	}
	merchantID := strconv.FormatInt(id, 10)

	userID := "itest-dun-owner-" + label
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, merchant_id, name, first_name, last_name, email, tel, password, token, enabled, created_at)
		VALUES ($1, $2, $5, 'Owner', 'Test', $3, '+33600000001', 'itest-hash', $4, TRUE, now())`,
		userID, merchantID, "itest-dun-owner-"+label+"@example.com", "utok-"+userID, "Owner-"+userID); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, admin, enabled)
		VALUES ($1, $2, $3, TRUE, TRUE)`,
		userID, merchantID, "t"+strconv.FormatInt(time.Now().UnixNano(), 36)+"r"); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
		VALUES ($1, 1, '', 'monthly', 'active')`, merchantID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM subscription_dunning WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM users_rights WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM users WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM merchant WHERE id = $1`, id)
	})
	return merchantID
}

func getSubscriptionStatus(t *testing.T, db *sql.DB, merchantID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(context.Background(), `SELECT status FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&status); err != nil {
		t.Fatalf("read subscriptions.status: %v", err)
	}
	return status
}

// TestHandlePaymentFailed_FirstFailure_Postgres — 1st invoice.payment_failed:
// past_due + a dunning row + an immediate informational email, no SMS.
func TestHandlePaymentFailed_FirstFailure_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedDunningMerchant(t, db, "first")

	mail, txt := &fakeMailer{}, &fakeSMS{}
	svc := newTestService(db, mail, txt, &fakeStripeRetry{})

	if err := svc.HandlePaymentFailed(ctx, merchantID); err != nil {
		t.Fatalf("HandlePaymentFailed: %v", err)
	}

	if got := getSubscriptionStatus(t, db, merchantID); got != "past_due" {
		t.Fatalf("subscriptions.status = %q, want past_due", got)
	}
	state, err := svc.repo.GetState(ctx, merchantID)
	if err != nil || state == nil {
		t.Fatalf("GetState: %v / %+v", err, state)
	}
	if state.SecondFailedAt != nil || state.SuspensionDeadline != nil {
		t.Fatalf("state = %+v, want only first_failed_at set", state)
	}
	if len(mail.sentTemplates) != 1 || mail.sentTemplates[0] != "dunning_first_notice.html" {
		t.Fatalf("mail.sentTemplates = %v, want [dunning_first_notice.html]", mail.sentTemplates)
	}
	if txt.sentCount != 0 {
		t.Fatalf("txt.sentCount = %d, want 0 (no SMS on 1st failure)", txt.sentCount)
	}
}

// TestHandlePaymentFailed_SecondFailure_Postgres — 2nd invoice.payment_failed:
// opens the 14-day window, SMS + email.
func TestHandlePaymentFailed_SecondFailure_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedDunningMerchant(t, db, "second")

	mail, txt := &fakeMailer{}, &fakeSMS{}
	svc := newTestService(db, mail, txt, &fakeStripeRetry{})

	if err := svc.HandlePaymentFailed(ctx, merchantID); err != nil {
		t.Fatalf("1st HandlePaymentFailed: %v", err)
	}
	if err := svc.HandlePaymentFailed(ctx, merchantID); err != nil {
		t.Fatalf("2nd HandlePaymentFailed: %v", err)
	}

	state, err := svc.repo.GetState(ctx, merchantID)
	if err != nil || state == nil || state.SecondFailedAt == nil || state.SuspensionDeadline == nil {
		t.Fatalf("GetState after 2nd failure: %v / %+v", err, state)
	}
	wantDeadline := state.SecondFailedAt.Add(suspensionWindow)
	if diff := state.SuspensionDeadline.Sub(wantDeadline); diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("SuspensionDeadline = %v, want ~%v (14 days after 2nd failure)", state.SuspensionDeadline, wantDeadline)
	}
	if len(mail.sentTemplates) != 2 || mail.sentTemplates[1] != "dunning_second_notice.html" {
		t.Fatalf("mail.sentTemplates = %v, want 2nd = dunning_second_notice.html", mail.sentTemplates)
	}
	if txt.sentCount != 1 {
		t.Fatalf("txt.sentCount = %d, want 1 (SMS on 2nd failure)", txt.sentCount)
	}

	// 3rd failure: no-op — deadline/second_failed_at unchanged, no new send.
	if err := svc.HandlePaymentFailed(ctx, merchantID); err != nil {
		t.Fatalf("3rd HandlePaymentFailed: %v", err)
	}
	state2, _ := svc.repo.GetState(ctx, merchantID)
	if !state2.SecondFailedAt.Equal(*state.SecondFailedAt) || !state2.SuspensionDeadline.Equal(*state.SuspensionDeadline) {
		t.Fatalf("3rd failure changed state: before=%+v after=%+v", state, state2)
	}
	if len(mail.sentTemplates) != 2 || txt.sentCount != 1 {
		t.Fatalf("3rd failure sent something: templates=%v sms=%d", mail.sentTemplates, txt.sentCount)
	}
}

// TestClearDunning_Postgres — invoice.paid's effect: the dunning row is
// gone, so the cron will no longer act on this merchant ("toutes les
// relances programmées annulées").
func TestClearDunning_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedDunningMerchant(t, db, "clear")

	svc := newTestService(db, &fakeMailer{}, &fakeSMS{}, &fakeStripeRetry{})
	if err := svc.HandlePaymentFailed(ctx, merchantID); err != nil {
		t.Fatalf("HandlePaymentFailed: %v", err)
	}
	if state, _ := svc.repo.GetState(ctx, merchantID); state == nil {
		t.Fatal("expected a dunning row before ClearDunning")
	}

	if err := svc.ClearDunning(ctx, merchantID); err != nil {
		t.Fatalf("ClearDunning: %v", err)
	}
	state, err := svc.repo.GetState(ctx, merchantID)
	if err != nil || state != nil {
		t.Fatalf("GetState after ClearDunning = %v / %+v, want nil", err, state)
	}
}

// TestRunCascade_SuspendsAfterDeadline_Postgres — the cron's own escalation:
// once suspension_deadline has passed, status flips to suspended.
func TestRunCascade_SuspendsAfterDeadline_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedDunningMerchant(t, db, "suspend")

	// No hours_of_operation row -> FetchActiveSlots returns no slots ->
	// ComputePOSStatus.IsOpen = false (closed), so the cascade isn't
	// skipped as "pendant les services".
	if _, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET status = 'past_due' WHERE merchant_id = $1`, merchantID); err != nil {
		t.Fatalf("set past_due: %v", err)
	}
	past := time.Now().Add(-1 * time.Hour)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscription_dunning (merchant_id, first_failed_at, second_failed_at, suspension_deadline)
		VALUES ($1, $2, $2, $3)`, merchantID, past.Add(-15*24*time.Hour), past); err != nil {
		t.Fatalf("seed subscription_dunning: %v", err)
	}

	svc := newTestService(db, &fakeMailer{}, &fakeSMS{}, &fakeStripeRetry{})
	if errs := svc.RunCascade(ctx); len(errs) != 0 {
		t.Fatalf("RunCascade errors: %v", errs)
	}

	if got := getSubscriptionStatus(t, db, merchantID); got != "suspended" {
		t.Fatalf("subscriptions.status = %q, want suspended", got)
	}
}

// TestRunCascade_FinalNoticeAndWeeklyReminder_Postgres.
func TestRunCascade_FinalNoticeAndWeeklyReminder_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	// Case A: within 48h of the deadline -> final notice, not suspended yet.
	merchantA := seedDunningMerchant(t, db, "final-notice")
	if _, err := db.ExecContext(ctx, `UPDATE subscriptions SET status = 'past_due' WHERE merchant_id = $1`, merchantA); err != nil {
		t.Fatalf("set past_due A: %v", err)
	}
	deadlineA := time.Now().Add(24 * time.Hour) // within the 48h window
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscription_dunning (merchant_id, first_failed_at, second_failed_at, suspension_deadline)
		VALUES ($1, $2, $3, $3)`, merchantA, deadlineA.Add(-14*24*time.Hour), deadlineA); err != nil {
		t.Fatalf("seed dunning A: %v", err)
	}

	mailA, smsA := &fakeMailer{}, &fakeSMS{}
	svcA := newTestService(db, mailA, smsA, &fakeStripeRetry{})
	if errs := svcA.RunCascade(ctx); len(errs) != 0 {
		t.Fatalf("RunCascade A errors: %v", errs)
	}
	if got := getSubscriptionStatus(t, db, merchantA); got != "past_due" {
		t.Fatalf("merchantA status = %q, want still past_due (not yet at deadline)", got)
	}
	if len(mailA.sentTemplates) != 1 || mailA.sentTemplates[0] != "dunning_final_notice.html" || smsA.sentCount != 1 {
		t.Fatalf("A: mail=%v sms=%d, want final_notice once + 1 SMS", mailA.sentTemplates, smsA.sentCount)
	}
	stateA, _ := svcA.repo.GetState(ctx, merchantA)
	if stateA.FinalNoticeSentAt == nil {
		t.Fatal("A: final_notice_sent_at not recorded")
	}
	// Running again the same "day" must not resend it.
	if errs := svcA.RunCascade(ctx); len(errs) != 0 {
		t.Fatalf("RunCascade A (2nd run) errors: %v", errs)
	}
	if len(mailA.sentTemplates) != 1 {
		t.Fatalf("A: final notice sent twice: %v", mailA.sentTemplates)
	}

	// Case B: past_due but no 2nd failure yet (no deadline) and never
	// reminded -> a plain weekly reminder, no suspension date mentioned.
	merchantB := seedDunningMerchant(t, db, "weekly")
	if _, err := db.ExecContext(ctx, `UPDATE subscriptions SET status = 'past_due' WHERE merchant_id = $1`, merchantB); err != nil {
		t.Fatalf("set past_due B: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscription_dunning (merchant_id, first_failed_at)
		VALUES ($1, now() - interval '8 days')`, merchantB); err != nil {
		t.Fatalf("seed dunning B: %v", err)
	}
	mailB := &fakeMailer{}
	svcB := newTestService(db, mailB, &fakeSMS{}, &fakeStripeRetry{})
	if errs := svcB.RunCascade(ctx); len(errs) != 0 {
		t.Fatalf("RunCascade B errors: %v", errs)
	}
	if len(mailB.sentTemplates) != 1 || mailB.sentTemplates[0] != "dunning_weekly_reminder.html" {
		t.Fatalf("B: mail=%v, want a single weekly reminder", mailB.sentTemplates)
	}
	stateB, _ := svcB.repo.GetState(ctx, merchantB)
	if stateB.ReminderCount != 1 || stateB.LastReminderSentAt == nil {
		t.Fatalf("B: state = %+v, want reminder_count=1", stateB)
	}
	// Running again immediately must not send a second reminder (< 7 days).
	if errs := svcB.RunCascade(ctx); len(errs) != 0 {
		t.Fatalf("RunCascade B (2nd run) errors: %v", errs)
	}
	if len(mailB.sentTemplates) != 1 {
		t.Fatalf("B: reminder sent twice within the week: %v", mailB.sentTemplates)
	}
}

// TestRetryNow_Postgres — the "réessayer maintenant" button resolves the
// merchant's platform_billing_customers row and retries via Stripe.
func TestRetryNow_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedDunningMerchant(t, db, "retry")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO platform_billing_customers (id, merchant_id, stripe_customer_id, is_primary_for_merchant)
		VALUES ($1, $2, 'cus_itest_retry', TRUE)`, "pbc-itest-retry", merchantID); err != nil {
		t.Fatalf("seed platform_billing_customers: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM platform_billing_customers WHERE merchant_id = $1`, merchantID)
	})

	stripeFake := &fakeStripeRetry{}
	svc := newTestService(db, &fakeMailer{}, &fakeSMS{}, stripeFake)

	if err := svc.RetryNow(ctx, merchantID); err != nil {
		t.Fatalf("RetryNow: %v", err)
	}
	if len(stripeFake.retriedCustomerIDs) != 1 || stripeFake.retriedCustomerIDs[0] != "cus_itest_retry" {
		t.Fatalf("retriedCustomerIDs = %v, want [cus_itest_retry]", stripeFake.retriedCustomerIDs)
	}
}
