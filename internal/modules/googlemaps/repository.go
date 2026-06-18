package googlemaps

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// GoogleMapsRepository définit l'interface pour le stockage des logs
type GoogleMapsRepository interface {
	SaveLog(userID, origin, destination string) error
	RecordGoogleMapsCall(ctx context.Context, merchantID int64, count int) error
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
func (r *logRepository) RecordGoogleMapsCall(ctx context.Context, merchantID int64, count int) error {
	query := `
	INSERT INTO merchant_google_maps_monthly(merchant_id, month, call_count)
	VALUES(?, DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01'), ?)
	ON DUPLICATE KEY UPDATE
	call_count = call_count + ?
	`

	_, err := r.db.ExecContext(ctx, query, merchantID, count, count)

	return err
}
