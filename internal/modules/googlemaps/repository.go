package googlemaps

import (
	"context"
	"database/sql"
	"log"
	"time"

	"welloresto-api/internal/database/dbx"
)

// GoogleMapsRepository définit l'interface pour le stockage des logs
type GoogleMapsRepository interface {
	SaveLog(userID, origin, destination string) error
	RecordGoogleMapsCall(ctx context.Context, merchantID string, count int) error
}

type logRepository struct {
	db *sql.DB
}

func NewGoogleMapsRepository(db *sql.DB) GoogleMapsRepository {
	return &logRepository{db: db}
}

func (r *logRepository) SaveLog(userID, origin, destination string) error {
	// Simulation d'une insertion en base de données
	// Ex: INSERT INTO route_logs (user_id, origin, dest, created_at) VALUES (...)
	log.Printf("[DB LOG] User: %s a demandé un trajet de '%s' vers '%s' à %s",
		userID, origin, destination, time.Now().Format(time.RFC3339))
	return nil
}

// RecordGoogleMapsCall upserts the per-merchant, per-calendar-month Google Maps call
// counter, mirroring messaggio.RecordSMSCost's merchant_sms_monthly pattern.
func (r *logRepository) RecordGoogleMapsCall(ctx context.Context, merchantID string, count int) error {
	db := dbx.GetDB(ctx, r.db)

	// Premier jour du mois courant (UTC), calculé côté Go pour rester
	// identique sur les deux dialectes (remplace DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01')).
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")

	query := `
	INSERT INTO merchant_google_maps_monthly(merchant_id, month, call_count)
	VALUES(?, ?, ?)
	ON DUPLICATE KEY UPDATE
	call_count = call_count + ?
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
	INSERT INTO merchant_google_maps_monthly(merchant_id, month, call_count)
	VALUES(?, ?, ?)
	ON CONFLICT (merchant_id, month) DO UPDATE SET
	call_count = merchant_google_maps_monthly.call_count + ?
	`
	}

	_, err := db.ExecContext(ctx, query, merchantID, month, count, count)

	return err
}
