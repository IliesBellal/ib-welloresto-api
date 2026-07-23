package daycomments

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/database/dbx"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const selectPlanningDayCommentColumns = `id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at`

func (r *Repository) ListByDateRange(ctx context.Context, merchantID string, startDate, endDate time.Time) ([]PlanningDayComment, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT `+selectPlanningDayCommentColumns+`
		FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date >= ? AND comment_date <= ?
		ORDER BY comment_date ASC
	`, merchantID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningDayComment, 0)
	for rows.Next() {
		item, err := scanPlanningDayCommentRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetByDate(ctx context.Context, merchantID string, commentDate time.Time) (*PlanningDayComment, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT `+selectPlanningDayCommentColumns+`
		FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date = ?
		LIMIT 1
	`, merchantID, commentDate.Format("2006-01-02"))
	return scanPlanningDayCommentRow(row)
}

// Upsert creates the day comment or overwrites its text in place. The
// unique key (merchant_id, comment_date) makes this atomic — no read before
// write is needed here, and `created_by`/`id` are left untouched by the
// UPDATE branch so the original author/id survive edits.
func (r *Repository) Upsert(ctx context.Context, merchantID string, commentDate time.Time, comment, userID string) (*PlanningDayComment, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()

	var actor *string
	if userID != "" {
		actor = &userID
	}

	// clé unique (merchant_id, comment_date) — ON CONFLICT côté PG
	upsertQuery := `
		INSERT INTO planning_day_comments (
			id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			comment = VALUES(comment),
			updated_by = VALUES(updated_by),
			updated_at = VALUES(updated_at)
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		upsertQuery = `
		INSERT INTO planning_day_comments (
			id, merchant_id, comment_date, comment, created_by, updated_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (merchant_id, comment_date) DO UPDATE SET
			comment = EXCLUDED.comment,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at
	`
	}
	_, err := db.ExecContext(ctx, upsertQuery, helpers.GeneratePrefixedID(helpers.PlanningDayCommentIDPrefix), merchantID, commentDate.Format("2006-01-02"), comment, actor, actor, now, now)
	if err != nil {
		return nil, err
	}
	return r.GetByDate(ctx, merchantID, commentDate)
}

func (r *Repository) Delete(ctx context.Context, merchantID string, commentDate time.Time) error {
	db := dbx.GetDB(ctx, r.db)
	res, err := db.ExecContext(ctx, `
		DELETE FROM planning_day_comments
		WHERE merchant_id = ? AND comment_date = ?
	`, merchantID, commentDate.Format("2006-01-02"))
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanPlanningDayCommentRow(row scannable) (*PlanningDayComment, error) {
	item := &PlanningDayComment{}
	var createdBy, updatedBy sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.MerchantID,
		&item.CommentDate,
		&item.Comment,
		&createdBy,
		&updatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		item.CreatedBy = &createdBy.String
	}
	if updatedBy.Valid {
		item.UpdatedBy = &updatedBy.String
	}
	return item, nil
}
