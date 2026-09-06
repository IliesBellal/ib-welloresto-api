//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"
)

// TestGetPaymentsByMerchant_Postgres is PROMPT 24 Phase 2's coverage for
// Règlements' new group_by=merchant — mirrors
// TestGetRevenueByMerchant_Postgres/TestGetOrdersByMerchant_Postgres exactly:
// plain COUNT/SUM, so by_merchant rows must sum EXACTLY to the ungrouped
// total, no apportionment involved.
func TestGetPaymentsByMerchant_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	when := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	orderA1 := seedOrder(t, ctx, db, merchantA, 9301, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, when)
	orderA2 := seedOrder(t, ctx, db, merchantA, 9302, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 2000, when)
	orderB1 := seedOrder(t, ctx, db, merchantB, 9303, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500, when)
	seedPayment(t, ctx, db, merchantA, orderA1, "CB", 1000, true)
	seedPayment(t, ctx, db, merchantA, orderA2, "ES", 2000, true)
	seedPayment(t, ctx, db, merchantB, orderB1, "CB", 500, true)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	start, end := when.AddDate(0, 0, -1), when.AddDate(0, 0, 1)
	byMerchant, err := repo.GetPaymentsByMerchant(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetPaymentsByMerchant: %v", err)
	}
	got := map[string]PaymentsMerchantTotal{}
	for _, row := range byMerchant {
		got[row.MerchantID] = row
	}
	if got[merchantA].TotalAmountCents != 3000 || got[merchantA].PaymentCount != 2 {
		t.Fatalf("merchant A: expected 3000 cents / 2 payments, got %+v", got[merchantA])
	}
	if got[merchantB].TotalAmountCents != 500 || got[merchantB].PaymentCount != 1 {
		t.Fatalf("merchant B: expected 500 cents / 1 payment, got %+v", got[merchantB])
	}

	totals, err := repo.GetPaymentsTotals(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetPaymentsTotals: %v", err)
	}
	var sumAmount, sumCount int64
	for _, row := range byMerchant {
		sumAmount += row.TotalAmountCents
		sumCount += row.PaymentCount
	}
	if sumAmount != totals.TotalAmountCents || sumCount != totals.PaymentCount {
		t.Fatalf("by_merchant does not reconcile to the ungrouped total: sum=%d/%d total=%d/%d", sumAmount, sumCount, totals.TotalAmountCents, totals.PaymentCount)
	}
}

// TestGetPayments_GroupByMerchant_EndToEnd exercises the whole service path
// with a real multi-establishment accessible scope — same shape as
// TestGetOrders_GroupByMerchant_EndToEnd.
func TestGetPayments_GroupByMerchant_EndToEnd(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)
	svc := NewService(repo, nil)

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	const userID = "itest-groupby-payments-user"
	roleA := seedScopeRole(t, ctx, db, merchantA, "itest-gb-pay-role-a", []permission.Key{permission.POSAnalytics})
	roleB := seedScopeRole(t, ctx, db, merchantB, "itest-gb-pay-role-b", []permission.Key{permission.POSAnalytics})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantA, token: "itest-gb-pay-tok-a", roleID: &roleA, enabled: true, loginEnabled: true})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantB, token: "itest-gb-pay-tok-b", roleID: &roleB, enabled: true, loginEnabled: true})

	when := time.Now().UTC().AddDate(0, 0, -1)
	orderA := seedOrder(t, ctx, db, merchantA, 9401, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1200, when)
	orderB := seedOrder(t, ctx, db, merchantB, 9402, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 800, when)
	seedPayment(t, ctx, db, merchantA, orderA, "CB", 1200, true)
	seedPayment(t, ctx, db, merchantB, orderB, "CB", 800, true)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	user := &auth.UserLoginRow{UserID: userID, MerchantID: merchantA}
	ctx = middleware.WithUser(ctx, user)

	dateFrom := when.AddDate(0, 0, -1).Format("2006-01-02")
	dateTo := when.AddDate(0, 0, 1).Format("2006-01-02")

	resp, err := svc.GetPayments(ctx, PaymentsRequest{
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		MerchantIDs: []string{merchantA, merchantB},
		GroupBy:     GroupByMerchant,
	})
	if err != nil {
		t.Fatalf("GetPayments: %v", err)
	}
	if resp.Scope.GroupBy != GroupByMerchant {
		t.Fatalf("expected echoed group_by=%q, got %q", GroupByMerchant, resp.Scope.GroupBy)
	}
	if len(resp.ByMerchant) != 2 {
		t.Fatalf("expected 2 by_merchant rows, got %d: %+v", len(resp.ByMerchant), resp.ByMerchant)
	}

	_, err = svc.GetPayments(ctx, PaymentsRequest{
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		MerchantIDs: []string{merchantA, merchantB},
		GroupBy:     "not-a-real-value",
	})
	if err != ErrInvalidRequest {
		t.Fatalf("expected ErrInvalidRequest for an unrecognized group_by, got %v", err)
	}
}

// TestGetVAT_GroupByMerchant_PartsSumToOwnTotal_Postgres is PROMPT 24 Phase
// 2's central TVA requirement: "l'apportionnement par plus fort reste doit
// s'appliquer par établissement en mode comparé — sinon les parts de chaque
// établissement ne sommeront pas à son propre total". Two establishments,
// each with its own HT total, are apportioned independently. If a
// regression apportioned each establishment's by_rate/by_channel shares
// against the COMBINED scope's total instead of its own (apportionCents
// forces its output to sum to whatever total it's given — see
// apportion.go), this test fails immediately: merchant A's single-rate share
// would be inflated to sum to the combined total, not to merchant A's own
// TotalHTCents.
func TestGetVAT_GroupByMerchant_PartsSumToOwnTotal_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	var tvaID20, tvaID10 int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest GB TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories 20: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('1', 'ITest GB TVA 10', 'itest', 10) RETURNING tva_id`).Scan(&tvaID10); err != nil {
		t.Fatalf("seed tva_categories 10: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id IN ($1, $2)`, tvaID20, tvaID10)
	}()

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	productA, err := seedProduct(t, ctx, db, merchantA, "ITest GB Product A", tvaID20)
	if err != nil {
		t.Fatalf("seed product A: %v", err)
	}
	productB1, err := seedProduct(t, ctx, db, merchantB, "ITest GB Product B1", tvaID20)
	if err != nil {
		t.Fatalf("seed product B1: %v", err)
	}
	productB2, err := seedProduct(t, ctx, db, merchantB, "ITest GB Product B2", tvaID10)
	if err != nil {
		t.Fatalf("seed product B2: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id IN ($1, $2, $3)`, productA, productB1, productB2)
	}()

	when := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	// Merchant A: single order, single rate (20%) — TTC 1200 -> HT 1000
	// exact. Its own total is 1000, nowhere near the combined scope's total.
	orderA := seedOrder(t, ctx, db, merchantA, 9501, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1200, when)
	seedOrderItem(t, ctx, db, orderA, productA, merchantA, 1, 1200)

	// Merchant B: two orders, two different rates — 20% (TTC 800 -> HT
	// 666.66) and 10% (TTC 2000 -> HT 1818.18), forcing an actual
	// largest-remainder split within merchant B alone.
	orderB1 := seedOrder(t, ctx, db, merchantB, 9502, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 800, when)
	seedOrderItem(t, ctx, db, orderB1, productB1, merchantB, 1, 800)
	orderB2 := seedOrder(t, ctx, db, merchantB, 9503, "WELLO_RESTO", "ACCEPTED", "DONE", "DELIVERY", 2000, when)
	seedOrderItem(t, ctx, db, orderB2, productB2, merchantB, 1, 2000)

	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	start, end := when.AddDate(0, 0, -1), when.AddDate(0, 0, 1)

	const userID = "itest-groupby-vat-user"
	roleA := seedScopeRole(t, ctx, db, merchantA, "itest-gb-vat-role-a", []permission.Key{permission.POSAnalytics})
	roleB := seedScopeRole(t, ctx, db, merchantB, "itest-gb-vat-role-b", []permission.Key{permission.POSAnalytics})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantA, token: "itest-gb-vat-tok-a", roleID: &roleA, enabled: true, loginEnabled: true})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantB, token: "itest-gb-vat-tok-b", roleID: &roleB, enabled: true, loginEnabled: true})

	svc := NewService(repo, nil)
	svcCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: merchantA})

	resp, err := svc.GetVAT(svcCtx, VATRequest{
		DateFrom:    start.Format("2006-01-02"),
		DateTo:      end.Format("2006-01-02"),
		MerchantIDs: []string{merchantA, merchantB},
		GroupBy:     GroupByMerchant,
	})
	if err != nil {
		t.Fatalf("GetVAT: %v", err)
	}
	if len(resp.ByMerchant) != 2 {
		t.Fatalf("expected 2 by_merchant rows, got %d: %+v", len(resp.ByMerchant), resp.ByMerchant)
	}

	byMerchant := map[string]VATMerchantTotal{}
	for _, row := range resp.ByMerchant {
		byMerchant[row.MerchantID] = row
	}

	// The central assertion: EACH establishment's own by_rate/by_channel
	// parts sum to ITS OWN TotalHTCents/TotalVATCents — not the combined
	// scope's total, and not each other's.
	for _, merchantID := range []string{merchantA, merchantB} {
		mt := byMerchant[merchantID]
		var rateHTSum, rateVATSum int64
		for _, r := range mt.ByRate {
			rateHTSum += r.BaseHTCents
			rateVATSum += r.VATCents
		}
		if rateHTSum != mt.TotalHTCents {
			t.Fatalf("merchant %s: by_rate HT parts sum to %d, want exactly its own total %d", merchantID, rateHTSum, mt.TotalHTCents)
		}
		if rateVATSum != mt.TotalVATCents {
			t.Fatalf("merchant %s: by_rate VAT parts sum to %d, want exactly its own total %d", merchantID, rateVATSum, mt.TotalVATCents)
		}

		var channelHTSum, channelVATSum int64
		for _, c := range mt.ByChannel {
			channelHTSum += c.BaseHTCents
			channelVATSum += c.VATCents
		}
		if channelHTSum != mt.TotalHTCents {
			t.Fatalf("merchant %s: by_channel HT parts sum to %d, want exactly its own total %d", merchantID, channelHTSum, mt.TotalHTCents)
		}
		if channelVATSum != mt.TotalVATCents {
			t.Fatalf("merchant %s: by_channel VAT parts sum to %d, want exactly its own total %d", merchantID, channelVATSum, mt.TotalVATCents)
		}
	}

	if byMerchant[merchantA].TotalHTCents != 1000 {
		t.Fatalf("merchant A: expected TotalHTCents=1000 (exact), got %d", byMerchant[merchantA].TotalHTCents)
	}
	if got := byMerchant[merchantB].TotalHTCents; got == byMerchant[merchantA].TotalHTCents {
		t.Fatalf("merchant B's total (%d) should differ from merchant A's (%d) — otherwise a global-total bug would be indistinguishable from correct per-establishment apportionment", got, byMerchant[merchantA].TotalHTCents)
	}

	// The combined scope's TTC reconciliation still holds exactly (plain
	// integer sum, no rounding involved) — the reconciliation check PROMPT
	// 24 Phase 2 asks for on every comparable tab.
	combinedTotals, err := repo.GetVATTotals(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetVATTotals: %v", err)
	}
	var ttcSum int64
	for _, row := range resp.ByMerchant {
		ttcSum += row.TotalTTCCents
	}
	if ttcSum != combinedTotals.TotalTTCCents {
		t.Fatalf("by_merchant TTC parts sum to %d, want exactly the combined period total %d", ttcSum, combinedTotals.TotalTTCCents)
	}
}

// TestGetVAT_InvalidGroupBy_Postgres confirms VATRequest validates group_by
// the same way Revenue/Orders/Payments do.
func TestGetVAT_InvalidGroupBy_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)
	svc := NewService(repo, nil)

	m := seedScopeMerchants(t, ctx, db, 1)
	defer m.cleanup()
	merchantA := m.id(0)

	const userID = "itest-vat-invalid-groupby-user"
	role := seedScopeRole(t, ctx, db, merchantA, "itest-vat-invalid-role", []permission.Key{permission.POSAnalytics})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantA, token: "itest-vat-invalid-tok", roleID: &role, enabled: true, loginEnabled: true})

	svcCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: merchantA})
	now := time.Now().UTC()
	_, err := svc.GetVAT(svcCtx, VATRequest{
		DateFrom:    now.AddDate(0, 0, -1).Format("2006-01-02"),
		DateTo:      now.Format("2006-01-02"),
		MerchantIDs: []string{merchantA},
		GroupBy:     "not-a-real-value",
	})
	if err != ErrInvalidRequest {
		t.Fatalf("expected ErrInvalidRequest for an unrecognized group_by, got %v", err)
	}
}

// TestGetCancellationsByMerchant_Postgres is PROMPT 24 Phase 2's coverage for
// Annulations' new group_by=merchant on the aggregate endpoint only (the
// nominative by-staff ranking stays merged, per the brief's explicit
// decision — untouched by this lot). Every field here is a direct COUNT/SUM,
// so — like Revenue/Orders/Payments — rows must sum exactly to the ungrouped
// total.
func TestGetCancellationsByMerchant_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	when := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	// Merchant A: one order created, one cancelled (STAFF).
	seedCancelledOrder(t, ctx, db, merchantA, 9601, "IN", 1000, "STAFF", when)
	// Merchant B: two orders created, one cancelled (PLATFORM), one normal
	// (must count toward orders-created but not cancellations).
	seedCancelledOrder(t, ctx, db, merchantB, 9602, "IN", 2000, "PLATFORM", when)
	seedOrder(t, ctx, db, merchantB, 9603, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500, when)

	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	start, end := when.AddDate(0, 0, -1), when.AddDate(0, 0, 1)

	ordersCreatedByMerchant, err := repo.GetOrdersCreatedCountByMerchant(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCountByMerchant: %v", err)
	}
	createdByID := map[string]int64{}
	for _, row := range ordersCreatedByMerchant {
		createdByID[row.MerchantID] = row.Count
	}
	if createdByID[merchantA] != 1 {
		t.Fatalf("merchant A: expected 1 order created, got %d", createdByID[merchantA])
	}
	if createdByID[merchantB] != 2 {
		t.Fatalf("merchant B: expected 2 orders created, got %d", createdByID[merchantB])
	}

	cancellationsByMerchant, err := repo.GetCancellationsTotalsByMerchant(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetCancellationsTotalsByMerchant: %v", err)
	}
	cancelledByID := map[string]CancellationsMerchantTotals{}
	for _, row := range cancellationsByMerchant {
		cancelledByID[row.MerchantID] = row
	}
	if cancelledByID[merchantA].CancelledCount != 1 || cancelledByID[merchantA].InternalCancelledCount != 1 {
		t.Fatalf("merchant A: expected 1 cancelled/1 internal, got %+v", cancelledByID[merchantA])
	}
	if cancelledByID[merchantB].CancelledCount != 1 || cancelledByID[merchantB].PlatformCancelledCount != 1 {
		t.Fatalf("merchant B: expected 1 cancelled/1 platform, got %+v", cancelledByID[merchantB])
	}

	combinedOrdersCreated, err := repo.GetOrdersCreatedCount(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCount: %v", err)
	}
	combinedCancellations, err := repo.GetCancellationsTotals(ctx, []string{merchantA, merchantB}, start, end)
	if err != nil {
		t.Fatalf("GetCancellationsTotals: %v", err)
	}
	var sumCreated, sumCancelled int64
	for _, row := range ordersCreatedByMerchant {
		sumCreated += row.Count
	}
	for _, row := range cancellationsByMerchant {
		sumCancelled += row.CancelledCount
	}
	if sumCreated != combinedOrdersCreated {
		t.Fatalf("by_merchant orders-created parts sum to %d, want exactly the combined total %d", sumCreated, combinedOrdersCreated)
	}
	if sumCancelled != combinedCancellations.CancelledCount {
		t.Fatalf("by_merchant cancelled parts sum to %d, want exactly the combined total %d", sumCancelled, combinedCancellations.CancelledCount)
	}
}

// TestGetCancellations_GroupByMerchant_EndToEnd mirrors
// TestGetOrders_GroupByMerchant_EndToEnd/TestGetPayments_GroupByMerchant_EndToEnd
// for the Annulations aggregate endpoint.
func TestGetCancellations_GroupByMerchant_EndToEnd(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)
	svc := NewService(repo, nil)

	m := seedScopeMerchants(t, ctx, db, 2)
	defer m.cleanup()
	merchantA, merchantB := m.id(0), m.id(1)

	const userID = "itest-groupby-cancellations-user"
	roleA := seedScopeRole(t, ctx, db, merchantA, "itest-gb-cxl-role-a", []permission.Key{permission.POSAnalytics})
	roleB := seedScopeRole(t, ctx, db, merchantB, "itest-gb-cxl-role-b", []permission.Key{permission.POSAnalytics})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantA, token: "itest-gb-cxl-tok-a", roleID: &roleA, enabled: true, loginEnabled: true})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantB, token: "itest-gb-cxl-tok-b", roleID: &roleB, enabled: true, loginEnabled: true})

	when := time.Now().UTC().AddDate(0, 0, -1)
	seedCancelledOrder(t, ctx, db, merchantA, 9701, "IN", 1200, "STAFF", when)
	seedCancelledOrder(t, ctx, db, merchantB, 9702, "IN", 800, "CUSTOMER", when)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}()

	user := &auth.UserLoginRow{UserID: userID, MerchantID: merchantA}
	svcCtx := middleware.WithUser(ctx, user)

	dateFrom := when.AddDate(0, 0, -1).Format("2006-01-02")
	dateTo := when.AddDate(0, 0, 1).Format("2006-01-02")

	resp, err := svc.GetCancellations(svcCtx, CancellationsRequest{
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		MerchantIDs: []string{merchantA, merchantB},
		GroupBy:     GroupByMerchant,
	})
	if err != nil {
		t.Fatalf("GetCancellations: %v", err)
	}
	if resp.Scope.GroupBy != GroupByMerchant {
		t.Fatalf("expected echoed group_by=%q, got %q", GroupByMerchant, resp.Scope.GroupBy)
	}
	if len(resp.ByMerchant) != 2 {
		t.Fatalf("expected 2 by_merchant rows, got %d: %+v", len(resp.ByMerchant), resp.ByMerchant)
	}

	_, err = svc.GetCancellations(svcCtx, CancellationsRequest{
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		MerchantIDs: []string{merchantA, merchantB},
		GroupBy:     "not-a-real-value",
	})
	if err != ErrInvalidRequest {
		t.Fatalf("expected ErrInvalidRequest for an unrecognized group_by, got %v", err)
	}
}

// seedProduct creates one product on merchantID taxed at tvaID for every
// service type (in/take-away/delivery) — enough for this file's single-line
// VAT scenarios, which never mix service types on the same product.
func seedProduct(t *testing.T, ctx context.Context, db *sql.DB, merchantID, name string, tvaID int64) (int64, error) {
	t.Helper()
	var productID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, $2, 'itest-cat', 1000, $3, $3, $3) RETURNING product_id`,
		merchantID, name, tvaID).Scan(&productID)
	return productID, err
}

// seedCancelledOrder seeds one order already in the CANCELED state, matching
// AnalyticsCancellationsScope (upper(brand_status)='CANCELED') — the
// cancellation counterpart to seedOrder.
func seedCancelledOrder(t *testing.T, ctx context.Context, db *sql.DB, merchantID string, orderNum int, orderType string, priceCents int64, cancelledByType string, creationTime time.Time) int64 {
	t.Helper()
	var orderID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date, cancelled_by_type)
		VALUES ($1, $2, 'WELLO_RESTO', 'CANCELED', 'CLOSED', $3, $4, 0, 0, 'itest-analytics', $5, $6)
		RETURNING order_id`,
		merchantID, orderNum, orderType, priceCents, creationTime.UTC(), cancelledByType,
	).Scan(&orderID)
	if err != nil {
		t.Fatalf("seed cancelled order: %v", err)
	}
	return orderID
}
