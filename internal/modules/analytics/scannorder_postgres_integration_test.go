//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/timeutil"
)

// TestScanNOrderStats_Postgres : onglet ScanNOrder (GetScanNOrderStats) —
// totaux, entonnoir et visites (sno_events), sources avec le hub comme une
// source parmi d'autres (sno_order_attributions), franchises du périmètre.
func TestScanNOrderStats_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const tzName = "Europe/Paris"
	tz, _ := time.LoadLocation(tzName)
	local := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, tz) }
	const franchiseID = "fra-itest-sno"

	var merchantIntID int64
	if _, err := db.ExecContext(ctx, `INSERT INTO franchises (franchise_id, name, slug) VALUES ($1, 'ITest SNO', 'itest-sno')`, franchiseID); err != nil {
		t.Fatalf("seed franchise: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, franchise_id)
		VALUES ('ITest SNO Merchant', 'addr', '1', 'street', '57000', 'Metz', 'siret-sno', 'https://example.com', '0600000000', 'tok-sno', $1, $2)
		RETURNING id`, tzName, franchiseID).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM sno_events WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM sno_order_attributions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		_, _ = db.ExecContext(ctx, `DELETE FROM franchises WHERE franchise_id = $1`, franchiseID)
	})

	const v1, v2 = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	for _, e := range []struct {
		typ, visit string
		src        interface{}
		at         time.Time
	}{
		{"menu_view", v1, "hub", local(1, 12)},
		{"product_view", v1, nil, local(1, 12)},
		{"add_to_cart", v1, nil, local(1, 12)},
		{"checkout_start", v1, nil, local(1, 12)},
		{"payment_redirect", v1, nil, local(1, 12)},
		{"order_created", v1, nil, local(1, 12)},
		{"menu_view", v2, "qr_table", local(2, 19)},
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO sno_events (event_type, merchant_id, visit_id, source, created_at) VALUES ($1, $2, CAST($3 AS uuid), $4, $5)`,
			e.typ, merchantID, e.visit, e.src, e.at.UTC()); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}
	orderNum := 0
	seedOrder := func(source string, price int64, at time.Time, attrSource string) {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date, order_source)
			VALUES ($1, $2, 'WELLO_RESTO', 'DONE', 'CLOSED', 'TAKE_AWAY', $3, 0, 0, 'itest-sno', $4, $5)
			RETURNING order_id`, merchantID, orderNum, price, at.UTC(), source).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		if attrSource != "" {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO sno_order_attributions (order_id, merchant_id, source, franchise_id) VALUES ($1, $2, $3, $4)`,
				id, merchantID, attrSource, franchiseID); err != nil {
				t.Fatalf("seed attribution: %v", err)
			}
		}
	}
	seedOrder("SCANNORDER", 2400, local(1, 12), "hub")
	seedOrder("SCANNORDER", 1600, local(2, 19), "")
	seedOrder("WELLO_RESTO_POS", 5000, local(2, 12), "")

	startUTC, endUTC := timeutil.LocalDayRangeBounds(local(1, 0), local(2, 0), tz)
	got, err := NewRepository(db).GetScanNOrderStats(ctx, []string{merchantID}, startUTC, endUTC, tz)
	if err != nil {
		t.Fatalf("GetScanNOrderStats: %v", err)
	}
	if got.Totals != (ScanNOrderTotals{SNOOrders: 2, SNORevenueTTCCents: 4000, AllRevenueTTCCents: 9000}) {
		t.Errorf("totals = %+v", got.Totals)
	}
	if got.Funnel != (ScanNOrderFunnel{Visits: 2, ProductViews: 1, AddToCart: 1, CheckoutStart: 1, PaymentRedirect: 1, Orders: 1}) {
		t.Errorf("funnel = %+v", got.Funnel)
	}
	sources := map[string]ScanNOrderSourceRow{}
	for _, s := range got.Sources {
		sources[s.Source] = s
	}
	if sources["hub"] != (ScanNOrderSourceRow{Source: "hub", Visits: 1, Orders: 1, RevenueTTCCents: 2400}) ||
		sources["qr_table"] != (ScanNOrderSourceRow{Source: "qr_table", Visits: 1}) ||
		sources["unknown"] != (ScanNOrderSourceRow{Source: "unknown", Orders: 1, RevenueTTCCents: 1600}) {
		t.Errorf("sources = %+v", got.Sources)
	}
	if !got.Hub.InFranchise || len(got.Hub.Franchises) != 1 || got.Hub.Franchises[0] != "ITest SNO" {
		t.Errorf("hub = %+v", got.Hub)
	}
	if len(got.ByMerchant) != 1 || got.ByMerchant[0] != (ScanNOrderMerchantRow{MerchantID: merchantID, Visits: 2, SNOOrders: 2, SNORevenueTTCCents: 4000}) {
		t.Errorf("by merchant = %+v", got.ByMerchant)
	}
	days := map[string]ScanNOrderDay{}
	for _, d := range got.Daily {
		days[d.Date] = d
	}
	if days["2026-10-01"] != (ScanNOrderDay{Date: "2026-10-01", Visits: 1, SNOOrders: 1}) ||
		days["2026-10-02"] != (ScanNOrderDay{Date: "2026-10-02", Visits: 1, SNOOrders: 1}) {
		t.Errorf("daily = %+v", got.Daily)
	}
}
