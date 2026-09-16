package kiosk

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

var getKioskByIDForMerchantQuery = regexp.QuoteMeta(`
	SELECT id, merchant_id, name, location_id, status, app_version, hardware_model, admin_pin_encrypted, os_version,
	       last_heartbeat_at, last_ip, last_error, last_error_at, enabled, created_at, updated_at
	FROM kiosks
	WHERE merchant_id = ? AND id = ?`)

// TestRevokeKiosk_ClearsPairedReader — la ligne kiosks révoquée n'est jamais
// recréée : sans effacer stripe_reader_id ici, l'index unique partiel
// uq_kiosks_stripe_reader_id (migration 146) bloquerait définitivement le
// ré-appairage de ce reader physique à un autre kiosk — voir
// docs/KIOSK_DECISIONS.md.
func TestRevokeKiosk_ClearsPairedReader(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	mock.ExpectQuery(getKioskByIDForMerchantQuery).
		WithArgs("merch-1", "kiosk-9").
		WillReturnRows(sqlmock.NewRows(candidateColumns()).AddRow(candidateRow("kiosk-9", "active", nil, nil)...))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosk_device_tokens SET revoked_at =`)).
		WithArgs("kiosk-9").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosks SET stripe_reader_id = NULL, stripe_reader_label = NULL, stripe_reader_serial = NULL WHERE id =`)).
		WithArgs("kiosk-9").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosks SET status = ? WHERE id = ?`)).
		WithArgs("revoked", "kiosk-9").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := svc.RevokeKiosk(context.Background(), "merch-1", "kiosk-9"); err != nil {
		t.Fatalf("RevokeKiosk: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (ClearKioskReader must run inside the revoke transaction): %v", err)
	}
}
