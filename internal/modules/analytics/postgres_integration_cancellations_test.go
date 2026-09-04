//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestCancellations_Postgres seeds a known dataset and checks every
// GetCancellations* query against an independent, hand-computed
// expectation — PROMPT 10's mandatory accuracy test. Covers:
//   - the cancellation scope (brand_status='CANCELED' only; a DENIED order
//     in the same window must be excluded — PROMPT 10 §1)
//   - the rate's denominator (every order created in the period, cancelled
//     or not — PROMPT 10 §3)
//   - the STAFF/CUSTOMER/SYSTEM/PLATFORM/UNKNOWN author-type partition and
//     its internal/platform/unknown subtotals (§3)
//   - deletion_reason_id's two data-quality bugs found while building this
//     tab: a stray-quoted value ('X') that must still join the catalog, and
//     an uncatalogued/truncated value that must not vanish (§3/§5)
//   - every breakdown (reason/author_type/channel) summing exactly to the
//     period total (§3)
//   - the by-staff ranking: only STAFF-type, only real users, an
//     unattributable STAFF cancellation surfacing as its own row, and the
//     cross-endpoint coherence check (§4/§6)
//   - an establishment with zero cancellations in the period returning
//     zeros, not an error (§6)
func TestCancellations_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID, emptyMerchantIntID int64
	var reasonID int64

	cleanup := func() {
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if emptyMerchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(emptyMerchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, emptyMerchantIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id LIKE 'itest-cancel-%'`)
		if reasonID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM labels WHERE label_value = $1 AND label_type = 'deletion_reason'`, itoa(reasonID))
			_, _ = db.ExecContext(ctx, `DELETE FROM deletion_reasons WHERE deletion_reason_id = $1`, reasonID)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Cancellations Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-cancel', 'https://example.com', '0600000000', 'tok-cancel', 'Europe/Paris')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Empty Cancellations Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-cancel-empty', 'https://example.com', '0600000000', 'tok-cancel-empty', 'Europe/Paris')
		RETURNING id`).Scan(&emptyMerchantIntID); err != nil {
		t.Fatalf("seed empty merchant: %v", err)
	}
	emptyMerchantID := itoa(emptyMerchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO deletion_reasons (deletion_reason_type, deletion_reason_object, deletion_reason_desc, requires_comment)
		VALUES ('itest', 'order', 'ITest cancel reason', false) RETURNING deletion_reason_id`).Scan(&reasonID); err != nil {
		t.Fatalf("seed deletion_reasons: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO labels (label_value, label_type, lang, label)
		VALUES ($1, 'deletion_reason', 'FR', 'Raison de test ITest')`, itoa(reasonID)); err != nil {
		t.Fatalf("seed labels: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
		VALUES ('itest-cancel-user1', 'Jean Test', 'Jean', 'Test', 'itest-pw', 'itest-cancel-user1@example.com', 'itest-cancel-user1-token')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	loc, _ := time.LoadLocation("Europe/Paris")
	base := time.Date(2026, 2, 1, 10, 0, 0, 0, loc)
	startUTC, endUTC := time.Date(2026, 2, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 2, 2, 0, 0, 0, 0, loc).UTC()

	reasonIDStr := itoa(reasonID)
	quotedReasonID := "'" + reasonIDStr + "'" // stray-quote data-quality bug, see cancellations.go

	// A: STAFF cancel, attributable to user1, catalogued reason, dine_in.
	seedCancelOrder(t, ctx, db, merchantID, 201, "WELLO_RESTO", "CANCELED", "IN", 1000, base, "itest-cancel-user1", strPtr("STAFF"), strPtr(reasonIDStr))
	// B: CUSTOMER self-service cancel, no reason recorded, delivery.
	seedCancelOrder(t, ctx, db, merchantID, 202, "WELLO_RESTO", "CANCELED", "DELIVERY", 500, base, "-1", strPtr("CUSTOMER"), nil)
	// C: SYSTEM cancel (e.g. ScanNOrder expiry), same catalogued reason, takeaway.
	seedCancelOrder(t, ctx, db, merchantID, 203, "WELLO_RESTO", "CANCELED", "TAKE_AWAY", 800, base, "SCANNORDER", strPtr("SYSTEM"), strPtr(reasonIDStr))
	// D: PLATFORM cancel (Uber Eats), reason stored with stray quotes — must
	// still join the same catalogued reason after trimming.
	seedCancelOrder(t, ctx, db, merchantID, 204, "UBER_EATS", "CANCELED", "DELIVERY", 1500, base, "ubereats-sync", strPtr("PLATFORM"), strPtr(quotedReasonID))
	// E: cancelled_by_type NULL (never determined), uncatalogued/truncated
	// reason code — must surface as its own explicit buckets, not vanish.
	seedCancelOrder(t, ctx, db, merchantID, 205, "WELLO_RESTO", "CANCELED", "IN", 300, base, "itest-other", nil, strPtr("TRUNCATED12"))
	// F: DENIED — refused at intake, must be EXCLUDED from the cancellation
	// scope entirely (PROMPT 10 §1).
	seedCancelOrder(t, ctx, db, merchantID, 206, "WELLO_RESTO", "DENIED", "IN", 999, base, "itest-other", strPtr("SYSTEM"), nil)
	// G: a normal, non-cancelled order — counts toward TotalOrdersCreated
	// (the rate's denominator) but not toward CancelledCount.
	seedCancelOrder(t, ctx, db, merchantID, 207, "WELLO_RESTO", "ACCEPTED", "IN", 2000, base, "itest-cancel-user1", nil, nil)
	// H: STAFF cancel by a created_by that matches no real users.user_id —
	// must surface in the by-staff endpoint as the unattributed row, not
	// silently disappear (PROMPT 10 §4/§6 coherence).
	seedCancelOrder(t, ctx, db, merchantID, 208, "WELLO_RESTO", "CANCELED", "IN", 700, base, "itest-cancel-ghost", strPtr("STAFF"), nil)

	repo := NewRepository(db)

	// --- Totals: CancelledCount excludes F (DENIED); TotalOrdersCreated
	// counts every order including F and G. ---
	totals, err := repo.GetCancellationsTotals(ctx, []string{merchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetCancellationsTotals: %v", err)
	}
	if totals.CancelledCount != 6 {
		t.Fatalf("expected CancelledCount=6 (A,B,C,D,E,H; F is DENIED), got %d", totals.CancelledCount)
	}
	if totals.CancelledAmountCents != 1000+500+800+1500+300+700 {
		t.Fatalf("expected CancelledAmountCents=4800, got %d", totals.CancelledAmountCents)
	}
	if totals.InternalCancelledCount != 4 { // A(STAFF)+B(CUSTOMER)+C(SYSTEM)+H(STAFF)
		t.Fatalf("expected InternalCancelledCount=4, got %d", totals.InternalCancelledCount)
	}
	if totals.PlatformCancelledCount != 1 { // D
		t.Fatalf("expected PlatformCancelledCount=1, got %d", totals.PlatformCancelledCount)
	}
	if totals.UnknownCancelledCount != 1 { // E
		t.Fatalf("expected UnknownCancelledCount=1, got %d", totals.UnknownCancelledCount)
	}
	if totals.StaffCancelledCount != 2 { // A, H
		t.Fatalf("expected StaffCancelledCount=2, got %d", totals.StaffCancelledCount)
	}

	ordersCreated, err := repo.GetOrdersCreatedCount(ctx, []string{merchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCount: %v", err)
	}
	if ordersCreated != 8 { // A..H
		t.Fatalf("expected TotalOrdersCreated=8, got %d", ordersCreated)
	}

	// --- ByReason: sums exactly to CancelledCount; quoted value joins the
	// same catalogued bucket; uncatalogued and "no reason" both surface
	// explicitly. ---
	byReason, err := repo.GetCancellationsByReason(ctx, []string{merchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetCancellationsByReason: %v", err)
	}
	reasonCounts := map[string]int64{}
	var reasonSum int64
	for _, r := range byReason {
		reasonCounts[r.ReasonID] = r.Count
		reasonSum += r.Count
	}
	if reasonSum != totals.CancelledCount {
		t.Fatalf("by_reason sums to %d, want exactly CancelledCount %d — got %+v", reasonSum, totals.CancelledCount, byReason)
	}
	if reasonCounts[reasonIDStr] != 3 { // A, C, D (D's stray quotes trimmed)
		t.Fatalf("expected reason %q count=3 (A,C,D — D's quoted id must join here), got %+v", reasonIDStr, byReason)
	}
	if reasonCounts["none"] != 2 { // B, H
		t.Fatalf("expected reason \"none\" count=2 (B,H), got %+v", byReason)
	}
	if reasonCounts["uncatalogued:TRUNCATED12"] != 1 { // E
		t.Fatalf("expected reason \"uncatalogued:TRUNCATED12\" count=1 (E), got %+v", byReason)
	}

	// --- ByAuthorType: sums exactly to CancelledCount/CancelledAmountCents;
	// NULL surfaces as CancellationAuthorUnknown, never dropped. ---
	byAuthorType, err := repo.GetCancellationsByAuthorType(ctx, []string{merchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetCancellationsByAuthorType: %v", err)
	}
	authorCounts := map[string]int64{}
	var authorCountSum, authorAmountSum int64
	for _, a := range byAuthorType {
		authorCounts[a.AuthorType] = a.Count
		authorCountSum += a.Count
		authorAmountSum += a.AmountCents
	}
	if authorCountSum != totals.CancelledCount || authorAmountSum != totals.CancelledAmountCents {
		t.Fatalf("by_author_type sums to (%d, %d), want exactly (%d, %d) — got %+v",
			authorCountSum, authorAmountSum, totals.CancelledCount, totals.CancelledAmountCents, byAuthorType)
	}
	if authorCounts["STAFF"] != 2 || authorCounts["CUSTOMER"] != 1 || authorCounts["SYSTEM"] != 1 ||
		authorCounts["PLATFORM"] != 1 || authorCounts[CancellationAuthorUnknown] != 1 {
		t.Fatalf("unexpected by_author_type breakdown: %+v", byAuthorType)
	}

	// --- ByChannel: sums exactly to CancelledCount/CancelledAmountCents. ---
	byChannel, err := repo.GetCancellationsByChannel(ctx, []string{merchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetCancellationsByChannel: %v", err)
	}
	var channelCountSum, channelAmountSum int64
	channelCounts := map[string]int64{}
	for _, c := range byChannel {
		channelCounts[c.Channel] = c.Count
		channelCountSum += c.Count
		channelAmountSum += c.AmountCents
	}
	if channelCountSum != totals.CancelledCount || channelAmountSum != totals.CancelledAmountCents {
		t.Fatalf("by_channel sums to (%d, %d), want exactly (%d, %d) — got %+v",
			channelCountSum, channelAmountSum, totals.CancelledCount, totals.CancelledAmountCents, byChannel)
	}
	if channelCounts[ChannelDineIn] != 3 { // A, E, H
		t.Fatalf("expected dine_in=3 (A,E,H), got %+v", byChannel)
	}

	// --- By-staff: only STAFF-type, only real users named; unattributable
	// STAFF cancellation (H) surfaces as its own row; cross-endpoint
	// coherence with the aggregate's STAFF subtotal (PROMPT 10 §6). ---
	byStaff, err := repo.GetCancellationsByStaff(ctx, []string{merchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetCancellationsByStaff: %v", err)
	}
	var staffSum int64
	var user1Row, unattributedRow *StaffCancellationRow
	for i := range byStaff {
		staffSum += byStaff[i].CancelledCount
		switch byStaff[i].UserID {
		case "itest-cancel-user1":
			user1Row = &byStaff[i]
		case CancellationUnattributedUserID:
			unattributedRow = &byStaff[i]
		}
	}
	if staffSum != totals.StaffCancelledCount {
		t.Fatalf("by-staff SUM(cancelled_count)=%d, want exactly the aggregate's StaffCancelledCount=%d — got %+v",
			staffSum, totals.StaffCancelledCount, byStaff)
	}
	if user1Row == nil {
		t.Fatalf("expected a row for itest-cancel-user1, got %+v", byStaff)
	}
	if user1Row.Name != "Jean Test" {
		t.Fatalf("expected user1 name %q, got %q", "Jean Test", user1Row.Name)
	}
	if user1Row.OrdersCreated != 2 { // A + G
		t.Fatalf("expected user1 OrdersCreated=2 (A,G), got %d", user1Row.OrdersCreated)
	}
	if user1Row.CancelledCount != 1 { // A
		t.Fatalf("expected user1 CancelledCount=1 (A), got %d", user1Row.CancelledCount)
	}
	if user1Row.RateAvailable {
		t.Fatalf("expected user1 RateAvailable=false (2 orders, well below the effectif floor), got true")
	}
	if unattributedRow == nil {
		t.Fatalf("expected the unattributed row for H's unmatched creator, got %+v", byStaff)
	}
	if unattributedRow.CancelledCount != 1 || unattributedRow.OrdersCreated != 0 || unattributedRow.RateAvailable {
		t.Fatalf("unexpected unattributed row: %+v", unattributedRow)
	}

	// --- Zero-cancellation establishment: zeros, not an error. ---
	emptyTotals, err := repo.GetCancellationsTotals(ctx, []string{emptyMerchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetCancellationsTotals (empty merchant): %v", err)
	}
	if emptyTotals.CancelledCount != 0 || emptyTotals.CancelledAmountCents != 0 {
		t.Fatalf("expected zero totals for a merchant with no cancellations, got %+v", emptyTotals)
	}
	emptyByStaff, err := repo.GetCancellationsByStaff(ctx, []string{emptyMerchantID}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetCancellationsByStaff (empty merchant): %v", err)
	}
	if len(emptyByStaff) != 0 {
		t.Fatalf("expected no by-staff rows for a merchant with no cancellations, got %+v", emptyByStaff)
	}
}

func seedCancelOrder(t *testing.T, ctx context.Context, db *sql.DB, merchantID string, orderNum int, brand, brandStatus, orderType string, priceCents int64, creationTime time.Time, createdBy string, cancelledByType, deletionReasonID *string) int64 {
	t.Helper()
	var orderID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date, cancelled_by_type, deletion_reason_id)
		VALUES ($1, $2, $3, $4, 'CLOSED', $5, $6, 0, 0, $7, $8, $9, $10)
		RETURNING order_id`,
		merchantID, orderNum, brand, brandStatus, orderType, priceCents, createdBy, creationTime.UTC(), cancelledByType, deletionReasonID,
	).Scan(&orderID)
	if err != nil {
		t.Fatalf("seed cancel order (%s/%s): %v", brand, brandStatus, err)
	}
	return orderID
}

func strPtr(s string) *string { return &s }
