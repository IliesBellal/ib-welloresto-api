//go:build postgres_integration

package notification

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestNotificationRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-notif-m1"
	const deviceIDRecent = "itest-device-recent"
	const deviceIDStale = "itest-device-stale"
	const accessToken = "itest-access-token"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_devices WHERE device_id IN ($1, $2)`, deviceIDRecent, deviceIDStale)
		_, _ = db.ExecContext(ctx, `DELETE FROM firebase_fcm_access_token WHERE access_token = $1`, accessToken)
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_devices (user_id, merchant_id, app, device_id, fcm_token, last_used)
		VALUES ('u1', $1, 'pos', $2, 'fcm-recent', $3)`,
		merchantID, deviceIDRecent, now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("seed recent device: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_devices (user_id, merchant_id, app, device_id, fcm_token, last_used)
		VALUES ('u2', $1, 'pos', $2, 'fcm-stale', $3)`,
		merchantID, deviceIDStale, now.Add(-72*time.Hour)); err != nil {
		t.Fatalf("seed stale device: %v", err)
	}

	repo := NewNotificationRepository(db)

	// GetDeviceTokens: last_used >= now() - interval '2 days' excludes the stale device.
	tokens, err := repo.GetDeviceTokens(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetDeviceTokens failed against postgres: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "fcm-recent" {
		t.Fatalf("expected only the recent token, got %v", tokens)
	}

	if err := repo.DeleteDeviceToken(ctx, "fcm-recent"); err != nil {
		t.Fatalf("DeleteDeviceToken failed against postgres: %v", err)
	}
	tokens, err = repo.GetDeviceTokens(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetDeviceTokens (after delete) failed: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("expected no tokens after delete, got %v", tokens)
	}

	// No valid FCM token yet.
	tok, err := repo.GetValidFCMTokenOld(ctx)
	if err != nil {
		t.Fatalf("GetValidFCMTokenOld (empty) failed against postgres: %v", err)
	}
	if tok != "" {
		t.Fatalf("expected no valid token yet, got %q", tok)
	}

	// StoreFCMToken: now() + interval '50 minutes' expiry.
	if err := repo.StoreFCMToken(ctx, accessToken); err != nil {
		t.Fatalf("StoreFCMToken failed against postgres: %v", err)
	}

	tok, err = repo.GetValidFCMTokenOld(ctx)
	if err != nil {
		t.Fatalf("GetValidFCMTokenOld failed against postgres: %v", err)
	}
	if tok != accessToken {
		t.Fatalf("expected %q, got %q", accessToken, tok)
	}

	tok, expiry, err := repo.GetValidFCMToken(ctx)
	if err != nil {
		t.Fatalf("GetValidFCMToken failed against postgres: %v", err)
	}
	if tok != accessToken {
		t.Fatalf("expected %q, got %q", accessToken, tok)
	}
	if expiry.Before(now.Add(45*time.Minute)) || expiry.After(now.Add(55*time.Minute)) {
		t.Fatalf("expiry %v not within expected +50min window of %v", expiry, now)
	}

	if err := repo.DeleteAccessToken(ctx, accessToken); err != nil {
		t.Fatalf("DeleteAccessToken failed against postgres: %v", err)
	}
	tok, err = repo.GetValidFCMTokenOld(ctx)
	if err != nil {
		t.Fatalf("GetValidFCMTokenOld (after delete) failed: %v", err)
	}
	if tok != "" {
		t.Fatalf("expected no valid token after delete, got %q", tok)
	}
}
