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

// TestGetOnboarding_Postgres covers LOT A Semaine 2, Chantier 6c's read
// endpoint: CreateDefaultTasks seeds the five fixed tasks, ListByMerchant
// (via the service, scoped to the caller's own merchant) returns them.
func TestGetOnboarding_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func(merchantID string) {
		if merchantID == "" {
			return
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM onboarding_tasks WHERE merchant_id = $1`, merchantID)
	}

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Onboarding', 'a', '1', 's', '75001', 'Paris', 'siret-onboarding', 'https://x', '06', 'mtok-onboarding', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() {
		cleanup(merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	repo := NewRepository(db)
	svc := NewService(repo)

	if err := repo.CreateDefaultTasks(ctx, merchantID); err != nil {
		t.Fatalf("CreateDefaultTasks: %v", err)
	}

	// --- scoped to the caller's own merchant: succeeds ---
	callerCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-onboarding-user", MerchantID: merchantID})
	tasks, err := svc.GetOnboarding(callerCtx, merchantID)
	if err != nil {
		t.Fatalf("GetOnboarding: %v", err)
	}
	if len(tasks) != len(TaskKeys) {
		t.Fatalf("got %d tasks, want %d", len(tasks), len(TaskKeys))
	}
	for i, want := range TaskKeys {
		if tasks[i].TaskKey != want || tasks[i].Status != StatusPending {
			t.Fatalf("tasks[%d] = %+v, want task_key=%q status=%q", i, tasks[i], want, StatusPending)
		}
	}

	// --- a different merchant's token must not read this one's tasks ---
	otherCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-onboarding-other", MerchantID: "999999"})
	if _, err := svc.GetOnboarding(otherCtx, merchantID); err == nil {
		t.Fatal("GetOnboarding from a different merchant's token: expected an error, got nil")
	}
}
