//go:build postgres_integration

package franchises

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/timeutil"
)

// TestFranchiseStats_Postgres : Franchise → Statistiques (FranchiseStats) —
// affichages, provenance et clics du hub (sno_events), commandes venues du hub
// (sno_order_attributions, au sens de la page Analyse), jour local.
func TestFranchiseStats_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const tzName = "Europe/Paris"
	tz, _ := time.LoadLocation(tzName)
	local := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, tz) }
	const franchiseID, otherFranchiseID = "fra-itest-stats", "fra-itest-other"

	var merchantIntID int64
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM sno_events WHERE franchise_id IN ($1, $2)`, franchiseID, otherFranchiseID)
		if merchantIntID != 0 {
			id := strconv.FormatInt(merchantIntID, 10)
			_, _ = db.ExecContext(ctx, `DELETE FROM sno_order_attributions WHERE merchant_id = $1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM franchises WHERE franchise_id IN ($1, $2)`, franchiseID, otherFranchiseID)
	})

	for _, f := range []struct{ id, slug string }{{franchiseID, "itest-stats"}, {otherFranchiseID, "itest-other"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO franchises (franchise_id, name, slug) VALUES ($1, 'ITest', $2)`, f.id, f.slug); err != nil {
			t.Fatalf("seed franchise: %v", err)
		}
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, franchise_id)
		VALUES ('ITest Franchise Merchant', 'addr', '1', 'street', '57000', 'Metz', 'siret-fra', 'https://example.com', '0600000000', 'tok-fra', $1, $2)
		RETURNING id`, tzName, franchiseID).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	type ev struct {
		typ, franchise string
		merchant, src  interface{}
		at             time.Time
	}
	for _, e := range []ev{
		{"hub_view", franchiseID, nil, "search", local(1, 12)},
		{"hub_view", franchiseID, nil, nil, local(1, 13)}, // sans source = direct
		{"hub_view", franchiseID, nil, nil, local(2, 0)},  // minuit local : 2 octobre
		{"hub_order_click", franchiseID, merchantID, nil, local(1, 12)},
		{"hub_reservation_click", franchiseID, merchantID, nil, local(2, 9)},
		{"hub_view", otherFranchiseID, nil, nil, local(1, 12)}, // autre franchise
		{"hub_view", franchiseID, nil, nil, local(9, 12)},      // hors période
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO sno_events (event_type, franchise_id, merchant_id, source, created_at) VALUES ($1, $2, $3, $4, $5)`,
			e.typ, e.franchise, e.merchant, e.src, e.at.UTC()); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	orderNum := 0
	seedOrder := func(price int64, brandStatus string, at time.Time, attributedTo string) {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date, order_source)
			VALUES ($1, $2, 'WELLO_RESTO', $3, 'CLOSED', 'TAKE_AWAY', $4, 0, 0, 'SCANNORDER', $5, 'SCANNORDER')
			RETURNING order_id`, merchantID, orderNum, brandStatus, price, at.UTC()).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		if attributedTo != "" {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO sno_order_attributions (order_id, merchant_id, source, franchise_id) VALUES ($1, $2, 'hub', $3)`,
				id, merchantID, attributedTo); err != nil {
				t.Fatalf("seed attribution: %v", err)
			}
		}
	}
	seedOrder(2400, "DONE", local(1, 12), franchiseID)
	seedOrder(1800, "DONE", local(2, 12), franchiseID)
	seedOrder(9900, "CANCELED", local(2, 13), franchiseID)    // annulée : hors périmètre
	seedOrder(3000, "DONE", local(2, 14), "")                 // pas venue du hub
	seedOrder(5000, "DONE", local(2, 15), otherFranchiseID)   // attribuée à une autre franchise

	repo := NewRepository(db)
	gotTZ, err := repo.FranchiseTimezone(ctx, franchiseID)
	if err != nil || gotTZ != tzName {
		t.Fatalf("timezone = %q, %v", gotTZ, err)
	}
	startUTC, endUTC := timeutil.LocalDayRangeBounds(local(1, 0), local(2, 0), tz)
	got, err := repo.FranchiseStats(ctx, franchiseID, []string{merchantID}, startUTC, endUTC, tzName)
	if err != nil {
		t.Fatalf("FranchiseStats: %v", err)
	}

	if got.Totals.HubViews != 3 || got.Totals.OrderClicks != 1 || got.Totals.ReservationClicks != 1 ||
		got.Totals.HubOrders != 2 || got.Totals.HubRevenueTTC != 4200 {
		t.Errorf("totals = %+v", got.Totals)
	}
	sources := map[string]int64{}
	for _, s := range got.Sources {
		sources[s.Source] = s.Views
	}
	if sources["direct"] != 2 || sources["search"] != 1 || len(sources) != 2 {
		t.Errorf("sources = %+v", got.Sources)
	}
	if len(got.Merchants) != 1 {
		t.Fatalf("merchants = %+v", got.Merchants)
	}
	m := got.Merchants[0]
	if m.MerchantID != merchantID || m.OrderClicks != 1 || m.ReservationClicks != 1 || m.HubOrders != 2 || m.HubRevenueTTC != 4200 {
		t.Errorf("merchant = %+v", m)
	}
	days := map[string]StatsDay{}
	for _, d := range got.Daily {
		days[d.Date] = d
	}
	if days["2026-10-01"] != (StatsDay{Date: "2026-10-01", HubViews: 2, OrderClicks: 1, HubOrders: 1}) ||
		days["2026-10-02"] != (StatsDay{Date: "2026-10-02", HubViews: 1, HubOrders: 1}) {
		t.Errorf("daily = %+v", got.Daily)
	}
	if got.TrackingSince == nil || *got.TrackingSince != "2026-10-01" {
		t.Errorf("tracking since = %v", got.TrackingSince)
	}
}
