//go:build postgres_integration

package stats

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestStatsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Stats Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-stats', 'https://example.com', '0600000000', 'tok', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	repo := NewStatsRepository(db)

	tz, err := repo.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetMerchantTimezone failed against postgres: %v", err)
	}
	if tz != merchantTZ {
		t.Fatalf("expected %q, got %q", merchantTZ, tz)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// Seed an order at 2026-06-15 23:30 Europe/Paris (summer, UTC+2) — falls on
	// 2026-06-15 21:30 UTC, but local calendar day/hour must read as 06-15/23,
	// not 06-15 UTC's own day/hour. This is exactly what CONVERT_TZ's
	// AT TIME ZONE translation (with the interval-cast sign fix) must get right.
	localOrderTime := time.Date(2026, 6, 15, 23, 30, 0, 0, loc)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, price, ht, tva, created_by, state, order_type, ispaid, creation_date)
		VALUES ($1, 1, 'WELLO_RESTO', 'ACCEPTED', 3000, 2500, 500, 'itest', 'DONE', 'IN', true, $2)`,
		merchantID, localOrderTime.UTC()); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	// GetRevenue / GetOrderCount / GetAverageBasket: exercise dbx.GetDB plumbing
	// on buildOrdersAggregateQuery (no dialect-specific SQL beyond placeholders).
	dateInTz := localOrderTime
	today, _, _, _, _, _, err := repo.GetRevenue(ctx, merchantID, loc, dateInTz)
	if err != nil {
		t.Fatalf("GetRevenue failed against postgres: %v", err)
	}
	if today != 3000 {
		t.Fatalf("expected today revenue 3000, got %d", today)
	}

	orderCount, _, err := repo.GetOrderCount(ctx, merchantID, loc, dateInTz)
	if err != nil {
		t.Fatalf("GetOrderCount failed against postgres: %v", err)
	}
	if orderCount != 1 {
		t.Fatalf("expected 1 order today, got %d", orderCount)
	}

	// ListRevenueHTByLocalDay: the CONVERT_TZ -> AT TIME ZONE(::interval) fix.
	// Query a UTC window wide enough to catch the order regardless of which
	// calendar day (UTC vs local) it's attributed to, then assert the LOCAL day.
	tzOffset := GetTZOffset(loc, dateInTz)
	windowStart := localOrderTime.UTC().Add(-2 * time.Hour)
	windowEnd := localOrderTime.UTC().Add(2 * time.Hour)
	htByDay, err := repo.ListRevenueHTByLocalDay(ctx, merchantID, tzOffset, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("ListRevenueHTByLocalDay failed against postgres: %v", err)
	}
	if len(htByDay) != 1 {
		t.Fatalf("expected exactly 1 local day bucket, got %+v", htByDay)
	}
	if htByDay[0].LocalDay != "2026-06-15" {
		t.Fatalf("expected local day 2026-06-15 (sign-corrected CONVERT_TZ), got %q — check AT TIME ZONE sign handling", htByDay[0].LocalDay)
	}
	if htByDay[0].RevenueHTCents != 2500 {
		t.Fatalf("expected HT revenue 2500, got %d", htByDay[0].RevenueHTCents)
	}

	// GetHourlyData: HOUR(CONVERT_TZ(...)) fix + isPaid boolean literal.
	hourlyRevenue, hourlyOrders, err := repo.GetHourlyData(ctx, merchantID, loc, dateInTz)
	if err != nil {
		t.Fatalf("GetHourlyData failed against postgres: %v", err)
	}
	if len(hourlyRevenue) != 1 || hourlyRevenue[0]["hour"] != 23 {
		t.Fatalf("expected a single hour=23 bucket (local time), got %+v", hourlyRevenue)
	}
	if hourlyRevenue[0]["sur_place"] != int64(3000) {
		t.Fatalf("expected sur_place revenue 3000, got %+v", hourlyRevenue[0])
	}
	if len(hourlyOrders) != 1 || hourlyOrders[0]["sur_place"] != int64(1) {
		t.Fatalf("unexpected hourly order counts: %+v", hourlyOrders)
	}
}

func TestStatsRepository_Upsell_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var orderID int64
	var tvaID int64
	var productID int64

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM extra WHERE merchant_id = $1`, itoaPtr(&merchantIntID))
		_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoaPtr(&merchantIntID))
		if orderID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE order_id = $1`, orderID)
		}
		if productID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productID)
		}
		if tvaID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID)
		}
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token)
		VALUES ('ITest Upsell Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-upsell', 'https://example.com', '0600000000', 'tok')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest TVA', 'itest', 10) RETURNING tva_id`).Scan(&tvaID); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id)
		VALUES ($1, 'ITest Product', 'itest-cat', 500, $2) RETURNING product_id`,
		merchantID, tvaID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	now := time.Now().UTC()
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, price, ht, tva, created_by, state, order_type, creation_date)
		VALUES ($1, 2, 'WELLO_RESTO', 'ACCEPTED', 550, 500, 50, 'itest-user-1', 'DONE', 'IN', $2)
		RETURNING order_id`, merchantID, now).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price, is_upsell)
		VALUES ($1, $2, $3, 1, 500, true)`, orderID, productID, merchantID); err != nil {
		t.Fatalf("seed orderitem: %v", err)
	}

	repo := NewStatsRepository(db)
	start := now.Add(-1 * time.Hour)
	end := now.Add(1 * time.Hour)

	// GetUpsellTotals: exercises IFNULL->COALESCE and is_upsell boolean fixes,
	// plus the tva_rate-based HT computation (100 / (100+tva_rate) branch).
	totals, err := repo.GetUpsellTotals(ctx, merchantID, start, end)
	if err != nil {
		t.Fatalf("GetUpsellTotals failed against postgres: %v", err)
	}
	if totals.TotalLines != 1 {
		t.Fatalf("expected 1 upsell line, got %d", totals.TotalLines)
	}
	// price 500 * 100 / (100 + 10) = 454.5454... rounds to 455.
	if totals.RevenueHTCents != 455 {
		t.Fatalf("expected HT revenue 455, got %d", totals.RevenueHTCents)
	}

	count, err := repo.GetOrdersWithUpsellCount(ctx, merchantID, start, end)
	if err != nil {
		t.Fatalf("GetOrdersWithUpsellCount failed against postgres: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 order with upsell, got %d", count)
	}

	byServer, err := repo.ListUpsellByServer(ctx, merchantID, start, end)
	if err != nil {
		t.Fatalf("ListUpsellByServer failed against postgres: %v", err)
	}
	if len(byServer) != 1 || byServer[0].ServerID != "itest-user-1" || byServer[0].UpsellLines != 1 {
		t.Fatalf("unexpected upsell-by-server result: %+v", byServer)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func itoaPtr(v *int64) string {
	if v == nil || *v == 0 {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}
