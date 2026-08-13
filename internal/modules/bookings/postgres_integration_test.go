//go:build postgres_integration

package bookings

import (
	"context"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookingcore"
)

// Vérification réelle du module bookings contre le Postgres de dev — le module
// le plus dense en fonctions de date du repo : chaque fenêtre temporelle
// (occupation, expiration, rappels, auto-seat) est exercée sur données réelles.
func TestBookingsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM deletion_reasons WHERE deletion_reason_object = 'itest-bkg-obj' OR deletion_reason_desc = 'itest-bkg-raison'`)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM booking_waitlist WHERE merchant_id = $1`,
			`DELETE FROM booked_location WHERE booking_id IN (SELECT booking_id FROM bookings WHERE merchant_id = $1)`,
			`DELETE FROM bookings WHERE merchant_id = $1`,
			`DELETE FROM booking_duration_rules WHERE merchant_id = $1`,
			`DELETE FROM hours_of_operation WHERE merchant_id = $1`,
			`DELETE FROM locations WHERE merchant_id = $1`,
			`DELETE FROM customer WHERE merchant_id = $1`,
			`DELETE FROM bookings_settings WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-bkg' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url)
		VALUES ('ITest Bkg Resto', 'a', '1', 's', '75001', 'Paris', 'siret-bkg', 'https://x', '06', 'mtok-bkg', 'UTC', 'https://logo/itest.png')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	var tableT1, tableT2 int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO locations (merchant_id, location_name, location_desc, seats) VALUES ($1, 'T1', 'terrasse', 4) RETURNING location_id`, merchantID).Scan(&tableT1); err != nil {
		t.Fatalf("seed location T1: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO locations (merchant_id, location_name, location_desc, seats) VALUES ($1, 'T2', 'salle', 6) RETURNING location_id`, merchantID).Scan(&tableT2); err != nil {
		t.Fatalf("seed location T2: %v", err)
	}

	var reasonID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO deletion_reasons (deletion_reason_type, deletion_reason_object, deletion_reason_desc)
		VALUES ('itest', 'booking', 'itest-bkg-raison') RETURNING deletion_reason_id`).Scan(&reasonID); err != nil {
		t.Fatalf("seed deletion_reasons: %v", err)
	}
	reasonStr := strconv.FormatInt(reasonID, 10)

	repo := NewBookingsRepository(db, zap.NewNop())

	// --- settings : défauts sans ligne, puis upsert réel (bug ON DUPLICATE corrigé) ---
	settings, err := repo.GetBookingSettings(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetBookingSettings(défauts): %v", err)
	}
	if !settings.Enabled || settings.SMSEnabled || settings.DefaultBookingDuration != 90 || settings.PhysicalCapacity != 10 {
		t.Fatalf("GetBookingSettings défauts = %+v", settings)
	}

	putReq := &PutBookingSettingsRequest{
		Enabled: true, Code: "itest-bkg", AutoAcceptReserveBookings: true,
		SlotIntervalMinutes: 30, DefaultBookingDuration: 90,
		ReserveMaximumPartySize: 8, ReserveMinimumPartySize: 1,
		LastBookingOffsetMinutes: 60, MinBookingNoticeMinutes: 0,
		MaxBookingHorizonDays: 90, OverbookingPercent: 0,
		CancelableByCustomer: true, CancelBookingLimitOffsetHours: 48,
		PendingExpirationHours: 24, SMSEnabled: true,
		WaitlistEnabled: true, WaitlistMaxSize: 5, WaitlistSlotExpiryMinutes: 15,
	}
	if err := repo.UpsertBookingSettings(ctx, merchantID, putReq); err != nil {
		t.Fatalf("UpsertBookingSettings(insert): %v", err)
	}
	putReq.SlotIntervalMinutes = 15
	putReq.WaitlistMaxSize = 3
	if err := repo.UpsertBookingSettings(ctx, merchantID, putReq); err != nil {
		t.Fatalf("UpsertBookingSettings(update): %v", err)
	}
	var settingsCount, slotInterval int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(slot_interval_minutes) FROM bookings_settings WHERE merchant_id = $1`, merchantID).Scan(&settingsCount, &slotInterval); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if settingsCount != 1 || slotInterval != 15 {
		t.Fatalf("upsert settings = %d lignes, interval %d — le bug de duplication ON DUPLICATE doit être corrigé", settingsCount, slotInterval)
	}

	// --- règles de durée ---
	rule, err := repo.CreateBookingDurationRule(ctx, merchantID, CreateDurationRuleRequest{MinPartySize: 1, MaxPartySize: 4, DurationMinutes: 60})
	if err != nil || rule.DurationMinutes != 60 || !rule.Enabled {
		t.Fatalf("CreateBookingDurationRule = (%+v, %v)", rule, err)
	}
	newMax := 6
	rule, err = repo.UpdateBookingDurationRule(ctx, merchantID, rule.RuleID, PatchDurationRuleRequest{MaxPartySize: &newMax})
	if err != nil || rule.MaxPartySize != 6 {
		t.Fatalf("UpdateBookingDurationRule = (%+v, %v)", rule, err)
	}
	rules, err := repo.ListBookingDurationRules(ctx, merchantID)
	if err != nil || len(rules) != 1 {
		t.Fatalf("ListBookingDurationRules = (%d, %v)", len(rules), err)
	}
	if err := repo.DeleteBookingDurationRule(ctx, merchantID, rule.RuleID); err != nil {
		t.Fatalf("DeleteBookingDurationRule: %v", err)
	}
	if err := repo.DeleteBookingDurationRule(ctx, merchantID, rule.RuleID); err != models.ErrNotFound {
		t.Fatalf("DeleteBookingDurationRule(2e) = %v, want ErrNotFound", err)
	}

	// --- horaires (upsert transactionnel, formats time/timestamp) ---
	cap10 := 10
	if err := repo.ReplaceBookingHours(ctx, merchantID, []models.POSHoursOfOperationPatch{
		{DayOfWeekFrom: 1, DayOfWeekTo: 7, HourFrom: "10:00:00", HourTo: "22:00:00", BookingCapacity: &cap10},
	}); err != nil {
		t.Fatalf("ReplaceBookingHours: %v", err)
	}
	hoursList, err := repo.ListBookingHours(ctx, merchantID)
	if err != nil || len(hoursList) != 1 || hoursList[0].HourFrom != "10:00:00" || hoursList[0].HourTo != "22:00:00" {
		t.Fatalf("ListBookingHours = (%+v, %v)", hoursList, err)
	}
	// second passage : maj de la ligne existante (ON CONFLICT) + nouvelle plage
	firstID := hoursList[0].ID
	if err := repo.ReplaceBookingHours(ctx, merchantID, []models.POSHoursOfOperationPatch{
		{ID: &firstID, DayOfWeekFrom: 1, DayOfWeekTo: 7, HourFrom: "10:00:00", HourTo: "23:00:00", BookingCapacity: &cap10},
		{DayOfWeekFrom: 6, DayOfWeekTo: 7, HourFrom: "12:00:00", HourTo: "14:00:00", BookingCapacity: &cap10},
	}); err != nil {
		t.Fatalf("ReplaceBookingHours(2e): %v", err)
	}
	hoursList, err = repo.ListBookingHours(ctx, merchantID)
	if err != nil || len(hoursList) != 2 {
		t.Fatalf("ListBookingHours après upsert = (%d, %v)", len(hoursList), err)
	}

	// --- création staff avec table (bookingcore + booked_location) ---
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02")
	custName := "ITest Bkg Client"
	custTel := "+33698765432"
	comment := "fenêtre"
	req := &BookingObjectRequest{
		MerchantID: merchantID,
		CreatedBy:  "itest-staff",
		Customer:   models.Customer{CustomerName: &custName, CustomerTel: &custTel},
		Booking: Booking{
			Status: bookingcore.StatusConfirmed, PartySize: 4, Comment: &comment,
			StartDate: tomorrow + " 14:00:00", EndDate: tomorrow + " 15:00:00",
			Locations: []BookingLocation{{LocationID: strconv.FormatInt(tableT1, 10)}},
		},
	}
	bookingID, err := repo.CreateBooking(ctx, req)
	if err != nil || bookingID == "" {
		t.Fatalf("CreateBooking = (%q, %v)", bookingID, err)
	}

	// --- fetcher (jointures castées, unix timestamps, tables) ---
	booking, err := repo.GetBookingByID(ctx, merchantID, bookingID)
	if err != nil {
		t.Fatalf("GetBookingByID: %v", err)
	}
	if booking.Customer.CustomerName == nil || *booking.Customer.CustomerName != custName {
		t.Fatalf("fetcher customer = %+v", booking.Customer)
	}
	if len(booking.Locations) != 1 || booking.Locations[0].LocationName != "T1" {
		t.Fatalf("fetcher locations = %+v", booking.Locations)
	}
	wantFrom, _ := time.Parse("2006-01-02 15:04:05", tomorrow+" 14:00:00")
	if booking.BookingDateFrom != wantFrom.Unix() {
		t.Fatalf("fetcher booking_date_from = %d, want %d", booking.BookingDateFrom, wantFrom.Unix())
	}

	// --- back-office (IN dynamique, LIKE, string_agg, pagination) ---
	search := "Bkg Client"
	items, total, err := repo.ListBookingsBackOffice(ctx, merchantID, BookingListFilters{
		Statuses: []string{bookingcore.StatusConfirmed, bookingcore.StatusPending},
		Search:   &search,
		SortBy:   "booking_date_from", SortDir: "asc", Page: 1, Limit: 10,
	})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("ListBookingsBackOffice = (%d/%d, %v)", len(items), total, err)
	}
	if len(items[0].AssignedTables) != 1 || items[0].AssignedTables[0] != "T1" {
		t.Fatalf("assigned_tables = %+v", items[0].AssignedTables)
	}

	// --- conflits de tables (IN + FOR UPDATE + fin effective de résa) ---
	// FindConflictingBookings ne matche que les statuts legacy (cf. commentaire
	// T-08 dans le repository) : on bascule temporairement la résa en ACCEPTED.
	t1Str := strconv.FormatInt(tableT1, 10)
	if _, err := db.ExecContext(ctx, `UPDATE bookings SET status = 'ACCEPTED' WHERE booking_id = $1`, bookingID); err != nil {
		t.Fatalf("switch statut legacy: %v", err)
	}
	conflicts, err := repo.FindConflictingBookings(ctx, merchantID, []string{t1Str}, tomorrow+" 14:30:00", tomorrow+" 15:30:00", "")
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("FindConflictingBookings(chevauche) = (%+v, %v)", conflicts, err)
	}
	conflicts, err = repo.FindConflictingBookings(ctx, merchantID, []string{t1Str}, tomorrow+" 15:00:00", tomorrow+" 16:00:00", "")
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("FindConflictingBookings(dos à dos) = (%+v, %v), want 0", conflicts, err)
	}
	conflicts, err = repo.FindConflictingBookings(ctx, merchantID, []string{t1Str}, tomorrow+" 14:30:00", tomorrow+" 15:30:00", bookingID)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("FindConflictingBookings(exclusion) = (%+v, %v), want 0", conflicts, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE bookings SET status = 'confirmed' WHERE booking_id = $1`, bookingID); err != nil {
		t.Fatalf("restore statut confirmed: %v", err)
	}

	// --- disponibilité (moteur d'occupation : le scan des dates doit rester
	// parsable par bookingcore, format RFC3339 des deux drivers) ---
	avail, err := repo.GetBookingAvailability(ctx, merchantID, tomorrow)
	if err != nil {
		t.Fatalf("GetBookingAvailability: %v", err)
	}
	foundSlot := false
	for _, slot := range avail.Slots {
		if slot.DateFrom == tomorrow+" 14:00:00" {
			foundSlot = true
			if slot.Capacity != 10 || slot.RemainingCapacity != 6 {
				t.Fatalf("slot 14:00 = %+v, want capacité 10 / restant 6 (occupation de la résa de 4)", slot)
			}
		}
	}
	if !foundSlot {
		t.Fatalf("slot 14:00 absent (%d slots)", len(avail.Slots))
	}

	// --- CheckCapacityForWindow ---
	ok, err := repo.CheckCapacityForWindow(ctx, merchantID, tomorrow+" 16:00:00", tomorrow+" 17:00:00", 2, "")
	if err != nil || !ok {
		t.Fatalf("CheckCapacityForWindow(2 couverts) = (%v, %v), want true", ok, err)
	}
	ok, err = repo.CheckCapacityForWindow(ctx, merchantID, tomorrow+" 14:00:00", tomorrow+" 15:00:00", 8, "")
	if err != nil || ok {
		t.Fatalf("CheckCapacityForWindow(8 + 4 occupés > 10) = (%v, %v), want false", ok, err)
	}
	ok, err = repo.CheckCapacityForWindow(ctx, merchantID, tomorrow+" 14:00:00", tomorrow+" 15:00:00", 8, bookingID)
	if err != nil || !ok {
		t.Fatalf("CheckCapacityForWindow(exclusion) = (%v, %v), want true", ok, err)
	}

	// --- reschedule (durée calculée côté Go) ---
	if err := repo.RescheduleBooking(ctx, merchantID, bookingID, tomorrow+" 14:00:00", tomorrow+" 15:30:00", nil, nil, nil); err != nil {
		t.Fatalf("RescheduleBooking: %v", err)
	}
	var storedDuration int
	if err := db.QueryRowContext(ctx, `SELECT booking_duration FROM bookings WHERE booking_id = $1`, bookingID).Scan(&storedDuration); err != nil || storedDuration != 90 {
		t.Fatalf("booking_duration après reschedule = (%d, %v), want 90", storedDuration, err)
	}

	// --- réattribution de tables ---
	if err := repo.ReplaceBookingLocations(ctx, merchantID, bookingID, []string{strconv.FormatInt(tableT2, 10)}); err != nil {
		t.Fatalf("ReplaceBookingLocations: %v", err)
	}

	// --- rappels (fenêtre paramétrée + marquage) ---
	reminders, err := repo.ListBookingsForReminder(ctx, 48)
	if err != nil {
		t.Fatalf("ListBookingsForReminder: %v", err)
	}
	foundReminder := false
	for _, c := range reminders {
		if c.BookingID == bookingID {
			foundReminder = true
			if !c.SMSEnabled || c.MerchantSlug != "itest-bkg" || c.CustomerName != custName {
				t.Fatalf("reminder contact = %+v", c)
			}
		}
	}
	if !foundReminder {
		t.Fatalf("résa absente des rappels sous 48h")
	}
	if err := repo.MarkReminderSent(ctx, bookingID); err != nil {
		t.Fatalf("MarkReminderSent: %v", err)
	}

	// --- seat + lien commande ---
	if err := repo.SetBookingSeatedWithOrder(ctx, merchantID, bookingID, "424242"); err != nil {
		t.Fatalf("SetBookingSeatedWithOrder: %v", err)
	}
	linked, err := repo.FindSeatedBookingByOrderID(ctx, merchantID, "424242")
	if err != nil || linked != bookingID {
		t.Fatalf("FindSeatedBookingByOrderID = (%q, %v)", linked, err)
	}

	// --- auto-seat (fenêtre ±30 min autour de maintenant) ---
	nowStart := time.Now().UTC().Add(-10 * time.Minute).Format("2006-01-02 15:04:05")
	nowEnd := time.Now().UTC().Add(80 * time.Minute).Format("2006-01-02 15:04:05")
	req2 := &BookingObjectRequest{
		MerchantID: merchantID, CreatedBy: "itest-staff",
		Customer: models.Customer{CustomerName: &custName, CustomerTel: &custTel},
		Booking: Booking{
			Status: bookingcore.StatusConfirmed, PartySize: 2,
			StartDate: nowStart, EndDate: nowEnd,
			Locations: []BookingLocation{{LocationID: strconv.FormatInt(tableT1, 10)}},
		},
	}
	booking2, err := repo.CreateBooking(ctx, req2)
	if err != nil {
		t.Fatalf("CreateBooking(2): %v", err)
	}
	autoSeat, err := repo.FindConfirmedBookingForAutoSeat(ctx, merchantID, []string{t1Str})
	if err != nil || autoSeat != booking2 {
		t.Fatalf("FindConfirmedBookingForAutoSeat = (%q, %v), want %q", autoSeat, err, booking2)
	}

	// --- deny / cancel / raison de suppression ---
	valid, err := repo.IsValidDeletionReason(ctx, reasonStr)
	if err != nil || !valid {
		t.Fatalf("IsValidDeletionReason(%s) = (%v, %v)", reasonStr, valid, err)
	}
	valid, err = repo.IsValidDeletionReason(ctx, "raison-inconnue")
	if err != nil || valid {
		t.Fatalf("IsValidDeletionReason(non numérique) = (%v, %v), want false (coercition MySQL)", valid, err)
	}
	if err := repo.DenyBooking(ctx, merchantID, booking2, "itest-staff", &DenyBookingRequest{DeletionReasonID: &reasonStr}); err != nil {
		t.Fatalf("DenyBooking: %v", err)
	}
	var status2 string
	var deletionDate2 *time.Time
	if err := db.QueryRowContext(ctx, `SELECT status, deletion_date FROM bookings WHERE booking_id = $1`, booking2).Scan(&status2, &deletionDate2); err != nil || status2 != bookingcore.StatusDenied || deletionDate2 == nil {
		t.Fatalf("deny résultat = (%q, %v, %v)", status2, deletionDate2, err)
	}
	if err := repo.CancelBooking(ctx, merchantID, bookingID, "itest-staff", nil); err != nil {
		t.Fatalf("CancelBooking: %v", err)
	}
	if err := repo.SetBookingState(ctx, merchantID, bookingID, bookingcore.StatusConfirmed); err != nil {
		t.Fatalf("SetBookingState: %v", err)
	}

	// --- expiration des pending (fenêtre paramétrée par bookings_settings) ---
	pastStart := time.Now().UTC().Add(-30 * time.Hour).Format("2006-01-02 15:04:05")
	pastEnd := time.Now().UTC().Add(-29 * time.Hour).Format("2006-01-02 15:04:05")
	req3 := &BookingObjectRequest{
		MerchantID: merchantID, CreatedBy: "itest-staff",
		Customer: models.Customer{CustomerName: &custName, CustomerTel: &custTel},
		Booking: Booking{
			Status: bookingcore.StatusPending, PartySize: 2,
			StartDate: pastStart, EndDate: pastEnd,
		},
	}
	booking3, err := repo.CreateBooking(ctx, req3)
	if err != nil {
		t.Fatalf("CreateBooking(3): %v", err)
	}
	toExpire, err := repo.ListPendingBookingsToExpire(ctx)
	if err != nil {
		t.Fatalf("ListPendingBookingsToExpire: %v", err)
	}
	foundExpire := false
	for _, c := range toExpire {
		if c.BookingID == booking3 {
			foundExpire = true
		}
	}
	if !foundExpire {
		t.Fatalf("résa pending de -30h absente de la liste d'expiration (%d)", len(toExpire))
	}
	expired, err := repo.ExpirePendingBookings(ctx)
	if err != nil || expired < 1 {
		t.Fatalf("ExpirePendingBookings = (%d, %v)", expired, err)
	}
	var status3 string
	if err := db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE booking_id = $1`, booking3).Scan(&status3); err != nil || status3 != bookingcore.StatusCancelled {
		t.Fatalf("statut après expiration = (%q, %v)", status3, err)
	}

	// --- waitlist (enum, intervalles paramétrés, cycle complet) ---
	custID, err := repo.FindOrCreateCustomerByPhone(ctx, merchantID, custName, custTel)
	if err != nil || custID == nil {
		t.Fatalf("FindOrCreateCustomerByPhone(existant) = (%v, %v)", custID, err)
	}
	entry := &WaitlistEntry{
		ID: "itest-bkg-wl-1", MerchantID: merchantID, CustomerID: custID,
		PartySize: 3, CustomerName: custName, CustomerPhone: custTel,
	}
	if err := repo.InsertWaitlistEntry(ctx, entry); err != nil {
		t.Fatalf("InsertWaitlistEntry: %v", err)
	}
	if n, err := repo.CountActiveWaitlist(ctx, merchantID); err != nil || n != 1 {
		t.Fatalf("CountActiveWaitlist = (%d, %v)", n, err)
	}
	wl, err := repo.ListWaitlist(ctx, merchantID)
	if err != nil || len(wl) != 1 || wl[0].Status != bookingcore.WaitlistWaiting || wl[0].CreatedAt == "" {
		t.Fatalf("ListWaitlist = (%+v, %v)", wl, err)
	}
	first, err := repo.GetFirstWaitingEntry(ctx, merchantID)
	if err != nil || first == nil || first.ID != entry.ID {
		t.Fatalf("GetFirstWaitingEntry = (%+v, %v)", first, err)
	}
	if err := repo.MarkWaitlistNotified(ctx, merchantID, entry.ID, 15); err != nil {
		t.Fatalf("MarkWaitlistNotified: %v", err)
	}
	if err := repo.MarkWaitlistNotified(ctx, merchantID, entry.ID, 15); err != models.ErrNotFound {
		t.Fatalf("MarkWaitlistNotified(2e) = %v, want ErrNotFound (garde waiting)", err)
	}
	got, err := repo.GetWaitlistEntry(ctx, merchantID, entry.ID)
	if err != nil || got.Status != bookingcore.WaitlistNotified || got.NotifiedAt == nil || got.ExpiresAt == nil {
		t.Fatalf("GetWaitlistEntry après notif = (%+v, %v)", got, err)
	}
	// expiration : on force expires_at dans le passé puis on liste
	if _, err := db.ExecContext(ctx, `UPDATE booking_waitlist SET expires_at = now() - interval '1 minute' WHERE id = $1`, entry.ID); err != nil {
		t.Fatalf("force expires_at: %v", err)
	}
	expiredWl, err := repo.ListExpiredNotifiedWaitlist(ctx)
	if err != nil {
		t.Fatalf("ListExpiredNotifiedWaitlist: %v", err)
	}
	foundWl := false
	for _, e := range expiredWl {
		if e.ID == entry.ID {
			foundWl = true
		}
	}
	if !foundWl {
		t.Fatalf("entrée notifiée expirée absente (%d)", len(expiredWl))
	}
	if err := repo.SetWaitlistStatus(ctx, merchantID, entry.ID, bookingcore.WaitlistExpired); err != nil {
		t.Fatalf("SetWaitlistStatus: %v", err)
	}
	if err := repo.DeleteWaitlistEntry(ctx, merchantID, entry.ID); err != nil {
		t.Fatalf("DeleteWaitlistEntry: %v", err)
	}

	// --- divers ---
	if email, err := repo.GetCustomerEmail(ctx, merchantID, *custID); err != nil || email != "" {
		t.Fatalf("GetCustomerEmail = (%q, %v)", email, err)
	}
	if name, err := repo.GetMerchantBusinessName(ctx, merchantID); err != nil || name != "ITest Bkg Resto" {
		t.Fatalf("GetMerchantBusinessName = (%q, %v)", name, err)
	}
	if cap, err := repo.GetMaxBookingCapacity(ctx, merchantID); err != nil || cap != 10 {
		t.Fatalf("GetMaxBookingCapacity = (%d, %v)", cap, err)
	}
}
