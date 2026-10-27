//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/timeutil"
)

// TestKioskStats_Postgres : onglet Borne (GetKioskStats) sur un jeu de données
// calculé à la main — commandes borne (AnalyticsOrdersScope, carte via
// stripe_payments capturé, borne supprimée), audience (kiosk_events :
// entonnoir, abandon, échec carte, durée médiane et 90e centile), série
// quotidienne en jour local.
func TestKioskStats_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const tzName = "Europe/Paris"
	tz, _ := time.LoadLocation(tzName)
	local := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, tz) }

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Kiosk Merchant', 'addr', '1', 'street', '57000', 'Metz', 'siret-kiosk', 'https://example.com', '0600000000', 'tok-kiosk', $1)
		RETURNING id`, tzName).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)
	const kioskA, kioskB, kioskGone = "kiosk-itest-a", "kiosk-itest-b", "kiosk-itest-gone"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosk_events WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM stripe_payments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM kiosks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	for _, k := range []struct{ id, name string }{{kioskA, "Borne entrée"}, {kioskB, "Borne terrasse"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO kiosks (id, merchant_id, name, status) VALUES ($1, $2, $3, 'active')`, k.id, merchantID, k.name); err != nil {
			t.Fatalf("seed kiosk: %v", err)
		}
	}

	orderNum := 0
	seedOrder := func(source, kioskID, brandStatus, orderType string, price int64, at time.Time) int64 {
		t.Helper()
		orderNum++
		var id int64
		var kiosk interface{}
		if kioskID != "" {
			kiosk = kioskID
		}
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date, order_source, kiosk_id)
			VALUES ($1, $2, 'WELLO_RESTO', $3, 'CLOSED', $4, $5, 0, 0, 'itest-kiosk', $6, $7, $8)
			RETURNING order_id`, merchantID, orderNum, brandStatus, orderType, price, at.UTC(), source, kiosk).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return id
	}
	cardOrder := seedOrder("KIOSK", kioskA, "DONE", "IN", 1500, local(1, 12))
	seedOrder("KIOSK", kioskA, "DONE", "TAKE_AWAY", 2500, local(2, 9))
	seedOrder("KIOSK", kioskGone, "DONE", "TAKE_AWAY", 1000, local(2, 10))
	seedOrder("WELLO_RESTO_POS", "", "DONE", "IN", 4000, local(1, 13))    // CA tous canaux seulement
	seedOrder("KIOSK", kioskA, "CANCELED", "IN", 9900, local(1, 14))      // hors périmètre (annulée)
	seedOrder("KIOSK", kioskA, "DONE", "IN", 7700, local(5, 12))          // hors période
	if _, err := db.ExecContext(ctx, `
		INSERT INTO stripe_payments (order_id, payment_intent_id, success_key, payment_intent_status)
		VALUES ($1, 'pi_itest_kiosk', '', 'CAPTURED')`, cardOrder); err != nil {
		t.Fatalf("seed stripe payment: %v", err)
	}

	const s1, s2, s3, s4 = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"
	type ev struct {
		session, typ string
		elapsed      int64
		product      interface{}
		amount       interface{}
		detail, step interface{}
		at           time.Time
	}
	for _, e := range []ev{
		{s1, "session_start", 0, nil, nil, nil, nil, local(1, 11)},
		{s1, "fulfillment_chosen", 3000, nil, nil, "IN", nil, local(1, 11)},
		{s1, "product_view", 9000, "p-itest", nil, nil, nil, local(1, 11)},
		{s1, "add_to_cart", 15000, "p-itest", nil, nil, nil, local(1, 11)},
		{s1, "summary_view", 40000, nil, nil, nil, nil, local(1, 11)},
		{s1, "payment_start", 45000, nil, nil, "card", nil, local(1, 11)},
		{s1, "payment_failed", 60000, nil, nil, "declined", nil, local(1, 11)},
		{s1, "order_created", 90000, nil, nil, nil, nil, local(1, 11)},
		{s2, "session_start", 0, nil, nil, nil, nil, local(1, 15)},
		{s2, "product_view", 8000, "p-itest", nil, nil, nil, local(1, 15)},
		{s2, "session_abandoned", 70000, nil, int64(1890), "inactivity", "cart", local(1, 15)},
		{s3, "session_start", 0, nil, nil, nil, nil, local(2, 9)},
		{s3, "order_created", 150000, nil, nil, nil, nil, local(2, 9)},
		{s4, "session_start", 0, nil, nil, nil, nil, local(5, 9)}, // hors période
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO kiosk_events (event_type, merchant_id, kiosk_id, session_id, elapsed_ms, product_id, amount_cents, detail, step, created_at)
			VALUES ($1, $2, $3, CAST($4 AS uuid), $5, $6, $7, $8, $9, $10)`,
			e.typ, merchantID, kioskA, e.session, e.elapsed, e.product, e.amount, e.detail, e.step, e.at.UTC()); err != nil {
			t.Fatalf("seed event %s: %v", e.typ, err)
		}
	}

	startUTC, endUTC := timeutil.LocalDayRangeBounds(local(1, 0), local(2, 0), tz)
	got, err := NewRepository(db).GetKioskStats(ctx, []string{merchantID}, startUTC, endUTC, tz)
	if err != nil {
		t.Fatalf("GetKioskStats: %v", err)
	}

	if got.Totals != (KioskTotals{KioskOrders: 3, KioskRevenueTTCCents: 5000, AllRevenueTTCCents: 9000}) {
		t.Errorf("totals = %+v", got.Totals)
	}
	split := func(rows []KioskSplitRow) map[string]KioskSplitRow {
		m := map[string]KioskSplitRow{}
		for _, r := range rows {
			m[r.Key] = r
		}
		return m
	}
	if m := split(got.ByOrderType); m["TAKE_AWAY"].Orders != 2 || m["TAKE_AWAY"].RevenueTTCCents != 3500 || m["IN"].Orders != 1 || m["IN"].RevenueTTCCents != 1500 {
		t.Errorf("by_order_type = %+v", got.ByOrderType)
	}
	if m := split(got.ByPayment); m["card"].Orders != 1 || m["card"].RevenueTTCCents != 1500 || m["counter"].Orders != 2 || m["counter"].RevenueTTCCents != 3500 {
		t.Errorf("by_payment = %+v", got.ByPayment)
	}

	wantFunnel := KioskFunnel{Sessions: 3, FulfillmentChosen: 1, ProductViews: 2, AddToCart: 1, SummaryViews: 1, PaymentStarts: 1, Orders: 2}
	if got.Funnel != wantFunnel {
		t.Errorf("funnel = %+v, want %+v", got.Funnel, wantFunnel)
	}
	if got.PaymentFailures.CardStarts != 1 || got.PaymentFailures.Failures != 1 ||
		len(got.PaymentFailures.ByReason) != 1 || got.PaymentFailures.ByReason[0] != (KioskCount{Key: "declined", Count: 1}) {
		t.Errorf("payment failures = %+v", got.PaymentFailures)
	}
	if got.Abandonments.Sessions != 1 || got.Abandonments.AmountCents != 1890 ||
		len(got.Abandonments.ByStep) != 1 || got.Abandonments.ByStep[0] != (KioskAbandonStep{Step: "cart", Sessions: 1, AmountCents: 1890}) ||
		len(got.Abandonments.ByReason) != 1 || got.Abandonments.ByReason[0] != (KioskCount{Key: "inactivity", Count: 1}) {
		t.Errorf("abandonments = %+v", got.Abandonments)
	}
	// Médiane de 90 s et 150 s = 120 s ; 90e centile = 90 + 0,9 × 60 = 144 s.
	if got.Duration.Orders != 2 || got.Duration.MedianSeconds == nil || *got.Duration.MedianSeconds != 120 ||
		got.Duration.P90Seconds == nil || *got.Duration.P90Seconds != 144 {
		t.Errorf("duration = %+v (median %v, p90 %v)", got.Duration, deref(got.Duration.MedianSeconds), deref(got.Duration.P90Seconds))
	}
	if len(got.TopProducts) != 1 || got.TopProducts[0].ProductID != "p-itest" || got.TopProducts[0].Views != 2 || got.TopProducts[0].AddToCart != 1 {
		t.Errorf("top products = %+v", got.TopProducts)
	}
	if got.AudienceSince == nil || *got.AudienceSince != "2026-10-01" {
		t.Errorf("audience since = %v", got.AudienceSince)
	}

	days := map[string]KioskDay{}
	for _, d := range got.Daily {
		days[d.Date] = d
	}
	if days["2026-10-01"] != (KioskDay{Date: "2026-10-01", Sessions: 2, KioskOrders: 1, Abandonments: 1}) ||
		days["2026-10-02"] != (KioskDay{Date: "2026-10-02", Sessions: 1, KioskOrders: 2}) {
		t.Errorf("daily = %+v", got.Daily)
	}

	if len(got.ByMerchant) != 1 || got.ByMerchant[0] != (KioskMerchantRow{MerchantID: merchantID, Sessions: 3, KioskOrders: 3, KioskRevenueTTCCents: 5000}) {
		t.Errorf("by merchant = %+v", got.ByMerchant)
	}

	devices := map[string]KioskDeviceRow{}
	for _, d := range got.ByKiosk {
		devices[d.KioskID] = d
	}
	a := devices[kioskA]
	if a.Name != "Borne entrée" || a.Sessions != 3 || a.Orders != 2 || a.RevenueTTCCents != 4000 || a.Abandonments != 1 || a.PaymentFailures != 1 ||
		a.MedianSeconds == nil || *a.MedianSeconds != 120 {
		t.Errorf("kiosk A = %+v", a)
	}
	if b, ok := devices[kioskB]; !ok || b.Name != "Borne terrasse" || b.Orders != 0 || b.Sessions != 0 {
		t.Errorf("kiosk B (sans activité) = %+v, present %v", b, ok)
	}
	if g, ok := devices[kioskGone]; !ok || g.Name != "" || g.Orders != 1 || g.RevenueTTCCents != 1000 {
		t.Errorf("borne supprimée = %+v, present %v", g, ok)
	}
	if len(got.ByKiosk) != 3 || got.ByKiosk[0].KioskID != kioskA {
		t.Errorf("by kiosk order = %+v", got.ByKiosk)
	}
}

func deref(f *float64) interface{} {
	if f == nil {
		return nil
	}
	return *f
}
