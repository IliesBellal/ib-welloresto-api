package settings

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
)

// vacationDateTimeFmt rend start_at/end_at en texte "YYYY-MM-DD HH:MM:SS"
// quel que soit le dialecte actif — meme motif que pos.posDateTimeFmt,
// necessaire car to_char n'existe pas en MySQL et DATE_FORMAT n'existe pas en
// Postgres.
func vacationDateTimeFmt(col string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "to_char(" + col + ", 'YYYY-MM-DD HH24:MI:SS')"
	}
	return "DATE_FORMAT(" + col + ", '%Y-%m-%d %H:%i:%s')"
}

func (r *Repository) ListPlanningVacationPeriods(ctx context.Context, merchantID string) ([]PlanningVacationPeriod, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, label, `+vacationDateTimeFmt("start_at")+`, `+vacationDateTimeFmt("end_at")+`, enabled, created_at, updated_at
		FROM planning_vacation_periods
		WHERE merchant_id = ? AND enabled = TRUE
		ORDER BY start_at ASC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningVacationPeriod, 0)
	for rows.Next() {
		item, err := scanPlanningVacationPeriod(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetPlanningVacationPeriod(ctx context.Context, merchantID, id string) (*PlanningVacationPeriod, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, label, `+vacationDateTimeFmt("start_at")+`, `+vacationDateTimeFmt("end_at")+`, enabled, created_at, updated_at
		FROM planning_vacation_periods
		WHERE id = ? AND merchant_id = ? AND enabled = TRUE
		LIMIT 1
	`, id, merchantID)
	return scanPlanningVacationPeriodRow(row)
}

func (r *Repository) CreatePlanningVacationPeriod(ctx context.Context, merchantID string, item PlanningVacationPeriod) (*PlanningVacationPeriod, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	item.ID = helpers.GeneratePrefixedID(helpers.PlanningVacationPeriodIDPrefix)
	item.Enabled = true
	item.CreatedAt = now
	item.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_vacation_periods (
			id, merchant_id, label, start_at, end_at, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, TRUE, ?, ?)
	`, item.ID, merchantID, item.Label, item.StartAt, item.EndAt, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) UpdatePlanningVacationPeriod(ctx context.Context, merchantID string, item PlanningVacationPeriod) (*PlanningVacationPeriod, error) {
	db := dbx.GetDB(ctx, r.db)
	item.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_vacation_periods
		SET label = ?, start_at = ?, end_at = ?, updated_at = ?
		WHERE id = ? AND merchant_id = ? AND enabled = TRUE
	`, item.Label, item.StartAt, item.EndAt, item.UpdatedAt, item.ID, merchantID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return &item, nil
}

func (r *Repository) SoftDeletePlanningVacationPeriod(ctx context.Context, merchantID, id string) error {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_vacation_periods
		SET enabled = FALSE, deleted_at = ?, updated_at = ?
		WHERE id = ? AND merchant_id = ? AND enabled = TRUE
	`, now, now, id, merchantID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ResolveVacationOverlap indique si `currentDatetime` (heure locale du
// marchand, meme convention que openinghours.FetchActiveSlots) tombe dans une
// periode de vacances active — consomme par pos.GetPOSStatus pour forcer la
// fermeture de l'etablissement, meme mecanisme que ResolvePlanningHoliday
// pour les jours feries mais sur une plage de dates.
func (r *Repository) ResolveVacationOverlap(ctx context.Context, merchantID string, currentDatetime time.Time) (bool, error) {
	db := dbx.GetDB(ctx, r.db)
	now := currentDatetime.Format("2006-01-02 15:04:05")
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM planning_vacation_periods
			WHERE merchant_id = ? AND enabled = TRUE
			  AND start_at <= ? AND end_at >= ?
		)
	`, merchantID, now, now).Scan(&exists)
	return exists, err
}

// ResolveVacationRangeOverlap indique si l'intervalle [rangeStart, rangeEnd)
// (heure locale naive du marchand, meme convention que ResolveVacationOverlap)
// chevauche une periode de vacances active. A la difference de
// ResolveVacationOverlap (un instant precis, pour le statut temps reel
// POS/scannorder), cette variante couvre une plage — utilisee par le module
// reservation pour bloquer toute une journee de disponibilites.
func (r *Repository) ResolveVacationRangeOverlap(ctx context.Context, merchantID string, rangeStart, rangeEnd time.Time) (bool, error) {
	db := dbx.GetDB(ctx, r.db)
	start := rangeStart.Format("2006-01-02 15:04:05")
	end := rangeEnd.Format("2006-01-02 15:04:05")
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM planning_vacation_periods
			WHERE merchant_id = ? AND enabled = TRUE
			  AND start_at < ? AND end_at > ?
		)
	`, merchantID, end, start).Scan(&exists)
	return exists, err
}

type planningVacationPeriodScannable interface {
	Scan(dest ...any) error
}

func scanPlanningVacationPeriod(rows planningVacationPeriodScannable) (*PlanningVacationPeriod, error) {
	return scanPlanningVacationPeriodRow(rows)
}

func scanPlanningVacationPeriodRow(row planningVacationPeriodScannable) (*PlanningVacationPeriod, error) {
	item := &PlanningVacationPeriod{}
	var label sql.NullString
	if err := row.Scan(&item.ID, &label, &item.StartAt, &item.EndAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if label.Valid {
		item.Label = &label.String
	}
	return item, nil
}
