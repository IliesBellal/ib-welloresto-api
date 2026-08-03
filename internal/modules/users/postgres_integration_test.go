//go:build postgres_integration

package users

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func boolPtr(b bool) *bool { return &b }
func strPtr(s string) *string { return &s }
func f64Ptr(f float64) *float64 { return &f }

func TestUsersRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const userID = "itest-users-user-1"
	const linkableUserID = "itest-users-user-2"
	var merchantIntID int64
	var sessionID int64
	var orderIntID int64
	var customerIntID int64

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM delivery_position WHERE user_id IN ($1, $2)`, userID, linkableUserID)
		if sessionID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM delivery_session_order WHERE delivery_session_id = $1`, sessionID)
			_, _ = db.ExecContext(ctx, `DELETE FROM delivery_session WHERE id = $1`, sessionID)
		}
		if orderIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE order_id = $1`, orderIntID)
		}
		if customerIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE customer_id = $1`, customerIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id IN ($1, $2)`, userID, linkableUserID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id IN ($1, $2)`, userID, linkableUserID)
		if merchantIntID != 0 {
			merchantID := strconv.FormatInt(merchantIntID, 10)
			_, _ = db.ExecContext(ctx, `DELETE FROM employees WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM components WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng, country)
		VALUES ('ITest Users Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-users', 'https://example.com', '0600000000', 'mtok-users', 'Europe/Paris', 1.0, 2.0, 'FR')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open)
		VALUES ($1, now(), 'EUR', true)`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, activated)
		VALUES ($1, 't', 'd', 'k', 'french', true)`, merchantID); err != nil {
		t.Fatalf("seed scannorder_settings: %v", err)
	}
	// GetUserByToken selects p.* columns without COALESCE — a package/subscription
	// must exist for the scan into plain Go bools to succeed (same as auth's test).
	var packageIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO packages (package_name, stripe_price_id, allow_waiter_account, allow_delivery_account, kiosks_enabled)
		VALUES ('ITest Users Package', 'price_itest_users', true, true, true) RETURNING id`).Scan(&packageIntID); err != nil {
		t.Fatalf("seed packages: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM packages WHERE id = $1`, packageIntID) })
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (stripe_subscription_id, merchant_id, package_id, kiosks_enabled)
		VALUES ('itest-users-sub', $1, $2, true)`, merchantID, packageIntID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID) })
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, merchantID) })

	repo := NewUserRepository(db)

	// --- create_repository: CreateUser + InsertUserRights (InsertReturningID) ---
	if err := repo.CreateUser(ctx, userID, "ITest User", "ITest", "User", "itest-users@example.com", "+33611111111", "hash-1", "user-tok-users"); err != nil {
		t.Fatalf("CreateUser failed against postgres: %v", err)
	}
	rightsID, err := repo.InsertUserRights(ctx, userID, merchantID, true, "rights-tok-users")
	if err != nil {
		t.Fatalf("InsertUserRights failed against postgres: %v", err)
	}
	if rightsID <= 0 {
		t.Fatalf("expected a positive generated rights id, got %d", rightsID)
	}
	// Link the user to its rights row so GetUserByToken's INNER JOIN resolves.
	if _, err := db.ExecContext(ctx, `UPDATE users SET access_id = $1 WHERE user_id = $2`, rightsID, userID); err != nil {
		t.Fatalf("set access_id: %v", err)
	}

	// --- GetUserByToken: 7-way merchant.id CAST joins ---
	row, err := repo.GetUserByToken(ctx, "rights-tok-users")
	if err != nil {
		t.Fatalf("GetUserByToken failed against postgres: %v", err)
	}
	if row == nil || row.UserID != userID || row.MerchantID != merchantID {
		t.Fatalf("unexpected GetUserByToken row: %+v", row)
	}

	// --- SetUserLocation: lat/lng formatted to text, heading rounded, UTCNow ---
	if err := repo.SetUserLocation(ctx, models.UpdateLocationRequest{
		UserID: userID, Lat: 48.8566, Lng: 2.3522, Heading: f64Ptr(90.6),
	}); err != nil {
		t.Fatalf("SetUserLocation failed against postgres: %v", err)
	}
	var lat, lng string
	var heading int
	if err := db.QueryRowContext(ctx, `SELECT lat, lng, heading FROM users WHERE user_id = $1`, userID).Scan(&lat, &lng, &heading); err != nil {
		t.Fatalf("read back location: %v", err)
	}
	if lat != "48.8566" || lng != "2.3522" || heading != 91 {
		t.Fatalf("unexpected location: lat=%q lng=%q heading=%d", lat, lng, heading)
	}
	// nil heading resets to 0 (MySQL non-strict NULL->default behaviour preserved).
	if err := repo.SetUserLocation(ctx, models.UpdateLocationRequest{UserID: userID, Lat: 1, Lng: 2}); err != nil {
		t.Fatalf("SetUserLocation (nil heading) failed: %v", err)
	}

	// --- GetUserLocation: view + CAST joins; inherited numeric join means no match
	// for non-numeric user ids — must not error out ---
	loc, err := repo.GetUserLocation(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("GetUserLocation failed against postgres: %v", err)
	}
	if loc != nil {
		t.Fatalf("expected nil (inherited ur.id=user_id join can't match a non-numeric user id), got %+v", loc)
	}

	// --- delivery flow ---
	if err := db.QueryRowContext(ctx, `
		INSERT INTO delivery_session (user_id, merchant_id, start_date, status)
		VALUES ($1, $2, now(), 'active') RETURNING id`, userID, merchantID).Scan(&sessionID); err != nil {
		t.Fatalf("seed delivery_session: %v", err)
	}

	gotSession, currentOrder, err := repo.GetActiveDeliverySessionForUser(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("GetActiveDeliverySessionForUser failed against postgres: %v", err)
	}
	if gotSession != strconv.FormatInt(sessionID, 10) || currentOrder != "" {
		t.Fatalf("unexpected active session: id=%q order=%q", gotSession, currentOrder)
	}

	if err := repo.InsertDeliveryPosition(ctx, userID, gotSession, 48.85, 2.35, f64Ptr(10), f64Ptr(5), nil); err != nil {
		t.Fatalf("InsertDeliveryPosition failed against postgres: %v", err)
	}
	var positions int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_position WHERE delivery_session_id = $1`, sessionID).Scan(&positions); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	if positions != 1 {
		t.Fatalf("expected 1 recorded position, got %d", positions)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (customer_name, customer_lat, customer_lng, customer_temporary_lat, customer_temporary_lng, merchant_id)
		VALUES ('ITest Customer', 48.80, 2.30, 48.90, 2.40, $1) RETURNING customer_id`, merchantID).Scan(&customerIntID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, price, TVA, HT, created_by, customer_id, use_customer_temporary_address)
		VALUES ($1, 1, 'OPEN', 1000, 100, 900, 'itest', $2, false) RETURNING order_id`, merchantID, customerIntID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO delivery_session_order (delivery_session_id, order_id, priority, status)
		VALUES ($1, $2, 1, 'en_route')`, sessionID, orderIntID); err != nil {
		t.Fatalf("seed delivery_session_order: %v", err)
	}

	status, dLat, dLng, ok, err := repo.GetDeliveryStopDestination(ctx, gotSession, orderID)
	if err != nil {
		t.Fatalf("GetDeliveryStopDestination failed against postgres: %v", err)
	}
	if !ok || status != "en_route" || dLat != 48.80 || dLng != 2.30 {
		t.Fatalf("unexpected destination: status=%q lat=%v lng=%v ok=%v", status, dLat, dLng, ok)
	}
	// Temporary-address branch (temporary lat/lng scanned as strings then parsed).
	if _, err := db.ExecContext(ctx, `UPDATE orders SET use_customer_temporary_address = true WHERE order_id = $1`, orderIntID); err != nil {
		t.Fatalf("switch to temporary address: %v", err)
	}
	status, dLat, dLng, ok, err = repo.GetDeliveryStopDestination(ctx, gotSession, orderID)
	if err != nil {
		t.Fatalf("GetDeliveryStopDestination (temporary) failed: %v", err)
	}
	if !ok || dLat != 48.90 || dLng != 2.40 {
		t.Fatalf("unexpected temporary destination: status=%q lat=%v lng=%v ok=%v", status, dLat, dLng, ok)
	}

	arrived, err := repo.MarkStopArrived(ctx, gotSession, orderID)
	if err != nil {
		t.Fatalf("MarkStopArrived failed against postgres: %v", err)
	}
	if !arrived {
		t.Fatal("expected MarkStopArrived to update the en_route stop")
	}
	arrived, err = repo.MarkStopArrived(ctx, gotSession, orderID)
	if err != nil {
		t.Fatalf("MarkStopArrived (repeat) failed: %v", err)
	}
	if arrived {
		t.Fatal("expected MarkStopArrived to be a no-op the second time")
	}

	// --- profile ---
	if err := repo.UpdateUserProfile(ctx, userID, &models.UpdateUserProfileRequest{
		FirstName: strPtr("Updated"),
		Email:     strPtr("itest-users-new@example.com"),
		City:      strPtr("Lyon"),
		Lat:       f64Ptr(45.75),
	}); err != nil {
		t.Fatalf("UpdateUserProfile failed against postgres: %v", err)
	}
	profile, err := repo.GetUserProfile(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserProfile failed against postgres: %v", err)
	}
	if profile.FirstName != "Updated" || profile.Email != "itest-users-new@example.com" || profile.City != "Lyon" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if profile.EmailVerified {
		t.Fatal("expected email_verified to be reset after email change")
	}

	verif, err := repo.GetUserVerificationStatus(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserVerificationStatus failed against postgres: %v", err)
	}
	if verif.Email != "itest-users-new@example.com" || verif.EmailVerified {
		t.Fatalf("unexpected verification status: %+v", verif)
	}

	if err := repo.UpdateUserAvatar(ctx, userID, "https://cdn/itest.png"); err != nil {
		t.Fatalf("UpdateUserAvatar failed against postgres: %v", err)
	}
	avatar, err := repo.GetUserAvatarURL(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserAvatarURL failed against postgres: %v", err)
	}
	if avatar != "https://cdn/itest.png" {
		t.Fatalf("unexpected avatar: %q", avatar)
	}

	country, err := repo.GetMerchantCountryCode(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetMerchantCountryCode failed against postgres: %v", err)
	}
	if country != "FR" {
		t.Fatalf("unexpected country: %q", country)
	}

	// --- UpdatePassword (token rotation on users + users_rights) ---
	newToken, err := repo.UpdatePassword(ctx, userID, merchantID, "hash-2")
	if err != nil {
		t.Fatalf("UpdatePassword failed against postgres: %v", err)
	}
	if newToken == "" {
		t.Fatal("expected a rotated merchant token")
	}

	// --- components: boolean enabled + LIMIT param ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO components (merchant_id, name, unit_of_measure, stock, enabled, category_id)
		VALUES ($1, 'ITest Flour', 1, 0, true, 'CAT1'),
		       ($1, 'ITest Sugar', 1, -2, true, 'CAT1'),
		       ($1, 'ITest Hidden', 1, 0, false, 'CAT1'),
		       ($1, 'ITest Temp', 1, 0, true, 'UBER_EATS_TEMP'),
		       ($1, 'ITest Stocked', 1, 5, true, 'CAT1')`, merchantID); err != nil {
		t.Fatalf("seed components: %v", err)
	}
	count, names, err := repo.GetOutOfStockComponents(ctx, merchantID, 1)
	if err != nil {
		t.Fatalf("GetOutOfStockComponents failed against postgres: %v", err)
	}
	if count != 2 || len(names) != 1 || names[0] != "ITest Flour" {
		t.Fatalf("unexpected out-of-stock result: count=%d names=%v", count, names)
	}

	// --- admin repository ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO employees (id, merchant_id, user_id, first_name, last_name, position_id, contract_type_code, enabled)
		VALUES ('itest-emp-1', $1, $2, 'ITest', 'Employee', 'pos-1', 'CDI', true)`, merchantID, userID); err != nil {
		t.Fatalf("seed employees: %v", err)
	}

	items, total, err := repo.ListMerchantUsers(ctx, merchantID, MerchantUserListFilters{
		Search: "User", Active: boolPtr(true), LinkedEmployee: boolPtr(true), Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListMerchantUsers failed against postgres: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].UserID != userID || items[0].EmployeeID == nil {
		t.Fatalf("unexpected merchant users: total=%d items=%+v", total, items)
	}

	detail, err := repo.GetMerchantUserByID(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("GetMerchantUserByID failed against postgres: %v", err)
	}
	if detail.UserID != userID {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	rights, err := repo.GetMerchantUserRights(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("GetMerchantUserRights failed against postgres: %v", err)
	}
	if !rights.Admin {
		t.Fatalf("expected admin rights, got %+v", rights)
	}

	token, err := repo.GetUsersRightsToken(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("GetUsersRightsToken failed against postgres: %v", err)
	}
	if token != newToken {
		t.Fatalf("expected rotated token %q, got %q", newToken, token)
	}

	exists, err := repo.UserExists(ctx, userID)
	if err != nil || !exists {
		t.Fatalf("UserExists = (%v, %v), want (true, nil)", exists, err)
	}
	linked, err := repo.MerchantUserLinkExists(ctx, merchantID, userID)
	if err != nil || !linked {
		t.Fatalf("MerchantUserLinkExists = (%v, %v), want (true, nil)", linked, err)
	}

	// Upsert on an existing link updates it in place.
	upsertID, err := repo.UpsertMerchantUserRights(ctx, userID, merchantID, "rights-tok-upserted", MerchantUserRightsUpsertRequest{
		Admin: false, Permissions: MerchantUserPermissions{ManageMenu: true},
	})
	if err != nil {
		t.Fatalf("UpsertMerchantUserRights (update branch) failed: %v", err)
	}
	if upsertID != int64(rightsID) {
		t.Fatalf("expected update of existing rights row %d, got %d", rightsID, upsertID)
	}

	// Second linkable user exercises the insert branch of the upsert.
	if err := repo.CreateUser(ctx, linkableUserID, "ITest Linkable", "Linkme", "User", "itest-linkable@example.com", "+33622222222", "hash-3", "user-tok-linkable"); err != nil {
		t.Fatalf("CreateUser (linkable) failed: %v", err)
	}
	linkables, totalLinkable, err := repo.SearchLinkableUsers(ctx, merchantID, LinkableUserSearchFilters{Search: "Linkme", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchLinkableUsers failed against postgres: %v", err)
	}
	if totalLinkable != 1 || len(linkables) != 1 || linkables[0].UserID != linkableUserID {
		t.Fatalf("unexpected linkable users: total=%d items=%+v", totalLinkable, linkables)
	}
	insertedID, err := repo.UpsertMerchantUserRights(ctx, linkableUserID, merchantID, "rights-tok-2", MerchantUserRightsUpsertRequest{Admin: false})
	if err != nil {
		t.Fatalf("UpsertMerchantUserRights (insert branch) failed: %v", err)
	}
	if insertedID <= 0 {
		t.Fatalf("expected a generated rights id, got %d", insertedID)
	}

	if err := repo.UpdateMerchantUserRights(ctx, merchantID, userID, MerchantUserRightsUpsertRequest{
		Admin: true, LoginEnabled: true, Permissions: MerchantUserPermissions{ManageUsers: true},
	}); err != nil {
		t.Fatalf("UpdateMerchantUserRights failed against postgres: %v", err)
	}
	rights, err = repo.GetMerchantUserRights(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("GetMerchantUserRights (after update) failed: %v", err)
	}
	if !rights.Admin || !rights.Permissions.ManageUsers {
		t.Fatalf("rights not updated: %+v", rights)
	}

	cleared, err := repo.ClearMerchantEmployeeLinks(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("ClearMerchantEmployeeLinks failed against postgres: %v", err)
	}
	if cleared != 1 {
		t.Fatalf("expected 1 cleared employee link, got %d", cleared)
	}

	okDisable, err := repo.DisableMerchantUserLink(ctx, merchantID, userID)
	if err != nil || !okDisable {
		t.Fatalf("DisableMerchantUserLink = (%v, %v), want (true, nil)", okDisable, err)
	}
	linked, err = repo.MerchantUserLinkExists(ctx, merchantID, userID)
	if err != nil || linked {
		t.Fatalf("expected link disabled, got linked=%v err=%v", linked, err)
	}
}

// TestRotateRightsTokensExcept_Postgres proves the multi-merchant session fix:
// a password change must not leave the user logged in on another merchant.
// See docs/PASSWORD_RESET.md (decision D10).
func TestRotateRightsTokensExcept_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const userID = "itest-rotate-user-1"
	const tokenA = "itest-rotate-token-merchant-a"
	const tokenB = "itest-rotate-token-merchant-b"
	const tokenC = "itest-rotate-token-merchant-c"
	var merchantA, merchantB, merchantC int64

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		for _, id := range []int64{merchantA, merchantB, merchantC} {
			if id != 0 {
				_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, id)
			}
		}
	})

	seedMerchant := func(name, siret string) int64 {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
			VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $2, 'Europe/Paris', 1.0, 2.0)
			RETURNING id`, name, siret).Scan(&id); err != nil {
			t.Fatalf("seed merchant %s: %v", name, err)
		}
		return id
	}

	merchantA = seedMerchant("ITest Rotate A", "siret-rotate-a")
	merchantB = seedMerchant("ITest Rotate B", "siret-rotate-b")
	merchantC = seedMerchant("ITest Rotate C", "siret-rotate-c")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
		VALUES ($1, 'ITest Rotate', 'ITest', 'Rotate', 'hash', 'itest-rotate@example.com', '+33600000000', 'user-tok-rotate', true)`,
		userID); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	for _, link := range []struct {
		merchant int64
		token    string
	}{{merchantA, tokenA}, {merchantB, tokenB}, {merchantC, tokenC}} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO users_rights (user_id, merchant_id, token, enabled, login_enabled)
			VALUES ($1, $2, $3, true, true)`,
			userID, strconv.FormatInt(link.merchant, 10), link.token); err != nil {
			t.Fatalf("seed users_rights: %v", err)
		}
	}

	repo := NewUserRepository(db)
	merchantAID := strconv.FormatInt(merchantA, 10)

	oldTokens, err := repo.RotateRightsTokensExcept(ctx, userID, merchantAID)
	if err != nil {
		t.Fatalf("RotateRightsTokensExcept() error = %v", err)
	}

	// B and C are returned so the caller can evict them from Redis; A is not.
	if len(oldTokens) != 2 {
		t.Fatalf("got %d old tokens %v, want 2 (merchants B and C)", len(oldTokens), oldTokens)
	}
	returned := map[string]bool{}
	for _, tok := range oldTokens {
		returned[tok] = true
	}
	if !returned[tokenB] || !returned[tokenC] {
		t.Fatalf("old tokens = %v, want %q and %q", oldTokens, tokenB, tokenC)
	}
	if returned[tokenA] {
		t.Fatalf("the excluded merchant's token was returned — the caller's own session would be dropped")
	}

	// In the database: A untouched, B and C rotated to something new.
	readToken := func(merchant int64) string {
		var tok string
		if err := db.QueryRowContext(ctx,
			`SELECT token FROM users_rights WHERE user_id = $1 AND merchant_id = $2`,
			userID, strconv.FormatInt(merchant, 10)).Scan(&tok); err != nil {
			t.Fatalf("read token for merchant %d: %v", merchant, err)
		}
		return tok
	}

	if got := readToken(merchantA); got != tokenA {
		t.Fatalf("excluded merchant token = %q, want it unchanged (%q)", got, tokenA)
	}
	if got := readToken(merchantB); got == tokenB {
		t.Fatal("merchant B token was not rotated — that session survives the password change")
	}
	if got := readToken(merchantC); got == tokenC {
		t.Fatal("merchant C token was not rotated — that session survives the password change")
	}
	if readToken(merchantB) == readToken(merchantC) {
		t.Fatal("merchants B and C got the same token — each link must get its own")
	}

	// Empty exceptMerchantID rotates every link, including A.
	allOld, err := repo.RotateRightsTokensExcept(ctx, userID, "")
	if err != nil {
		t.Fatalf("RotateRightsTokensExcept(all) error = %v", err)
	}
	if len(allOld) != 3 {
		t.Fatalf("got %d old tokens, want 3 when no merchant is excluded", len(allOld))
	}
	if readToken(merchantA) == tokenA {
		t.Fatal("merchant A token was not rotated when no merchant was excluded")
	}
}
