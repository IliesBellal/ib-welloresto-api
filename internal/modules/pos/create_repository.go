package pos

import (
	"context"
	"strconv"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

// InsertMerchant inserts a row into the merchant table and returns the auto-incremented ID.
func (r *POSRepository) InsertMerchant(ctx context.Context, req CreateMerchantRequest, token string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	country := req.Country
	if country == "" {
		country = "France"
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO merchant
			(fullName, address, street_number, street, zip_code, city, country,
			 SIRET, merchantTel, web_site, email, token)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.FullName, req.Address, req.StreetNumber, req.Street, req.ZipCode,
		req.City, country, req.SIRET, req.Tel, req.WebSite, req.Email, token,
	)
	if err != nil {
		log.Error("InsertMerchant: failed to insert merchant: " + err.Error())
		return "", err
	}

	id, err := res.LastInsertId()
	return strconv.FormatInt(id, 10), err
}

// InitMerchantSatellites creates the companion rows expected for every new merchant.
func (r *POSRepository) InitMerchantSatellites(ctx context.Context, merchantID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 2 QR codes — one for standard menu, one for menu-only (mywelloresto flag)
	for _, row := range []struct{ menuOnly, mywelloresto int }{{0, 0}, {1, 1}} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO qrcodes (merchant_id, code, menu_only, mywelloresto_flag) VALUES (?, UUID(), ?, ?)`,
			merchantID, row.menuOnly, row.mywelloresto,
		); err != nil {
			log.Error("InitMerchantSatellites: failed to insert QR code: " + err.Error())
			return err
		}
	}

	// scannorder_settings (PK = merchant_id)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scannorder_settings (merchant_id) VALUES (?)`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert scan order settings: " + err.Error())
		return err
	}

	// merchant_parameters (PK = merchant_id)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO merchant_parameters (merchant_id) VALUES (?)`, merchantID,
	); err != nil {
		log.Error("InitMerchantSatellites: failed to insert merchant parameters: " + err.Error())
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

// InsertUserRights inserts a row into users_rights and returns the auto-incremented ID.
func (r *POSRepository) InsertUserRights(ctx context.Context, userID, merchantID string, admin bool, token string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	adminVal := 0
	if admin {
		adminVal = 1
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO users_rights
			(user_id, merchant_id, token, admin, enabled)
		VALUES (?, ?, ?, ?, 1)`,
		userID, merchantID, token, adminVal,
	)
	if err != nil {
		log.Error("InsertUserRights: failed to insert user rights: " + err.Error())
		return 0, err
	}

	id, err := res.LastInsertId()
	return int(id), err
}
