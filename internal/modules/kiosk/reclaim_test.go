package kiosk

import (
	"context"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// TestMain configure une clé de chiffrement AES-256 valide pour tout le
// binaire de test de ce package — requise par helpers.Encrypt/Decrypt
// (verifyAdminPinCore), mise en cache une seule fois (sync.Once) au premier
// appel : elle doit donc être positionnée avant tout test, pas dans un test
// individuel.
func TestMain(m *testing.M) {
	key := make([]byte, 32)
	os.Setenv(helpers.EncryptionKeyEnvVar, base64.StdEncoding.EncodeToString(key))
	os.Exit(m.Run())
}

func newReclaimTestService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	repo := NewRepository(db)
	svc := NewService(
		Config{
			EnrollmentCodeTTLMinutes:  15,
			DeviceRefreshTokenTTLDays: 30,
			AccessTokenTTLMinutes:     15,
			Pepper:                    "test-pepper",
		},
		repo, db, nil, nil, nil, nil, nil, nil, nil,
	)
	return svc, mock
}

var findCandidatesQuery = regexp.QuoteMeta(`
	SELECT id, merchant_id, name, location_id, status, app_version, hardware_model, admin_pin_encrypted, os_version,
	       last_heartbeat_at, last_ip, last_error, last_error_at, enabled, created_at, updated_at
	FROM kiosks
	WHERE device_id = ? AND status IN ('active', 'inactive')`)

func candidateColumns() []string {
	return []string{
		"id", "merchant_id", "name", "location_id", "status", "app_version", "hardware_model", "admin_pin_encrypted", "os_version",
		"last_heartbeat_at", "last_ip", "last_error", "last_error_at", "enabled", "created_at", "updated_at",
	}
}

func candidateRow(kioskID, status string, adminPinEncrypted []byte, lastHeartbeatAt *time.Time) []driver.Value {
	return []driver.Value{
		kioskID, "merch-1", "Borne 1", nil, status, nil, nil, adminPinEncrypted, nil,
		lastHeartbeatAt, nil, nil, nil, true, time.Now().UTC(), nil,
	}
}

func TestReclaimDevice_EmptyDeviceID_ReturnsInvalidInput(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	_, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "   "}, "1.2.3.4")
	if !errors.Is(err, models.ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expected no DB call, got unmet/unexpected expectations: %v", err)
	}
}

func TestReclaimDevice_NoCandidates_ReturnsNotFound(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-unknown").
		WillReturnRows(sqlmock.NewRows(candidateColumns()))

	_, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-unknown"}, "1.2.3.4")
	if !errors.Is(err, models.ErrKioskNotFound) {
		t.Fatalf("want ErrKioskNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReclaimDevice_CollidingCandidates_ReturnsNotFound(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	rows := sqlmock.NewRows(candidateColumns()).
		AddRow(candidateRow("kiosk-a", "active", nil, nil)...).
		AddRow(candidateRow("kiosk-b", "active", nil, nil)...)
	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-shared").
		WillReturnRows(rows)

	_, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-shared"}, "1.2.3.4")
	if !errors.Is(err, models.ErrKioskNotFound) {
		t.Fatalf("collision must map to ErrKioskNotFound (never a distinct response), got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReclaimDevice_RevokedCandidateNeverEligible(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	// La requête elle-même exclut 'revoked' — ce test documente qu'aucune ligne
	// n'est retournée pour un device_id qui n'existe que sur une borne révoquée
	// (le WHERE ne filtre jamais ce cas dans le mock, donc on simule directement
	// le résultat attendu de la requête réelle : 0 ligne).
	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-revoked-only").
		WillReturnRows(sqlmock.NewRows(candidateColumns()))

	_, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-revoked-only"}, "1.2.3.4")
	if !errors.Is(err, models.ErrKioskNotFound) {
		t.Fatalf("want ErrKioskNotFound, got %v", err)
	}
}

func TestReclaimDevice_RecentHeartbeat_SilentReissueIgnoresPin(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	recent := time.Now().UTC().Add(-24 * time.Hour)
	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-recent").
		WillReturnRows(sqlmock.NewRows(candidateColumns()).AddRow(candidateRow("kiosk-1", "active", nil, &recent)...))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosk_device_tokens SET revoked_at =`)).
		WithArgs("kiosk-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO kiosk_device_tokens`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosks SET last_heartbeat_at =`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// AdminPin volontairement absent : un candidat éligible au silencieux ne
	// doit jamais exiger ni vérifier de PIN, même si aucun n'est configuré
	// (adminPinEncrypted nil ici) — voir docs/KIOSK_DECISIONS.md.
	resp, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-recent"}, "9.9.9.9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.KioskID != "kiosk-1" {
		t.Fatalf("KioskID = %q, want kiosk-1", resp.KioskID)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" || resp.ExpiresAt == "" {
		t.Fatalf("expected non-empty tokens/expiry, got %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReclaimDevice_StaleHeartbeat_NoPin_ReturnsPinRequired(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	stale := time.Now().UTC().Add(-31 * 24 * time.Hour)
	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-stale").
		WillReturnRows(sqlmock.NewRows(candidateColumns()).AddRow(candidateRow("kiosk-2", "inactive", []byte{0x01, 0x02}, &stale)...))

	_, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-stale"}, "9.9.9.9")
	if !errors.Is(err, models.ErrKioskReclaimPinRequired) {
		t.Fatalf("want ErrKioskReclaimPinRequired, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReclaimDevice_NilHeartbeat_NoPin_ReturnsPinRequired(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-never-seen").
		WillReturnRows(sqlmock.NewRows(candidateColumns()).AddRow(candidateRow("kiosk-3", "active", []byte{0x01}, nil)...))

	_, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-never-seen"}, "9.9.9.9")
	if !errors.Is(err, models.ErrKioskReclaimPinRequired) {
		t.Fatalf("never-seen kiosk (NULL last_heartbeat_at) must require PIN, got %v", err)
	}
}

func TestReclaimDevice_StaleHeartbeat_WrongPin_ReturnsAdminPinInvalid(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	encrypted, err := helpers.Encrypt("4321")
	if err != nil {
		t.Fatalf("helpers.Encrypt: %v", err)
	}
	stale := time.Now().UTC().Add(-40 * 24 * time.Hour)
	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-wrongpin").
		WillReturnRows(sqlmock.NewRows(candidateColumns()).AddRow(candidateRow("kiosk-4", "active", encrypted, &stale)...))

	_, err = svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-wrongpin", AdminPin: "0000"}, "9.9.9.9")
	if !errors.Is(err, models.ErrKioskAdminPinInvalid) {
		t.Fatalf("want ErrKioskAdminPinInvalid, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReclaimDevice_StaleHeartbeat_CorrectPin_ReissuesTokens(t *testing.T) {
	svc, mock := newReclaimTestService(t)

	encrypted, err := helpers.Encrypt("4321")
	if err != nil {
		t.Fatalf("helpers.Encrypt: %v", err)
	}
	stale := time.Now().UTC().Add(-40 * 24 * time.Hour)
	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-rightpin").
		WillReturnRows(sqlmock.NewRows(candidateColumns()).AddRow(candidateRow("kiosk-5", "active", encrypted, &stale)...))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosk_device_tokens SET revoked_at =`)).
		WithArgs("kiosk-5").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO kiosk_device_tokens`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosks SET last_heartbeat_at =`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	resp, err := svc.ReclaimDevice(context.Background(), ReclaimDeviceRequest{DeviceID: "dev-rightpin", AdminPin: "4321"}, "9.9.9.9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.KioskID != "kiosk-5" {
		t.Fatalf("KioskID = %q, want kiosk-5", resp.KioskID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFindKioskCandidatesByDeviceID_MapsRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewRepository(db)

	heartbeat := time.Now().UTC()
	mock.ExpectQuery(findCandidatesQuery).
		WithArgs("dev-x").
		WillReturnRows(sqlmock.NewRows(candidateColumns()).AddRow(candidateRow("kiosk-9", "active", []byte{0x9}, &heartbeat)...))

	rows, err := repo.FindKioskCandidatesByDeviceID(context.Background(), "dev-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "kiosk-9" || rows[0].Status != "active" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateKioskLastSeenOnReclaim_DoesNotTouchAppVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE kiosks SET last_heartbeat_at =`)).
		WithArgs("10.0.0.9", "kiosk-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateKioskLastSeenOnReclaim(context.Background(), "kiosk-1", "10.0.0.9"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
