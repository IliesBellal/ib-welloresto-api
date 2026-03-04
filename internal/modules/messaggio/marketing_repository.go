package messaggio

import (
	"context"
	"database/sql"
)

type MarketingRepository interface {
	GetMarketingSettings(ctx context.Context, merchantID int64) (*MarketingSettings, error)
	RecordSMSCost(ctx context.Context, merchantID int64, count int, unitPrice float64) error
}

type marketingRepository struct {
	db *sql.DB
}

func NewMarketingRepository(db *sql.DB) MarketingRepository {
	return &marketingRepository{db: db}
}

func (r *marketingRepository) GetMarketingSettings(ctx context.Context, merchantID int64) (*MarketingSettings, error) {

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
			AND qr.enabled = 1
			AND qr.merchant_id = mms.merchant_id
			AND qr.menu_only = 0
		WHERE mms.merchant_id = ?
	`

	row := r.db.QueryRowContext(ctx, query, merchantID)

	var settings MarketingSettings
	var smsEnabled int

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
	settings.SMSEnabled = smsEnabled == 1

	return &settings, nil
}

func (r *marketingRepository) RecordSMSCost(
	ctx context.Context,
	merchantID int64,
	count int,
	unitPrice float64,
) error {

	query := `
	INSERT INTO merchant_sms_monthly(merchant_id, month, sms_count, total_cost)
	VALUES(?, DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01'), ?, ? * ?)
	ON DUPLICATE KEY UPDATE
	sms_count = sms_count + ?,
	total_cost = total_cost + (? * ?)
	`

	_, err := r.db.ExecContext(
		ctx,
		query,
		merchantID,
		count,
		unitPrice,
		count,
		count,
		unitPrice,
		count,
	)

	return err
}
