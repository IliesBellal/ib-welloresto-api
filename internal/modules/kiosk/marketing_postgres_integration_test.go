//go:build postgres_integration

package kiosk

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Valide contre un vrai Postgres la migration 190 et les requêtes du ruban
// marketing : contraintes CHECK, lecture/écriture des colonnes marketing_* de
// kiosk_settings, cloisonnement par merchant.
//
// Prérequis : migrations/todo/190_kiosk_marketing_ribbon.up.sql appliquée.
func TestKioskMarketing_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	// kiosk_settings et kiosk_marketing_media n'ont aucune FK vers merchant :
	// des identifiants synthétiques suffisent.
	const (
		merchantA = "itest-kiosk-mkt-a"
		merchantB = "itest-kiosk-mkt-b"
	)
	cleanup := func() {
		for _, m := range []string{merchantA, merchantB} {
			_, _ = db.ExecContext(ctx, `DELETE FROM kiosk_marketing_media WHERE merchant_id = $1`, m)
			_, _ = db.ExecContext(ctx, `DELETE FROM kiosk_settings WHERE merchant_id = $1`, m)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// --- réglages : défauts de la migration, puis aller-retour par UpsertSettings ---
	settings := defaultKioskSettingsRow(merchantA)
	if err := repo.UpsertSettings(ctx, settings); err != nil {
		t.Fatalf("UpsertSettings (défauts) : %v", err)
	}
	got, err := repo.GetSettingsByMerchant(ctx, merchantA)
	if err != nil || got == nil {
		t.Fatalf("GetSettingsByMerchant = (%+v, %v)", got, err)
	}
	if got.MarketingRibbonEnabled || got.MarketingDefaultDurationSeconds != defaultMarketingDurationSeconds {
		t.Fatalf("défauts = (%v, %d), attendu (false, %d)", got.MarketingRibbonEnabled, got.MarketingDefaultDurationSeconds, defaultMarketingDurationSeconds)
	}

	settings.MarketingRibbonEnabled = true
	settings.MarketingDefaultDurationSeconds = 25
	if err := repo.UpsertSettings(ctx, settings); err != nil {
		t.Fatalf("UpsertSettings (ruban actif) : %v", err)
	}
	got, _ = repo.GetSettingsByMerchant(ctx, merchantA)
	if !got.MarketingRibbonEnabled || got.MarketingDefaultDurationSeconds != 25 {
		t.Fatalf("relecture = (%v, %d), attendu (true, 25)", got.MarketingRibbonEnabled, got.MarketingDefaultDurationSeconds)
	}

	// Le CHECK de la base refuse une durée hors bornes (le service la refuse
	// avant, mais la base est le dernier garde-fou).
	settings.MarketingDefaultDurationSeconds = 0
	if err := repo.UpsertSettings(ctx, settings); err == nil {
		t.Fatal("UpsertSettings avec une durée de 0 aurait dû violer le CHECK")
	}

	// --- médias ---
	next, err := repo.NextMarketingSortOrder(ctx, merchantA)
	if err != nil || next != 0 {
		t.Fatalf("NextMarketingSortOrder (vide) = (%d, %v), attendu 0", next, err)
	}

	own := 20
	items := []MarketingMediaRow{
		{ID: "itest-mkt-1", MerchantID: merchantA, Kind: "image", URL: "https://r2/1.jpg", SortOrder: 0},
		{ID: "itest-mkt-2", MerchantID: merchantA, Kind: "video", URL: "https://r2/2.mp4", SortOrder: 1},
		{ID: "itest-mkt-3", MerchantID: merchantA, Kind: "image", URL: "https://r2/3.png", DurationSeconds: &own, SortOrder: 2},
	}
	for _, item := range items {
		if err := repo.CreateMarketingMedia(ctx, item); err != nil {
			t.Fatalf("CreateMarketingMedia(%s) : %v", item.ID, err)
		}
	}

	if err := repo.CreateMarketingMedia(ctx, MarketingMediaRow{ID: "itest-mkt-bad", MerchantID: merchantA, Kind: "qr", URL: "x"}); err == nil {
		t.Fatal("un kind « qr » aurait dû violer le CHECK")
	}
	tooLong := 500
	if err := repo.CreateMarketingMedia(ctx, MarketingMediaRow{ID: "itest-mkt-bad2", MerchantID: merchantA, Kind: "image", URL: "x", DurationSeconds: &tooLong}); err == nil {
		t.Fatal("une durée de 500 s aurait dû violer le CHECK")
	}

	list, err := repo.ListMarketingMedia(ctx, merchantA)
	if err != nil || len(list) != 3 {
		t.Fatalf("ListMarketingMedia = (%d, %v), attendu 3", len(list), err)
	}
	if list[0].ID != "itest-mkt-1" || list[2].ID != "itest-mkt-3" {
		t.Fatalf("ordre inattendu : %s, %s, %s", list[0].ID, list[1].ID, list[2].ID)
	}
	if list[0].DurationSeconds != nil || list[2].DurationSeconds == nil || *list[2].DurationSeconds != 20 {
		t.Fatal("les durées NULL / propre n'ont pas fait l'aller-retour")
	}

	if count, err := repo.CountMarketingMedia(ctx, merchantA); err != nil || count != 3 {
		t.Fatalf("CountMarketingMedia = (%d, %v), attendu 3", count, err)
	}
	if next, _ := repo.NextMarketingSortOrder(ctx, merchantA); next != 3 {
		t.Fatalf("NextMarketingSortOrder = %d, attendu 3", next)
	}

	// --- cloisonnement : un autre merchant ne voit ni ne touche ces médias ---
	if other, _ := repo.GetMarketingMedia(ctx, merchantB, "itest-mkt-1"); other != nil {
		t.Fatal("un média d'un autre merchant ne doit pas être lisible")
	}
	if ok, _ := repo.DeleteMarketingMedia(ctx, merchantB, "itest-mkt-1"); ok {
		t.Fatal("un média d'un autre merchant ne doit pas être supprimable")
	}
	if ok, _ := repo.UpdateMarketingMediaDuration(ctx, merchantB, "itest-mkt-1", &own); ok {
		t.Fatal("la durée d'un média d'un autre merchant ne doit pas être modifiable")
	}

	// --- réordonnancement ---
	if err := repo.ReorderMarketingMedia(ctx, merchantA, []string{"itest-mkt-3", "itest-mkt-1", "itest-mkt-2"}); err != nil {
		t.Fatalf("ReorderMarketingMedia : %v", err)
	}
	list, _ = repo.ListMarketingMedia(ctx, merchantA)
	if list[0].ID != "itest-mkt-3" || list[1].ID != "itest-mkt-1" || list[2].ID != "itest-mkt-2" {
		t.Fatalf("ordre après réordonnancement : %s, %s, %s", list[0].ID, list[1].ID, list[2].ID)
	}

	// --- durées ---
	if ok, err := repo.UpdateMarketingMediaDuration(ctx, merchantA, "itest-mkt-1", &own); err != nil || !ok {
		t.Fatalf("UpdateMarketingMediaDuration = (%v, %v)", ok, err)
	}
	cleared, err := repo.ClearMarketingMediaDurations(ctx, merchantA)
	if err != nil || cleared != 2 {
		t.Fatalf("ClearMarketingMediaDurations = (%d, %v), attendu 2 (médias 1 et 3)", cleared, err)
	}

	// --- suppression ---
	if ok, err := repo.DeleteMarketingMedia(ctx, merchantA, "itest-mkt-2"); err != nil || !ok {
		t.Fatalf("DeleteMarketingMedia = (%v, %v)", ok, err)
	}
	if ok, _ := repo.DeleteMarketingMedia(ctx, merchantA, "itest-mkt-2"); ok {
		t.Fatal("une seconde suppression ne doit rien supprimer")
	}
}
