//go:build postgres_integration

package subscriptions

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pricing"
)

// seedSubscription inserts a minimal subscriptions row for merchantID —
// subscription_items/subscriptions have no foreign key to merchant (same
// posture as B1a's package_id finding), so no real merchant row is needed.
func seedSubscription(t *testing.T, db *sql.DB, merchantID string, billingCycle string, overridePriceCents *int) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, override_price_cents, status)
		VALUES ($1, 1, '', $2, $3, 'active')
	`, merchantID, billingCycle, overridePriceCents); err != nil {
		t.Fatalf("seedSubscription(%s): %v", merchantID, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
	})
}

func newTestService(db *sql.DB) *Service {
	return NewService(db, NewRepository(db), pricing.NewRepository(db), pricing.NewService(pricing.NewRepository(db)))
}

func cleanupItems(t *testing.T, db *sql.DB, merchantID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM subscription_items WHERE merchant_id = $1`, merchantID)
	})
}

// TestComputeSubscriptionAmount_Nominal_Postgres — rule 2: sum of active
// items at pricing_catalog prices, monthly, no override, no discount.
func TestComputeSubscriptionAmount_Nominal_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-amt-nominal"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}
	if _, err := repo.AddItem(ctx, merchantID, CodeHACCP, KindModule, 1, 3500); err != nil {
		t.Fatalf("AddItem(haccp): %v", err)
	}

	amount, err := newTestService(db).ComputeSubscriptionAmount(ctx, merchantID)
	if err != nil {
		t.Fatalf("ComputeSubscriptionAmount: %v", err)
	}
	if amount.Overridden {
		t.Fatalf("Overridden = true, want false")
	}
	if amount.TotalCents != 7900+3500 {
		t.Fatalf("TotalCents = %d, want %d", amount.TotalCents, 7900+3500)
	}
	if len(amount.Breakdown) != 2 {
		t.Fatalf("Breakdown = %+v, want 2 lines", amount.Breakdown)
	}
}

// TestComputeSubscriptionAmount_Override_Postgres — rule 1: override wins as
// TotalCents, but Breakdown still reflects the full grid computation.
func TestComputeSubscriptionAmount_Override_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-amt-override"
	override := 5000
	seedSubscription(t, db, merchantID, "monthly", &override)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeComplet, KindPlan, 1, 18900); err != nil {
		t.Fatalf("AddItem(complet): %v", err)
	}

	amount, err := newTestService(db).ComputeSubscriptionAmount(ctx, merchantID)
	if err != nil {
		t.Fatalf("ComputeSubscriptionAmount: %v", err)
	}
	if !amount.Overridden {
		t.Fatal("Overridden = false, want true")
	}
	if amount.TotalCents != 5000 {
		t.Fatalf("TotalCents = %d, want 5000 (the override)", amount.TotalCents)
	}
	if len(amount.Breakdown) != 1 || amount.Breakdown[0].AmountCents != 18900 {
		t.Fatalf("Breakdown = %+v, want the grid detail (18900) for comparison", amount.Breakdown)
	}
}

// TestComputeSubscriptionAmount_PlanningEmployeeVariableQuantity_Postgres —
// rule 3: planning_employee's quantity is recomputed at call time from live
// employee records, not read from the stored row, with the 10-employee
// franchise on Pro/Complet.
func TestComputeSubscriptionAmount_PlanningEmployeeVariableQuantity_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-amt-planning-emp"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM employees WHERE merchant_id = $1`, merchantID)
	})

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodePro, KindPlan, 1, 12900); err != nil {
		t.Fatalf("AddItem(pro): %v", err)
	}
	// Stored quantity (99) must be ignored — recomputed from live employees.
	if _, err := repo.AddItem(ctx, merchantID, CodePlanningEmployee, KindMetered, 99, 250); err != nil {
		t.Fatalf("AddItem(planning_employee): %v", err)
	}

	for i := 0; i < 13; i++ {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO employees (id, merchant_id, first_name, last_name, position_id, contract_type_code)
			VALUES ($1, $2, 'Test', 'Employee', 'pos-dummy', 'cdi')
		`, "itest-amt-emp-"+string(rune('a'+i)), merchantID); err != nil {
			t.Fatalf("seed employee %d: %v", i, err)
		}
	}

	amount, err := newTestService(db).ComputeSubscriptionAmount(ctx, merchantID)
	if err != nil {
		t.Fatalf("ComputeSubscriptionAmount: %v", err)
	}
	var planningEmpLine *BreakdownLine
	for i := range amount.Breakdown {
		if amount.Breakdown[i].Code == CodePlanningEmployee {
			planningEmpLine = &amount.Breakdown[i]
		}
	}
	if planningEmpLine == nil {
		t.Fatalf("no planning_employee line in %+v", amount.Breakdown)
	}
	// 13 employees, Pro plan -> 13 - 10 = 3, at 250 cents.
	if planningEmpLine.Quantity != 3 || planningEmpLine.AmountCents != 750 {
		t.Fatalf("planning_employee line = %+v, want quantity=3 amount=750", planningEmpLine)
	}
}

// TestComputeSubscriptionAmount_ExtraPOS_Postgres — rule 3: "postes actifs
// moins 1", bridged from subscription_items' "extra_pos" to pricing_catalog's
// "extra_seat" addon code.
func TestComputeSubscriptionAmount_ExtraPOS_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-amt-extrapos"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM cash_desks WHERE merchant_id = $1`, merchantID)
	})

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}
	if _, err := repo.AddItem(ctx, merchantID, CodeExtraPOS, KindMetered, 99, 2500); err != nil {
		t.Fatalf("AddItem(extra_pos): %v", err)
	}

	// 3 active cash desks -> 3 - 1 = 2 billed extra POS.
	for i := 0; i < 3; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO cash_desks (merchant_id, name) VALUES ($1, $2)`, merchantID, "Caisse "+string(rune('A'+i))); err != nil {
			t.Fatalf("seed cash_desk %d: %v", i, err)
		}
	}

	amount, err := newTestService(db).ComputeSubscriptionAmount(ctx, merchantID)
	if err != nil {
		t.Fatalf("ComputeSubscriptionAmount: %v", err)
	}
	var extraPOSLine *BreakdownLine
	for i := range amount.Breakdown {
		if amount.Breakdown[i].Code == CodeExtraPOS {
			extraPOSLine = &amount.Breakdown[i]
		}
	}
	if extraPOSLine == nil || extraPOSLine.Quantity != 2 || extraPOSLine.AmountCents != 5000 {
		t.Fatalf("extra_pos line = %+v, want quantity=2 amount=5000", extraPOSLine)
	}
}

// TestComputeSubscriptionAmount_AnnualCycle_Postgres — rule 4: "dix mois
// facturés sur douze".
func TestComputeSubscriptionAmount_AnnualCycle_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-amt-annual"
	seedSubscription(t, db, merchantID, "annual", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}

	amount, err := newTestService(db).ComputeSubscriptionAmount(ctx, merchantID)
	if err != nil {
		t.Fatalf("ComputeSubscriptionAmount: %v", err)
	}
	if amount.TotalCents != 7900*10 {
		t.Fatalf("TotalCents = %d, want %d (10 months)", amount.TotalCents, 7900*10)
	}
}

// TestComputeSubscriptionAmount_MultiMerchantDiscount_Postgres — rule 5: 10%
// off when the merchant's admin also administers another merchant.
func TestComputeSubscriptionAmount_MultiMerchantDiscount_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-amt-mm-1"
	otherMerchantID := "itest-amt-mm-2"
	ownerUserID := "itest-amt-mm-owner"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users_rights WHERE user_id = $1`, ownerUserID)
	})

	for i, mid := range []string{merchantID, otherMerchantID} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users_rights (user_id, merchant_id, token, admin, enabled) VALUES ($1, $2, $3, TRUE, TRUE)
		`, ownerUserID, mid, "itest-amt-mm-token-"+string(rune('a'+i))); err != nil {
			t.Fatalf("seed users_rights for %s: %v", mid, err)
		}
	}

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}

	amount, err := newTestService(db).ComputeSubscriptionAmount(ctx, merchantID)
	if err != nil {
		t.Fatalf("ComputeSubscriptionAmount: %v", err)
	}
	want := 7900 - 7900*10/100
	if amount.TotalCents != want {
		t.Fatalf("TotalCents = %d, want %d (10%% off)", amount.TotalCents, want)
	}
	found := false
	for _, line := range amount.Breakdown {
		if line.Code == "multi_merchant_discount" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no multi_merchant_discount line in %+v", amount.Breakdown)
	}
}

// TestComputeSubscriptionAmount_UnsupportedCode_Postgres — "kiosk" and "sms"
// are valid subscription_items codes (AddItem accepts them) but
// pricing_catalog has no usable price for either (see
// models.ErrSubscriptionItemPriceUnavailable's doc comment) — must fail
// loudly, never silently bill 0.
func TestComputeSubscriptionAmount_UnsupportedCode_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-amt-unsupported"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeKiosk, KindModule, 1, 18900); err != nil {
		t.Fatalf("AddItem(kiosk): %v", err)
	}

	if _, err := newTestService(db).ComputeSubscriptionAmount(ctx, merchantID); !errors.Is(err, models.ErrSubscriptionItemPriceUnavailable) {
		t.Fatalf("ComputeSubscriptionAmount(kiosk) = %v, want ErrSubscriptionItemPriceUnavailable", err)
	}
}

// TestComputeSubscriptionAmount_SubscriptionNotFound_Postgres — no
// subscriptions row at all for merchantID.
func TestComputeSubscriptionAmount_SubscriptionNotFound_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	if _, err := newTestService(db).ComputeSubscriptionAmount(ctx, "itest-amt-does-not-exist"); !errors.Is(err, models.ErrSubscriptionNotFound) {
		t.Fatalf("ComputeSubscriptionAmount = %v, want ErrSubscriptionNotFound", err)
	}
}
