//go:build postgres_integration

package haccp

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestHACCPRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		merchantID = "999927"
		userID     = "itest-haccp-user"
	)

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM temperature_reading_corrective_actions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM temperature_readings WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM temperature_sessions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM temperature_zones WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM haccp_corrective_actions WHERE id IN ('itest-ca-1','itest-ca-2')`)
		_, _ = db.ExecContext(ctx, `DELETE FROM cleaning_executions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cleaning_sessions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cleaning_surfaces WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cleaning_zones WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM goods_receipts WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM haccp_settings WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM components WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM component_category WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure_desc WHERE id = 9902 AND lang = 'FR'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
		VALUES ($1, 'ITest HACCP', 'Haccp', 'User', 'x', 'itest-haccp@example.com', 'haccp-tok')`, userID); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	repo := NewRepository(db)

	// --- settings : insert (UTCNow) puis replace ---
	settings, err := repo.GetOrCreateSettings(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetOrCreateSettings failed against postgres: %v", err)
	}
	if settings.TempEntryRequired {
		t.Fatal("expected default settings")
	}
	settings.TempEntryRequired = true
	settings.CleaningPhoto = true
	updated, err := repo.ReplaceSettings(ctx, merchantID, *settings)
	if err != nil {
		t.Fatalf("ReplaceSettings failed against postgres: %v", err)
	}
	if !updated.TempEntryRequired || !updated.CleaningPhoto {
		t.Fatalf("settings not updated: %+v", updated)
	}

	// --- zones de température ---
	zone, err := repo.CreateTemperatureZone(ctx, merchantID, CreateZoneRequest{Name: "Frigo 1", TargetTempMin: 0, TargetTempMax: 4})
	if err != nil {
		t.Fatalf("CreateTemperatureZone failed against postgres: %v", err)
	}
	zone2, err := repo.CreateTemperatureZone(ctx, merchantID, CreateZoneRequest{Name: "Congelo", TargetTempMin: -22, TargetTempMax: -18})
	if err != nil {
		t.Fatalf("CreateTemperatureZone (2) failed: %v", err)
	}
	zones, err := repo.ListTemperatureZones(ctx, merchantID)
	if err != nil || len(zones) != 2 {
		t.Fatalf("ListTemperatureZones = (%d zones, %v), want 2", len(zones), err)
	}
	found, err := repo.FindZonesByIDs(ctx, merchantID, []string{zone.ID, zone2.ID})
	if err != nil || len(found) != 2 {
		t.Fatalf("FindZonesByIDs = (%d, %v), want 2", len(found), err)
	}
	if _, err := repo.ReplaceTemperatureZone(ctx, merchantID, zone2.ID, ReplaceZoneRequest{Name: "Congelo 2", TargetTempMin: -25, TargetTempMax: -18}); err != nil {
		t.Fatalf("ReplaceTemperatureZone failed against postgres: %v", err)
	}
	if err := repo.SoftDeleteTemperatureZone(ctx, merchantID, zone2.ID); err != nil {
		t.Fatalf("SoftDeleteTemperatureZone failed against postgres: %v", err)
	}
	zones, err = repo.ListTemperatureZones(ctx, merchantID)
	if err != nil || len(zones) != 1 {
		t.Fatalf("expected 1 zone after soft delete, got (%d, %v)", len(zones), err)
	}

	// --- actions correctives (référentiel) ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO haccp_corrective_actions (id, code, label, description, severity_scope, active) VALUES
		('itest-ca-1', 'ITEST_RECONTROLE', 'Recontrôle', 'desc', 'alert', true),
		('itest-ca-2', 'ITEST_JETER', 'Jeter le produit', NULL, NULL, true)`); err != nil {
		t.Fatalf("seed corrective actions: %v", err)
	}
	actions, err := repo.ListCorrectiveActions(ctx)
	if err != nil {
		t.Fatalf("ListCorrectiveActions failed against postgres: %v", err)
	}
	if len(actions) < 2 {
		t.Fatalf("expected at least the 2 seeded actions, got %d", len(actions))
	}
	actionsByID, err := repo.FindCorrectiveActionsByIDs(ctx, []string{"itest-ca-1", "itest-ca-2"})
	if err != nil || len(actionsByID) != 2 {
		t.Fatalf("FindCorrectiveActionsByIDs = (%d, %v), want 2", len(actionsByID), err)
	}

	// --- session de température + relevés en batch ---
	session, err := repo.CreateTemperatureSession(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("CreateTemperatureSession failed against postgres: %v", err)
	}
	readings := []Reading{
		{ZoneID: zone.ID, Value: 3.5, Status: "ok"},
		{ZoneID: zone.ID, Value: 9.8, Status: "alert"},
	}
	if err := repo.InsertTemperatureReadingsBatch(ctx, merchantID, userID, session.ID, readings); err != nil {
		t.Fatalf("InsertTemperatureReadingsBatch failed against postgres: %v", err)
	}
	if err := repo.InsertTemperatureReadingCorrectiveActionsBatch(ctx, merchantID, userID, []readingCorrectiveActionCreate{
		{ReadingID: readings[1].ID, ActionID: "itest-ca-1"},
	}); err != nil {
		t.Fatalf("InsertTemperatureReadingCorrectiveActionsBatch failed against postgres: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02") + " 00:00:00"
	listed, err := repo.ListTemperatureReadings(ctx, merchantID, today, zone.ID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("ListTemperatureReadings = (%d, %v), want 2", len(listed), err)
	}

	dayStart := time.Now().UTC().Truncate(24 * time.Hour)
	summary, err := repo.GetLatestTemperatureSessionSummary(ctx, merchantID, dayStart, dayStart.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("GetLatestTemperatureSessionSummary failed against postgres: %v", err)
	}
	if summary == nil || summary.ID != session.ID || summary.Status != "alert" {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	detail, err := repo.GetTemperatureSessionDetail(ctx, merchantID, session.ID)
	if err != nil {
		t.Fatalf("GetTemperatureSessionDetail failed against postgres: %v", err)
	}
	if len(detail.Readings) != 2 {
		t.Fatalf("expected 2 readings in detail, got %d", len(detail.Readings))
	}
	var alertReading *Reading
	for i := range detail.Readings {
		if detail.Readings[i].Status == "alert" {
			alertReading = &detail.Readings[i]
		}
	}
	if alertReading == nil || len(alertReading.CorrectiveActions) != 1 || alertReading.CorrectiveActions[0].Code != "ITEST_RECONTROLE" {
		t.Fatalf("expected corrective action on alert reading, got %+v", alertReading)
	}

	// --- nettoyage : zones / surfaces / session / exécutions ---
	cz, err := repo.CreateCleaningZone(ctx, merchantID, CreateCleaningZoneRequest{Name: "Cuisine"})
	if err != nil {
		t.Fatalf("CreateCleaningZone failed against postgres: %v", err)
	}
	if _, err := repo.UpdateCleaningZone(ctx, merchantID, cz.ID, UpdateCleaningZoneRequest{Name: "Cuisine principale"}); err != nil {
		t.Fatalf("UpdateCleaningZone failed against postgres: %v", err)
	}
	czones, err := repo.ListCleaningZones(ctx, merchantID)
	if err != nil || len(czones) != 1 {
		t.Fatalf("ListCleaningZones = (%d, %v), want 1", len(czones), err)
	}

	surface, err := repo.CreateCleaningSurface(ctx, merchantID, CreateCleaningSurfaceRequest{
		ZoneID: cz.ID, Name: "Plan de travail", FrequencyUnit: "day", FrequencyCount: 1,
	})
	if err != nil {
		t.Fatalf("CreateCleaningSurface failed against postgres: %v", err)
	}
	if _, err := repo.UpdateCleaningSurface(ctx, merchantID, surface.ID, UpdateCleaningSurfaceRequest{
		ZoneID: cz.ID, Name: "Plan de travail inox", FrequencyUnit: "day", FrequencyCount: 1,
	}); err != nil {
		t.Fatalf("UpdateCleaningSurface failed against postgres: %v", err)
	}
	surfaces, err := repo.ListCleaningSurfaces(ctx, merchantID, cz.ID)
	if err != nil || len(surfaces) != 1 {
		t.Fatalf("ListCleaningSurfaces = (%d, %v), want 1", len(surfaces), err)
	}
	surfacesByID, err := repo.FindCleaningSurfacesByIDs(ctx, merchantID, []string{surface.ID})
	if err != nil || len(surfacesByID) != 1 {
		t.Fatalf("FindCleaningSurfacesByIDs = (%d, %v), want 1", len(surfacesByID), err)
	}

	csession, err := repo.CreateCleaningSession(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("CreateCleaningSession failed against postgres: %v", err)
	}
	if err := repo.InsertCleaningExecutionsBatch(ctx, merchantID, userID, csession.ID, []CleaningExecution{
		{SurfaceID: surface.ID},
	}); err != nil {
		t.Fatalf("InsertCleaningExecutionsBatch failed against postgres: %v", err)
	}

	csessions, err := repo.ListCleaningSessions(ctx, merchantID, today, "")
	if err != nil || len(csessions) != 1 || csessions[0].ExecutionsCount != 1 {
		t.Fatalf("ListCleaningSessions = (%+v, %v), want 1 session with 1 execution", csessions, err)
	}
	cdetail, err := repo.GetCleaningSessionDetail(ctx, merchantID, csession.ID)
	if err != nil || len(cdetail.Executions) != 1 {
		t.Fatalf("GetCleaningSessionDetail = (%+v, %v)", cdetail, err)
	}
	doneIDs, err := repo.ListCompletedCleaningSurfaceIDs(ctx, merchantID, dayStart, dayStart.Add(24*time.Hour))
	if err != nil || len(doneIDs) != 1 || doneIDs[0] != surface.ID {
		t.Fatalf("ListCompletedCleaningSurfaceIDs = (%v, %v)", doneIDs, err)
	}

	// suppression douce surface/zone
	if err := repo.SoftDeleteCleaningSurface(ctx, merchantID, surface.ID); err != nil {
		t.Fatalf("SoftDeleteCleaningSurface failed against postgres: %v", err)
	}
	if err := repo.SoftDeleteCleaningZone(ctx, merchantID, cz.ID); err != nil {
		t.Fatalf("SoftDeleteCleaningZone failed against postgres: %v", err)
	}

	// --- activités (UNION ALL + filtre statut + pagination) ---
	items, total, err := repo.ListActivities(ctx, merchantID, dayStart, dayStart.Add(24*time.Hour), "", "", 1, 10)
	if err != nil {
		t.Fatalf("ListActivities failed against postgres: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 activities (température + nettoyage), got total=%d items=%d", total, len(items))
	}
	items, total, err = repo.ListActivities(ctx, merchantID, dayStart, dayStart.Add(24*time.Hour), "", "alert", 1, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].Type != "temperatures" {
		t.Fatalf("filtered activities = (total=%d items=%+v err=%v), want the alert temperature session", total, items, err)
	}

	// --- réception marchandise (jsonb) ---
	receipt, err := repo.CreateGoodsReceipt(ctx, merchantID, userID, CreateGoodsReceiptRequest{
		Supplier: "Metro", ProductType: "frais", BatchNumber: "LOT-42", ProductTemp: 2.5,
		QuantitiesVerified: true, NonConformities: []string{"emballage abîmé"},
	})
	if err != nil {
		t.Fatalf("CreateGoodsReceipt failed against postgres: %v", err)
	}
	var storedNC string
	if err := db.QueryRowContext(ctx, `SELECT non_conformities::text FROM goods_receipts WHERE id = $1`, receipt.ID).Scan(&storedNC); err != nil {
		t.Fatalf("read back goods receipt: %v", err)
	}
	if storedNC == "" || storedNC == "null" {
		t.Fatalf("expected non_conformities jsonb stored, got %q", storedNC)
	}

	// --- composants HACCP (catégories + unités) ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO unit_of_measure_desc (id, lang, uom_desc, uom_short_desc) VALUES (9902, 'FR', 'kilogrammes', 'kg')`); err != nil {
		t.Fatalf("seed uom: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO component_category (merchant_id, merchant_categ_id, name, categ_order)
		VALUES ($1, 'CAT-H1', 'Frais', 1)`, merchantID); err != nil {
		t.Fatalf("seed component_category: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO components (merchant_id, name, unit_of_measure, stock, enabled, category_id, conservation_days, conservation_type, storage_temp_min, storage_temp_max)
		VALUES ($1, 'ITest Saumon', 9902, 5, true, 'CAT-H1', 3, 'froid', 0, 4)`, merchantID); err != nil {
		t.Fatalf("seed components: %v", err)
	}
	cats, err := repo.GetHaccpComponents(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetHaccpComponents failed against postgres: %v", err)
	}
	if len(cats) != 1 || len(cats[0].Components) != 1 || cats[0].Components[0].UnitOfMeasure != "kg" {
		t.Fatalf("unexpected haccp components: %+v", cats)
	}
}
