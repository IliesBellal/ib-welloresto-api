package schedule

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	planningcommpkg "welloresto-api/internal/modules/planningcomm"
)

type stubPlanningSettingsReader struct {
	settings *settingspkg.PlanningSettings
	err      error
}

func (s stubPlanningSettingsReader) GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error) {
	return s.settings, s.err
}

type stubPlanningPublisher struct {
	messages []planningcommpkg.PublishedWeekMessage
}

func (s *stubPlanningPublisher) SendPublishedWeek(ctx context.Context, msg planningcommpkg.PublishedWeekMessage) {
	s.messages = append(s.messages, msg)
}

func TestResolveNotificationMode_Defaults(t *testing.T) {
	mode, err := resolveNotificationMode(nil, false)
	if err != nil || mode != notificationModeAll {
		t.Fatalf("resolve first publish = (%s, %v)", mode, err)
	}
	mode, err = resolveNotificationMode(nil, true)
	if err != nil || mode != notificationModeChangesOnly {
		t.Fatalf("resolve republish = (%s, %v)", mode, err)
	}
}

func TestEmployeeShiftsChanged(t *testing.T) {
	previous := []publishedShiftSnapshot{newPublishedShiftSnapshot("emp_1", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", "Service")}
	currentSame := []publishedShiftSnapshot{newPublishedShiftSnapshot("emp_1", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", "Service")}
	currentChanged := []publishedShiftSnapshot{newPublishedShiftSnapshot("emp_1", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "10:00:00", "18:00:00", "Service")}
	if employeeShiftsChanged(previous, currentSame) {
		t.Fatal("expected no diff for identical shifts")
	}
	if !employeeShiftsChanged(previous, currentChanged) {
		t.Fatal("expected diff for modified shift")
	}
}

func TestPublishPlanningWeek_FirstPublishDefaultsToAll(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	publisher := &stubPlanningPublisher{}
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil, WithSettingsReader(stubPlanningSettingsReader{settings: &settingspkg.PlanningSettings{PlanningSMSNotificationsEnabled: false}}), WithPlanningPublisher(publisher))
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		LIMIT 1
	`)).WithArgs("merchant_1", "week_1").WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
		"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, nil, now, now, nil,
	))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT s.employee_id, s.shift_date, s.start_time, s.end_time,
			COALESCE(NULLIF(TRIM(s.position), ''), COALESCE(p.label, '')) AS position_label
		FROM planning_shifts s
		LEFT JOIN planning_positions p ON p.id = s.position_id AND p.merchant_id = s.merchant_id AND p.enabled = TRUE
		WHERE s.merchant_id = ? AND s.week_id = ? AND s.employee_id IS NOT NULL AND s.enabled = TRUE
		ORDER BY s.employee_id ASC, s.shift_date ASC, s.start_time ASC, s.created_at ASC
	`)).WithArgs("merchant_1", "week_1").WillReturnRows(sqlmock.NewRows([]string{"employee_id", "shift_date", "start_time", "end_time", "position_label"}).AddRow(
		"emp_1", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", "Service",
	))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT employee_id, shift_date, start_time, end_time, position_label
		FROM planning_published_shift_snapshots
		WHERE merchant_id = ? AND week_id = ?
		ORDER BY employee_id ASC, shift_date ASC, start_time ASC
	`)).WithArgs("merchant_1", "week_1").WillReturnRows(sqlmock.NewRows([]string{"employee_id", "shift_date", "start_time", "end_time", "position_label"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
			UPDATE planning_weeks
			SET status = 'published', published_at = COALESCE(published_at, ?), updated_at = ?
			WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		`)).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "merchant_1", "week_1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
			UPDATE planning_shifts
			SET status = 'published', updated_at = ?
			WHERE merchant_id = ? AND week_id = ? AND enabled = TRUE AND status <> 'published'
		`)).WithArgs(sqlmock.AnyArg(), "merchant_1", "week_1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
			DELETE FROM planning_published_shift_snapshots
			WHERE merchant_id = ? AND week_id = ?
		`)).WithArgs("merchant_1", "week_1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`
			INSERT INTO planning_published_shift_snapshots (
				id, merchant_id, week_id, employee_id, shift_date, start_time, end_time, position_label, published_at, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)).WithArgs(sqlmock.AnyArg(), "merchant_1", "week_1", "emp_1", sqlmock.AnyArg(), "09:00:00", "17:00:00", "Service", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
			FROM planning_weeks
			WHERE merchant_id = ? AND id = ? AND enabled = TRUE
			LIMIT 1
		`)).WithArgs("merchant_1", "week_1").WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
		"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "published", now, nil, now, now, nil,
	))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT fullName
		FROM merchant
		WHERE id = ?
		LIMIT 1
	`)).WithArgs("merchant_1").WillReturnRows(sqlmock.NewRows([]string{"fullName"}).AddRow("Le Bistrot"))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT e.id, e.first_name, e.last_name,
			COALESCE(NULLIF(TRIM(u.email), ''), NULLIF(TRIM(e.email), '')) AS email,
			COALESCE(NULLIF(TRIM(u.tel), ''), NULLIF(TRIM(e.phone), '')) AS phone,
			u.last_login_at, du.last_used_at
		FROM employees e
		LEFT JOIN users u ON u.user_id = e.user_id AND u.enabled = TRUE
		LEFT JOIN (
			SELECT user_id, MAX(last_used) AS last_used_at
			FROM users_devices
			WHERE merchant_id = ?
			GROUP BY user_id
		) du ON du.user_id = e.user_id
		WHERE e.merchant_id = ? AND e.enabled = TRUE AND e.id IN (?)
		ORDER BY e.id ASC
	`)).WithArgs("merchant_1", "merchant_1", "emp_1").WillReturnRows(sqlmock.NewRows([]string{"id", "first_name", "last_name", "email", "phone", "last_login_at", "last_used_at"}).AddRow(
		"emp_1", "Jean", "Dupont", "jean@example.com", "+33612345678", now, now,
	))

	week, err := svc.PublishPlanningWeekWithOptions(ctx, "week_1", PublishPlanningWeekRequest{})
	if err != nil {
		t.Fatalf("PublishPlanningWeek() error = %v", err)
	}
	if week.Status != "published" {
		t.Fatalf("unexpected week status: %s", week.Status)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("expected 1 notification on first publish, got %d", len(publisher.messages))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestPublishPlanningWeek_NoneSkipsNotifications(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	publisher := &stubPlanningPublisher{}
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil, WithSettingsReader(stubPlanningSettingsReader{settings: &settingspkg.PlanningSettings{PlanningSMSNotificationsEnabled: true}}), WithPlanningPublisher(publisher))
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()
	mode := notificationModeNone

	mock.ExpectQuery("SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at").WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
		"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, nil, now, now, nil,
	))
	mock.ExpectQuery("SELECT s.employee_id, s.shift_date, s.start_time, s.end_time,").WillReturnRows(sqlmock.NewRows([]string{"employee_id", "shift_date", "start_time", "end_time", "position_label"}))
	mock.ExpectQuery("SELECT employee_id, shift_date, start_time, end_time, position_label").WillReturnRows(sqlmock.NewRows([]string{"employee_id", "shift_date", "start_time", "end_time", "position_label"}))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE planning_weeks").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE planning_shifts").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM planning_published_shift_snapshots").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at").WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
		"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "published", now, nil, now, now, nil,
	))

	if _, err := svc.PublishPlanningWeekWithOptions(ctx, "week_1", PublishPlanningWeekRequest{NotificationMode: &mode}); err != nil {
		t.Fatalf("PublishPlanningWeek() error = %v", err)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("expected no notifications, got %d", len(publisher.messages))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
