//go:build postgres_integration

package auth

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// A merchant created through POST /pos/create without a resolved Google place
// has lat/lng stored as NULL (InsertMerchant maps 0 to NULL) and never sets
// timezone. Login by rights token and the merchant switcher's list used to
// scan those columns into plain float64/string: login failed with "converting
// NULL to float64 is unsupported", and GetMerchants swallowed the same error
// and returned a half-filled row with an empty token — so the establishment
// was created but could not be switched to.
func TestAuthRepository_Postgres_MerchantWithoutCoordinates(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const userID = "itest-nocoords-user"
	const rightsToken = "itest-nocoords-rights-token"

	var merchantIntID int64
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if merchantIntID != 0 {
			merchantID := strconv.FormatInt(merchantIntID, 10)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	})

	// Same column set as InsertMerchant, lat/lng/timezone deliberately left out.
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, country, siret, merchanttel, web_site, email, token)
		VALUES ('ITest No Coords', '', '', '', '', '', 'France', '12345678900012', '0600000000', '', '', 'itest-nocoords-mtok')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open)
		VALUES ($1, $2, 'EUR', true)`, merchantID, time.Now().UTC()); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, tel, token, enabled)
		VALUES ($1, 'ITest NoCoords', 'ITest', 'NoCoords', 'x', 'itest-nocoords@example.com', '+33600000000', 'user-tok-nc', true)`, userID); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, enabled, login_enabled)
		VALUES ($1, $2, $3, true, true)`, userID, merchantID, rightsToken); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}

	repo := NewAuthRepository(db)

	got, err := repo.Login(ctx, "", "", rightsToken)
	if err != nil {
		t.Fatalf("Login by token must work for a merchant without coordinates: %v", err)
	}
	if got.MerchantLat != 0 || got.MerchantLng != 0 {
		t.Fatalf("expected zero coordinates for NULL lat/lng, got (%v, %v)", got.MerchantLat, got.MerchantLng)
	}

	byToken, err := repo.GetUserByToken(ctx, rightsToken)
	if err != nil {
		t.Fatalf("GetUserByToken must work for a merchant without coordinates: %v", err)
	}
	if byToken == nil || byToken.MerchantID != merchantID {
		t.Fatalf("unexpected GetUserByToken row: %+v", byToken)
	}

	merchants, err := repo.GetMerchants(ctx, userID)
	if err != nil {
		t.Fatalf("GetMerchants: %v", err)
	}
	if len(merchants) != 1 || merchants[0].MerchantID != merchantID || merchants[0].Token != rightsToken {
		t.Fatalf("switcher must list the merchant with its token, got %+v", merchants)
	}
}
