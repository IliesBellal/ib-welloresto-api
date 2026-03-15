// notification/notification_repository.go

package notification

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go.uber.org/zap"
)

type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB, log *zap.Logger) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) GetDeviceTokens(ctx context.Context, merchantID string) ([]string, error) {

	rows, err := r.db.QueryContext(ctx, `
        SELECT fcm_token
        FROM users_devices ud
        INNER JOIN users u ON u.user_id = ud.user_id
        WHERE u.merchant_id = ?
        AND ud.last_used > DATE_ADD(UTC_TIMESTAMP(), INTERVAL -24 HOUR)
    `, merchantID)
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

func (r *NotificationRepository) GetValidFCMTokenOld(ctx context.Context) (string, error) {

	var token string

	err := r.db.QueryRowContext(ctx, `
        SELECT access_token
        FROM firebase_fcm_access_token
        WHERE UTC_TIMESTAMP() <= expiration_date
        LIMIT 1
    `).Scan(&token)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	return token, err
}

func (r *NotificationRepository) GetValidFCMToken(ctx context.Context) (string, time.Time, error) {
	var token string
	var expiration time.Time

	err := r.db.QueryRowContext(ctx, `
        SELECT access_token, expiration_date
        FROM firebase_fcm_access_token
        WHERE UTC_TIMESTAMP() <= expiration_date
        ORDER BY expiration_date DESC
        LIMIT 1
    `).Scan(&token, &expiration)

	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}

	return token, expiration, err
}

// Optionnel : On peut aussi uniformiser StoreFCMToken pour qu'il soit explicite
func (r *NotificationRepository) StoreFCMToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO firebase_fcm_access_token(access_token, expiration_date)
        VALUES(?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 50 MINUTE))
    `, token)
	return err
}
