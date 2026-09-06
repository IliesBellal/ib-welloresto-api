//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"
)

// TestGetRevenueByMerchant_Postgres and TestGetOrdersByMerchant_Postgres are
// PROMPT 23 Phase 3's missing coverage: group_by=merchant has been part of
// the CA/Commandes contract since PROMPT 03, but neither
// Repository.GetRevenueByMerchant nor the request's validation had ANY test
// before this lot — an omission this brief exists specifically to close (it
// is also how OrdersResponse's missing ByMerchant field, fixed in this same
// lot, went unnoticed for this long). Both tests seed 2 establishments with
// known, different order volumes and assert:
//   - one row per establishment, with the right numbers
//   - the rows sum EXACTLY to the ungrouped period total (both are plain
//     COUNT/SUM on integer columns — no apportionment is needed, unlike a
//     derived HT figure would)
func TestGetRevenueByMerchant_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	when := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	seedOrder(t, ctx, db, merchantA, 9001, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, when)
	seedOrder(t, ctx, db, merchantA, 9002, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 2000, when)
	seedOrder(t, ctx, db, merchantB, 9003, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500, when)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	start, end := when.AddDate(0, 0, -1), when.AddDate(0, 0, 1)
	byMerchant, err := repo.GetRevenueByMerchant(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetRevenueByMerchant: %v", err)
	}
	got := map[string]RevenueMerchantTotal{}
	for _, row := range byMerchant {
		got[row.MerchantID] = row
	}
	if got[merchantA].TotalTTCCents != 3000 || got[merchantA].OrderCount != 2 {
		t.Fatalf("merchant A: expected 3000 cents / 2 orders, got %+v", got[merchantA])
	}
	if got[merchantB].TotalTTCCents != 500 || got[merchantB].OrderCount != 1 {
		t.Fatalf("merchant B: expected 500 cents / 1 order, got %+v", got[merchantB])
	}

	totals, err := repo.GetRevenueTotalsTTC(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetRevenueTotalsTTC: %v", err)
	}
	var sumTTC, sumCount int64
	for _, row := range byMerchant {
		sumTTC += row.TotalTTCCents
		sumCount += row.OrderCount
	}
	if sumTTC != totals.TotalTTCCents || sumCount != totals.OrderCount {
		t.Fatalf("by_merchant does not reconcile to the ungrouped total: sum=%d/%d total=%d/%d", sumTTC, sumCount, totals.TotalTTCCents, totals.OrderCount)
	}
}

func TestGetOrdersByMerchant_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	when := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	seedOrder(t, ctx, db, merchantA, 9101, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, when)
	seedOrder(t, ctx, db, merchantB, 9102, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 400, when)
	seedOrder(t, ctx, db, merchantB, 9103, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 600, when)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	start, end := when.AddDate(0, 0, -1), when.AddDate(0, 0, 1)
	byMerchant, err := repo.GetOrdersByMerchant(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetOrdersByMerchant: %v", err)
	}
	got := map[string]OrdersMerchantTotal{}
	for _, row := range byMerchant {
		got[row.MerchantID] = row
	}
	if got[merchantA].OrderCount != 1 || got[merchantA].TotalTTCCents != 1000 {
		t.Fatalf("merchant A: expected 1 order / 1000 cents, got %+v", got[merchantA])
	}
	if got[merchantB].OrderCount != 2 || got[merchantB].TotalTTCCents != 1000 {
		t.Fatalf("merchant B: expected 2 orders / 1000 cents, got %+v", got[merchantB])
	}

	totals, err := repo.GetOrdersTotals(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetOrdersTotals: %v", err)
	}
	var sumCount, sumTTC int64
	for _, row := range byMerchant {
		sumCount += row.OrderCount
		sumTTC += row.TotalTTCCents
	}
	if sumCount != totals.OrderCount || sumTTC != totals.TotalTTCCents {
		t.Fatalf("by_merchant does not reconcile to the ungrouped total: sum=%d/%d total=%d/%d", sumCount, sumTTC, totals.OrderCount, totals.TotalTTCCents)
	}
}

// TestGetOrders_GroupByMerchant_EndToEnd exercises the whole service path
// (not just the repository query): a real multi-establishment accessible
// scope, group_by=merchant, and the resulting response actually carrying a
// ByMerchant breakdown — the exact gap PROMPT 23 Phase 3 closes (before this
// lot, GetOrders accepted and echoed group_by=merchant but silently
// computed nothing).
func TestGetOrders_GroupByMerchant_EndToEnd(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)
	svc := NewService(repo, nil)

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	const userID = "itest-groupby-orders-user"
	role := seedScopeRole(t, ctx, db, merchantA, "itest-groupby-orders-role", []permission.Key{permission.POSAnalytics})
	roleB := seedScopeRole(t, ctx, db, merchantB, "itest-groupby-orders-role-b", []permission.Key{permission.POSAnalytics})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantA, token: "itest-groupby-orders-tok-a", roleID: &role, enabled: true, loginEnabled: true})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantB, token: "itest-groupby-orders-tok-b", roleID: &roleB, enabled: true, loginEnabled: true})

	when := time.Now().UTC().AddDate(0, 0, -1)
	seedOrder(t, ctx, db, merchantA, 9201, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1200, when)
	seedOrder(t, ctx, db, merchantB, 9202, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 800, when)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	user := &auth.UserLoginRow{UserID: userID, MerchantID: merchantA}
	ctx = middleware.WithUser(ctx, user)

	dateFrom := when.AddDate(0, 0, -1).Format("2006-01-02")
	dateTo := when.AddDate(0, 0, 1).Format("2006-01-02")

	resp, err := svc.GetOrders(ctx, OrdersRequest{
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		MerchantIDs: []string{merchantA, merchantB},
		GroupBy:     GroupByMerchant,
	})
	if err != nil {
		t.Fatalf("GetOrders: %v", err)
	}
	if resp.Scope.GroupBy != GroupByMerchant {
		t.Fatalf("expected echoed group_by=%q, got %q", GroupByMerchant, resp.Scope.GroupBy)
	}
	if len(resp.ByMerchant) != 2 {
		t.Fatalf("expected 2 by_merchant rows, got %d: %+v", len(resp.ByMerchant), resp.ByMerchant)
	}

	_, err = svc.GetOrders(ctx, OrdersRequest{
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		MerchantIDs: []string{merchantA, merchantB},
		GroupBy:     "not-a-real-value",
	})
	if err != ErrInvalidRequest {
		t.Fatalf("expected ErrInvalidRequest for an unrecognized group_by, got %v", err)
	}
}
