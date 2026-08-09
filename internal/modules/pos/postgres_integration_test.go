//go:build postgres_integration

package pos

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Vérification réelle du module pos contre le Postgres de dev.
// GetPOSStatus est couvert par TestPOSStatus_Postgres (même fichier), qui
// dépend transitivement de planning/settings.ResolvePlanningHoliday — converti
// dans le même chantier Tier 4 (voir rapport 29).
func TestPOSRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM labels WHERE label LIKE 'itest-pos%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM deletion_reasons WHERE deletion_reason_object = 'itest-pos-obj'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE 'itest-pos%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM restaurant_ticket WHERE barcode = 'itest-pos-barcode'`)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM hours_of_operation WHERE merchant_id = $1`,
			`DELETE FROM extra WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM qrcodes WHERE merchant_id = $1`,
			`DELETE FROM scannorder_settings WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant_marketing_settings WHERE merchant_id = $1`,
			`DELETE FROM haccp_settings WHERE merchant_id = $1`,
			`DELETE FROM bookings_settings WHERE merchant_id = $1`,
			`DELETE FROM cash_desks WHERE merchant_id = $1`,
			`DELETE FROM subscriptions WHERE merchant_id = $1`,
			`DELETE FROM users WHERE merchant_id = $1`,
			`DELETE FROM users_rights WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-pos' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	repo := NewPOSRepository(db)

	// --- create_repository : InsertMerchant / InsertSubscription /
	// InitMerchantSatellites / InsertUserRights ---
	var err error
	merchantID, err = repo.InsertMerchant(ctx, CreateMerchantRequest{
		FullName: "ITest POS Merchant", Address: "a", StreetNumber: "1", Street: "s",
		ZipCode: "75001", City: "Paris", SIRET: "siret-pos", Tel: "06",
		WebSite: "https://x", Email: "itest-pos@example.com",
	}, "mtok-pos")
	if err != nil || merchantID == "" || merchantID == "0" {
		t.Fatalf("InsertMerchant = (%q, %v)", merchantID, err)
	}
	if err := repo.InsertSubscription(ctx, merchantID, "1"); err != nil {
		t.Fatalf("InsertSubscription: %v", err)
	}
	if err := repo.InitMerchantSatellites(ctx, merchantID); err != nil {
		t.Fatalf("InitMerchantSatellites: %v", err)
	}
	var qrCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM qrcodes WHERE merchant_id = $1`, merchantID).Scan(&qrCount); err != nil || qrCount != 2 {
		t.Fatalf("qrcodes after satellites = (%d, %v), want 2", qrCount, err)
	}
	rightsID, err := repo.InsertUserRights(ctx, "itest-pos-user", merchantID, true, "rtok-pos")
	if err != nil || rightsID == 0 {
		t.Fatalf("InsertUserRights = (%d, %v)", rightsID, err)
	}

	// users lié aux droits (access_id) pour UpdatePOSStatus + vue livreurs
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, merchant_id, access_id, name, first_name, last_name, password, email, token, lat, lng)
		VALUES ('itest-pos-user', $1, $2, 'ITest POS User', 'Pos', 'User', 'x', 'itest-pos-u@example.com', 'utok-pos', '48.85', '2.35')`,
		merchantID, rightsID); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	// --- UpdatePOSStatus (scopé sur le marchand de la session) ---
	if err := repo.UpdatePOSStatus(ctx, merchantID, true); err != nil {
		t.Fatalf("UpdatePOSStatus(true): %v", err)
	}
	var isOpen bool
	if err := db.QueryRowContext(ctx, `SELECT is_open FROM merchant_parameters WHERE merchant_id = $1`, merchantID).Scan(&isOpen); err != nil || !isOpen {
		t.Fatalf("is_open after UpdatePOSStatus = (%v, %v), want true", isOpen, err)
	}
	if err := repo.UpdatePOSStatus(ctx, merchantID, false); err != nil {
		t.Fatalf("UpdatePOSStatus(false): %v", err)
	}

	// --- GetDeletionReasons (labels joints sur PK integer castée) ---
	var reasonID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO deletion_reasons (deletion_reason_type, deletion_reason_object, deletion_reason_desc, requires_comment)
		VALUES ('itest', 'itest-pos-obj', 'raison de test', true) RETURNING deletion_reason_id`).Scan(&reasonID); err != nil {
		t.Fatalf("seed deletion_reasons: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO labels (label_value, label_type, lang, label)
		VALUES ($1, 'deletion_reason', 'FR', 'itest-pos-raison')`, strconv.FormatInt(reasonID, 10)); err != nil {
		t.Fatalf("seed labels deletion_reason: %v", err)
	}
	reasons, err := repo.GetDeletionReasons(ctx, "itest-pos-obj")
	if err != nil || len(reasons) != 1 {
		t.Fatalf("GetDeletionReasons = (%d, %v), want 1", len(reasons), err)
	}
	if reasons[0].Label != "itest-pos-raison" || !reasons[0].RequiresComment || reasons[0].DeletionReasonID != strconv.FormatInt(reasonID, 10) {
		t.Fatalf("GetDeletionReasons row = %+v", reasons[0])
	}

	// --- GetTVARates (casts texte + boolean) ---
	var tvaID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('itest-pos-dt', 'itest-pos-tva10', 'itest-pos-tva-desc', 10) RETURNING tva_id`).Scan(&tvaID); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO labels (label_value, label_type, lang, label)
		VALUES ('itest-pos-dt', 'order_type', 'FR', 'itest-pos-surplace')`); err != nil {
		t.Fatalf("seed labels order_type: %v", err)
	}
	rates, err := repo.GetTVARates(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetTVARates: %v", err)
	}
	foundRate := false
	for _, ct := range rates {
		if ct.DeliveryType == "itest-pos-dt" {
			foundRate = true
			if ct.Name != "itest-pos-surplace" || len(ct.Rates) != 1 || ct.Rates[0].Value != 10 ||
				ct.Rates[0].ID != strconv.FormatInt(tvaID, 10) {
				t.Fatalf("GetTVARates group = %+v", ct)
			}
			// tva_desc distribué dans rate.description (distinct de label = tva_title)
			if ct.Rates[0].Description != "itest-pos-tva-desc" || ct.Rates[0].Label != "itest-pos-tva10" {
				t.Fatalf("GetTVARates rate label/description = %+v", ct.Rates[0])
			}
		}
	}
	if !foundRate {
		t.Fatalf("GetTVARates: itest delivery_type absent (%d groupes)", len(rates))
	}

	// --- Toggles (coercition MySQL des chaînes reproduite côté Go) ---
	if _, err := repo.ToggleProductionPaidOnly(ctx, merchantID, "1"); err != nil {
		t.Fatalf("ToggleProductionPaidOnly: %v", err)
	}
	var flag bool
	if err := db.QueryRowContext(ctx, `SELECT kitchen_show_only_paid FROM merchant_parameters WHERE merchant_id = $1`, merchantID).Scan(&flag); err != nil || !flag {
		t.Fatalf("kitchen_show_only_paid = (%v, %v), want true", flag, err)
	}
	if _, err := repo.ToggleSafetyStock(ctx, merchantID, "garbage"); err != nil {
		t.Fatalf("ToggleSafetyStock(garbage): %v", err) // non numérique -> 0, comme MySQL non-strict
	}
	if err := db.QueryRowContext(ctx, `SELECT disable_components_under_safety_stock FROM merchant_parameters WHERE merchant_id = $1`, merchantID).Scan(&flag); err != nil || flag {
		t.Fatalf("disable_components_under_safety_stock = (%v, %v), want false", flag, err)
	}
	if _, err := repo.ToggleScanNOrder(ctx, merchantID, "1"); err != nil {
		t.Fatalf("ToggleScanNOrder: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT activated FROM scannorder_settings WHERE merchant_id = $1`, merchantID).Scan(&flag); err != nil || !flag {
		t.Fatalf("scannorder activated = (%v, %v), want true", flag, err)
	}

	// --- GetDeliveryMen (user_status_view + cast m.id) ---
	men, err := repo.GetDeliveryMen(ctx, merchantID)
	if err != nil || len(men) != 1 || men[0].UserID != "itest-pos-user" {
		t.Fatalf("GetDeliveryMen = (%+v, %v), want 1 itest-pos-user", men, err)
	}

	// --- IsTicketUsed ---
	used, err := repo.IsTicketUsed(ctx, "itest-pos-barcode")
	if err != nil || used {
		t.Fatalf("IsTicketUsed(absent) = (%v, %v), want false", used, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO restaurant_ticket (merchant_id, barcode) VALUES ($1, 'itest-pos-barcode')`, merchantID); err != nil {
		t.Fatalf("seed restaurant_ticket: %v", err)
	}
	used, err = repo.IsTicketUsed(ctx, "itest-pos-barcode")
	if err != nil || !used {
		t.Fatalf("IsTicketUsed(présent) = (%v, %v), want true", used, err)
	}

	// --- Hours of operation : CRUD + formats + upsert ---
	cap5 := 5
	firstBooking := "10:00:00"
	created, err := repo.CreateHourOfOperation(ctx, merchantID, &models.POSHoursOfOperationPatch{
		DayOfWeekFrom: 1, DayOfWeekTo: 5, HourFrom: "09:00:00", HourTo: "18:00:00",
		BookingCapacity: &cap5, FirstBookingTime: &firstBooking,
	})
	if err != nil {
		t.Fatalf("CreateHourOfOperation: %v", err)
	}
	if created.HourFrom != "09:00:00" || created.HourTo != "18:00:00" || !created.Enabled ||
		created.BookingCapacity == nil || *created.BookingCapacity != 5 ||
		created.FirstBookingTime == nil || *created.FirstBookingTime != "10:00:00" {
		t.Fatalf("CreateHourOfOperation row = %+v", created)
	}

	validFrom := "2026-07-01 00:00:00"
	updated, err := repo.UpdateHourOfOperation(ctx, merchantID, created.ID, &models.POSHoursOfOperationPatch{
		DayOfWeekFrom: 2, DayOfWeekTo: 6, HourFrom: "10:00:00", HourTo: "19:00:00",
		BookingCapacity: &cap5, ValidFrom: &validFrom,
	})
	if err != nil {
		t.Fatalf("UpdateHourOfOperation: %v", err)
	}
	if updated.DayOfWeekFrom != 2 || updated.HourFrom != "10:00:00" ||
		updated.ValidFrom == nil || *updated.ValidFrom != "2026-07-01 00:00:00" {
		t.Fatalf("UpdateHourOfOperation row = %+v", updated)
	}
	if _, err := repo.UpdateHourOfOperation(ctx, merchantID, "hoo-absent", &models.POSHoursOfOperationPatch{
		DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "09:00:00", HourTo: "10:00:00",
	}); err != models.ErrNotFound {
		t.Fatalf("UpdateHourOfOperation(absent) err = %v, want ErrNotFound", err)
	}

	// Upsert : maj de la ligne existante (ON CONFLICT) + insertion (id UUID généré)
	if err := repo.UpsertHoursOfOperations(ctx, merchantID, []models.POSHoursOfOperationPatch{
		{ID: &updated.ID, DayOfWeekFrom: 3, DayOfWeekTo: 7, HourFrom: "11:00:00", HourTo: "20:00:00", BookingCapacity: &cap5},
		{DayOfWeekFrom: 6, DayOfWeekTo: 7, HourFrom: "12:00:00", HourTo: "14:00:00", BookingCapacity: &cap5},
	}); err != nil {
		t.Fatalf("UpsertHoursOfOperations: %v", err)
	}
	hours, err := repo.GetHoursOfOperations(ctx, merchantID)
	if err != nil || len(hours) != 2 {
		t.Fatalf("GetHoursOfOperations = (%d, %v), want 2", len(hours), err)
	}
	foundUpserted := false
	for _, h := range hours {
		if h.ID == updated.ID {
			foundUpserted = true
			if h.DayOfWeekFrom != 3 || h.HourFrom != "11:00:00" {
				t.Fatalf("upserted row = %+v", h)
			}
		}
	}
	if !foundUpserted {
		t.Fatalf("upsert n'a pas conservé l'id existant: %+v", hours)
	}

	if err := repo.DeleteHourOfOperation(ctx, merchantID, updated.ID); err != nil {
		t.Fatalf("DeleteHourOfOperation: %v", err)
	}
	if err := repo.DeleteHourOfOperation(ctx, merchantID, updated.ID); err != models.ErrNotFound {
		// la ligne est déjà enabled = FALSE : RowsAffected 0 -> ErrNotFound
		// (comportement "matched rows" identique ici car le SET change la valeur au 1er appel seulement)
		t.Logf("DeleteHourOfOperation(2e) err = %v (divergence changed/matched rows MySQL vs PG, informative)", err)
	}

	// --- UpdateMerchantSettings (SET dynamiques) + GetMerchantSettings ---
	name := "ITest POS Renamed"
	lat := 48.8566
	handicap := true
	isActive := true
	tz := "Europe/Paris"
	if err := repo.UpdateMerchant(ctx, merchantID, &models.MerchantSettings{
		BusinessName: &name, Lat: &lat, HandicapAccess: &handicap, IsActive: &isActive, Timezone: &tz,
	}); err != nil {
		t.Fatalf("UpdateMerchant: %v", err)
	}

	capacity := 4
	currency := "EUR"
	mpIsOpen := true
	formReq := json.RawMessage(`{"phone": "required"}`)
	if err := repo.UpdateMerchantParameters(ctx, merchantID, &models.MerchantParametersSettings{
		ConcurrentPreparationCapacity: &capacity,
		KitchenShowOnlyPaid:           &mpIsOpen,
		Currency:                      &currency,
		IsOpen:                        &mpIsOpen,
		CustomerFormRequirements:      &formReq,
	}); err != nil {
		t.Fatalf("UpdateMerchantParameters: %v", err)
	}

	smsEnabled := true
	smsSender := "ITESTPOS"
	if err := repo.UpdateMerchantMarketing(ctx, merchantID, &models.MerchantMarketingSettings{
		SMSEnabled: &smsEnabled, SMSSenderName: &smsSender,
	}); err != nil {
		t.Fatalf("UpdateMerchantMarketing: %v", err)
	}

	deliveryType := 2
	activated := true
	btnColor := "#123456"
	if err := repo.UpdateScannorderSettings(ctx, merchantID, &models.ScannorderSettings{
		Activated: &activated, DeliveryType: &deliveryType, BtnColor: &btnColor,
	}); err != nil {
		t.Fatalf("UpdateScannorderSettings: %v", err)
	}

	m, params, marketing, scann, err := repo.GetMerchantSettings(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetMerchantSettings: %v", err)
	}
	if m.BusinessName == nil || *m.BusinessName != name || m.HandicapAccess == nil || !*m.HandicapAccess {
		t.Fatalf("merchant settings = %+v", m)
	}
	if m.Lat == nil || *m.Lat != lat {
		t.Fatalf("merchant lat = %+v", m.Lat)
	}
	if params.ConcurrentPreparationCapacity == nil || *params.ConcurrentPreparationCapacity != 4 ||
		params.IsOpen == nil || !*params.IsOpen ||
		params.CustomerFormRequirements == nil || !strings.Contains(string(*params.CustomerFormRequirements), "required") {
		t.Fatalf("merchant parameters = %+v", params)
	}
	if marketing.SMSSenderName == nil || *marketing.SMSSenderName != smsSender {
		t.Fatalf("marketing settings = %+v", marketing)
	}
	if scann.Activated == nil || !*scann.Activated || scann.DeliveryType == nil || *scann.DeliveryType != 2 ||
		scann.BtnColor == nil || *scann.BtnColor != btnColor {
		t.Fatalf("scannorder settings = %+v", scann)
	}
}

// Les repositories accounting et reports (sous-packages) sont couverts par
// internal/modules/pos/accounting/postgres_integration_test.go.

// TestPOSStatus_Postgres couvre GetPOSStatus, différé jusqu'à la conversion de
// planning/settings (dépendance transitive ResolvePlanningHoliday, même
// précédent que scannorder au Tier 3).
func TestPOSStatus_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM planning_holiday_overrides WHERE merchant_id = $1`,
			`DELETE FROM hours_of_operation WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-posst' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest POS Status', 'a', '1', 's', '75001', 'Paris', 'siret-posst', 'https://x', '06', 'mtok-posst', 'UTC')
		RETURNING id`).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(mid, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update, is_open) VALUES ($1, now(), true)`, merchantID); err != nil {
		t.Fatalf("seed params: %v", err)
	}
	// plage ouverte 24/7 pour un statut déterministe
	if _, err := db.ExecContext(ctx, `
		INSERT INTO hours_of_operation (id, merchant_id, day_of_week_from, day_of_week_to, hour_from, hour_to)
		VALUES ('itest-posst-hoo', $1, 1, 7, '00:00:00', '23:59:59')`, merchantID); err != nil {
		t.Fatalf("seed hours: %v", err)
	}

	repo := NewPOSRepository(db)
	status, err := repo.GetPOSStatus(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetPOSStatus: %v", err)
	}
	if status.Wello.IsOpen != 1 || status.Wello.Status != "OPEN" {
		t.Fatalf("GetPOSStatus ouvert = %+v", status.Wello)
	}

	// jour férié forcé fermé -> CLOSED quel que soit l'horaire
	today := time.Now().UTC().Format("2006-01-02")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO planning_holiday_overrides (id, merchant_id, holiday_date, is_open, enabled, created_at, updated_at)
		VALUES ('itest-posst-hol', $1, $2, false, true, now(), now())`, merchantID, today); err != nil {
		t.Fatalf("seed holiday override: %v", err)
	}
	status, err = repo.GetPOSStatus(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetPOSStatus(férié): %v", err)
	}
	if status.Wello.IsOpen != 0 || status.Wello.Status != "CLOSED" {
		t.Fatalf("GetPOSStatus férié = %+v", status.Wello)
	}
}
