//go:build postgres_integration

package accountingsync

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// À lancer après application de la migration 181 :
//
//	POSTGRES_URL=... go test -tags postgres_integration ./internal/modules/accountingsync/
//
// Écrit sous l'établissement fictif « itest-acc » (clôtures comprises) et
// nettoie avant et après.
func TestAccountingSync_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)
	const merchant, provider = "itest-acc", "fake"

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM accounting_sync_items WHERE merchant_id = 'itest-acc'`,
			`DELETE FROM accounting_connections WHERE merchant_id = 'itest-acc'`,
			`DELETE FROM fiscal_closures WHERE merchant_id = 'itest-acc'`,
		} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatalf("cleanup: %v", err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// Une seule connexion par établissement.
	if err := repo.CreateConnection(ctx, Connection{MerchantID: merchant, Provider: provider, Credentials: []byte("enc"), AccountLabel: "Test"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateConnection(ctx, Connection{MerchantID: merchant, Provider: "other", Credentials: []byte("enc")}); !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("seconde connexion : %v", err)
	}

	// Activation : sync_from n'est fixé qu'à la première.
	first := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.SaveMapping(ctx, merchant, testMapping(), json.RawMessage(`{"journal_id":9}`), first); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveMapping(ctx, merchant, testMapping(), json.RawMessage(`{"journal_id":9}`), first.AddDate(0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	conn, err := repo.GetConnection(ctx, merchant)
	if err != nil || conn == nil || conn.Status != StatusActive || conn.SyncFrom.Format("2006-01-02") != "2026-10-01" {
		t.Fatalf("connexion = %+v err=%v", conn, err)
	}
	if conn.Config.JournalCode != "VE" || conn.Config.DifferenceAccount != "471000" {
		t.Errorf("correspondance relue = %+v", conn.Config)
	}

	// Quatre clôtures journalières ; une hors période (avant sync_from).
	for _, day := range []string{"2026-09-30", "2026-10-01", "2026-10-02", "2026-10-03"} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO fiscal_closures (merchant_id, period_type, period_start, period_end, timezone, closed_at,
				sales_ttc, sales_ht, refunds_ttc, refunds_ht, net_ttc, net_ht, vat_by_rate, payments_by_mop, by_channel,
				receipts_count, orders_count, grand_total_period, perpetual_total, previous_hash, hash, signature)
			VALUES ('itest-acc', 'DAY', $1::date, $1::date, 'Europe/Paris', now(),
				11000, 10000, 0, 0, 11000, 10000,
				'[{"rate":10,"ttc":11000,"ht":10000,"tva":1000}]'::jsonb,
				'[{"mop":"CB","operation_type":"SALE","enabled":true,"amount":11000,"count":1}]'::jsonb, '[]'::jsonb,
				1, 1, 0, 0, 'p', 'h-'||$1::text, 's')`, day); err != nil {
			t.Fatalf("clôture %s: %v", day, err)
		}
	}

	pending, err := repo.PendingClosures(ctx, merchant, provider, *conn.SyncFrom, maxAttempts, 10)
	if err != nil || len(pending) != 3 || pending[0].Date != "2026-10-01" {
		t.Fatalf("à envoyer = %+v err=%v", pending, err)
	}
	var vat DailySales
	if err := json.Unmarshal(pending[0].VATRaw, &vat.VATByRate); err != nil || len(vat.VATByRate) != 1 || vat.VATByRate[0].HT != 10000 {
		t.Fatalf("TVA relue = %+v err=%v", vat.VATByRate, err)
	}

	// Envoyé, échoué, incertain : seuls les échecs (sous le plafond) reviennent.
	record := func(date, status string) {
		t.Helper()
		if err := repo.RecordItem(ctx, ItemUpdate{MerchantID: merchant, Provider: provider, Date: date, ClosureID: pending[0].ID, ClosureHash: "h", Status: status, ExternalID: "e-" + date, Entry: []byte(`{"lines":[]}`)}); err != nil {
			t.Fatal(err)
		}
	}
	record("2026-10-01", ItemSent)
	record("2026-10-02", ItemFailed)
	record("2026-10-03", ItemUncertain)
	pending, _ = repo.PendingClosures(ctx, merchant, provider, *conn.SyncFrom, maxAttempts, 10)
	if len(pending) != 1 || pending[0].Date != "2026-10-02" || pending[0].Attempts != 1 {
		t.Fatalf("après envois : %+v", pending)
	}
	if pending, _ = repo.PendingClosures(ctx, merchant, provider, *conn.SyncFrom, 1, 10); len(pending) != 0 {
		t.Errorf("échec au plafond de tentatives encore proposé : %+v", pending)
	}

	// Un jour envoyé n'est pas retentable ; un jour inconnu est introuvable.
	if err := repo.ResetItem(ctx, merchant, provider, "2026-10-01"); !errors.Is(err, ErrItemNotRetryable) {
		t.Errorf("reset d'un jour envoyé : %v", err)
	}
	if err := repo.ResetItem(ctx, merchant, provider, "2026-12-25"); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("reset d'un jour inconnu : %v", err)
	}
	if err := repo.ResetItem(ctx, merchant, provider, "2026-10-03"); err != nil {
		t.Fatalf("reset d'un jour incertain : %v", err)
	}
	items, _ := repo.ListItems(ctx, merchant, provider, 10)
	if len(items) != 3 || items[0].Date != "2026-10-03" || items[0].Status != ItemFailed || items[2].Status != ItemSent || items[2].ExternalID != "e-2026-10-01" {
		t.Errorf("journal = %+v", items)
	}

	// Verrou d'envoi : un second preneur est refusé jusqu'à la libération.
	release, ok, err := repo.TryLockSync(ctx, merchant)
	if err != nil || !ok {
		t.Fatalf("verrou : ok=%v err=%v", ok, err)
	}
	if _, ok2, _ := repo.TryLockSync(ctx, merchant); ok2 {
		t.Error("deux envois simultanés pour le même établissement")
	}
	release()
	release2, ok3, _ := repo.TryLockSync(ctx, merchant)
	if !ok3 {
		t.Fatal("verrou non libéré")
	}
	release2()

	// Rafraîchissement des identifiants sous verrou de ligne.
	if err := repo.WithCredentialsLock(ctx, merchant, func(cur []byte) ([]byte, error) {
		if string(cur) != "enc" {
			t.Errorf("identifiants lus = %q", cur)
		}
		return []byte("enc2"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if conn, _ = repo.GetConnection(ctx, merchant); string(conn.Credentials) != "enc2" {
		t.Errorf("identifiants après mise à jour = %q", conn.Credentials)
	}

	// Déconnexion : la connexion part, le journal reste.
	if err := repo.DeleteConnection(ctx, merchant); err != nil {
		t.Fatal(err)
	}
	if conn, _ = repo.GetConnection(ctx, merchant); conn != nil {
		t.Error("connexion encore présente")
	}
	if items, _ = repo.ListItems(ctx, merchant, provider, 10); len(items) != 3 {
		t.Errorf("journal après déconnexion = %d lignes", len(items))
	}
}
