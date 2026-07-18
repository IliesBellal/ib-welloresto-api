package messaggio

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
)

type MarketingRepository interface {
	GetMarketingSettings(ctx context.Context, merchantID string) (*MarketingSettings, error)
	RecordSMSCost(ctx context.Context, merchantID string, count int, unitPrice float64) error
}

type marketingRepository struct {
	db *sql.DB
}

func NewMarketingRepository(db *sql.DB) MarketingRepository {
	return &marketingRepository{db: db}
}

func (r *marketingRepository) GetMarketingSettings(ctx context.Context, merchantID string) (*MarketingSettings, error) {
	db := dbx.GetDB(ctx, r.db)

	query := `
		SELECT mms.sms_enabled,
			   mms.messaggio_login,
			   mms.messaggio_from,
			   mms.tracking_template,
			   qr.code,
			   mms.sms_unit_price
		FROM merchant_marketing_settings mms
		INNER JOIN qrcodes qr
			ON qr.creation_date IS NULL
			AND qr.enabled = TRUE
			AND qr.merchant_id = mms.merchant_id
			AND qr.menu_only = FALSE
		WHERE mms.merchant_id = ?
	`

	row := db.QueryRowContext(ctx, query, merchantID)

	var settings MarketingSettings
	var smsEnabled bool

	err := row.Scan(
		&smsEnabled,
		&settings.MessaggioLogin,
		&settings.MessaggioFrom,
		&settings.TrackingTemplate,
		&settings.QRCode,
		&settings.SMSUnitPrice,
	)

	if err != nil {
		return nil, err
	}

	settings.MerchantID = merchantID
	settings.SMSEnabled = smsEnabled

	return &settings, nil
}

func (r *marketingRepository) RecordSMSCost(
	ctx context.Context,
	merchantID string,
	count int,
	unitPrice float64,
) error {
	db := dbx.GetDB(ctx, r.db)

	// Premier jour du mois courant (UTC), calculé côté Go pour rester
	// identique sur les deux dialectes (remplace DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01')).
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")

	// Coût calculé côté Go : `? * ?` en SQL laisse les deux paramètres sans
	// type inférable en Postgres (erreur « operator is not unique »).
	cost := unitPrice * float64(count)

	// L'upsert n'a pas de syntaxe commune : ON DUPLICATE KEY UPDATE (MySQL)
	// vs ON CONFLICT ... DO UPDATE (Postgres, sur la PK (merchant_id, month)).
	// Les paramètres sont identiques dans les deux variantes.
	query := `
	INSERT INTO merchant_sms_monthly(merchant_id, month, sms_count, total_cost)
	VALUES(?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
	sms_count = sms_count + ?,
	total_cost = total_cost + ?
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
	INSERT INTO merchant_sms_monthly(merchant_id, month, sms_count, total_cost)
	VALUES(?, ?, ?, ?)
	ON CONFLICT (merchant_id, month) DO UPDATE SET
	sms_count = merchant_sms_monthly.sms_count + ?,
	total_cost = merchant_sms_monthly.total_cost + ?
	`
	}

	_, err := db.ExecContext(
		ctx,
		query,
		merchantID,
		month,
		count,
		cost,
		count,
		cost,
	)

	return err
}
