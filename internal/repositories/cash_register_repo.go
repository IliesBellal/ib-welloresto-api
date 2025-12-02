package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type CashRegisterRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewCashRegisterRepository(db *sql.DB, log *zap.Logger) *CashRegisterRepository {
	return &CashRegisterRepository{db: db, log: log}
}

func (r *CashRegisterRepository) OpenCashRegister(ctx context.Context, req *models.OpenCashRegisterRequest, merchantID string) (map[string]interface{}, error) {

	r.log.Info("OpenCashRegister START",
		zap.String("merchant_id", merchantID),
		zap.String("device_id", req.DeviceID),
	)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	rollback := func(err error) (map[string]interface{}, error) {
		_ = tx.Rollback()
		return nil, err
	}

	// -------------------------------------------------------------
	// 1) Vérifier si un registre déjà ouvert pour ce device
	// -------------------------------------------------------------
	var exists int
	err = tx.QueryRowContext(ctx, `
        SELECT 1
        FROM cash_registers
        WHERE end_date IS NULL
        AND device_id = ?
        AND merchant_id = ?
        LIMIT 1
    `, req.DeviceID, merchantID).Scan(&exists)

	if err == nil {
		// Requête a retourné une ligne → device déjà utilisé
		return map[string]interface{}{
			"status": "device_already_opened_cash_register",
		}, nil
	}
	if err != sql.ErrNoRows {
		return rollback(err)
	}

	var cashRegisterID int64

	// -------------------------------------------------------------
	// 2) Sous-registre (cash_register_id fourni)
	// -------------------------------------------------------------
	if req.CashRegister.CashRegisterID != nil {

		// Vérifier si un sous-registre existe déjà pour ce device
		err = tx.QueryRowContext(ctx, `
            SELECT 1
            FROM cash_registers cr
            INNER JOIN sub_cash_registers scr ON scr.cash_register_id = cr.cash_register_id
            WHERE cr.end_date IS NULL
            AND scr.device_id = ?
            AND cr.merchant_id = ?
            LIMIT 1
        `, req.DeviceID, merchantID).Scan(&exists)

		if err == nil {
			return map[string]interface{}{
				"status": "device_already_opened_sub_cash_register",
			}, nil
		}
		if err != sql.ErrNoRows {
			return rollback(err)
		}

		// Créer le sous-registre
		_, err := tx.ExecContext(ctx, `
            INSERT INTO sub_cash_registers (cash_register_id, device_id, start_date)
            VALUES (?, ?, UTC_TIMESTAMP)
        `, *req.CashRegister.CashRegisterID, req.DeviceID)
		if err != nil {
			return rollback(err)
		}

		// ID du registre parent existant
		cashRegisterID, err = strconv.ParseInt(*req.CashRegister.CashRegisterID, 10, 64)
		if err != nil {
			return rollback(err)
		}

	} else {
		// -------------------------------------------------------------
		// 3) Nouveau registre principal
		// -------------------------------------------------------------

		// Vérifier si la caisse a déjà un registre ouvert
		err = tx.QueryRowContext(ctx, `
            SELECT 1
            FROM cash_registers
            WHERE end_date IS NULL
            AND cash_desk_id = ?
            AND merchant_id = ?
            LIMIT 1
        `, req.CashRegister.CashDeskID, merchantID).Scan(&exists)

		if err == nil {
			return map[string]interface{}{
				"status": "cash_desk_already_opened_in_a_cash_register",
			}, nil
		}
		if err != sql.ErrNoRows {
			return rollback(err)
		}

		// Créer le registre
		res, err := tx.ExecContext(ctx, `
            INSERT INTO cash_registers
            (cash_desk_id, device_id, user_id, merchant_id, cash_fund, start_date)
            VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP)
        `,
			req.CashRegister.CashDeskID,
			req.DeviceID,
			req.CashRegister.UserID,
			merchantID,
			req.CashRegister.CashFund,
		)
		if err != nil {
			return rollback(err)
		}

		cashRegisterID, err = res.LastInsertId()
		if err != nil {
			return rollback(err)
		}
	}

	// -------------------------------------------------------------
	// COMMIT
	// -------------------------------------------------------------
	if err := tx.Commit(); err != nil {
		return rollback(err)
	}

	return map[string]interface{}{
		"status": "cash_register_created",
		"cash_register": map[string]interface{}{
			"cash_register": cashRegisterID,
		},
	}, nil
}

func (r *CashRegisterRepository) GetCashRegisterReport(ctx context.Context, cashRegisterID string) (*models.CashRegisterReport, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		startDate string
		endDate   *string
		cashFund  float64
	)

	// --------------------------------------------------------------
	// 1) Récupérer infos du registre
	// --------------------------------------------------------------
	row := tx.QueryRowContext(ctx, `
		SELECT start_date, end_date, cash_fund
		FROM cash_registers
		WHERE cash_register_id = ?
	`, cashRegisterID)

	err = row.Scan(&startDate, &endDate, &cashFund)
	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------------
	// 2) CALL GET_CASH_REGISTER_REPORT
	// --------------------------------------------------------------
	rows, err := tx.QueryContext(ctx, `CALL GET_CASH_REGISTER_REPORT(?)`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type ReportRow struct {
		DeliveryType string
		Label        string
		TVATitle     string
		HT           int
		TTC          int
		TVA          int
	}

	var reportRows []ReportRow

	var (
		HT_CR  int
		TTC_CR int
		TVA_CR int
	)

	for rows.Next() {
		var rr ReportRow
		err := rows.Scan(
			&rr.DeliveryType,
			&rr.Label,
			&rr.TVATitle,
			&rr.HT,
			&rr.TTC,
			&rr.TVA,
		)
		if err != nil {
			return nil, err
		}

		reportRows = append(reportRows, rr)

		HT_CR += rr.HT
		TTC_CR += rr.TTC
		TVA_CR += rr.TVA
	}
	// obligatoire :
	for rows.NextResultSet() {
		// just drain
	}

	// --------------------------------------------------------------
	// 3) CALL GET_CASH_REGISTER_REPORT_MOP
	// --------------------------------------------------------------
	mopRows, err := tx.QueryContext(ctx, `CALL GET_CASH_REGISTER_REPORT_MOP(?)`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer mopRows.Close()

	var mopList []models.MOPLine
	var TTC_MOP float64

	for mopRows.Next() {
		var line models.MOPLine
		err := mopRows.Scan(
			&line.MOP,
			&line.Amount,
		)
		if err != nil {
			return nil, err
		}

		mopList = append(mopList, line)
		TTC_MOP += line.Amount
	}
	for mopRows.NextResultSet() {
		// just drain
	}

	// --------------------------------------------------------------
	// 4) Arrondis comme en PHP
	// --------------------------------------------------------------
	/*
		TTC_MOP = math.Round(TTC_MOP*100) / 100
		HT_CR = math.Round(HT_CR*100) / 100
		TTC_CR = math.Round(TTC_CR*100) / 100
		TVA_CR = math.Round(TVA_CR*100) / 100
	*/

	// --------------------------------------------------------------
	// 5) Construction du cash_report (groupé par delivery_type)
	// --------------------------------------------------------------
	cashReport := []models.CashReportDeliveryGroup{}

	// Phase 1 : créer les groupes
	for _, r := range reportRows {
		exists := false
		for _, group := range cashReport {
			if group.DeliveryTypeID == r.DeliveryType {
				exists = true
				break
			}
		}
		if !exists {
			cashReport = append(cashReport, models.CashReportDeliveryGroup{
				DeliveryTypeID:    r.DeliveryType,
				DeliveryTypeLabel: r.Label,
				TVACategories:     []models.TVACategoryLine{},
			})
		}
	}

	// Phase 2 : remplir les catégories
	for _, r := range reportRows {
		for i := range cashReport {
			if cashReport[i].DeliveryTypeID == r.DeliveryType {
				cashReport[i].TVACategories = append(cashReport[i].TVACategories,
					models.TVACategoryLine{
						TVATitle: r.TVATitle,
						HT:       r.HT,
						TTC:      r.TTC,
						TVA:      r.TVA,
					})
			}
		}
	}

	// --------------------------------------------------------------
	// 6) Retour final
	// --------------------------------------------------------------
	res := &models.CashRegisterReport{
		Status:         1,
		CashReportID:   cashRegisterID,
		PeriodFrom:     startDate,
		PeriodTo:       safeString(endDate),
		CashFund:       cashFund,
		HT:             HT_CR,
		TTC:            TTC_CR,
		TVA:            TVA_CR,
		CashReport:     cashReport,
		MOP:            mopList,
		CashReportType: "Z",
	}

	tx.Commit()

	return res, nil
}

func (r *CashRegisterRepository) CloseCashRegister(ctx context.Context, cashRegisterID string, merchantID string, req *models.CloseCashRegisterRequest) (map[string]interface{}, error) {

	r.log.Info("CloseCashRegister START",
		zap.String("cash_register_id", cashRegisterID),
	)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	rollback := func(err error) (map[string]interface{}, error) {
		tx.Rollback()
		return nil, err
	}

	// ---------------------------------------------------------------------
	//  ZONE VALIDATIONS FUTURES
	// ---------------------------------------------------------------------
	// - vérifier que userID a le droit de fermer le registre
	// - vérifier que device_id correspond au registre ouvert ?
	// - vérifier si la caisse est verrouillée ?
	// ---------------------------------------------------------------------

	// 1. Vérifier commandes encore ouvertes
	var tmp string
	err = tx.QueryRowContext(ctx, `
		SELECT o.order_id
		FROM orders o
		WHERE o.cash_register_id = ?
		AND o.state NOT IN ('CLOSED','DONE')
		AND (o.scheduled = '0'
		     OR (o.scheduled = '1' AND o.estimated_ready > UTC_TIMESTAMP))
	`, cashRegisterID).Scan(&tmp)

	if err == nil {
		// commande en cours
		tx.Rollback()
		return map[string]interface{}{
			"status": "orders_still_opened",
			"error":  "Orders still pending",
		}, nil
	}
	if err != sql.ErrNoRows {
		return rollback(err)
	}

	// 2. Associer paiements (STRIPE)
	_, err = tx.ExecContext(ctx, `
		UPDATE payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		SET p.cash_register_id = ?
		WHERE o.state = 'CLOSED'
		  AND p.mop = 'STRIPE'
		  AND p.cash_register_id IS NULL
		  AND p.merchant_id = ?
	`, cashRegisterID, merchantID)
	if err != nil {
		return rollback(err)
	}

	// 3. Associer paiements Uber / Deliveroo
	_, err = tx.ExecContext(ctx, `
		UPDATE payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		SET p.cash_register_id = ?
		WHERE o.state = 'CLOSED'
		  AND p.mop IN ('UBER_EATS','DELIVEROO')
		  AND (p.cash_register_id IS NULL OR p.cash_register_id IN ('UBER_EATS','DELIVEROO'))
		  AND p.merchant_id = ?
	`, cashRegisterID, merchantID)
	if err != nil {
		return rollback(err)
	}

	// 4. Fermer le registre
	_, err = tx.ExecContext(ctx, `
		UPDATE cash_registers
		SET end_date = UTC_TIMESTAMP
		WHERE cash_register_id = ?
		AND end_date IS NULL
	`, cashRegisterID)
	if err != nil {
		return rollback(err)
	}

	// COMMIT 1
	if err := tx.Commit(); err != nil {
		return rollback(err)
	}

	// 5. Récupérer rapport caisse (tu le réimplémenteras ensuite)
	report, err := r.GetCashRegisterReport(ctx, cashRegisterID)
	if err != nil {
		return nil, err
	}

	// 6. Deuxième transaction pour insérer détails MOP
	tx2, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	rollback2 := func(err error) (map[string]interface{}, error) {
		tx2.Rollback()
		return nil, err
	}

	for _, mopLine := range report.MOP {
		_, err := tx2.ExecContext(context.Background(), `
			INSERT INTO cash_registers_items (cash_register_id, mop, amount)
			VALUES (?, ?, ?)
		`, cashRegisterID, mopLine.MOP, mopLine.Amount)
		if err != nil {
			return rollback2(err)
		}
	}

	if err := tx2.Commit(); err != nil {
		return rollback2(err)
	}

	return map[string]interface{}{
		"status":               "cash_register_closed",
		"cash_register_report": report,
	}, nil
}

func (r *CashRegisterRepository) GetCashRegisterSummary(ctx context.Context, cashRegisterID string, merchantID string) (*models.CashRegisterSummaryResponse, error) {

	r.log.Info("GetCashRegisterSummary START",
		zap.String("cash_register_id", cashRegisterID),
	)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// ----------------------------------------------------------------
	// 1) QUERY PRINCIPALE
	// ----------------------------------------------------------------
	row := tx.QueryRowContext(ctx, `
		SELECT 
			cr.cash_register_id, cr.start_date, cr.end_date,
			cd.cash_desk_id, cd.name,
			cr.cash_fund, cr.closed, cr.closure_comment,
			u.user_id, u.first_name, u.last_name,
			mp.currency,
			cb.user_id as closed_by_user_id,
			cb.first_name as closed_by_first_name,
			cb.last_name as closed_by_last_name
		FROM cash_registers cr
		INNER JOIN cash_desks cd ON cd.cash_desk_id = cr.cash_desk_id
		INNER JOIN users u ON u.user_id = cr.user_id
		INNER JOIN merchant_parameters mp ON cd.merchant_id = mp.merchant_id
		LEFT JOIN users cb ON cb.user_id = cr.closed_by
		WHERE cr.cash_register_id = ?
	`, cashRegisterID)

	var cr models.CashRegisterSummary

	err = row.Scan(
		&cr.CashRegisterID,
		&cr.StartDate,
		&cr.EndDate,
		&cr.CashDesk.CashDeskID,
		&cr.CashDesk.CashDeskName,
		&cr.CashFund,
		&cr.Closed,
		&cr.ClosureComment,
		&cr.OpenedBy.UserID,
		&cr.OpenedBy.FirstName,
		&cr.OpenedBy.LastName,
		&cr.Currency,
		&cr.ClosedBy.UserID,
		&cr.ClosedBy.FirstName,
		&cr.ClosedBy.LastName,
	)
	if err != nil {
		return nil, err
	}

	// ----------------------------------------------------------------
	// 2) ITEMS
	// ----------------------------------------------------------------
	itemsRows, err := tx.QueryContext(ctx, `
		SELECT cri.id, cri.mop, l.label, cri.amount
		FROM cash_registers_items cri
		LEFT JOIN labels l ON l.label_value = cri.mop
			AND l.lang = 'FR' AND l.label_type = 'mop'
		WHERE cri.cash_register_id = ?
		ORDER BY cri.id ASC
	`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer itemsRows.Close()

	for itemsRows.Next() {
		var it models.CRItem
		err := itemsRows.Scan(
			&it.ItemID,
			&it.MOP,
			&it.Label,
			&it.Amount,
		)
		if err != nil {
			return nil, err
		}

		it.Currency = cr.Currency
		cr.Items = append(cr.Items, it)
	}

	// ----------------------------------------------------------------
	// CUSTOM ITEMS
	// ----------------------------------------------------------------
	customRows, err := tx.QueryContext(ctx, `
		SELECT id, label, amount
		FROM cash_registers_custom_items
		WHERE cash_register_id = ?
		  AND enabled = 1
		ORDER BY id ASC
	`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer customRows.Close()

	for customRows.Next() {
		var ci models.CRCustomItem
		err := customRows.Scan(&ci.ItemID, &ci.Label, &ci.Amount)
		if err != nil {
			return nil, err
		}
		ci.Currency = cr.Currency
		cr.CustomItems = append(cr.CustomItems, ci)
	}

	// ----------------------------------------------------------------
	// ORDERS
	// ----------------------------------------------------------------
	ordersRows, err := tx.QueryContext(ctx, `
		SELECT o.order_id, o.order_num, o.creation_date, o.price, o.isDelivery, o.status,
		       u.first_name, u.last_name
		FROM orders o
		INNER JOIN users u ON u.user_id = o.created_by
		WHERE o.cash_register_id = ?
		ORDER BY o.creation_date ASC
	`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer ordersRows.Close()

	for ordersRows.Next() {
		var o models.CROrder
		err := ordersRows.Scan(
			&o.OrderID,
			&o.OrderNum,
			&o.CreationDate,
			&o.Price,
			&o.IsDelivery,
			&o.Status,
			&o.OrderedBy.FirstName,
			&o.OrderedBy.LastName,
		)
		if err != nil {
			return nil, err
		}

		o.Currency = cr.Currency
		cr.Orders = append(cr.Orders, o)
	}

	// ----------------------------------------------------------------
	// PAYMENTS
	// ----------------------------------------------------------------
	payRows, err := tx.QueryContext(ctx, `
		SELECT p.order_id, o.order_num, p.payment_id, p.amount, p.mop, p.enabled,
		       p.payment_date, u.user_id, u.first_name, u.last_name
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		INNER JOIN users u ON u.user_id = p.user_id
		WHERE p.cash_register_id = ?
		ORDER BY p.payment_date ASC
	`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer payRows.Close()

	for payRows.Next() {
		var p models.CRPayment
		err := payRows.Scan(
			&p.OrderID,
			&p.OrderNum,
			&p.PaymentID,
			&p.Amount,
			&p.MOP,
			&p.Enabled,
			&p.PaymentDate,
			&p.CollectedBy.UserID,
			&p.CollectedBy.FirstName,
			&p.CollectedBy.LastName,
		)
		if err != nil {
			return nil, err
		}

		p.Currency = cr.Currency
		cr.Payments = append(cr.Payments, p)
	}

	tx.Commit()

	return &models.CashRegisterSummaryResponse{
		Status:       "1",
		CashRegister: &cr,
	}, nil
}

func (r *CashRegisterRepository) AddCustomItem(ctx context.Context, cashRegisterID string, label string, value float64) (string, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Check if cash register still open
	var exists int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM cash_registers
		WHERE cash_register_id = ? AND closed = false
	`, cashRegisterID).Scan(&exists)

	if err != nil {
		return "", err
	}
	if exists == 0 {
		return "", errors.New("cash_register_closed")
	}

	valueInt := int(value)

	// Insert custom item
	res, err := tx.ExecContext(ctx, `
		INSERT INTO cash_registers_custom_items (label, amount, cash_register_id)
		VALUES (?, ?, ?)
	`, label, valueInt, cashRegisterID)

	if err != nil {
		return "", err
	}

	insertID, _ := res.LastInsertId()

	tx.Commit()
	return fmt.Sprintf("%d", insertID), nil
}

func (r *CashRegisterRepository) DeleteCustomItem(ctx context.Context, cashRegisterID string, itemID string) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if open
	var exists int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM cash_registers
		WHERE cash_register_id = ? AND closed = 0
	`, cashRegisterID).Scan(&exists)

	if err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("cash_register_closed")
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE cash_registers_custom_items
		SET enabled = 0
		WHERE cash_register_id = ? AND id = ?
	`, cashRegisterID, itemID)
	if err != nil {
		return err
	}

	tx.Commit()
	return nil
}

func (r *CashRegisterRepository) EncloseCashRegister(ctx context.Context, userID string, cashRegisterID string, comment string) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Is open ?
	var exists int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM cash_registers
		WHERE cash_register_id = ? AND closed = 0
	`, cashRegisterID).Scan(&exists)

	if err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("cash_register_closed")
	}

	// Update
	_, err = tx.ExecContext(ctx, `
		UPDATE cash_registers
		SET closed = 1,
		    closed_by = ?,
		    closure_comment = ?
		WHERE cash_register_id = ?
	`, userID, comment, cashRegisterID)

	if err != nil {
		return err
	}

	tx.Commit()
	return nil
}
