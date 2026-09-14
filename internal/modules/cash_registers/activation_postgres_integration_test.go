//go:build postgres_integration

package cash_registers

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestIsActivatedForOrdering_Postgres — LOT B B2b-2/B2b-3 (§7.5/§7.6) :
// activation_state != 'LIVE' or subscriptions.status = 'suspended' both
// refuse cash register opening; LIVE + any non-suspended status allows it.
func TestIsActivatedForOrdering_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	seed := func(label, activationState, subStatus string) string {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email, activation_state)
			VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $3, 'Europe/Paris', 'https://example.com/logo.png', $4, $5)
			RETURNING id`,
			"ITest Activation "+label, "siret-act-"+label, "t"+strconv.FormatInt(time.Now().UnixNano(), 36), "itest-act-"+label+"@example.com", activationState,
		).Scan(&id); err != nil {
			t.Fatalf("seed merchant (%s): %v", label, err)
		}
		merchantID := strconv.FormatInt(id, 10)
		if subStatus != "" {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
				VALUES ($1, 1, '', 'monthly', $2)`, merchantID, subStatus); err != nil {
				t.Fatalf("seed subscriptions (%s): %v", label, err)
			}
		}
		t.Cleanup(func() {
			bg := context.Background()
			_, _ = db.ExecContext(bg, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(bg, `DELETE FROM merchant WHERE id = $1`, id)
		})
		return merchantID
	}

	repo := NewCashRegisterRepository(db)

	cases := []struct {
		name            string
		activationState string
		subStatus       string
		wantActivated   bool
	}{
		{"setup, no subscription row", "SETUP", "", false},
		{"live, active subscription", "LIVE", "active", true},
		{"live, suspended subscription", "LIVE", "suspended", false},
		{"live, past_due subscription still allowed", "LIVE", "past_due", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			merchantID := seed(strconv.Itoa(len(c.name)), c.activationState, c.subStatus)
			got, err := repo.IsActivatedForOrdering(ctx, merchantID)
			if err != nil {
				t.Fatalf("IsActivatedForOrdering: %v", err)
			}
			if got != c.wantActivated {
				t.Fatalf("IsActivatedForOrdering(%s/%s) = %v, want %v", c.activationState, c.subStatus, got, c.wantActivated)
			}
		})
	}
}
