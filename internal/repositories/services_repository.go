package repositories

import (
	"context"
	"database/sql"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type ServicesRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewServicesRepository(db *sql.DB, log *zap.Logger) *ServicesRepository {
	return &ServicesRepository{db: db, log: log}
}

func (r *ServicesRepository) GetCurrentService(ctx context.Context, userID string, deviceID string) (*models.CurrentServiceResponse, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// ------------------------------------------------------
	// 1 — CURRENT SERVICE
	// ------------------------------------------------------

	qService := `
		SELECT sp.id, sp.start_date, sp.end_date
		FROM services_performed sp
		WHERE sp.user_id = ?
		AND sp.end_date IS NULL
	`

	var svc *models.PerformedService

	row := tx.QueryRowContext(ctx, qService, userID)
	var (
		sID       string
		startDate *string
		endDate   *string
	)

	switch errScan := row.Scan(&sID, &startDate, &endDate); errScan {
	case sql.ErrNoRows:
		svc = nil
	case nil:
		svc = &models.PerformedService{
			ServiceID: sID,
			StartDate: startDate,
			EndDate:   endDate,
		}
	default:
		err = errScan
		return nil, err
	}

	// ------------------------------------------------------
	// 2 — CASH REGISTER (current device)
	// ------------------------------------------------------

	qCR := `
		SELECT cr.device_id, cd.name, cr.cash_register_id, cd.cash_desk_id
		FROM cash_registers cr
		LEFT JOIN sub_cash_registers scr ON scr.cash_register_id = cr.cash_register_id
		INNER JOIN cash_desks cd ON cr.cash_desk_id = cd.cash_desk_id
		INNER JOIN users u ON u.merchant_id = cr.merchant_id
		WHERE (cr.device_id = ? OR scr.device_id = ?)
		AND u.user_id = ?
		AND cr.end_date IS NULL
		LIMIT 1;
	`

	var cr *models.CashRegisterInfo

	rowCR := tx.QueryRowContext(ctx, qCR, deviceID, deviceID, userID)

	var (
		crDeviceID, crName, crRegisterID, crDeskID string
	)

	if errScan := rowCR.Scan(&crDeviceID, &crName, &crRegisterID, &crDeskID); errScan == nil {
		cr = &models.CashRegisterInfo{
			DeviceID:       crDeviceID,
			CashRegisterID: crRegisterID,
			CashDesk: models.CashRegisterDesk{
				CashDeskName: crName,
				CashDeskID:   crDeskID,
			},
		}
	}

	// ------------------------------------------------------
	// 3 — CASH DESKS LIST
	// ------------------------------------------------------

	qDesks := `
		SELECT cd.cash_desk_id, cd.name, cr.device_id, cr.cash_register_id,
		       CASE WHEN cr.cash_register_id IS NULL THEN 0 ELSE 1 END AS active,
		       opened_by.first_name, opened_by.last_name, opened_by.user_id
		FROM cash_desks cd
		INNER JOIN users u ON u.merchant_id = cd.merchant_id
		LEFT JOIN cash_registers cr ON cr.cash_desk_id = cd.cash_desk_id AND cr.end_date IS NULL
		LEFT JOIN users opened_by ON opened_by.user_id = cr.user_id
		WHERE u.user_id = ?
		AND u.enabled = 1
		AND cd.enabled = 1
	`

	rows, err := tx.QueryContext(ctx, qDesks, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	desks := []models.CashDeskInfo{}

	for rows.Next() {
		var d models.CashDeskInfo

		err = rows.Scan(
			&d.CashDeskID,
			&d.CashDeskName,
			&d.DeviceID,
			&d.CashRegisterID,
			&d.Active,
			&d.OpenedBy.FirstName,
			&d.OpenedBy.LastName,
			&d.OpenedBy.UserID,
		)
		if err != nil {
			return nil, err
		}

		desks = append(desks, d)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// ------------------------------------------------------
	// COMMIT
	// ------------------------------------------------------

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	// ------------------------------------------------------
	// BUILD RESPONSE
	// ------------------------------------------------------

	return &models.CurrentServiceResponse{
		Service:      svc,
		CashRegister: cr,
		CashDesks:    desks,
	}, nil
}
