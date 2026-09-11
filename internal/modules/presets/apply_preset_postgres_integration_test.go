//go:build postgres_integration

package presets_test

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/locations"
	"welloresto-api/internal/modules/menu"
	posModule "welloresto-api/internal/modules/pos"
	"welloresto-api/internal/modules/presets"
)

// LOT A Semaine 3, Chantier 9 (docs/decisions.md) : archétypes v2, qui
// corrigent deux bugs de v1 — "snack" avait manage_on_site = false (un kebab
// ne pourrait pas encaisser sur place), "fast_food" avait un plan de salle
// alors que la spécification prévoit un fonctionnement par numéro de
// commande appelé, sans salle.
//
// "snack" est le cas nominal explicitement demandé par le chantier (canaux,
// notamment manage_on_site désormais true), "traditional" couvre le plan de
// salle multi-zones (zones + tables), "fast_food" couvre l'absence de plan de
// salle, et un code inconnu couvre le rejet explicite.
func TestApplyPreset_Snack_CategoriesAndChannels_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	merchantID := seedMerchantForPreset(t, ctx, db, "siret-preset-snack", "ITest Preset Snack")

	presetsRepo := presets.NewRepository(db)
	menuRepo := menu.NewMenuRepository(db, nil)
	locationsRepo := locations.NewLocationsRepository(db)
	svc := presets.NewService(presetsRepo, menuRepo, locationsRepo)

	if err := svc.ApplyPreset(ctx, merchantID, "snack"); err != nil {
		t.Fatalf("ApplyPreset(snack): %v", err)
	}

	// --- channels: manage_on_site must be true (this was the v1 bug) ---
	var onSite, takeAway, delivery bool
	if err := db.QueryRowContext(ctx, `SELECT manage_on_site, manage_take_away, manage_delivery FROM merchant_parameters WHERE merchant_id = $1`, merchantID).
		Scan(&onSite, &takeAway, &delivery); err != nil {
		t.Fatalf("read back channels: %v", err)
	}
	if !onSite || !takeAway || !delivery {
		t.Fatalf("snack channels = (on_site=%v take_away=%v delivery=%v), want (true, true, true)", onSite, takeAway, delivery)
	}

	// --- categories, in order ---
	rows, err := db.QueryContext(ctx, `SELECT categ_name FROM productcateg WHERE merchant_id = $1 ORDER BY categ_order`, merchantID)
	if err != nil {
		t.Fatalf("read back categories: %v", err)
	}
	defer rows.Close()
	var gotCategories []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan category: %v", err)
		}
		gotCategories = append(gotCategories, name)
	}
	wantCategories := []string{"Sandwichs", "Assiettes", "Tacos", "Accompagnements", "Boissons", "Desserts"}
	if len(gotCategories) != len(wantCategories) {
		t.Fatalf("categories = %v, want %v", gotCategories, wantCategories)
	}
	for i, want := range wantCategories {
		if gotCategories[i] != want {
			t.Fatalf("categories[%d] = %q, want %q (full: %v)", i, gotCategories[i], want, gotCategories)
		}
	}

	// --- floor plan disabled for snack: no floors/locations created ---
	var floorCount, locationCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM floors WHERE merchant_id = $1`, merchantID).Scan(&floorCount)
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations WHERE merchant_id = $1`, merchantID).Scan(&locationCount)
	if floorCount != 0 || locationCount != 0 {
		t.Fatalf("snack floor plan: got %d floors / %d locations, want 0/0 (floor_plan.enabled=false)", floorCount, locationCount)
	}

	// --- covers_required + prep_times sample ---
	var coversRequired bool
	var prepMode string
	var prepTime int
	if err := db.QueryRowContext(ctx, `SELECT pos_covers_count_required, preparation_time_mode, preparation_time FROM merchant_parameters WHERE merchant_id = $1`, merchantID).
		Scan(&coversRequired, &prepMode, &prepTime); err != nil {
		t.Fatalf("read back covers/prep: %v", err)
	}
	if coversRequired || prepMode != "AUTO" || prepTime != 10 {
		t.Fatalf("snack covers/prep = (covers_required=%v mode=%q time=%d), want (false, AUTO, 10)", coversRequired, prepMode, prepTime)
	}

	// --- preset frozen on merchant, now v2 ---
	var presetCode string
	var presetVersion int
	if err := db.QueryRowContext(ctx, `SELECT preset_code, preset_version FROM merchant WHERE id = $1`, merchantID).
		Scan(&presetCode, &presetVersion); err != nil {
		t.Fatalf("read back merchant preset_code/version: %v", err)
	}
	if presetCode != "snack" || presetVersion != 2 {
		t.Fatalf("merchant preset = (%q, %d), want (snack, 2)", presetCode, presetVersion)
	}
}

// TestApplyPreset_FastFood_NoFloorPlan_Postgres covers the bug v2 fixes:
// fast_food must never create a floor plan (the spec calls for a
// called-order-number counter, not table service).
func TestApplyPreset_FastFood_NoFloorPlan_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	merchantID := seedMerchantForPreset(t, ctx, db, "siret-preset-fastfood", "ITest Preset FastFood")

	presetsRepo := presets.NewRepository(db)
	menuRepo := menu.NewMenuRepository(db, nil)
	locationsRepo := locations.NewLocationsRepository(db)
	svc := presets.NewService(presetsRepo, menuRepo, locationsRepo)

	if err := svc.ApplyPreset(ctx, merchantID, "fast_food"); err != nil {
		t.Fatalf("ApplyPreset(fast_food): %v", err)
	}

	var floorCount, locationCount int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM floors WHERE merchant_id = $1`, merchantID).Scan(&floorCount)
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations WHERE merchant_id = $1`, merchantID).Scan(&locationCount)
	if floorCount != 0 || locationCount != 0 {
		t.Fatalf("fast_food floor plan: got %d floors / %d locations, want 0/0 (floor_plan.enabled=false)", floorCount, locationCount)
	}

	var callNumbers bool
	if err := db.QueryRowContext(ctx, `SELECT pager_number_required FROM merchant_parameters WHERE merchant_id = $1`, merchantID).Scan(&callNumbers); err != nil {
		t.Fatalf("read back pager_number_required: %v", err)
	}
	if !callNumbers {
		t.Fatalf("fast_food pager_number_required = false, want true (called-order-number counter, no floor plan)")
	}
}

// TestApplyPreset_Traditional_FloorPlan_Postgres covers the floor_plan branch
// (zones -> floors, tables -> locations) that "snack" deliberately leaves untested.
func TestApplyPreset_Traditional_FloorPlan_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	merchantID := seedMerchantForPreset(t, ctx, db, "siret-preset-traditional", "ITest Preset Traditional")

	presetsRepo := presets.NewRepository(db)
	menuRepo := menu.NewMenuRepository(db, nil)
	locationsRepo := locations.NewLocationsRepository(db)
	svc := presets.NewService(presetsRepo, menuRepo, locationsRepo)

	if err := svc.ApplyPreset(ctx, merchantID, "traditional"); err != nil {
		t.Fatalf("ApplyPreset(traditional): %v", err)
	}

	var floorCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM floors WHERE merchant_id = $1`, merchantID).Scan(&floorCount); err != nil {
		t.Fatalf("count floors: %v", err)
	}
	if floorCount != 2 {
		t.Fatalf("floors = %d, want 2 (\"Salle\" + \"Terrasse\" zones)", floorCount)
	}

	for zone, wantTables := range map[string]int{"Salle": 14, "Terrasse": 6} {
		var tableCount int
		var minSeats, maxSeats int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*), MIN(seats), MAX(seats)
			FROM locations l JOIN floors f ON f.id = l.floor_id
			WHERE l.merchant_id = $1 AND f.name = $2
		`, merchantID, zone).Scan(&tableCount, &minSeats, &maxSeats); err != nil {
			t.Fatalf("count tables for %q: %v", zone, err)
		}
		if tableCount != wantTables || minSeats != 4 || maxSeats != 4 {
			t.Fatalf("%s tables = (count=%d seats %d-%d), want (%d, 4-4)", zone, tableCount, minSeats, maxSeats, wantTables)
		}
	}
}

// TestApplyPreset_UnknownCode_Postgres covers the rejection path — an
// unknown/inactive preset code must fail explicitly, writing nothing.
func TestApplyPreset_UnknownCode_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	merchantID := seedMerchantForPreset(t, ctx, db, "siret-preset-unknown", "ITest Preset Unknown")

	presetsRepo := presets.NewRepository(db)
	menuRepo := menu.NewMenuRepository(db, nil)
	locationsRepo := locations.NewLocationsRepository(db)
	svc := presets.NewService(presetsRepo, menuRepo, locationsRepo)

	err := svc.ApplyPreset(ctx, merchantID, "does-not-exist")
	if !errors.Is(err, presets.ErrPresetNotFound) {
		t.Fatalf("ApplyPreset(does-not-exist): err = %v, want presets.ErrPresetNotFound", err)
	}

	var presetCode sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT preset_code FROM merchant WHERE id = $1`, merchantID).Scan(&presetCode); err != nil {
		t.Fatalf("read back merchant preset_code: %v", err)
	}
	if presetCode.Valid {
		t.Fatalf("merchant.preset_code = %q, want NULL (nothing should be written on rejection)", presetCode.String)
	}
}

// seedMerchantForPreset creates a merchant through the same real path
// POSService.CreateMerchant uses up to (not including) EnsureSystemRoles —
// ApplyPreset only needs InitMerchantSatellites to have run (merchant_parameters
// row must exist for the UPDATE in UpdateMerchantParametersFromConfig to match).
func seedMerchantForPreset(t *testing.T, ctx context.Context, db *sql.DB, siret, fullName string) string {
	t.Helper()

	repo := posModule.NewPOSRepository(db)

	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM locations WHERE merchant_id = $1`,
			`DELETE FROM floors WHERE merchant_id = $1`,
			`DELETE FROM productcateg WHERE merchant_id = $1`,
			`DELETE FROM qrcodes WHERE merchant_id = $1`,
			`DELETE FROM scannorder_settings WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM subscriptions WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}

	merchantID, err := repo.InsertMerchant(ctx, posModule.CreateMerchantRequest{
		FullName: fullName, Address: "a", StreetNumber: "1", Street: "s",
		ZipCode: "75001", City: "Paris", SIRET: siret, Tel: "06",
		WebSite: "https://x", Email: siret + "@example.com",
	}, "tok-"+strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 36))
	if err != nil || merchantID == "" || merchantID == "0" {
		t.Fatalf("InsertMerchant = (%q, %v)", merchantID, err)
	}
	if err := repo.InsertSubscription(ctx, merchantID, "1"); err != nil {
		t.Fatalf("InsertSubscription: %v", err)
	}
	if err := repo.InitMerchantSatellites(ctx, merchantID); err != nil {
		t.Fatalf("InitMerchantSatellites: %v", err)
	}

	t.Cleanup(func() { cleanupFor(merchantID) })
	return merchantID
}
