//go:build postgres_integration

package discounts

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestDiscountsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-disc-m1"
	discountID := "itest-discount-1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts_schedules WHERE discount_id = $1`, discountID)
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts_products WHERE discount_id = $1`, discountID)
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	orderType := OrderTypeDelivery
	minOrderUnit := MinOrderUnitCurrency
	validFrom := time.Now().UTC().Add(-1 * time.Hour)
	validTo := time.Now().UTC().Add(48 * time.Hour)
	minOrder := 10.0

	created, err := repo.CreateDiscount(ctx, merchantID, &CreateDiscountRequest{
		DiscountID:         discountID,
		DiscountName:       "ITest Discount",
		DiscountDesc:       "integration test discount",
		PreferredOrder:     1,
		OrderType:          &orderType,
		DiscountValue:      15,
		DiscountUnit:       DiscountUnitPercentage,
		ValidFrom:          validFrom,
		ValidTo:            &validTo,
		MinOrderValue:      &minOrder,
		MinOrderUnit:       &minOrderUnit,
		DiscountedQuantity: 1,
		IsCumulative:       false,
		IsTimeLimited:      true,
		Available:          true,
		Products: []CreateProductRequest{
			{ProductID: "9001"},
		},
		Schedules: []CreateScheduleRequest{
			{DayOfWeek: 1, AvailableFrom: mustParseHM(t, "09:00"), AvailableTo: mustParseHM(t, "22:00")},
		},
	})
	if err != nil {
		t.Fatalf("CreateDiscount failed against postgres: %v", err)
	}
	if !created.Enabled || !created.Available || len(created.Products) != 1 || len(created.Schedules) != 1 {
		t.Fatalf("unexpected created discount: %+v", created)
	}

	// GetActiveDiscounts: enabled = true AND available = true AND valid window (dbx rebind + boolean literals).
	active, err := repo.GetActiveDiscounts(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetActiveDiscounts failed against postgres: %v", err)
	}
	found := false
	for _, d := range active {
		if d.DiscountID == discountID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected created discount in GetActiveDiscounts")
	}

	all, err := repo.GetAllDiscounts(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetAllDiscounts failed against postgres: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 discount from GetAllDiscounts, got %d", len(all))
	}

	fetched, err := repo.GetDiscountByID(ctx, merchantID, discountID)
	if err != nil {
		t.Fatalf("GetDiscountByID failed against postgres: %v", err)
	}
	if fetched.DiscountName != "ITest Discount" {
		t.Fatalf("unexpected fetched discount: %+v", fetched)
	}

	// UpdateDiscount: dynamic SET clause + schedule replacement (TIME column format fix).
	newName := "ITest Discount Renamed"
	updated, err := repo.UpdateDiscount(ctx, merchantID, discountID, &UpdateDiscountRequest{
		DiscountName: &newName,
		Schedules: []CreateScheduleRequest{
			{DayOfWeek: 2, AvailableFrom: mustParseHM(t, "10:00"), AvailableTo: mustParseHM(t, "20:00")},
		},
	})
	if err != nil {
		t.Fatalf("UpdateDiscount failed against postgres: %v", err)
	}
	if updated.DiscountName != newName {
		t.Fatalf("expected renamed discount, got %+v", updated)
	}
	if len(updated.Schedules) != 1 || updated.Schedules[0].DayOfWeek != 2 {
		t.Fatalf("expected schedule replaced with day_of_week 2, got %+v", updated.Schedules)
	}

	// DeleteDiscount: soft delete (enabled = false), then not found by ID or list.
	if err := repo.DeleteDiscount(ctx, merchantID, discountID); err != nil {
		t.Fatalf("DeleteDiscount failed against postgres: %v", err)
	}
	if _, err := repo.GetDiscountByID(ctx, merchantID, discountID); err != ErrDiscountNotFound {
		t.Fatalf("expected ErrDiscountNotFound after delete, got %v", err)
	}
	if err := repo.DeleteDiscount(ctx, merchantID, discountID); err != ErrDiscountNotFound {
		t.Fatalf("expected ErrDiscountNotFound deleting already-deleted discount, got %v", err)
	}
}

// Regression test: creating a discount with MinOrderValue left nil (as any
// client omitting/nulling this optional field would send) must not violate
// the discounts.min_order_value NOT NULL DEFAULT 0 constraint.
func TestDiscountsRepository_Postgres_NilMinOrderValue(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-disc-m2"
	discountID := "itest-discount-nil-min-order"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM discounts WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	created, err := repo.CreateDiscount(ctx, merchantID, &CreateDiscountRequest{
		DiscountID:         discountID,
		DiscountName:       "ITest Nil MinOrderValue",
		DiscountDesc:       "regression test for nil MinOrderValue",
		DiscountValue:      10,
		DiscountUnit:       DiscountUnitPercentage,
		ValidFrom:          time.Now().UTC(),
		MinOrderValue:      nil,
		DiscountedQuantity: 1,
		Available:          true,
	})
	if err != nil {
		t.Fatalf("CreateDiscount with nil MinOrderValue failed against postgres: %v", err)
	}
	if created.MinOrderValue == nil || *created.MinOrderValue != 0 {
		t.Fatalf("expected MinOrderValue to default to 0, got %+v", created.MinOrderValue)
	}
}

func mustParseHM(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse("15:04", s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tm
}
