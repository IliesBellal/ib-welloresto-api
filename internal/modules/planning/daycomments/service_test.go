package daycomments

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

func dayCommentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "merchant_id", "comment_date", "comment", "created_by", "updated_by", "created_at", "updated_at",
	})
}

func withMerchantUser(merchantID string) context.Context {
	return middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: merchantID})
}

func TestServiceListByDateRangeRequiresValidRange(t *testing.T) {
	svc := NewService(NewRepository(nil), nil)

	_, err := svc.ListByDateRange(withMerchantUser("merchant_1"), "2026-07-10", "2026-07-01")
	if !errors.Is(err, models.ErrPlanningInvalidDate) {
		t.Fatalf("ListByDateRange() error = %v, want %v", err, models.ErrPlanningInvalidDate)
	}
}

func TestServiceListByDateRangeRejectsRangeTooLarge(t *testing.T) {
	svc := NewService(NewRepository(nil), nil)

	_, err := svc.ListByDateRange(withMerchantUser("merchant_1"), "2026-01-01", "2026-12-31")
	if !errors.Is(err, models.ErrPlanningInvalidDate) {
		t.Fatalf("ListByDateRange() error = %v, want %v", err, models.ErrPlanningInvalidDate)
	}
}

func TestServiceListByDateRangeReturnsItems(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), nil)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date >= ? AND comment_date <= ?
		ORDER BY comment_date ASC
	`)).
		WithArgs("merchant_1", "2026-07-01", "2026-07-07").
		WillReturnRows(dayCommentRows().AddRow(
			"plan-day-comment-1", "merchant_1", "2026-07-02", "Livraison fournisseur a 10h", "user_1", "user_1", time.Now().UTC(), time.Now().UTC(),
		))

	items, err := svc.ListByDateRange(withMerchantUser("merchant_1"), "2026-07-01", "2026-07-07")
	if err != nil {
		t.Fatalf("ListByDateRange() error = %v", err)
	}
	if len(items) != 1 || items[0].Comment != "Livraison fournisseur a 10h" {
		t.Fatalf("ListByDateRange() items = %#v, want 1 item with the seeded comment", items)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceUpsertRejectsBlankComment(t *testing.T) {
	svc := NewService(NewRepository(nil), nil)

	_, err := svc.Upsert(withMerchantUser("merchant_1"), "2026-07-02", PlanningDayCommentUpsertRequest{Comment: "   "})
	if !errors.Is(err, models.ErrValidationError) {
		t.Fatalf("Upsert() error = %v, want %v", err, models.ErrValidationError)
	}
}

func TestServiceUpsertRejectsTooLongComment(t *testing.T) {
	svc := NewService(NewRepository(nil), nil)

	tooLong := strings.Repeat("a", MaxCommentLength+1)
	_, err := svc.Upsert(withMerchantUser("merchant_1"), "2026-07-02", PlanningDayCommentUpsertRequest{Comment: tooLong})
	if !errors.Is(err, models.ErrPlanningDayCommentTooLong) {
		t.Fatalf("Upsert() error = %v, want %v", err, models.ErrPlanningDayCommentTooLong)
	}
}

func TestServiceUpsertCreatesNewComment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), nil)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date = ?
		LIMIT 1
	`)).
		WithArgs("merchant_1", "2026-07-02").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_day_comments (
			id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			comment = VALUES(comment),
			updated_by = VALUES(updated_by),
			updated_at = VALUES(updated_at)
	`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date = ?
		LIMIT 1
	`)).
		WithArgs("merchant_1", "2026-07-02").
		WillReturnRows(dayCommentRows().AddRow(
			"plan-day-comment-1", "merchant_1", "2026-07-02", "Jour ferie, horaires speciaux", "user_1", "user_1", time.Now().UTC(), time.Now().UTC(),
		))

	item, err := svc.Upsert(withMerchantUser("merchant_1"), "2026-07-02", PlanningDayCommentUpsertRequest{Comment: "  Jour ferie, horaires speciaux  "})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if item.Comment != "Jour ferie, horaires speciaux" {
		t.Fatalf("Upsert() comment = %q, want trimmed seeded comment", item.Comment)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceDeleteNotFoundReturnsError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), nil)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date = ?
		LIMIT 1
	`)).
		WithArgs("merchant_1", "2026-07-02").
		WillReturnError(sql.ErrNoRows)

	err = svc.Delete(withMerchantUser("merchant_1"), "2026-07-02")
	if !errors.Is(err, models.ErrPlanningDayCommentNotFound) {
		t.Fatalf("Delete() error = %v, want %v", err, models.ErrPlanningDayCommentNotFound)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceDeleteRemovesExistingComment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), nil)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date = ?
		LIMIT 1
	`)).
		WithArgs("merchant_1", "2026-07-02").
		WillReturnRows(dayCommentRows().AddRow(
			"plan-day-comment-1", "merchant_1", "2026-07-02", "Jour ferie", "user_1", "user_1", time.Now().UTC(), time.Now().UTC(),
		))

	mock.ExpectExec(regexp.QuoteMeta(`
		DELETE FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date = ?
	`)).
		WithArgs("merchant_1", "2026-07-02").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := svc.Delete(withMerchantUser("merchant_1"), "2026-07-02"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
