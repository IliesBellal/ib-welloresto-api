//go:build postgres_integration

package scannorderseo

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/scannorder"
)

// Écriture des réglages depuis le back-office, puis relecture par la page
// publique (scannorder.GetMerchantSEOInfo) et le sitemap. Migration 222
// requise (seo_links, seo_updated_at).
func TestScanNOrderSEO_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const qrCode = "itest-seo-qr"

	cleanup := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM bookings_settings WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM qrcodes WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = CAST($1 AS integer)`, mid)
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-seo' LIMIT 1`).Scan(&oldID); err == nil {
		cleanup(strconv.FormatInt(oldID, 10))
	}

	var intID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('Da Mario', '1 rue X 57000 Metz', '1', 'rue X', '57000', 'Metz', 'siret-seo', 'www.da-mario.fr', '06', 'mtok-seo', 'Europe/Paris', 49.1, 6.17)
		RETURNING id`).Scan(&intID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	mid := strconv.FormatInt(intID, 10)
	t.Cleanup(func() { cleanup(mid) })

	menuUpdate := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	for desc, q := range map[string]string{
		"merchant_parameters": `INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open, delivery_fees, delivery_fees_limit, minimum_cart_for_delivery_order, enable_advance_orders, automatically_add_customer_rewards)
			VALUES ($1, '2026-01-10 12:00:00+00', 'EUR', true, 0, 0, 0, false, false)`,
		"scannorder_settings": `INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, activated, storefront_enabled)
			VALUES ($1, '', '', '', '', true, true)`,
		"bookings_settings": `INSERT INTO bookings_settings (merchant_id, code, enabled) VALUES ($1, 'da-mario-rsv', true)`,
	} {
		if _, err := db.ExecContext(ctx, q, mid); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO qrcodes (merchant_id, code, creation_date) VALUES ($1, $2, NULL)`, mid, qrCode); err != nil {
		t.Fatalf("seed qrcodes: %v", err)
	}

	repo := NewRepository(db)
	sno := scannorder.NewRepository(db)

	// État initial : rien de saisi, lastmod = mise à jour de la carte.
	row, err := repo.GetSettings(ctx, mid)
	if err != nil || row.Title != "" || row.Links != "" || row.UpdatedAt != nil {
		t.Fatalf("GetSettings initial = %+v, %v", row, err)
	}
	if got := sitemapLastmod(t, sno, qrCode); got == nil || !got.Equal(menuUpdate) {
		t.Fatalf("lastmod initial = %v, want %v", got, menuUpdate)
	}

	// Écriture depuis le back-office.
	cuisines := []string{"pizza", "italien"}
	links := []string{"instagram.com/da-mario"}
	u, err := validateUpdate(&UpdateRequest{Title: strp("Da Mario — Pizzeria à Metz"), Description: strp("Pizzas au feu de bois."), CuisineTypes: &cuisines, Links: &links})
	if err != nil {
		t.Fatal(err)
	}
	seoUpdate := time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)
	if err := repo.UpdateSettings(ctx, mid, u, seoUpdate); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	row, err = repo.GetSettings(ctx, mid)
	if err != nil || row.Cuisines != "pizza,italien" || row.Links != `["https://instagram.com/da-mario"]` || row.UpdatedAt == nil || !row.UpdatedAt.Equal(seoUpdate) {
		t.Fatalf("GetSettings après écriture = %+v, %v", row, err)
	}

	// Relecture par la page publique.
	info, err := sno.GetMerchantSEOInfo(ctx, mid)
	if err != nil {
		t.Fatalf("GetMerchantSEOInfo: %v", err)
	}
	if info.SEOTitle != "Da Mario — Pizzeria à Metz" || info.SEOCuisineType != "pizza,italien" ||
		info.SEOLinks != `["https://instagram.com/da-mario"]` || info.WebSite != "www.da-mario.fr" ||
		info.ReservationCode == nil || *info.ReservationCode != "da-mario-rsv" {
		t.Fatalf("GetMerchantSEOInfo = %+v (reservation=%v)", info, info.ReservationCode)
	}

	// Sitemap : la modification du référencement, plus récente, fait le lastmod.
	if got := sitemapLastmod(t, sno, qrCode); got == nil || !got.Equal(seoUpdate) {
		t.Fatalf("lastmod après écriture = %v, want %v", got, seoUpdate)
	}

	// Effacement des liens : NULL, plus de lien publié.
	empty := []string{}
	u, _ = validateUpdate(&UpdateRequest{Links: &empty})
	if err := repo.UpdateSettings(ctx, mid, u, seoUpdate.Add(time.Hour)); err != nil {
		t.Fatalf("UpdateSettings (effacement): %v", err)
	}
	var linksNull bool
	if err := db.QueryRowContext(ctx, `SELECT seo_links IS NULL FROM scannorder_settings WHERE merchant_id = $1`, mid).Scan(&linksNull); err != nil || !linksNull {
		t.Fatalf("seo_links IS NULL = %v, %v", linksNull, err)
	}

	// Réservation désactivée : plus de code.
	if _, err := db.ExecContext(ctx, `UPDATE bookings_settings SET enabled = false WHERE merchant_id = $1`, mid); err != nil {
		t.Fatal(err)
	}
	if info, err := sno.GetMerchantSEOInfo(ctx, mid); err != nil || info.ReservationCode != nil {
		t.Fatalf("réservation désactivée : %+v, %v", info, err)
	}

	// Identité pour l'invite IA ; merchant inconnu.
	if name, city, err := repo.GetMerchantIdentity(ctx, mid); err != nil || name != "Da Mario" || city != "Metz" {
		t.Fatalf("GetMerchantIdentity = %q, %q, %v", name, city, err)
	}
	if err := repo.UpdateSettings(ctx, "999999999", u, seoUpdate); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("UpdateSettings merchant inconnu = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetSettings(ctx, "999999999"); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("GetSettings merchant inconnu = %v, want ErrNotFound", err)
	}
}

func sitemapLastmod(t *testing.T, sno *scannorder.Repository, slug string) *time.Time {
	t.Helper()
	entries, err := sno.GetSitemapEntries(context.Background())
	if err != nil {
		t.Fatalf("GetSitemapEntries: %v", err)
	}
	for _, e := range entries {
		if e.Slug == slug {
			return e.UpdatedAt
		}
	}
	t.Fatalf("%s absent du sitemap", slug)
	return nil
}
