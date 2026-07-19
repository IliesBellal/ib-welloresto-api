// notification/notification_repository.go

package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
)

type NotificationRepository struct {
	database *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{database: db}
}

func (r *NotificationRepository) GetDeviceTokens(ctx context.Context, merchantID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)

	cutoff := "DATE_SUB(UTC_TIMESTAMP(), INTERVAL 2 DAY)"
	if dbx.ActiveDialect() == dbx.Postgres {
		cutoff = "now() - interval '2 days'"
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
        SELECT fcm_token
        FROM users_devices ud
        WHERE ud.merchant_id = ?
		AND ud.last_used >= %s
    `, cutoff), merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []string

	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}

	return tokens, nil
}

// DeleteDeviceToken : Supprime un token FCM invalide de la base de données
func (r *NotificationRepository) DeleteDeviceToken(ctx context.Context, token string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		DELETE FROM users_devices 
		WHERE fcm_token = ?
	`, token)

	return err
}

// DeleteAccessToken : Supprime le jeton d'accès FCM actuel (OAuth2) car il est rejeté par Google
func (r *NotificationRepository) DeleteAccessToken(ctx context.Context, token string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		DELETE FROM firebase_fcm_access_token 
		WHERE access_token = ?
	`, token)
	return err
}

func (r *NotificationRepository) GetValidFCMTokenOld(ctx context.Context) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var token string

	err := db.QueryRowContext(ctx, fmt.Sprintf(`
        SELECT access_token
        FROM firebase_fcm_access_token
        WHERE %s <= expiration_date
        LIMIT 1
    `, dbx.UTCNow())).Scan(&token)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	return token, err
}

func (r *NotificationRepository) GetValidFCMToken(ctx context.Context) (string, time.Time, error) {
	db := dbx.GetDB(ctx, r.database)
	var token string
	var expiration time.Time

	err := db.QueryRowContext(ctx, fmt.Sprintf(`
        SELECT access_token, expiration_date
        FROM firebase_fcm_access_token
        WHERE %s <= expiration_date
        ORDER BY expiration_date DESC
        LIMIT 1
    `, dbx.UTCNow())).Scan(&token, &expiration)

	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}

	return token, expiration, err
}

// Optionnel : On peut aussi uniformiser StoreFCMToken pour qu'il soit explicite
func (r *NotificationRepository) StoreFCMToken(ctx context.Context, token string) error {
	db := dbx.GetDB(ctx, r.database)

	expiresAt := "DATE_ADD(UTC_TIMESTAMP(), INTERVAL 50 MINUTE)"
	if dbx.ActiveDialect() == dbx.Postgres {
		expiresAt = "now() + interval '50 minutes'"
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
        INSERT INTO firebase_fcm_access_token(access_token, expiration_date)
        VALUES(?, %s)
    `, expiresAt), token)
	return err
}
