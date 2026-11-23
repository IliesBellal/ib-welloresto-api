//go:build postgres_integration

package reviewrequests

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Sélection des commandes, anti-doublon, désinscription et plafond contre le
// vrai schéma (migration 223 requise).
func TestReviewRequests_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM review_request_optouts WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM review_requests WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM subscriptions WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = CAST($1 AS integer)`, mid)
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-reviews' LIMIT 1`).Scan(&oldID); err == nil {
		cleanup(strconv.FormatInt(oldID, 10))
	}

	var intID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('Da Mario', '3 rue Serpenoise', '3', 'rue Serpenoise', '57000', 'Metz', 'siret-reviews', '', '06', 'mtok-reviews', 'Europe/Paris')
		RETURNING id`).Scan(&intID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	mid := strconv.FormatInt(intID, 10)
	t.Cleanup(func() { cleanup(mid) })

	exec := func(desc, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	exec("scannorder_settings", `
		INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, activated, storefront_enabled,
		                                 google_review_url, review_request_enabled)
		VALUES ($1, '', '', '', '', true, true, 'https://g.page/r/Cabc/review', true)`, mid)

	customer := func(name, email string) int64 {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO customer (merchant_id, customer_name, customer_first_name, customer_email, customer_brand)
			VALUES ($1, $2, $2, $3, 'WELLO_RESTO') RETURNING customer_id`, mid, name, email).Scan(&id); err != nil {
			t.Fatalf("seed customer: %v", err)
		}
		return id
	}
	camille := customer("Camille", "camille@example.fr")
	noEmail := customer("Sans", "")

	// Heure fixe (14 h à Paris) : le passage complet du service tombe dans la
	// plage d'envoi quelle que soit l'heure du test.
	now := time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC)
	order := func(num int, customerID int64, source, createdBy, state string, closedAgo time.Duration) string {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by, customer_id)
			VALUES ($1, $2, 'WELLO_RESTO', $3, $3, 'TAKE_AWAY', NULLIF($4, ''), 1100, 100, 1000, $5, $6, $7) RETURNING order_id`,
			mid, num, state, source, now.Add(-closedAgo), createdBy, customerID).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	due := order(1, camille, "SCANNORDER", "SCANNORDER", "CLOSED", 3*time.Hour)
	legacy := order(2, camille, "", "-1", "CLOSED", 5*time.Hour)                // ancien marqueur ScanNOrder, order_source vide
	_ = order(3, camille, "SCANNORDER", "SCANNORDER", "CLOSED", 30*time.Minute) // trop récente
	_ = order(4, camille, "SCANNORDER", "SCANNORDER", "CLOSED", 72*time.Hour)   // trop ancienne
	_ = order(5, camille, "WELLO_RESTO_POS", "itest", "CLOSED", 3*time.Hour)    // caisse
	_ = order(6, camille, "SCANNORDER", "SCANNORDER", "CANCELED", 3*time.Hour)  // annulée
	_ = order(7, noEmail, "SCANNORDER", "SCANNORDER", "CLOSED", 3*time.Hour)    // sans e-mail

	repo := NewRepository(db)
	from, to := now.Add(-ReviewWindow), now.Add(-ReviewDelay)
	list := func() []string {
		t.Helper()
		cands, err := repo.ListCandidates(ctx, from, to, 100)
		if err != nil {
			t.Fatalf("ListCandidates: %v", err)
		}
		var ids []string
		for _, c := range cands {
			if c.MerchantID == mid {
				if c.Email != "camille@example.fr" || c.FirstName != "Camille" || c.Timezone != "Europe/Paris" || c.ReviewURL != "https://g.page/r/Cabc/review" {
					t.Fatalf("candidat = %+v", c)
				}
				ids = append(ids, c.OrderID)
			}
		}
		sort.Strings(ids)
		return ids
	}
	want := []string{due, legacy}
	sort.Strings(want)
	if got := list(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("candidats = %v, want %v", got, want)
	}

	// Une ligne par commande : la commande traitée ne revient plus, un second
	// Insert est ignoré.
	hash := emailHash("camille@example.fr")
	row := requestRow{MerchantID: mid, OrderID: due, EmailHash: hash, Token: "tok-itest-reviews-000000000001", Status: StatusPending}
	if ok, err := repo.Insert(ctx, row, now); err != nil || !ok {
		t.Fatalf("Insert = %v, %v", ok, err)
	}
	if ok, err := repo.Insert(ctx, row, now); err != nil || ok {
		t.Fatalf("second Insert = %v, %v", ok, err)
	}
	if got := list(); len(got) != 1 || got[0] != legacy {
		t.Fatalf("après traitement : %v", got)
	}

	// Plafond : une demande pending ou sent compte, depuis la date donnée.
	if recent, err := repo.RequestedSince(ctx, mid, hash, now.Add(-time.Hour)); err != nil || !recent {
		t.Fatalf("RequestedSince = %v, %v", recent, err)
	}
	sentAt := now
	if err := repo.Finish(ctx, due, StatusSent, "msg-1", &sentAt); err != nil {
		t.Fatal(err)
	}
	var status, msg string
	if err := db.QueryRowContext(ctx, `SELECT status, provider_message_id FROM review_requests WHERE order_id = $1`, due).Scan(&status, &msg); err != nil || status != StatusSent || msg != "msg-1" {
		t.Fatalf("Finish : %s %s %v", status, msg, err)
	}
	if recent, _ := repo.RequestedSince(ctx, mid, hash, now.Add(time.Hour)); recent {
		t.Fatal("RequestedSince doit borner par la date")
	}

	// Désinscription par jeton, idempotente, annulable.
	info, err := repo.GetByToken(ctx, row.Token)
	if err != nil || info.MerchantID != mid || info.MerchantName != "Da Mario" || info.EmailHash != hash || info.OptedOut {
		t.Fatalf("GetByToken = %+v, %v", info, err)
	}
	for i := 0; i < 2; i++ {
		if err := repo.SetOptOut(ctx, mid, hash, true, now); err != nil {
			t.Fatal(err)
		}
	}
	if out, _ := repo.IsOptedOut(ctx, mid, hash); !out {
		t.Fatal("désinscription non lue")
	}
	if info, _ := repo.GetByToken(ctx, row.Token); !info.OptedOut {
		t.Fatal("GetByToken ne voit pas la désinscription")
	}
	if err := repo.SetOptOut(ctx, mid, hash, false, now); err != nil {
		t.Fatal(err)
	}
	if out, _ := repo.IsOptedOut(ctx, mid, hash); out {
		t.Fatal("annulation non prise en compte")
	}
	if _, err := repo.GetByToken(ctx, "jeton-inconnu-0000000000000"); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("jeton inconnu : %v", err)
	}

	// Réglages de l'envoi de test.
	if name, url, err := repo.GetReviewSettings(ctx, mid); err != nil || name != "Da Mario" || url != "https://g.page/r/Cabc/review" {
		t.Fatalf("GetReviewSettings = %q %q %v", name, url, err)
	}

	// Abonnement suspendu, puis demande désactivée : plus aucun candidat.
	exec("subscription", `INSERT INTO subscriptions (stripe_subscription_id, merchant_id, package_id, status) VALUES ('sub_itest_reviews', $1, 1, 'suspended')`, mid)
	if got := list(); len(got) != 0 {
		t.Fatalf("abonnement suspendu : %v", got)
	}
	exec("subscription", `DELETE FROM subscriptions WHERE merchant_id = $1`, mid)
	exec("disable", `UPDATE scannorder_settings SET review_request_enabled = false WHERE merchant_id = $1`, mid)
	if got := list(); len(got) != 0 {
		t.Fatalf("demande désactivée : %v", got)
	}

	// Passage complet du service, avec un faux mailer.
	exec("enable", `UPDATE scannorder_settings SET review_request_enabled = true WHERE merchant_id = $1`, mid)
	m := &fakeMailer{}
	svc := &Service{repo: repo, mail: m, baseURL: "https://sno.example", log: zap.NewNop(), now: func() time.Time { return now }}
	// La commande restante (ancien marqueur ScanNOrder) n'est pas envoyée :
	// Camille a déjà reçu une demande (plafond de 90 jours). Elle est
	// enregistrée comme ignorée et ne revient plus.
	if res, err := svc.Run(ctx); err != nil || res.Skipped < 1 || len(m.sent) != 0 {
		t.Fatalf("Run = %+v, %v, envois %d", res, err, len(m.sent))
	}
	if res, err := svc.Run(ctx); err != nil || res != (RunResult{}) {
		t.Fatalf("second Run = %+v, %v", res, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM review_requests WHERE order_id = $1`, legacy).Scan(&status); err != nil || status != StatusSkippedRecent {
		t.Fatalf("commande restante : %q, %v", status, err)
	}
}
