//go:build postgres_integration

package onboarding

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
)

// TestRecomputeOnboarding_Postgres covers LOT A Semaine 3, Chantier 13: each
// of the four automatic conditions (menu/device/team/logo) flips its task to
// 'done' once — and only once — its underlying business event is true, and a
// repeat call is a no-op (idempotent).
func TestRecomputeOnboarding_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Recompute', 'a', '1', 's', '75001', 'Paris', 'siret-recompute', 'https://x', '06', 'mtok-recompute', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM onboarding_tasks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	repo := NewRepository(db)
	svc := NewService(repo)
	if err := repo.CreateDefaultTasks(ctx, merchantID); err != nil {
		t.Fatalf("CreateDefaultTasks: %v", err)
	}

	// The owner's own users_rights row — normally created by signup
	// alongside the merchant; TeamConditionMet's "second active row" only
	// makes sense once this first one already exists.
	if _, err := db.ExecContext(ctx, `INSERT INTO users_rights (merchant_id, token) VALUES ($1, 'itest-owner-rights-token')`, merchantID); err != nil {
		t.Fatalf("seed owner users_rights: %v", err)
	}

	statusOf := func(taskKey string) string {
		t.Helper()
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM onboarding_tasks WHERE merchant_id = $1 AND task_key = $2`, merchantID, taskKey).Scan(&status); err != nil {
			t.Fatalf("status of %q: %v", taskKey, err)
		}
		return status
	}

	// --- before any event: everything pending ---
	if err := svc.RecomputeOnboarding(ctx, merchantID); err != nil {
		t.Fatalf("RecomputeOnboarding (nothing yet): %v", err)
	}
	for _, key := range []string{"menu", "device", "team", "logo"} {
		if got := statusOf(key); got != StatusPending {
			t.Fatalf("%s status = %q before any event, want %q", key, got, StatusPending)
		}
	}

	// --- menu: a priced product with all three VAT rates set ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO products (merchant_id, name, price, tva_in_id, tva_delivery_id, tva_take_away_id, category)
		VALUES ($1, 'ITest Product', 1000, 1, 1, 1, 'Test')
	`, merchantID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	// --- device: first kiosk pairing ---
	if _, err := db.ExecContext(ctx, `INSERT INTO kiosks (id, merchant_id, name) VALUES ($1, $2, 'ITest Kiosk')`, "itest-kiosk-"+merchantID, merchantID); err != nil {
		t.Fatalf("seed kiosk: %v", err)
	}

	// --- team: a second active users_rights row (the "owner" created at
	// signup is conceptually the first) ---
	if _, err := db.ExecContext(ctx, `INSERT INTO users_rights (merchant_id, token) VALUES ($1, 'itest-rights-token')`, merchantID); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}

	// --- logo ---
	if _, err := db.ExecContext(ctx, `UPDATE merchant SET logo_url = 'https://example.com/logo.png' WHERE id = $1`, merchantIntID); err != nil {
		t.Fatalf("seed logo_url: %v", err)
	}

	if err := svc.RecomputeOnboarding(ctx, merchantID); err != nil {
		t.Fatalf("RecomputeOnboarding (after events): %v", err)
	}
	for _, key := range []string{"menu", "device", "team", "logo"} {
		if got := statusOf(key); got != StatusDone {
			t.Fatalf("%s status = %q after its event, want %q", key, got, StatusDone)
		}
	}
	// "payment" is untouched — LOT B's hook, not this chantier.
	if got := statusOf("payment"); got != StatusPending {
		t.Fatalf("payment status = %q, want %q (untouched by RecomputeOnboarding)", got, StatusPending)
	}

	// --- idempotent: calling again changes nothing and errors on nothing ---
	if err := svc.RecomputeOnboarding(ctx, merchantID); err != nil {
		t.Fatalf("RecomputeOnboarding (repeat call): %v", err)
	}
	for _, key := range []string{"menu", "device", "team", "logo"} {
		if got := statusOf(key); got != StatusDone {
			t.Fatalf("%s status = %q after repeat call, want still %q", key, got, StatusDone)
		}
	}
}

// TestSkipTask_Postgres covers POST /v1/merchants/{id}/onboarding/{code}/skip:
// owner-only, restricted to team/logo, mandatory reason, refuses a task
// already done.
func TestSkipTask_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Skip', 'a', '1', 's', '75001', 'Paris', 'siret-skip', 'https://x', '06', 'mtok-skip', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM onboarding_tasks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	repo := NewRepository(db)
	svc := NewService(repo)
	if err := repo.CreateDefaultTasks(ctx, merchantID); err != nil {
		t.Fatalf("CreateDefaultTasks: %v", err)
	}
	// The owner's own users_rights row — see TestRecomputeOnboarding_Postgres.
	if _, err := db.ExecContext(ctx, `INSERT INTO users_rights (merchant_id, token) VALUES ($1, 'itest-owner-rights-token-skip')`, merchantID); err != nil {
		t.Fatalf("seed owner users_rights: %v", err)
	}

	ownerCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-owner", MerchantID: merchantID, Rights: auth.UserRowRights{Admin: true}})
	staffCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-staff", MerchantID: merchantID, Rights: auth.UserRowRights{Admin: false}})
	otherMerchantOwnerCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-other-owner", MerchantID: "999999", Rights: auth.UserRowRights{Admin: true}})

	// --- not skippable ---
	if err := svc.SkipTask(ownerCtx, merchantID, "menu", "not ready"); err == nil {
		t.Fatal("SkipTask(menu): expected ErrOnboardingTaskNotSkippable, got nil")
	}

	// --- missing reason ---
	if err := svc.SkipTask(ownerCtx, merchantID, "team", ""); err == nil {
		t.Fatal("SkipTask(team, no reason): expected ErrOnboardingSkipReasonRequired, got nil")
	}

	// --- non-owner forbidden ---
	if err := svc.SkipTask(staffCtx, merchantID, "team", "solo owner for now"); err == nil {
		t.Fatal("SkipTask by non-admin: expected an error, got nil")
	}

	// --- wrong merchant forbidden ---
	if err := svc.SkipTask(otherMerchantOwnerCtx, merchantID, "team", "solo owner for now"); err == nil {
		t.Fatal("SkipTask from a different merchant's token: expected an error, got nil")
	}

	// --- success ---
	if err := svc.SkipTask(ownerCtx, merchantID, "team", "solo owner for now"); err != nil {
		t.Fatalf("SkipTask(team): %v", err)
	}
	var status, reason string
	if err := db.QueryRowContext(ctx, `SELECT status, skip_reason FROM onboarding_tasks WHERE merchant_id = $1 AND task_key = 'team'`, merchantID).Scan(&status, &reason); err != nil {
		t.Fatalf("read back skipped task: %v", err)
	}
	if status != StatusSkipped || reason != "solo owner for now" {
		t.Fatalf("team task = (status=%q reason=%q), want (%q, %q)", status, reason, StatusSkipped, "solo owner for now")
	}

	// --- a business event still promotes a skipped task to done ---
	if _, err := db.ExecContext(ctx, `INSERT INTO users_rights (merchant_id, token) VALUES ($1, 'itest-rights-token-skip')`, merchantID); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}
	if err := svc.RecomputeOnboarding(ctx, merchantID); err != nil {
		t.Fatalf("RecomputeOnboarding after skip: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM onboarding_tasks WHERE merchant_id = $1 AND task_key = 'team'`, merchantID).Scan(&status); err != nil {
		t.Fatalf("read back team status: %v", err)
	}
	if status != StatusDone {
		t.Fatalf("team status after real event post-skip = %q, want %q (a real event still wins over a skip)", status, StatusDone)
	}

	// --- cannot skip an already-done task ---
	if err := svc.SkipTask(ownerCtx, merchantID, "logo", "later"); err != nil {
		t.Fatalf("SkipTask(logo): %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE onboarding_tasks SET status = $1 WHERE merchant_id = $2 AND task_key = 'logo'`, StatusDone, merchantID); err != nil {
		t.Fatalf("force logo done: %v", err)
	}
	if err := svc.SkipTask(ownerCtx, merchantID, "logo", "later again"); err == nil {
		t.Fatal("SkipTask(logo) on an already-done task: expected ErrOnboardingTaskAlreadyDone, got nil")
	}
}
