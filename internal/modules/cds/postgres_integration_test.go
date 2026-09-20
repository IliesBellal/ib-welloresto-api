//go:build postgres_integration

package cds

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestCDSRepository_Postgres exerce le repository contre un vrai Postgres.
//
// Ce que les tests unitaires ne peuvent pas couvrir, et qui est précisément
// ce qui casse en production :
//   - le scan de `o.isDistributed`, colonne en casse mixte non quotée que
//     Postgres replie en minuscules ;
//   - la syntaxe `INTERVAL '60' MINUTE` de la fenêtre H-1 (D12) ;
//   - le round-trip jsonb des filtres order_types/channels ;
//   - les CHECK constraints de la migration 147.
//
// Lancement :
//
//	DB_DIALECT=postgres POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev \
//	  go test -tags postgres_integration ./internal/modules/cds/...
func TestCDSRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		displayID  = "itest-cds-1"
		displayID2 = "itest-cds-2"
		codeID     = "itest-cds-code-1"
		tokenID    = "itest-cds-tok-1"
		tokenID2   = "itest-cds-tok-2"
		codeHash   = "itest-cds-code-hash"
		tokenHash  = "itest-cds-token-hash"
	)
	var merchantID string

	cleanupFor := func(mid string) {
		for _, id := range []string{displayID, displayID2} {
			_, _ = db.ExecContext(ctx, `DELETE FROM cds_media_items WHERE display_id = $1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM cds_settings WHERE display_id = $1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM cds_device_tokens WHERE display_id = $1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM cds_enrollment_codes WHERE display_id = $1`, id)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM cds_enrollment_codes WHERE id = $1`, codeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cds_displays WHERE id IN ($1, $2)`, displayID, displayID2)
		if mid == "" {
			return
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, mid)
	}

	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-cds' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest CDS Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-cds', 'https://x', '06', 'mtok-cds', 'Europe/Paris', 1, 2)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	repo := NewRepository(db)

	// ---- Enrôlement ----

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	codeName := "Ecran comptoir"
	if err := repo.CreateEnrollmentCode(ctx, codeID, merchantID, codeHash, &codeName, expiresAt, "itest-user"); err != nil {
		t.Fatalf("CreateEnrollmentCode: %v", err)
	}

	code, err := repo.GetEnrollmentCodeByHash(ctx, codeHash)
	if err != nil || code == nil {
		t.Fatalf("GetEnrollmentCodeByHash: got (%v, %v)", code, err)
	}
	if code.MerchantID != merchantID {
		t.Errorf("code.MerchantID = %q, want %q", code.MerchantID, merchantID)
	}

	pending, err := repo.ListPendingEnrollmentCodes(ctx, merchantID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPendingEnrollmentCodes: got %d codes, err=%v", len(pending), err)
	}

	deviceID := "itest-android-id"
	display, err := repo.CreateDisplay(ctx, displayID, merchantID, "Ecran comptoir", "BoxModel", "Android 11", []byte("enc"), &deviceID)
	if err != nil {
		t.Fatalf("CreateDisplay: %v", err)
	}
	if display.Status != "active" {
		t.Errorf("display.Status = %q, want active", display.Status)
	}

	if err := repo.MarkEnrollmentCodeUsed(ctx, codeID, displayID); err != nil {
		t.Fatalf("MarkEnrollmentCodeUsed: %v", err)
	}
	pending, err = repo.ListPendingEnrollmentCodes(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListPendingEnrollmentCodes after use: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("a used code must not appear as pending, got %d", len(pending))
	}

	if err := repo.CreateDefaultSettings(ctx, displayID); err != nil {
		t.Fatalf("CreateDefaultSettings: %v", err)
	}

	// ---- Défauts et round-trip jsonb des filtres ----

	settings, err := repo.GetSettings(ctx, displayID)
	if err != nil || settings == nil {
		t.Fatalf("GetSettings: got (%v, %v)", settings, err)
	}
	if len(settings.OrderTypes) != 3 || len(settings.Channels) != 3 {
		t.Errorf("defaults: OrderTypes=%v Channels=%v, want 3 and 3", settings.OrderTypes, settings.Channels)
	}
	if settings.ShowWaitTime {
		t.Error("show_wait_time doit etre desactive par defaut (D11)")
	}

	takeAwayOnly := []string{"TAKE_AWAY"}
	welloOnly := []string{"WELLO_RESTO"}
	if err := repo.UpdateSettings(ctx, displayID, UpdateSettingsRequest{
		OrderTypes: &takeAwayOnly,
		Channels:   &welloOnly,
	}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	settings, err = repo.GetSettings(ctx, displayID)
	if err != nil {
		t.Fatalf("GetSettings after update: %v", err)
	}
	if len(settings.OrderTypes) != 1 || settings.OrderTypes[0] != "TAKE_AWAY" {
		t.Errorf("OrderTypes round-trip = %v, want [TAKE_AWAY]", settings.OrderTypes)
	}

	// ---- Tokens ----

	tokenExpiry := time.Now().UTC().AddDate(0, 0, 30)
	if err := repo.CreateDeviceToken(ctx, tokenID, displayID, tokenHash, tokenExpiry); err != nil {
		t.Fatalf("CreateDeviceToken: %v", err)
	}
	tok, err := repo.GetDeviceTokenByHash(ctx, tokenHash)
	if err != nil || tok == nil {
		t.Fatalf("GetDeviceTokenByHash: got (%v, %v)", tok, err)
	}
	if err := repo.RotateDeviceToken(ctx, tokenID, tokenID2, displayID, tokenHash+"-2", tokenExpiry); err != nil {
		t.Fatalf("RotateDeviceToken: %v", err)
	}
	old, err := repo.GetDeviceTokenByHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetDeviceTokenByHash (old): %v", err)
	}
	if old.RevokedAt == nil {
		t.Error("l'ancien refresh token doit etre revoque par la rotation")
	}

	// ---- Plafond : seule la revocation libere une place (D4 / D15) ----

	count, err := repo.GetActiveDisplayCount(ctx, merchantID)
	if err != nil || count != 1 {
		t.Fatalf("GetActiveDisplayCount = %d (err=%v), want 1", count, err)
	}

	if _, err := repo.CreateDisplay(ctx, displayID2, merchantID, "Ecran livreurs", "BoxModel", "Android 11", nil, nil); err != nil {
		t.Fatalf("CreateDisplay 2: %v", err)
	}
	count, _ = repo.GetActiveDisplayCount(ctx, merchantID)
	if count != 2 {
		t.Errorf("GetActiveDisplayCount = %d, want 2", count)
	}

	revoked, err := repo.RevokeDisplay(ctx, merchantID, displayID2)
	if err != nil || !revoked {
		t.Fatalf("RevokeDisplay: revoked=%v err=%v", revoked, err)
	}
	count, _ = repo.GetActiveDisplayCount(ctx, merchantID)
	if count != 1 {
		t.Errorf("apres revocation, GetActiveDisplayCount = %d, want 1", count)
	}

	// ---- Projection du plateau ----

	seedOrder := func(orderNum int, orderType, brandStatus string, isDistributed bool, scheduled bool, estimatedReady *time.Time) string {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, order_type, brand, brand_status, state,
			                    merchant_approval, isDistributed, scheduled, estimated_ready,
			                    price, TVA, HT, created_by)
			VALUES ($1, $2, $3, 'WELLO_RESTO', $4, 'OPEN', 'ACCEPTED', $5, $6, $7, 700, 70, 630, 'ITEST')
			RETURNING order_id`,
			merchantID, orderNum, orderType, brandStatus, isDistributed, scheduled, estimatedReady).Scan(&id); err != nil {
			t.Fatalf("seed order %d: %v", orderNum, err)
		}
		return strconv.FormatInt(id, 10)
	}

	allTypes := []string{"IN", "TAKE_AWAY", "DELIVERY"}
	allChannels := []string{"WELLO_RESTO", "UBER_EATS", "DELIVEROO"}

	// Le cas central de D1 : commande sur place distribuee. brand_status vaut
	// 'DONE', jamais READY_* — seul isDistributed la revele comme prete.
	inDone := seedOrder(101, "IN", "DONE", true, false, nil)
	// Prete via SetReadyForDistribution : READY_* sans isDistributed.
	readyNoFlag := seedOrder(102, "TAKE_AWAY", "READY_FOR_TAKE_AWAY", false, false, nil)
	// En preparation.
	preparing := seedOrder(103, "TAKE_AWAY", "PENDING", false, false, nil)

	rows, err := repo.GetBoardOrders(ctx, merchantID, allTypes, allChannels)
	if err != nil {
		t.Fatalf("GetBoardOrders: %v", err)
	}

	statusByID := map[string]string{}
	for _, row := range rows {
		statusByID[row.OrderID] = resolveStatus(row)
	}

	if got := statusByID[inDone]; got != "ready" {
		t.Errorf("commande sur place distribuee: status = %q, want ready (D1)", got)
	}
	if got := statusByID[readyNoFlag]; got != "ready" {
		t.Errorf("READY_FOR_TAKE_AWAY sans isDistributed: status = %q, want ready", got)
	}
	if got := statusByID[preparing]; got != "preparing" {
		t.Errorf("commande PENDING: status = %q, want preparing", got)
	}

	// ---- Filtres (D2) ----

	rows, err = repo.GetBoardOrders(ctx, merchantID, []string{"TAKE_AWAY"}, allChannels)
	if err != nil {
		t.Fatalf("GetBoardOrders filtre TAKE_AWAY: %v", err)
	}
	for _, row := range rows {
		if row.OrderID == inDone {
			t.Error("une commande IN ne doit pas passer un filtre TAKE_AWAY")
		}
	}

	rows, err = repo.GetBoardOrders(ctx, merchantID, allTypes, []string{"UBER_EATS"})
	if err != nil {
		t.Fatalf("GetBoardOrders filtre UBER_EATS: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("filtre UBER_EATS sur des commandes WELLO_RESTO: got %d rows, want 0", len(rows))
	}

	// Un filtre vide n'affiche rien, et ne doit pas produire un IN () invalide.
	rows, err = repo.GetBoardOrders(ctx, merchantID, []string{}, allChannels)
	if err != nil {
		t.Fatalf("GetBoardOrders filtre vide doit reussir, got: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("filtre vide: got %d rows, want 0", len(rows))
	}

	// ---- Fenêtre H-1 des commandes planifiées (D12) ----

	farAhead := time.Now().UTC().Add(3 * time.Hour)
	soon := time.Now().UTC().Add(30 * time.Minute)
	scheduledFar := seedOrder(104, "TAKE_AWAY", "PENDING", false, true, &farAhead)
	scheduledSoon := seedOrder(105, "TAKE_AWAY", "PENDING", false, true, &soon)

	rows, err = repo.GetBoardOrders(ctx, merchantID, allTypes, allChannels)
	if err != nil {
		t.Fatalf("GetBoardOrders scheduled: %v", err)
	}
	present := map[string]bool{}
	for _, row := range rows {
		present[row.OrderID] = true
	}
	if present[scheduledFar] {
		t.Error("une commande planifiee a H-3 ne doit pas encore etre affichee (D12)")
	}
	if !present[scheduledSoon] {
		t.Error("une commande planifiee a H-0,5 doit etre affichee (D12)")
	}

	// ---- Médias marketing (D6) ----

	url := "https://example.test/promo.mp4"
	mediaID := "itest-cds-media-1"
	if err := repo.CreateMediaItem(ctx, MediaItemRow{
		ID: mediaID, DisplayID: displayID, Kind: "video", URL: &url,
		DurationSeconds: 10, SortOrder: 0, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateMediaItem: %v", err)
	}

	qr := "https://example.test/menu"
	if err := repo.CreateMediaItem(ctx, MediaItemRow{
		ID: "itest-cds-media-2", DisplayID: displayID, Kind: "qr", QRPayload: &qr,
		DurationSeconds: 15, SortOrder: 1, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateMediaItem qr: %v", err)
	}

	items, err := repo.ListMediaItems(ctx, displayID, true)
	if err != nil || len(items) != 2 {
		t.Fatalf("ListMediaItems: got %d items, err=%v", len(items), err)
	}

	next, err := repo.GetNextMediaSortOrder(ctx, displayID)
	if err != nil || next != 2 {
		t.Errorf("GetNextMediaSortOrder = %d (err=%v), want 2", next, err)
	}

	if err := repo.ReorderMediaItems(ctx, displayID, []string{"itest-cds-media-2", mediaID}); err != nil {
		t.Fatalf("ReorderMediaItems: %v", err)
	}
	items, _ = repo.ListMediaItems(ctx, displayID, false)
	if items[0].ID != "itest-cds-media-2" {
		t.Errorf("apres reorder, premier media = %q, want itest-cds-media-2", items[0].ID)
	}

	deleted, err := repo.DeleteMediaItem(ctx, displayID, mediaID)
	if err != nil || !deleted {
		t.Fatalf("DeleteMediaItem: deleted=%v err=%v", deleted, err)
	}

	// ---- Heartbeat ----

	if err := repo.UpdateDisplayHeartbeat(ctx, displayID, "1.0.0", "127.0.0.1"); err != nil {
		t.Fatalf("UpdateDisplayHeartbeat: %v", err)
	}
	row, err := repo.GetDisplayForMerchant(ctx, merchantID, displayID)
	if err != nil || row == nil {
		t.Fatalf("GetDisplayForMerchant: got (%v, %v)", row, err)
	}
	if row.LastHeartbeatAt == nil {
		t.Error("last_heartbeat_at doit etre renseigne apres un heartbeat")
	}
	if row.AppVersion == nil || *row.AppVersion != "1.0.0" {
		t.Errorf("app_version = %v, want 1.0.0", row.AppVersion)
	}

	// Un heartbeat sans app_version ne doit pas effacer la version connue
	// (COALESCE(NULLIF(...))) — sinon le back-office perdrait l'information
	// au premier heartbeat d'un client qui ne l'envoie pas.
	if err := repo.UpdateDisplayHeartbeat(ctx, displayID, "", "127.0.0.1"); err != nil {
		t.Fatalf("UpdateDisplayHeartbeat sans version: %v", err)
	}
	row, _ = repo.GetDisplayForMerchant(ctx, merchantID, displayID)
	if row.AppVersion == nil || *row.AppVersion != "1.0.0" {
		t.Errorf("app_version apres heartbeat vide = %v, want 1.0.0 conservee", row.AppVersion)
	}

	// ---- Cloisonnement multi-tenant ----

	other, err := repo.GetDisplayForMerchant(ctx, "999999", displayID)
	if err != nil {
		t.Fatalf("GetDisplayForMerchant autre merchant: %v", err)
	}
	if other != nil {
		t.Error("un ecran ne doit jamais etre lisible depuis un autre merchant")
	}
}
