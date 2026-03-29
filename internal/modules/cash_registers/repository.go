package cash_registers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/utils/security"

	"go.uber.org/zap"
)

type CashRegisterRepository struct {
	database *sql.DB
}

func NewCashRegisterRepository(db *sql.DB) *CashRegisterRepository {
	return &CashRegisterRepository{database: db}
}

func (r *CashRegisterRepository) OpenCashRegister(ctx context.Context, req *models.OpenCashRegisterRequest, merchantID string) (*models.CashRegisterOpenResponse, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1) Vérifier si un registre est déjà ouvert pour ce device
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM cash_registers
		WHERE end_date IS NULL
		AND device_id = ?
		AND merchant_id = ?
		LIMIT 1
	`, req.DeviceID, merchantID).Scan(&exists)

	if err == nil {
		return &models.CashRegisterOpenResponse{
			Status: "device_already_opened_cash_register",
		}, nil
	} else if err != sql.ErrNoRows {
		log.Error("Erreur lors de la vérification du registre", zap.Error(err))
		return nil, err
	}

	// 2) Nouveau registre principal
	res, err := db.ExecContext(ctx, `
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
		log.Error("Erreur lors de l'insertion du registre", zap.Error(err))
		return nil, err
	}

	lai, err := res.LastInsertId()
	if err != nil {
		log.Error("Erreur lors de la récupération de l'ID inséré", zap.Error(err))
		return nil, err
	}

	cashRegisterID := strconv.Itoa(int(lai))

	return &models.CashRegisterOpenResponse{
		Status: "cash_register_created",
		CashRegister: &models.CashRegisterOpen{
			CashRegisterId: cashRegisterID,
		},
	}, nil
}

func (r *CashRegisterRepository) GetCashRegisterReport(ctx context.Context, cashRegisterID string) (*models.CashRegisterReport, error) {
	db := dbutils.GetDB(ctx, r.database)

	var (
		startDate string
		endDate   *string
		cashFund  float64
	)

	// --------------------------------------------------------------
	// 1) Récupérer infos du registre
	// --------------------------------------------------------------
	row := db.QueryRowContext(ctx, `
		SELECT start_date, end_date, cash_fund
		FROM cash_registers
		WHERE cash_register_id = ?
	`, cashRegisterID)

	err := row.Scan(&startDate, &endDate, &cashFund)
	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------------
	// 2) CALL GET_CASH_REGISTER_REPORT
	// --------------------------------------------------------------
	rows, err := db.QueryContext(ctx, `CALL GET_CASH_REGISTER_REPORT(?)`, cashRegisterID)
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
	mopRows, err := db.QueryContext(ctx, `CALL GET_CASH_REGISTER_REPORT_MOP(?)`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer mopRows.Close()

	var mopList []models.MOPLine
	var TTC_MOP int

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
		PeriodTo:       helpers.SafeString(endDate),
		CashFund:       cashFund,
		HT:             HT_CR,
		TTC:            TTC_CR,
		TVA:            TVA_CR,
		CashReport:     cashReport,
		MOP:            mopList,
		CashReportType: "Z",
	}

	//tx.Commit()

	return res, nil
}

func (r *CashRegisterRepository) CloseCashRegister(ctx context.Context, cashRegisterID string, merchantID string, req *models.CloseCashRegisterRequest) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Vérifier commandes encore ouvertes
	var tmp string
	err := db.QueryRowContext(ctx, `
		SELECT o.order_id
		FROM orders o
		WHERE o.cash_register_id = ?
		AND o.state NOT IN ('CLOSED','DONE')
		AND (o.scheduled = false OR (o.scheduled = true AND UTC_TIMESTAMP > o.estimated_ready))
		LIMIT 1
	`, cashRegisterID).Scan(&tmp)

	if err == nil {
		log.Warn("Orders still opened, cannot close cash register", zap.String("cash_register_id", cashRegisterID))
		return models.ErrOrdersStillOpened
	} else if err != sql.ErrNoRows {
		return err
	}

	// 2. Associer paiements (STRIPE)
	_, err = db.ExecContext(ctx, `
		UPDATE payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		SET p.cash_register_id = ?
		WHERE o.state = 'CLOSED'
		  AND p.mop = 'STRIPE'
		  AND p.cash_register_id IS NULL
		  AND p.merchant_id = ?
	`, cashRegisterID, merchantID)
	if err != nil {
		return err
	}

	// 3. Associer paiements Uber / Deliveroo
	_, err = db.ExecContext(ctx, `
		UPDATE payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		SET p.cash_register_id = ?
		WHERE o.state = 'CLOSED'
		  AND p.mop IN ('UBER_EATS','DELIVEROO')
		  AND (p.cash_register_id IS NULL OR p.cash_register_id IN ('UBER_EATS','DELIVEROO'))
		  AND p.merchant_id = ?
	`, cashRegisterID, merchantID)
	if err != nil {
		return err
	}

	// 4. Récupérer rapport caisse
	report, err := r.GetCashRegisterReport(ctx, cashRegisterID)
	if err != nil {
		return err
	}

	// -------------------------------------------------------------
	// Calcul du fond de caisse théorique (Calculated Cash)
	// -------------------------------------------------------------
	// A. Récupérer le fond de caisse initial
	var initialCashFund int
	err = db.QueryRowContext(ctx, `
		SELECT cash_fund FROM cash_registers 
		WHERE cash_register_id = ?
	`, cashRegisterID).Scan(&initialCashFund)
	if err != nil {
		return fmt.Errorf("erreur récupération fond de caisse initial: %w", err)
	}

	// B. Isoler les ventes en espèces depuis le rapport et insérer les items
	var cashSales int
	for _, mopLine := range report.MOP {
		_, err := db.ExecContext(ctx, `
			INSERT INTO cash_registers_items (cash_register_id, mop, amount)
			VALUES (?, ?, ?)
		`, cashRegisterID, mopLine.MOP, mopLine.Amount)
		if err != nil {
			return err
		}

		// Remplacer "CASH" par l'ID exact que tu utilises pour l'espèce (ex: "ESPECES")
		if mopLine.MOP == "ES" {
			cashSales += mopLine.Amount
		}
	}

	// C. Calcul du fond de caisse final
	calculatedFinalCash := initialCashFund + cashSales
	// -------------------------------------------------------------

	// 6. LOGIQUE FISCALE : Récupération du précédent hash
	var prevHash sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT hash FROM cash_registers 
		WHERE merchant_id = ? AND end_date IS NOT NULL AND cash_register_id != ?
		ORDER BY end_date DESC LIMIT 1
	`, merchantID, cashRegisterID).Scan(&prevHash)

	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("erreur récupération prev_hash: %w", err)
	}

	actualPrevHash := "GENESIS_HASH"
	if prevHash.Valid && prevHash.String != "" {
		actualPrevHash = prevHash.String
	}

	// 7. LOGIQUE FISCALE : Calcul du nouveau Hash (utilisation de calculatedFinalCash et correction de %.2d en %.2f)
	dataToHash := fmt.Sprintf("%s|%s|%.2f|%s", cashRegisterID, merchantID, calculatedFinalCash, actualPrevHash)
	hashBytes := sha256.Sum256([]byte(dataToHash))
	newHash := hex.EncodeToString(hashBytes[:])

	signature := security.SignHash(newHash)

	// 8. Fermer le registre (avec MAJ des infos fiscales et du calculatedFinalCash)
	_, err = db.ExecContext(ctx, `
		UPDATE cash_registers
		SET end_date = UTC_TIMESTAMP,
			final_cash_fund = ?,
			previous_hash = ?,
			hash = ?,
			signature = ?
		WHERE cash_register_id = ?
		AND end_date IS NULL
	`, calculatedFinalCash, actualPrevHash, newHash, signature, cashRegisterID)

	if err != nil {
		return err
	}

	return nil
}

func (r *CashRegisterRepository) GetCashRegisterSummary(ctx context.Context, cashRegisterID string, merchantID string) (*models.CashRegisterSummaryResponse, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if !r.isCashRegisterClosed(ctx, cashRegisterID) {
		return nil, models.ErrCashRegisterStillOpen
	}

	// ----------------------------------------------------------------
	// 1) QUERY PRINCIPALE
	// ----------------------------------------------------------------
	row := db.QueryRowContext(ctx, `
		SELECT 
			cr.cash_register_id, cr.start_date, cr.end_date,
			cd.cash_desk_id, cd.name,
			cr.cash_fund, cr.final_cash_fund,
			cr.closed, cr.closure_comment,
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
	var start_date_temp, end_date_temp sql.NullTime

	err := row.Scan(
		&cr.CashRegisterID,
		&start_date_temp,
		&end_date_temp,
		&cr.CashDesk.CashDeskID,
		&cr.CashDesk.CashDeskName,
		&cr.CashFund,
		&cr.FinalCashFund,
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
		log.Error(err.Error())
		return nil, err
	}

	cr.StartDate = helpers.NullTimePtr(start_date_temp).UTC().Unix()
	cr.EndDate = helpers.NullTimeToNullUnixInt(end_date_temp)
	// ----------------------------------------------------------------
	// 2) ITEMS
	// ----------------------------------------------------------------
	itemsRows, err := db.QueryContext(ctx, `
		SELECT cri.id, cri.mop, l.label, cri.amount
		FROM cash_registers_items cri
		LEFT JOIN labels l ON l.label_value = cri.mop
			AND l.lang = 'FR' AND l.label_type = 'mop'
		WHERE cri.cash_register_id = ?
		ORDER BY cri.id ASC
	`, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
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
			log.Error(err.Error())
			return nil, err
		}

		it.Currency = cr.Currency
		cr.Items = append(cr.Items, it)
	}

	// ----------------------------------------------------------------
	// CUSTOM ITEMS
	// ----------------------------------------------------------------
	customRows, err := db.QueryContext(ctx, `
		SELECT id, label, amount
		FROM cash_registers_custom_items
		WHERE cash_register_id = ?
		  AND enabled = 1
		ORDER BY id ASC
	`, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
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
	ordersRows, err := db.QueryContext(ctx, `
		SELECT o.order_id, o.order_num, o.creation_date, o.price, /*o.isDelivery,*/ o.status,
		       u.first_name, u.last_name
		FROM orders o
		INNER JOIN users u ON u.user_id = o.created_by
		WHERE o.cash_register_id = ?
		ORDER BY o.creation_date ASC
	`, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
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
			//&o.IsDelivery,
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
	payRows, err := db.QueryContext(ctx, `
		SELECT p.order_id, o.order_num, p.payment_id, p.amount, p.mop, p.enabled,
		       p.payment_date, u.user_id, u.first_name, u.last_name
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		INNER JOIN users u ON u.user_id = p.user_id
		WHERE p.cash_register_id = ?
		ORDER BY p.payment_date ASC
	`, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
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
			log.Error(err.Error())
			return nil, err
		}

		p.Currency = cr.Currency
		cr.Payments = append(cr.Payments, p)
	}

	return &models.CashRegisterSummaryResponse{
		Status:       "1",
		CashRegister: &cr,
	}, nil
}

func (r *CashRegisterRepository) AddCustomItem(ctx context.Context, cashRegisterID string, req *models.AddCustomItemRequest, user *auth.UserLoginRow) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if !r.isCashRegisterClosed(ctx, cashRegisterID) {
		return "", models.ErrCashRegisterStillOpen
	}

	// Insert custom item
	res, err := db.ExecContext(ctx, `
		INSERT INTO cash_registers_custom_items (label, amount, cash_register_id, merchant_id, created_by)
		VALUES (?, ?, ?, ?, ?)
	`, req.Label, req.Value, cashRegisterID, user.MerchantID, user.UserID)

	if err != nil {
		log.Error(err.Error())
		return "", err
	}

	insertID, _ := res.LastInsertId()

	return fmt.Sprintf("%d", insertID), nil
}

func (r *CashRegisterRepository) isCashRegisterClosed(ctx context.Context, cashRegisterID string) bool {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Check if cash register still open
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM cash_registers
		WHERE cash_register_id = ? AND end_date is not null
	`, cashRegisterID).Scan(&exists)

	if err != nil {
		log.Error(err.Error())
		return true
	}
	if exists == 0 {
		return false
	}

	return true
}

func (r *CashRegisterRepository) DeleteCustomItem(ctx context.Context, cashRegisterID string, itemID string, user *auth.UserLoginRow) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if !r.isCashRegisterClosed(ctx, cashRegisterID) {
		return models.ErrCashRegisterStillOpen
	}

	_, err := db.ExecContext(ctx, `
		UPDATE cash_registers_custom_items
		SET enabled = 0
		WHERE cash_register_id = ? AND id = ? AND merchant_id = ?
	`, cashRegisterID, itemID, user.MerchantID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *CashRegisterRepository) EncloseCashRegister(ctx context.Context, userID, cashRegisterID, comment string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if !r.isCashRegisterClosed(ctx, cashRegisterID) {
		return models.ErrCashRegisterStillOpen
	}

	// Update
	_, err := db.ExecContext(ctx, `
		UPDATE cash_registers
		SET closed = true,
		    closed_by = ?,
		    closure_comment = ?
		WHERE cash_register_id = ?
	`, userID, comment, cashRegisterID)

	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *CashRegisterRepository) GetCashRegisterHistory(ctx context.Context, merchantID string, userID string, req models.OrderHistoryRequest) ([]models.CashRegister, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// ==========================================
	// 1️⃣ CONSTRUCTION DU WHERE + ARGUMENTS
	// ==========================================
	// On garde ta logique complexe de droits (Admin ou propriétaire)
	where := ` 
        WHERE cd.merchant_id = ? 
          AND cr.end_date IS NOT NULL 
          AND (
                cr.user_id = ? 
                OR EXISTS (
                    SELECT 1 
                    FROM users u 
                    INNER JOIN users_rights ur ON ur.id = u.access_id 
                    WHERE u.user_id = ? 
                      AND u.merchant_id = cd.merchant_id 
                      AND ur.admin = TRUE
                )
          ) `

	args := []interface{}{merchantID, userID, userID}

	// Ajout du filtre par date si présent dans la requête
	if req.DateFrom != nil && req.DateTo != nil {
		where += " AND cr.start_date BETWEEN ? AND ? "
		args = append(args, *req.DateFrom, *req.DateTo)
	}

	// ==========================================
	// 2️⃣ CALCUL DE LA PAGINATION
	// ==========================================
	limit := 50
	if req.Limit != nil && *req.Limit > 0 {
		limit = *req.Limit
	}

	page := 1
	if req.Page != nil && *req.Page > 0 {
		page = *req.Page
	}

	offset := (page - 1) * limit

	// ==========================================
	// 3️⃣ RÉCUPÉRATION DES IDs UNIQUEMENT
	// ==========================================
	idQuery := `
        SELECT cr.cash_register_id
        FROM cash_registers cr
        INNER JOIN cash_desks cd ON cd.cash_desk_id = cr.cash_desk_id
    ` + where + `
        ORDER BY cr.start_date DESC
        LIMIT ? OFFSET ?
    `
	idArgs := append(args, limit, offset)

	rows, err := db.QueryContext(ctx, idQuery, idArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var registerIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		registerIDs = append(registerIDs, id)
	}

	if len(registerIDs) == 0 {
		return []models.CashRegister{}, nil
	}

	// ==========================================
	// 4️⃣ CONSTRUCTION DU FILTRE IN (...)
	// ==========================================
	var inParts []string
	for _, id := range registerIDs {
		inParts = append(inParts, fmt.Sprintf("'%s'", id))
	}

	// ==========================================
	// 5️⃣ RÉCUPÉRATION DES DONNÉES COMPLÈTES
	// ==========================================
	fullQuery := fmt.Sprintf(`
        SELECT cr.cash_register_id,
               cr.start_date,
               cr.end_date,
               cd.cash_desk_id,
               cd.name,
               cr.closed
        FROM cash_registers cr
        INNER JOIN cash_desks cd ON cd.cash_desk_id = cr.cash_desk_id
        WHERE cr.cash_register_id IN (%s)
        ORDER BY cr.start_date DESC
    `, strings.Join(inParts, ","))

	fullRows, err := db.QueryContext(ctx, fullQuery)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer fullRows.Close()

	var history []models.CashRegister
	for fullRows.Next() {
		var h models.CashRegister
		var rawStartDate, rawEndDate sql.NullTime

		err := fullRows.Scan(
			&h.CashRegisterID,
			&rawStartDate,
			&rawEndDate,
			&h.CashDesk.CashDeskID,
			&h.CashDesk.CashDeskName,
			&h.Closed,
		)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}

		h.StartDate = helpers.NullTimeToNullUnixInt(rawStartDate)
		h.EndDate = helpers.NullTimeToNullUnixInt(rawEndDate)

		history = append(history, h)
	}

	return history, nil
}

func (r *CashRegisterRepository) GetCashRegisterTVADetails(ctx context.Context, merchantID, cashRegisterID string) (*models.CashRegisterDetails, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Retrieve header info
	var header struct {
		StartDate string
		EndDate   string
		CashFund  int
	}

	err := db.QueryRowContext(ctx, `
        SELECT start_date, end_date, cash_fund
        FROM cash_registers
        WHERE cash_register_id = ?
        AND merchant_id = ?
    `, cashRegisterID, merchantID).Scan(
		&header.StartDate, &header.EndDate, &header.CashFund,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// 2. Call stored procedure GET_CASH_REGISTER_REPORT
	rows, err := db.QueryContext(ctx, `CALL GET_CASH_REGISTER_REPORT(?)`, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var items []models.CashReportLine
	var totalHT, totalTTC, totalTVA int

	for rows.Next() {
		var line models.CashReportLine
		if err := rows.Scan(
			&line.DeliveryType,
			&line.Label,
			&line.TVATitle,
			&line.HT,
			&line.TTC,
			&line.TVA,
		); err != nil {
			return nil, err
		}
		totalHT += line.HT
		totalTTC += line.TTC
		totalTVA += line.TVA
		items = append(items, line)
	}
	for rows.NextResultSet() {
		// just drain
	}

	// 3. Call MOP stored procedure
	mopRows, err := db.QueryContext(ctx, `CALL GET_CASH_REGISTER_REPORT_MOP(?)`, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer mopRows.Close()

	var mops []models.MOPLine
	var totalMop int

	for mopRows.Next() {
		var m models.MOPLine
		if err := mopRows.Scan(&m.MOP, &m.Amount); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		totalMop += m.Amount
		mops = append(mops, m)
	}
	for mopRows.NextResultSet() {
		// just drain
	}

	// 4. Group by delivery_type like PHP
	grouped := make(map[string]*models.CashReportDeliveryGroup)

	for _, line := range items {
		g, ok := grouped[line.DeliveryType]
		if !ok {
			g = &models.CashReportDeliveryGroup{
				DeliveryTypeID:    line.DeliveryType,
				DeliveryTypeLabel: line.Label,
				TVACategories:     []models.TVACategoryLine{},
			}
			grouped[line.DeliveryType] = g
		}

		g.TVACategories = append(g.TVACategories, models.TVACategoryLine{
			TVATitle: line.TVATitle,
			HT:       line.HT,
			TTC:      line.TTC,
			TVA:      line.TVA,
		})
	}

	// convert map → slice
	var report []models.CashReportDeliveryGroup
	for _, v := range grouped {
		report = append(report, *v)
	}

	return &models.CashRegisterDetails{
		Status:         1,
		CashReportID:   cashRegisterID,
		PeriodFrom:     header.StartDate,
		PeriodTo:       header.EndDate,
		CashFund:       header.CashFund,
		HT:             totalHT,
		TTC:            totalTTC,
		TVA:            totalTVA,
		CashReport:     report,
		MOP:            mops,
		CashReportType: "Z",
	}, nil
}

func (r *CashRegisterRepository) UpsertDeviceLink(ctx context.Context, deviceID, userID, onBehalfOf string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := `
		INSERT INTO device_link (device_id, user_id, on_behalf_of, creation_date)
		VALUES (?, ?, ?, UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE 
			on_behalf_of = VALUES(on_behalf_of), 
			user_id = VALUES(user_id), 
			creation_date = UTC_TIMESTAMP()`

	_, err := db.ExecContext(ctx, query, deviceID, userID, onBehalfOf)
	if err != nil {
		log.Error(err.Error())
	}
	return err
}
