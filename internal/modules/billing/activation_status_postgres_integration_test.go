//go:build postgres_integration

package billing

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestGetActivationStatus_Postgres — B2b-3's bandeau data source: reads
// merchant.activation_state/subscriptions.status fresh (see the doc comment
// on Repository.GetActivationStatus for why this must never come from the
// Redis-cached authenticated user).
func TestGetActivationStatus_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email, activation_state)
		VALUES ('ITest Activation Status', 'addr', '1', 'street', '75001', 'Paris', 'siret-actstat', 'https://example.com', '0600000000', $1, 'Europe/Paris', 'https://example.com/logo.png', 'itest-actstat@example.com', 'SETUP')
		RETURNING id`, "t"+strconv.FormatInt(time.Now().UnixNano(), 36)).Scan(&id); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(id, 10)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM merchant WHERE id = $1`, id)
	})

	repo := NewRepository(db)

	st, err := repo.GetActivationStatus(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetActivationStatus (no subscription row): %v", err)
	}
	if st.ActivationState != "SETUP" || st.SubscriptionStatus != "" {
		t.Fatalf("GetActivationStatus = %+v, want SETUP/'' (no subscriptions row yet)", st)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
		VALUES ($1, 1, '', 'monthly', 'active')`, merchantID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE merchant SET activation_state = 'LIVE' WHERE id = $1`, id); err != nil {
		t.Fatalf("set LIVE: %v", err)
	}

	st, err = repo.GetActivationStatus(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetActivationStatus (LIVE): %v", err)
	}
	if st.ActivationState != "LIVE" || st.SubscriptionStatus != "active" {
		t.Fatalf("GetActivationStatus = %+v, want LIVE/active", st)
	}
}
