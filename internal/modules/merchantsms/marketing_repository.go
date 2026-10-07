package merchantsms

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"welloresto-api/internal/database/dbx"
)

// defaultSMSUnitPrice reprend le DEFAULT de merchant_marketing_settings.sms_unit_price
// (centimes), appliqué quand l'établissement n'a pas de ligne de réglages.
const defaultSMSUnitPrice = 7

type MarketingRepository interface {
	GetMarketingSettings(ctx context.Context, merchantID string) (*MarketingSettings, error)
	GetSMSUnitPrice(ctx context.Context, merchantID string) (float64, error)
	RecordSMSCost(ctx context.Context, merchantID string, count int, unitPrice float64) error
	GetOrderTrackingRef(ctx context.Context, merchantID, orderID string) (*OrderTrackingRef, error)
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
			   mms.sms_sender_name,
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
	var smsEnabled sql.NullBool
	var senderName, trackingTemplate sql.NullString

	err := row.Scan(
		&smsEnabled,
		&senderName,
		&trackingTemplate,
		&settings.QRCode,
		&settings.SMSUnitPrice,
	)

	if err != nil {
		return nil, err
	}

	settings.MerchantID = merchantID
	settings.SMSEnabled = smsEnabled.Bool
	settings.SMSSenderName = senderName.String
	settings.TrackingTemplate = trackingTemplate.String

	return &settings, nil
}

// GetSMSUnitPrice renvoie le prix unitaire (centimes) facturé à l'établissement
// pour un SMS ; le prix par défaut s'applique en l'absence de réglages.
func (r *marketingRepository) GetSMSUnitPrice(ctx context.Context, merchantID string) (float64, error) {
	db := dbx.GetDB(ctx, r.db)

	var unitPrice float64
	err := db.QueryRowContext(ctx,
		`SELECT sms_unit_price FROM merchant_marketing_settings WHERE merchant_id = ?`,
		merchantID,
	).Scan(&unitPrice)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultSMSUnitPrice, nil
	}
	if err != nil {
		return 0, err
	}
	return unitPrice, nil
}

// GetOrderTrackingRef renvoie ce qu'un SMS de suivi peut montrer au client
// d'une commande : son id public (lien) et son numéro de retrait.
func (r *marketingRepository) GetOrderTrackingRef(ctx context.Context, merchantID, orderID string) (*OrderTrackingRef, error) {
	db := dbx.GetDB(ctx, r.db)

	var publicID, orderNum sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT public_id, order_num FROM orders WHERE order_id = ? AND merchant_id = ?`,
		orderID, merchantID,
	).Scan(&publicID, &orderNum)
	if err != nil {
		return nil, err
	}
	return &OrderTrackingRef{PublicID: publicID.String, OrderNum: orderNum.String}, nil
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
