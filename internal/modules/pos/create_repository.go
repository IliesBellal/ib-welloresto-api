package pos

import (
	"context"
	"database/sql"
	"strconv"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

// InsertMerchant inserts a row into the merchant table and returns the auto-incremented ID.
func (r *POSRepository) InsertMerchant(ctx context.Context, req CreateMerchantRequest, token string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	country := req.Country
	if country == "" {
		country = "France"
	}

	id, err := db.InsertReturningID(ctx, `
		INSERT INTO merchant
			(fullName, address, street_number, street, zip_code, city, country,
			 SIRET, merchantTel, web_site, email, token)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "id",
		req.FullName, req.Address, req.StreetNumber, req.Street, req.ZipCode,
		req.City, country, req.SIRET, req.Tel, req.WebSite, req.Email, token,
	)
	if err != nil {
		log.Error("InsertMerchant: failed to insert merchant: " + err.Error())
		return "", err
	}

	return strconv.FormatInt(id, 10), nil
}

// InsertSubscription creates the effective merchant subscription for the selected package.
func (r *POSRepository) InsertSubscription(ctx context.Context, merchantID, packageID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// stripe_subscription_id est NOT NULL sans défaut : MySQL non-strict
	// insérait '' silencieusement, Postgres rejette — '' explicite pour un
	// résultat identique dans les deux dialectes.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id) VALUES (?, ?, '')`,
		merchantID, packageID,
	); err != nil {
		log.Error("InsertSubscription: failed to insert subscription: " + err.Error())
		return err
	}

	return nil
}

// InitMerchantSatellites creates the companion rows expected for every new merchant.
func (r *POSRepository) InitMerchantSatellites(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	uuidExpr := "UUID()"
	if dbx.ActiveDialect() == dbx.Postgres {
		uuidExpr = "CAST(gen_random_uuid() AS TEXT)"
	}

	// 2 QR codes — one for standard menu, one for menu-only (mywelloresto flag)
	for _, row := range []struct{ menuOnly, mywelloresto bool }{{false, false}, {true, true}} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO qrcodes (merchant_id, code, menu_only, mywelloresto_flag) VALUES (?, `+uuidExpr+`, ?, ?)`,
			merchantID, row.menuOnly, row.mywelloresto,
		); err != nil {
			log.Error("InitMerchantSatellites: failed to insert QR code: " + err.Error())
			return err
		}
	}

	// scannorder_settings (PK = merchant_id) — seo_* sont NOT NULL sans défaut
	// (MySQL non-strict insérait '') : '' explicite, même précédent que
	// subscriptions ci-dessus.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type)
		 VALUES (?, '', '', '', '')`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert scan order settings: " + err.Error())
		return err
	}

	// merchant_parameters (PK = merchant_id) — last_menu_update est NOT NULL
	// sans défaut (MySQL non-strict insérait le zéro-date) : horodatage
	// explicite pour la validité cross-dialecte.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES (?, `+dbx.UTCNow()+`)`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert merchant parameters: " + err.Error())
		return err
	}

	// merchant_marketing_settings (PK = merchant_id)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO merchant_marketing_settings (merchant_id) VALUES (?)`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert merchant marketing settings: " + err.Error())
		return err
	}

	// haccp_settings (bootstrapped with defaults by merchant_id)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO haccp_settings (merchant_id, created_at, updated_at) VALUES (?, `+dbx.UTCNow()+`, `+dbx.UTCNow()+`)`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert haccp settings: " + err.Error())
		return err
	}

	// bookings_settings (relies on DB defaults for the rest of the
	// configuration) — code est NOT NULL sans défaut : '' explicite, même
	// précédent que subscriptions ci-dessus.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO bookings_settings (merchant_id, code) VALUES (?, '')`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert bookings settings: " + err.Error())
		return err
	}

	// default cash desk
	if _, err := db.ExecContext(ctx,
		`INSERT INTO cash_desks (merchant_id, name) VALUES (?, 'Caisse principale')`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert cash desk: " + err.Error())
		return err
	}

	return nil
}

// SetDefaultRoleID points merchant.default_role_id at roleID, but only if it
// is still unset — never overwrites a default a merchant may already have
// (manually repointed to a custom role, for instance).
//
// merchant.id is an integer PK while merchantID/roleID circulate as strings
// everywhere in application code; the CAST mirrors authMerchantJoinCast in
// internal/modules/auth/repository.go, used for the same join elsewhere.
func (r *POSRepository) SetDefaultRoleID(ctx context.Context, merchantID, roleID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	castExpr := "CAST(id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		castExpr = "CAST(id AS TEXT)"
	}

	if _, err := db.ExecContext(ctx,
		"UPDATE merchant SET default_role_id = ? WHERE "+castExpr+" = ? AND default_role_id IS NULL",
		roleID, merchantID,
	); err != nil {
		log.Error("SetDefaultRoleID: failed to set merchant default_role_id: " + err.Error())
		return err
	}

	return nil
}

// MerchantDefaultRoleID returns merchant.default_role_id for merchantID, or
// models.ErrMerchantDefaultRoleNotSet if the establishment has no default
// role configured yet (RBAC lot 4: linking a user must fail explicitly
// instead of inserting a users_rights row with no role_id — see
// migrations/done/099_merchant_default_role_admin.up.sql). Returns
// sql.ErrNoRows if merchantID does not match any merchant.
func (r *POSRepository) MerchantDefaultRoleID(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	castExpr := "CAST(id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		castExpr = "CAST(id AS TEXT)"
	}

	var roleID sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT default_role_id FROM merchant WHERE "+castExpr+" = ?", merchantID,
	).Scan(&roleID); err != nil {
		return "", err
	}
	if !roleID.Valid {
		return "", models.ErrMerchantDefaultRoleNotSet
	}
	return roleID.String, nil
}

// InsertUserRights inserts a row into users_rights and returns the auto-incremented ID.
func (r *POSRepository) InsertUserRights(ctx context.Context, userID, merchantID string, admin bool, token string, roleID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	id, err := db.InsertReturningID(ctx, `
		INSERT INTO users_rights
			(user_id, merchant_id, token, admin, role_id, enabled)
		VALUES (?, ?, ?, ?, ?, TRUE)`, "id",
		userID, merchantID, token, admin, roleID,
	)
	if err != nil {
		log.Error("InsertUserRights: failed to insert user rights: " + err.Error())
		return 0, err
	}

	return int(id), nil
}
