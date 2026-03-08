package pos

import (
	"context"
	"database/sql"
)

// InsertMerchant inserts a row into the merchant table and returns the auto-incremented ID.
func (r *POSRepository) InsertMerchant(ctx context.Context, tx *sql.Tx, req CreateMerchantRequest, token string) (int, error) {
	country := req.Country
	if country == "" {
		country = "France"
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO merchant
			(fullName, address, street_number, street, zip_code, city, country,
			 SIRET, merchantTel, web_site, email, token)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.FullName, req.Address, req.StreetNumber, req.Street, req.ZipCode,
		req.City, country, req.SIRET, req.Tel, req.WebSite, req.Email, token,
	)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	return int(id), err
}

// InitMerchantSatellites creates the companion rows expected for every new merchant.
func (r *POSRepository) InitMerchantSatellites(ctx context.Context, tx *sql.Tx, merchantID int) error {
	// 2 QR codes — one for standard menu, one for menu-only (mywelloresto flag)
	for _, row := range []struct{ menuOnly, mywelloresto int }{{0, 0}, {1, 1}} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO qrcodes (merchant_id, code, menu_only, mywelloresto_flag) VALUES (?, UUID(), ?, ?)`,
			merchantID, row.menuOnly, row.mywelloresto,
		); err != nil {
			return err
		}
	}

	// scannorder_settings (PK = merchant_id)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO scannorder_settings (merchant_id) VALUES (?)`, merchantID,
	); err != nil {
		return err
	}

	// merchant_parameters (PK = merchant_id)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO merchant_parameters (merchant_id) VALUES (?)`, merchantID,
	); err != nil {
		return err
	}

	// default cash desk
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO cash_desks (merchant_id, name) VALUES (?, 'Caisse principale')`, merchantID,
	); err != nil {
		return err
	}

	return nil
}

// InsertUserRights inserts a row into users_rights and returns the auto-incremented ID.
func (r *POSRepository) InsertUserRights(ctx context.Context, tx *sql.Tx, userID string, merchantID int, admin, waiter bool, token string) (int, error) {
	adminVal := 0
	if admin {
		adminVal = 1
	}
	_ = waiter // waiter rights column not present in users_rights table schema

	res, err := tx.ExecContext(ctx, `
		INSERT INTO users_rights
			(user_id, merchant_id, token, admin, enabled)
		VALUES (?, ?, ?, ?, 1)`,
		userID, merchantID, token, adminVal,
	)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	return int(id), err
}
