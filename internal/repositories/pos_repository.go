package repositories

import (
	"context"
	"database/sql"
	"time"
	"welloresto-api/internal/models"
)

type POSRepository struct {
	db *sql.DB
}

func NewPOSRepository(db *sql.DB) *POSRepository {
	return &POSRepository{db: db}
}

// --------------------
// UPDATE is_open
// --------------------
func (r *POSRepository) UpdatePOSStatus(ctx context.Context, userID string, status bool) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}

	v := 0
	if status {
		v = 1
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE merchant_parameters mp
		INNER JOIN users u ON mp.merchant_id = u.merchant_id
		INNER JOIN users_rights ur ON ur.id = u.access_id
		SET is_open = ?
		WHERE u.user_id = ?`,
		v, userID,
	)

	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *POSRepository) GetDeletionReasons(ctx context.Context, object string) ([]models.DeletionReason, error) {

	query := `
		SELECT dr.deletion_reason_id,
		       dr.deletion_reason_type,
		       dr.deletion_reason_object,
		       dr.deletion_reason_desc,
		       dr.requires_comment,
		       l.label
		FROM deletion_reasons dr
		INNER JOIN labels l 
		       ON l.label_value = dr.deletion_reason_id
		      AND l.lang = 'FR'
		      AND l.label_type = 'deletion_reason'
		WHERE dr.enabled = TRUE
		  AND dr.deletion_reason_object = ?
	`

	rows, err := r.db.QueryContext(ctx, query, object)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.DeletionReason

	for rows.Next() {
		var d models.DeletionReason
		err := rows.Scan(
			&d.DeletionReasonID,
			&d.DeletionReasonType,
			&d.DeletionReasonObject,
			&d.DeletionReasonDesc,
			&d.RequiresComment,
			&d.Label,
		)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}

	return list, nil
}

func (r *POSRepository) GetPOSStatus(ctx context.Context, merchantID string) (*models.POSStatus, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}

	var timezone string

	err = tx.QueryRowContext(ctx,
		`SELECT timezone FROM merchant WHERE id = ?`,
		merchantID,
	).Scan(&timezone)

	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Convert timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	now := time.Now().In(loc)
	currentDate := now.Format("2006-01-02 15:04:05")
	currentTime := now.Format("15:04:05")
	currentDay := int(now.Weekday())
	if currentDay == 0 {
		currentDay = 7 // Sunday=7
	}

	// CALL GET_POS_STATUS
	_, err = tx.ExecContext(ctx,
		`CALL GET_POS_STATUS(?, ?, @p_is_open, @p_last_start, @p_last_end, @p_current_start, @p_current_end, @p_next_start, @p_next_end)`,
		merchantID, currentDate,
	)

	if err != nil {
		tx.Rollback()
		return nil, err
	}

	var isOpen int
	var nextStart, nextEnd sql.NullString

	err = tx.QueryRowContext(ctx,
		`SELECT @p_is_open, @p_next_start, @p_next_end`,
	).Scan(&isOpen, &nextStart, &nextEnd)

	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// OPEN/CLOSED based on hours
	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT 
		CASE WHEN ? BETWEEN hour_from AND hour_to THEN 'OPEN' ELSE 'CLOSED' END AS s
		FROM hours_of_operation
		WHERE merchant_id = ?
		  AND enabled = 1
		  AND day_of_week_from <= ?
		  AND day_of_week_to >= ?
		LIMIT 1`,
		currentTime, merchantID, currentDay, currentDay,
	).Scan(&status)

	if err != nil {
		status = "CLOSED" // default
	}

	// Full POS Status Query
	var result models.POSStatus

	err = tx.QueryRowContext(ctx, `
		SELECT 
			mp.is_open,
			?,
			iue.estimated_preparation_time,
			iue.delay_until,
			iue.delay_duration,
			iue.closed_until
		FROM merchant m
		INNER JOIN merchant_parameters mp ON mp.merchant_id = m.id
		LEFT JOIN integration_uber_eats iue ON iue.enabled = 1 AND iue.merchant_id = m.id
		WHERE m.id = ?`,
		status,
		merchantID,
	).Scan(
		&result.Wello.IsOpen,
		&result.Wello.Status,
		&result.Uber.EstimatedPrepTime,
		&result.Uber.DelayUntil,
		&result.Uber.DelayDuration,
		&result.Uber.ClosedUntil,
	)

	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Next schedules
	result.Wello.NextStart = nextStart.String
	result.Wello.NextEnd = nextEnd.String

	tx.Commit()

	return &result, nil
}

func (r *POSRepository) ToggleProductionPaidOnly(ctx context.Context, merchantID string, status string) (int64, error) {

	res, err := r.db.ExecContext(ctx,
		`UPDATE merchant_parameters 
		 SET kitchen_show_only_paid = ?
		 WHERE merchant_id = ?`,
		status, merchantID,
	)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (r *POSRepository) ToggleSafetyStock(ctx context.Context, merchantID string, status string) (int64, error) {

	res, err := r.db.ExecContext(ctx,
		`UPDATE merchant_parameters 
		 SET disable_components_under_safety_stock = ?
		 WHERE merchant_id = ?`,
		status, merchantID,
	)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (r *POSRepository) ToggleScanNOrder(ctx context.Context, merchantID string, status string) (int64, error) {

	res, err := r.db.ExecContext(ctx,
		`UPDATE scannorder_settings 
		 SET activated = ?
		 WHERE merchant_id = ?`,
		status, merchantID,
	)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (r *POSRepository) GetDeliveryMen(ctx context.Context, merchantID string) ([]models.DeliveryMan, error) {

	rows, err := r.db.QueryContext(ctx, `
        SELECT DISTINCT 
            usv.user_id,
            usv.first_name,
            usv.last_name,
            usv.lat,
            usv.lng,
            usv.status
        FROM user_status_view usv
        INNER JOIN users_rights ur ON ur.id = usv.user_id
        INNER JOIN merchant m ON m.id = ur.merchant_id
        WHERE ur.merchant_id = ?
          AND ur.enabled = TRUE
    `, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.DeliveryMan

	for rows.Next() {
		var d models.DeliveryMan
		err := rows.Scan(
			&d.UserID,
			&d.FirstName,
			&d.LastName,
			&d.Lat,
			&d.Lng,
			&d.Status,
		)
		if err != nil {
			return nil, err
		}

		result = append(result, d)
	}

	return result, nil
}

func (r *POSRepository) IsTicketUsed(ctx context.Context, code string) (bool, error) {
	var exists bool

	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM restaurant_ticket WHERE barcode = ?
		)`,
		code,
	).Scan(&exists)

	if err != nil {
		return false, err
	}

	return exists, nil
}
