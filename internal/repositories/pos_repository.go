package repositories

import (
	"context"
	"database/sql"
	"time"
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

// --------------------
// GET POS STATUS
// --------------------
type POSStatus struct {
	Wello struct {
		IsOpen    int    `json:"is_open"`
		Status    string `json:"status"`
		NextStart string `json:"next_start"`
		NextEnd   string `json:"next_end"`
	} `json:"wello_resto_status"`

	Uber struct {
		EstimatedPrepTime string      `json:"estimated_preparation_time"`
		DelayDuration     string      `json:"busy_mode_delay_duration"`
		DelayUntil        interface{} `json:"busy_mode_delay_until"`
		ClosedUntil       interface{} `json:"closed_until"`
	} `json:"uber_eats_status"`
}

func (r *POSRepository) GetPOSStatus(ctx context.Context, merchantID string) (*POSStatus, error) {
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
	var result POSStatus

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
