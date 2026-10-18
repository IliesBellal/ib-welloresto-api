package auth

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("1.7.6", 20); got != "1.7.6" {
		t.Errorf("short string altered: %q", got)
	}
	if got := truncateRunes("ééééé", 3); got != "ééé" {
		t.Errorf("truncation must count runes, got %q", got)
	}
}

func TestUpdateDeviceAppVersion_KeepsVersionCodeWhenAbsent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	// versionCode nil -> COALESCE(?, version_code) garde la valeur connue.
	mock.ExpectExec(`UPDATE users_devices\s+SET app_version = .*COALESCE\(.*version_code\).*WHERE device_id =`).
		WithArgs("1.7.6", nil, "dev-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateDeviceAppVersion(context.Background(), "dev-1", "1.7.6", nil); err != nil {
		t.Fatalf("UpdateDeviceAppVersion: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListDeviceVersions_ScopedToMerchantAndNullableVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM users_devices ud\s+LEFT JOIN users u .*WHERE ud.merchant_id =`).
		WithArgs("m-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"device_id", "app", "user_id", "name", "app_version", "version_code", "app_version_at", "last_used",
		}).
			AddRow("dev-1", "pos", "u-1", "Alice", "1.7.6", 135, now, now).
			AddRow("dev-2", "pos", "u-2", "", nil, nil, nil, now))

	got, err := repo.ListDeviceVersions(context.Background(), "m-1")
	if err != nil {
		t.Fatalf("ListDeviceVersions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 devices, got %d", len(got))
	}
	if got[0].AppVersion == nil || *got[0].AppVersion != "1.7.6" || got[0].VersionCode == nil || *got[0].VersionCode != 135 {
		t.Errorf("device 1 version not read: %+v", got[0])
	}
	if got[1].AppVersion != nil || got[1].VersionCode != nil || got[1].AppVersionAt != nil {
		t.Errorf("device 2 (old app) must expose null version fields: %+v", got[1])
	}
}
