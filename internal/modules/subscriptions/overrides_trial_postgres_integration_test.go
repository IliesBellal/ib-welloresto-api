//go:build postgres_integration

package subscriptions

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/infrastructure/mailer"
)

// fakeTrialMailer embeds the real interface (same pattern as
// internal/modules/bookings/reminders_test.go and dunning's own fakeMailer)
// so only SendAsync needs overriding.
type fakeTrialMailer struct {
	mailer.Service
	sentTemplates []string
}

func (f *fakeTrialMailer) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	f.sentTemplates = append(f.sentTemplates, templateName)
}

// seedTrialMerchant inserts a REAL merchant row (activation_state 'SETUP'
// by default) — unlike seedSubscription's synthetic merchant_id (no
// merchant row at all), GrantTrialActivation/RevertTrialActivation write to
// the merchant table by real integer id, so a trial test needs one to
// exist.
func seedTrialMerchant(t *testing.T, db *sql.DB, label string) string {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email)
		VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $3, 'Europe/Paris', 'https://example.com/logo.png', $4)
		RETURNING id`,
		"ITest Trial "+label, "siret-trial-"+label, "t"+strconv.FormatInt(time.Now().UnixNano(), 36), "itest-trial-"+label+"@example.com",
	).Scan(&id); err != nil {
		t.Fatalf("seed merchant (%s): %v", label, err)
	}
	merchantID := strconv.FormatInt(id, 10)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM subscription_overrides WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM sepa_mandates WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM merchant WHERE id = $1`, id)
	})
	return merchantID
}

func getMerchantActivation(t *testing.T, db *sql.DB, merchantID string) (string, bool) {
	t.Helper()
	var activationState string
	var wentLive bool
	if err := db.QueryRowContext(context.Background(), `SELECT activation_state, went_live_at IS NOT NULL FROM merchant WHERE id::text = $1`, merchantID).Scan(&activationState, &wentLive); err != nil {
		t.Fatalf("read merchant: %v", err)
	}
	return activationState, wentLive
}

// TestCreateOverride_PriceWithTrial_GrantsLive_Postgres — B2c-1: a 'price'
// override with a trial_ends_at grants LIVE access immediately, exactly
// like a real mandate would (B2a-3).
func TestCreateOverride_PriceWithTrial_GrantsLive_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTrialMerchant(t, db, "grant")
	seedSubscription(t, db, merchantID, "monthly", nil)

	before, wentLiveBefore := getMerchantActivation(t, db, merchantID)
	if before != "SETUP" || wentLiveBefore {
		t.Fatalf("merchant before override = %s/%v, want SETUP/false", before, wentLiveBefore)
	}

	svc := newTestService(db)
	deadline := time.Now().Add(30 * 24 * time.Hour)
	if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "0", OverrideReasonPartenaire, nil, "itest-staff", nil, &deadline); err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}

	after, wentLiveAfter := getMerchantActivation(t, db, merchantID)
	if after != "LIVE" || !wentLiveAfter {
		t.Fatalf("merchant after override = %s/%v, want LIVE/true", after, wentLiveAfter)
	}
	var subStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&subStatus); err != nil {
		t.Fatalf("read subscriptions.status: %v", err)
	}
	if subStatus != "active" {
		t.Fatalf("subscriptions.status = %q, want active", subStatus)
	}
}

// TestCreateOverride_PriceWithoutTrial_Unchanged_Postgres — "rien ne change"
// for a plain price override (trial_ends_at NULL) — no activation side
// effect, exactly the pre-B2c-1 behavior.
func TestCreateOverride_PriceWithoutTrial_Unchanged_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTrialMerchant(t, db, "no-trial")
	seedSubscription(t, db, merchantID, "monthly", nil)

	svc := newTestService(db)
	if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "5000", OverrideReasonCommercial, nil, "itest-staff", nil, nil); err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}

	state, _ := getMerchantActivation(t, db, merchantID)
	if state != "SETUP" {
		t.Fatalf("activation_state = %q, want unchanged SETUP (no trial_ends_at)", state)
	}
}

// TestRunTrialExpiryCheck_Reminders_Postgres — J-7 and J-1 reminders, each
// sent exactly once.
func TestRunTrialExpiryCheck_Reminders_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTrialMerchant(t, db, "reminders")
	seedSubscription(t, db, merchantID, "monthly", nil)

	svc := newTestService(db)
	mail := &fakeTrialMailer{}
	svc.SetMailer(mail)

	deadline := time.Now().Add(6 * 24 * time.Hour) // within the J-7 window, not yet J-1
	if _, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "0", OverrideReasonGeste, nil, "itest-staff", nil, &deadline); err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}

	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck errors: %v", errs)
	}
	if len(mail.sentTemplates) != 1 || mail.sentTemplates[0] != "trial_expiry_reminder.html" {
		t.Fatalf("sentTemplates = %v, want a single J-7 reminder", mail.sentTemplates)
	}

	// Running again immediately must not resend the J-7 reminder.
	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck (2nd) errors: %v", errs)
	}
	if len(mail.sentTemplates) != 1 {
		t.Fatalf("sentTemplates after 2nd run = %v, want still 1 (no resend)", mail.sentTemplates)
	}

	// Move the deadline into the J-1 window (simulating time passing) and
	// confirm a SECOND, distinct reminder fires.
	if _, err := db.ExecContext(ctx, `UPDATE subscription_overrides SET trial_ends_at = $1 WHERE merchant_id = $2`, time.Now().Add(20*time.Hour), merchantID); err != nil {
		t.Fatalf("move deadline into J-1 window: %v", err)
	}
	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck (J-1) errors: %v", errs)
	}
	if len(mail.sentTemplates) != 2 {
		t.Fatalf("sentTemplates after J-1 window = %v, want 2 (J-7 + J-1)", mail.sentTemplates)
	}
}

// TestRunTrialExpiryCheck_ExpiresWithoutMandate_RevertsToSetup_Postgres —
// §B2c-1: no mandate on file at the deadline -> the override is revoked AND
// (kind='price') merchant.activation_state falls back to SETUP, never
// SUSPENDED (it was never past_due).
func TestRunTrialExpiryCheck_ExpiresWithoutMandate_RevertsToSetup_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTrialMerchant(t, db, "expire-no-mandate")
	seedSubscription(t, db, merchantID, "monthly", nil)

	svc := newTestService(db)
	deadline := time.Now().Add(-1 * time.Hour) // already past
	override, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "0", OverrideReasonTest, nil, "itest-staff", nil, &deadline)
	if err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}

	state, _ := getMerchantActivation(t, db, merchantID)
	if state != "LIVE" {
		t.Fatalf("merchant not LIVE right after CreateOverride: %s", state)
	}

	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck errors: %v", errs)
	}

	active, err := svc.ListActiveOverrides(ctx)
	if err != nil {
		t.Fatalf("ListActiveOverrides: %v", err)
	}
	for _, o := range active {
		if o.ID == override.ID {
			t.Fatalf("override %s still active after expiry", override.ID)
		}
	}

	stateAfter, _ := getMerchantActivation(t, db, merchantID)
	if stateAfter != "SETUP" {
		t.Fatalf("activation_state after expiry (no mandate) = %q, want SETUP", stateAfter)
	}
	var subStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&subStatus); err != nil {
		t.Fatalf("read subscriptions.status: %v", err)
	}
	if subStatus != "setup" {
		t.Fatalf("subscriptions.status after expiry (no mandate) = %q, want setup", subStatus)
	}
}

// TestRunTrialExpiryCheck_ExpiresWithMandate_StaysLive_Postgres — a real
// mandate is on file by the deadline -> the override lapses quietly
// (revoked), activation_state/status are left exactly as the real
// subscription already has them (never reverted).
func TestRunTrialExpiryCheck_ExpiresWithMandate_StaysLive_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTrialMerchant(t, db, "expire-with-mandate")
	seedSubscription(t, db, merchantID, "monthly", nil)

	svc := newTestService(db)
	deadline := time.Now().Add(-1 * time.Hour)
	override, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "0", OverrideReasonMigration, nil, "itest-staff", nil, &deadline)
	if err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}

	// A real mandate shows up before the cron gets to it.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sepa_mandates (id, merchant_id, stripe_payment_method_id, status, accepted_at)
		VALUES ($1, $2, 'pm_itest_trial', 'active', now())`, "sepa-itest-trial", merchantID); err != nil {
		t.Fatalf("seed sepa_mandates: %v", err)
	}

	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck errors: %v", errs)
	}

	active, err := svc.ListActiveOverrides(ctx)
	if err != nil {
		t.Fatalf("ListActiveOverrides: %v", err)
	}
	for _, o := range active {
		if o.ID == override.ID {
			t.Fatalf("override %s still active after expiry", override.ID)
		}
	}

	stateAfter, _ := getMerchantActivation(t, db, merchantID)
	if stateAfter != "LIVE" {
		t.Fatalf("activation_state after expiry (mandate on file) = %q, want unchanged LIVE", stateAfter)
	}
}

// TestRunTrialExpiryCheck_MandateAcceptedBeforeDeadline_Postgres — LOT B F2's
// precise scenario: a 'price' override is LIVE via trial_ends_at, and a real
// SEPA mandate is accepted WHILE the trial is still running (well before its
// deadline), not at the last minute. Confirms the expiry check, run only
// once the deadline has actually passed, still finds that mandate and lets
// the trial lapse quietly (unlike TestRunTrialExpiryCheck_ExpiresWithMandate_StaysLive_Postgres,
// which creates the override with an already-past deadline — this test
// creates it with a real future deadline first, matching the order of
// events the brief asked to be confirmed rather than assumed).
func TestRunTrialExpiryCheck_MandateAcceptedBeforeDeadline_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTrialMerchant(t, db, "mandate-before-deadline")
	seedSubscription(t, db, merchantID, "monthly", nil)

	svc := newTestService(db)
	deadline := time.Now().Add(48 * time.Hour) // still running, not yet due
	override, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "0", OverrideReasonTest, nil, "itest-staff", nil, &deadline)
	if err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}
	state, _ := getMerchantActivation(t, db, merchantID)
	if state != "LIVE" {
		t.Fatalf("merchant not LIVE right after CreateOverride: %s", state)
	}

	// The mandate is accepted here, mid-trial — well before the deadline,
	// exactly the ordering the brief asked to confirm.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sepa_mandates (id, merchant_id, stripe_payment_method_id, status, accepted_at)
		VALUES ($1, $2, 'pm_itest_early', 'active', now())`, "sepa-itest-early", merchantID); err != nil {
		t.Fatalf("seed sepa_mandates: %v", err)
	}

	// Running the check now (deadline still in the future) must not touch
	// anything — trial expiry is only ever evaluated at/after the deadline.
	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck (before deadline) errors: %v", errs)
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
		t.Fatalf("override %s revoked before its deadline", override.ID)
	}

	// Time passes; the deadline is now in the past (same simulation as the
	// rest of this file — a real Test Clock is out of scope for a plain
	// Postgres integration test).
	if _, err := db.ExecContext(ctx, `UPDATE subscription_overrides SET trial_ends_at = $1 WHERE id = $2`, time.Now().Add(-1*time.Hour), override.ID); err != nil {
		t.Fatalf("move deadline into the past: %v", err)
	}

	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck (after deadline) errors: %v", errs)
	}
	active, err = svc.ListActiveOverrides(ctx)
	if err != nil {
		t.Fatalf("ListActiveOverrides: %v", err)
	}
	for _, o := range active {
		if o.ID == override.ID {
			t.Fatalf("override %s still active after expiry", override.ID)
		}
	}
	stateAfter, _ := getMerchantActivation(t, db, merchantID)
	if stateAfter != "LIVE" {
		t.Fatalf("activation_state after expiry (mandate accepted mid-trial) = %q, want unchanged LIVE", stateAfter)
	}
}

// TestRunTrialExpiryCheck_InactiveMandateOnly_RevertsToSetup_Postgres — LOT B
// F2: a sepa_mandates row existing is not enough on its own — it must be
// status='active'. This is the exact regression HasSepaMandate's original
// bare EXISTS(...) query (pre-F2) could not catch: no code path in this
// repository writes any other status today, so the bug was silent by
// accident, never exercised. Reproduced directly at the repository level
// (RunTrialExpiryCheck itself, through Service) since nothing in the current
// codebase can produce a non-active mandate row through its own API yet.
func TestRunTrialExpiryCheck_InactiveMandateOnly_RevertsToSetup_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTrialMerchant(t, db, "mandate-inactive")
	seedSubscription(t, db, merchantID, "monthly", nil)

	svc := newTestService(db)
	deadline := time.Now().Add(-1 * time.Hour) // already past
	override, err := svc.CreateOverride(ctx, merchantID, OverrideKindPrice, "0", OverrideReasonTest, nil, "itest-staff", nil, &deadline)
	if err != nil {
		t.Fatalf("CreateOverride: %v", err)
	}

	// A mandate row exists, but it was revoked/canceled — not the active
	// mandate that should let the trial lapse quietly.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sepa_mandates (id, merchant_id, stripe_payment_method_id, status, accepted_at)
		VALUES ($1, $2, 'pm_itest_revoked', 'canceled', now())`, "sepa-itest-revoked", merchantID); err != nil {
		t.Fatalf("seed sepa_mandates: %v", err)
	}

	if errs := svc.RunTrialExpiryCheck(ctx); len(errs) != 0 {
		t.Fatalf("RunTrialExpiryCheck errors: %v", errs)
	}

	active, err := svc.ListActiveOverrides(ctx)
	if err != nil {
		t.Fatalf("ListActiveOverrides: %v", err)
	}
	for _, o := range active {
		if o.ID == override.ID {
			t.Fatalf("override %s still active after expiry", override.ID)
		}
	}
	stateAfter, _ := getMerchantActivation(t, db, merchantID)
	if stateAfter != "SETUP" {
		t.Fatalf("activation_state after expiry (mandate present but not active) = %q, want SETUP", stateAfter)
	}
}
