//go:build postgres_integration

package subscriptions

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// TestPreviewChange_NoPackSwitch_Postgres — §7.7: a merchant on the
// essentiel plan alone is already on the cheapest option (79 < pro's flat
// 129 < complet's flat 189 with zero modules) — no pack_cheaper hint.
func TestPreviewChange_NoPackSwitch_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-preview-nopack"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}

	preview, err := newTestService(db).PreviewChange(ctx, merchantID, nil, nil)
	if err != nil {
		t.Fatalf("PreviewChange: %v", err)
	}
	if preview.CurrentTotalCents != 7900 || preview.NewTotalCents != 7900 {
		t.Fatalf("Current/NewTotalCents = %d/%d, want 7900/7900 (no change requested)", preview.CurrentTotalCents, preview.NewTotalCents)
	}
	if preview.PackCheaper {
		t.Fatalf("PackCheaper = true, want false: %+v", preview.PackComparison)
	}
}

// TestPreviewChange_PackSwitch_Postgres — §7.7: adding "reservation" (5900)
// to an essentiel-plan merchant (7900) makes the à-la-carte total (13800)
// cross above Pro's flat 12900 (reservation fits in Pro's 2 free module
// slots) — the preview must surface the comparison, and must not write
// anything (still just an essentiel item afterwards).
func TestPreviewChange_PackSwitch_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-preview-pack"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}

	svc := newTestService(db)
	preview, err := svc.PreviewChange(ctx, merchantID, []string{CodeReservation}, nil)
	if err != nil {
		t.Fatalf("PreviewChange: %v", err)
	}
	if preview.CurrentTotalCents != 7900 {
		t.Fatalf("CurrentTotalCents = %d, want 7900", preview.CurrentTotalCents)
	}
	if preview.NewTotalCents != 7900+5900 {
		t.Fatalf("NewTotalCents = %d, want %d", preview.NewTotalCents, 7900+5900)
	}
	if !preview.PackCheaper {
		t.Fatal("PackCheaper = false, want true (13800 à la carte vs Pro's 12900)")
	}
	if preview.PackComparison == nil || preview.PackComparison.PlanCode != "pro" || preview.PackComparison.MonthlyTotalCents != 12900 {
		t.Fatalf("PackComparison = %+v, want {pro 12900}", preview.PackComparison)
	}

	// Aucune écriture : toujours un seul item (essentiel) en base.
	active, err := repo.ListActive(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Code != CodeEssentiel {
		t.Fatalf("ListActive after preview = %+v, want unchanged (essentiel only) — preview must not write", active)
	}
}

// TestPreviewChange_RejectsNonBillableAdd_Postgres — a preview must not
// promise a change the real endpoint would refuse.
func TestPreviewChange_RejectsNonBillableAdd_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-preview-unpriceable"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}

	if _, err := newTestService(db).PreviewChange(ctx, merchantID, []string{CodeKiosk}, nil); !errors.Is(err, models.ErrSubscriptionItemPriceUnavailable) {
		t.Fatalf("PreviewChange(add=kiosk) = %v, want ErrSubscriptionItemPriceUnavailable", err)
	}
}

// TestApplyItemChanges_Postgres — B1e's POST /v1/subscriptions/items: a real
// add+remove recalculates subscription_items and never touches
// override_price_cents when one is already set.
func TestApplyItemChanges_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-apply-items"
	override := 1234
	seedSubscription(t, db, merchantID, "monthly", &override)
	cleanupItems(t, db, merchantID)

	repo := NewRepository(db)
	if _, err := repo.AddItem(ctx, merchantID, CodeEssentiel, KindPlan, 1, 7900); err != nil {
		t.Fatalf("AddItem(essentiel): %v", err)
	}
	if _, err := repo.AddItem(ctx, merchantID, CodeHACCP, KindModule, 1, 3500); err != nil {
		t.Fatalf("AddItem(haccp): %v", err)
	}

	svc := newTestService(db)
	amount, err := svc.ApplyItemChanges(ctx, merchantID, []string{CodeReservation}, []string{CodeHACCP})
	if err != nil {
		t.Fatalf("ApplyItemChanges: %v", err)
	}

	// override_price_cents (1234) still wins as TotalCents — untouched by
	// the item change, exactly as the brief requires.
	if !amount.Overridden || amount.TotalCents != 1234 {
		t.Fatalf("Amount = %+v, want Overridden=true TotalCents=1234 (override untouched)", amount)
	}

	active, err := repo.ListActive(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	codes := map[string]bool{}
	for _, it := range active {
		codes[it.Code] = true
	}
	if codes[CodeHACCP] {
		t.Fatal("haccp still active after removal")
	}
	if !codes[CodeReservation] {
		t.Fatal("reservation not active after addition")
	}
	if !codes[CodeEssentiel] {
		t.Fatal("essentiel plan item unexpectedly gone")
	}
}

// TestApplyItemChanges_RejectsNonBillableAdd_Postgres.
func TestApplyItemChanges_RejectsNonBillableAdd_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-apply-unpriceable"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	svc := newTestService(db)
	if _, err := svc.ApplyItemChanges(ctx, merchantID, []string{CodeSMS}, nil); !errors.Is(err, models.ErrSubscriptionItemPriceUnavailable) {
		t.Fatalf("ApplyItemChanges(add=sms) = %v, want ErrSubscriptionItemPriceUnavailable", err)
	}
	active, err := NewRepository(db).ListActive(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("ListActive = %+v, want none — the rejected add must not have been written", active)
	}
}

// TestServiceAddItem_RejectsNonBillableCode_Postgres — PRÉALABLE P3's other
// half: Service.AddItem (the guarded entry point real callers use, unlike
// Repository.AddItem) refuses kiosk/sms directly too.
func TestServiceAddItem_RejectsNonBillableCode_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-svc-additem-guard"
	seedSubscription(t, db, merchantID, "monthly", nil)
	cleanupItems(t, db, merchantID)

	svc := newTestService(db)
	for _, code := range []string{CodeKiosk, CodeSMS} {
		if _, err := svc.AddItem(ctx, merchantID, code, KindModule, 1, 9999); !errors.Is(err, models.ErrSubscriptionItemPriceUnavailable) {
			t.Fatalf("Service.AddItem(%s) = %v, want ErrSubscriptionItemPriceUnavailable", code, err)
		}
	}
	active, err := NewRepository(db).ListActive(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("ListActive = %+v, want none written", active)
	}
}
