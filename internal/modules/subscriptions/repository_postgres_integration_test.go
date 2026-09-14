//go:build postgres_integration

package subscriptions

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// TestRepository_Postgres — LOT B B1b. subscription_items has no foreign key
// to merchant (same posture as subscriptions.package_id, see B1a), so this
// runs against a synthetic merchant_id rather than a real merchant row.
func TestRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := "itest-subitems-merchant"

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM subscription_items WHERE merchant_id = $1`, merchantID)
	})
	// Clean slate in case a previous failed run left rows behind.
	if _, err := db.ExecContext(ctx, `DELETE FROM subscription_items WHERE merchant_id = $1`, merchantID); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}

	repo := NewRepository(db)

	if _, err := repo.AddItem(ctx, merchantID, CodeHACCP, "bogus_kind", 1, 3500); !errors.Is(err, models.ErrInvalidSubscriptionItemKind) {
		t.Fatalf("AddItem(bogus kind) = %v, want ErrInvalidSubscriptionItemKind", err)
	}
	if _, err := repo.AddItem(ctx, merchantID, "bogus_code", KindModule, 1, 3500); !errors.Is(err, models.ErrInvalidSubscriptionItemCode) {
		t.Fatalf("AddItem(bogus code) = %v, want ErrInvalidSubscriptionItemCode", err)
	}

	item, err := repo.AddItem(ctx, merchantID, CodeHACCP, KindModule, 1, 3500)
	if err != nil {
		t.Fatalf("AddItem(haccp): %v", err)
	}
	if item.ID == "" || item.Code != CodeHACCP || item.Kind != KindModule || item.Quantity != 1 || item.UnitPriceCents != 3500 || item.RemovedAt != nil {
		t.Fatalf("AddItem(haccp) = %+v, unexpected shape", item)
	}

	active, err := repo.ListActive(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != item.ID {
		t.Fatalf("ListActive = %+v, want 1 item %q", active, item.ID)
	}

	// A second active item for the same (merchant_id, code) is a double
	// billing risk — the partial unique index must reject it.
	if _, err := repo.AddItem(ctx, merchantID, CodeHACCP, KindModule, 1, 3500); err == nil {
		t.Fatal("AddItem(duplicate active haccp) = nil error, want a unique violation")
	}

	if err := repo.RemoveItem(ctx, item.ID); err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	active, err = repo.ListActive(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListActive after remove: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("ListActive after remove = %+v, want empty", active)
	}

	// Now that the previous line is removed, the same code can be re-added
	// (a plan change, not a duplicate) — the partial index only guards
	// concurrently-active rows.
	if _, err := repo.AddItem(ctx, merchantID, CodeHACCP, KindModule, 1, 3900); err != nil {
		t.Fatalf("AddItem(haccp again after removal): %v", err)
	}
}
