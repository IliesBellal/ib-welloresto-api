package cash_registers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

type CashRegisterRepository struct {
	database *sql.DB
}

func NewCashRegisterRepository(db *sql.DB) *CashRegisterRepository {
	return &CashRegisterRepository{database: db}
}

// IsActivatedForOrdering — LOT B B2b-3 (§7.6) : true tant que
// merchant.activation_state = 'LIVE' et subscriptions.status != 'suspended'.
// Lecture toujours fraîche (jamais via l'utilisateur authentifié mis en
// cache Redis — voir docs/decisions.md) : l'ouverture de caisse est
// justement l'endroit où un délai de cache serait le plus visible pour un
// marchand qui vient de régulariser son mandat ou son impayé.
func (r *CashRegisterRepository) IsActivatedForOrdering(ctx context.Context, merchantID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var activationState string
	var subStatus sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT m.activation_state, s.status
		FROM merchant m
		LEFT JOIN subscriptions s ON s.merchant_id = m.id::text
		WHERE m.id::text = ?
	`, merchantID).Scan(&activationState, &subStatus)
	if err != nil {
		return false, err
	}
	if activationState != "LIVE" {
		return false, nil
	}
	if subStatus.Valid && subStatus.String == "suspended" {
		return false, nil
	}
	return true, nil
}

func (r *CashRegisterRepository) OpenCashRegister(ctx context.Context, req *models.OpenCashRegisterRequest, merchantID string) (*models.CashRegisterOpenResponse, error) {
	db := dbx.GetDB(ctx, r.database)
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

	// 2) Mode de clôture en vigueur à la date d'ouverture (calendrier de
	// l'établissement), inscrit sur le registre : un registre ouvert le 31 au
	// soir et fermé le 1er garde le mode du 31 (cf. closing_mode.go).
	// Établissement absent de la table merchant : on n'empêche pas l'ouverture
	// d'une caisse pour un fuseau, repli sur Europe/Paris (seuls les endpoints
	// /admin refusent un établissement inconnu).
	loc, err := r.MerchantLocation(ctx, merchantID)
	if errors.Is(err, ErrClosingModeMerchantUnknown) {
		loc, err = time.LoadLocation("Europe/Paris")
	}
	if err != nil {
		log.Error("Erreur lors de la lecture du fuseau de l'établissement", zap.Error(err))
		return nil, err
	}
	closingMode, err := r.ResolveClosingMode(ctx, merchantID, localDay(time.Now(), loc))
	if err != nil {
		log.Error("Erreur lors de la résolution du mode de clôture", zap.Error(err))
		return nil, err
	}

	// 3) Nouveau registre principal.
	// closure_comment est NOT NULL sans défaut : MySQL non-strict insérait ''
	// implicitement, Postgres refuse l'omission — '' explicite, comportement
	// identique. cash_fund est une colonne integer alimentée par un float64
	// JSON : arrondi côté Go (MySQL arrondissait à l'assignation, pgx refuse
	// un float64 sur int4).
	lai, err := db.InsertReturningID(ctx, fmt.Sprintf(`
		INSERT INTO cash_registers
		(cash_desk_id, device_id, user_id, merchant_id, cash_fund, start_date, closure_comment, closing_mode)
		VALUES (?, ?, ?, ?, ?, %s, '', ?)
	`, dbx.UTCNow()), "cash_register_id",
		req.CashRegister.CashDeskID,
		req.DeviceID,
		req.CashRegister.UserID,
		merchantID,
		int(math.Round(req.CashRegister.CashFund)),
		closingMode,
	)
	if err != nil {
		log.Error("Erreur lors de l'insertion du registre", zap.Error(err))
		return nil, err
	}

	cashRegisterID := strconv.Itoa(int(lai))

	return &models.CashRegisterOpenResponse{
		Status: "cash_register_created",
		CashRegister: &models.CashRegisterOpen{
			CashRegisterId: cashRegisterID,
			ClosingMode:    closingMode,
		},
	}, nil
}

// La ventilation TVA du registre (ex-procédure GET_CASH_REGISTER_REPORT) est
// calculée par computeRegisterVAT (register_vat.go).

// Traduction SQL inline de l'ex-procédure stockée MySQL
// GET_CASH_REGISTER_REPORT_MOP. Le corps d'origine portait deux filtres
// commentés (o.cash_register_id = ? et o.state IN ('CLOSED','DONE')) —
// volontairement non repris, seul p.cash_register_id fait foi (cohérent avec
// la requalification des paiements à la clôture, cf. rapports 19/20).
// SUM(...) est re-casté en DECIMAL(20,0) : p.amount est un entier (centimes),
// mais round(numeric, 2) Postgres produirait un "123.00" que database/sql ne
// sait pas scanner dans un int Go.
// Filtre brand_status volontairement inchangé (pas de DENIED) : cette requête
// produit l'instantané figé cash_registers_items, comparé ensuite au recalcul
// live de pos/accounting.GetTrustedEnclosedRegisterIDs ; changer le filtre
// d'un seul côté ferait dériver les registres déjà enclosed. Les paiements
// d'une commande DENIED sont désactivés au refus (DisablePayments) et donc
// déjà exclus par p.enabled.
const cashRegisterReportMOPSQL = `
SELECT p.mop, CAST(SUM(ROUND(p.amount, 2)) AS DECIMAL(20,0)) AS amount
FROM orders o
INNER JOIN payments p ON p.order_id = o.order_id
WHERE p.cash_register_id = ?
  AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND p.enabled IS TRUE
GROUP BY p.mop`

func (r *CashRegisterRepository) queryCashRegisterReportMOP(ctx context.Context, cashRegisterID string) ([]models.MOPLine, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, cashRegisterReportMOPSQL, cashRegisterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []models.MOPLine
	for rows.Next() {
		var line models.MOPLine
		if err := rows.Scan(&line.MOP, &line.Amount); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func (r *CashRegisterRepository) GetCashRegisterReport(ctx context.Context, cashRegisterID string) (*models.CashRegisterReport, error) {
	db := dbx.GetDB(ctx, r.database)

	// cash_registers.cash_register_id est un PK numérique dans les deux
	// dialectes (integer en Postgres) — le paramètre doit être typé int.
	regID, err := strconv.Atoi(cashRegisterID)
	if err != nil {
		return nil, fmt.Errorf("cash_register_id non numérique %q: %w", cashRegisterID, err)
	}

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
	`, regID)

	err = row.Scan(&startDate, &endDate, &cashFund)
	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------------
	// 2) Ventilation TVA (ex CALL GET_CASH_REGISTER_REPORT)
	// --------------------------------------------------------------
	vat, err := r.computeRegisterVAT(ctx, cashRegisterID)
	if err != nil {
		return nil, err
	}
	reportRows := vat.Lines

	var (
		HT_CR  int
		TTC_CR int
		TVA_CR int
	)
	for _, rr := range reportRows {
		HT_CR += rr.HT
		TTC_CR += rr.TTC
		TVA_CR += rr.TVA
	}

	// --------------------------------------------------------------
	// 3) Ventilation MOP (ex CALL GET_CASH_REGISTER_REPORT_MOP)
	// --------------------------------------------------------------
	mopList, err := r.queryCashRegisterReportMOP(ctx, cashRegisterID)
	if err != nil {
		return nil, err
	}

	var TTC_MOP int
	for _, line := range mopList {
		TTC_MOP += line.Amount
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
						Rate:     r.Rate,
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
		GrossTTC:       vat.GrossTTC,
		Discounts:      vat.Discounts,
		CashReport:     cashReport,
		// MOP alimente l'instantané figé cash_registers_items à la fermeture :
		// remises comprises, inchangé (le contrôle de dérive de l'export
		// comptable le compare à un recalcul en direct). Elles sont écartées à
		// l'affichage (résumé, détail TVA, historique).
		MOP:            mopList,
		CashReportType: "Z",
	}

	//tx.Commit()

	return res, nil
}

// paymentsRequalifySQL retourne l'UPDATE de requalification des paiements vers
// la caisse qui se ferme (étapes 2 / 3 / 3bis de CloseCashRegister), selon le
// dialecte : MySQL utilise UPDATE ... INNER JOIN, Postgres UPDATE ... FROM
// (cible SET non qualifiée — PG refuse `SET p.colonne`). `cond` porte les
// filtres mop/cash_register_id, avec l'alias p. valide dans les deux formes.
// Arguments attendus, dans l'ordre : cashRegisterID, merchantID.
func paymentsRequalifySQL(cond string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return `
			UPDATE payments p
			SET cash_register_id = ?
			FROM orders o
			WHERE o.order_id = p.order_id
			  AND o.state = 'CLOSED'
			  AND ` + cond + `
			  AND p.merchant_id = ?`
	}
	return `
		UPDATE payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		SET p.cash_register_id = ?
		WHERE o.state = 'CLOSED'
		  AND ` + cond + `
		  AND p.merchant_id = ?`
}

// errCashRegisterClosedConcurrently : une fermeture concurrente du même
// registre a été validée entre le contrôle « déjà fermé » et le scellement.
// Fait annuler la transaction (sinon les lignes du Z seraient insérées une
// seconde fois), puis est rendu à l'appelant comme un registre déjà fermé.
var errCashRegisterClosedConcurrently = errors.New("cash register closed concurrently")

// CloseCashRegister ferme et scelle un registre dans une seule transaction
// (ou celle de l'appelant) : une erreur en cours de route n'y laisse plus les
// lignes du Z insérées sans fermeture, qu'une nouvelle tentative dupliquait
// (lot A conformité caisse, solution C). Le verrou de la chaîne
// n'est pris qu'au scellement, pas pendant le calcul du rapport.
func (r *CashRegisterRepository) CloseCashRegister(ctx context.Context, cashRegisterID string, merchantID string, req *models.CloseCashRegisterRequest) (bool, error) {
	var alreadyClosed bool
	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		var err error
		alreadyClosed, err = r.closeCashRegister(txCtx, cashRegisterID, merchantID, req)
		return err
	})
	if errors.Is(err, errCashRegisterClosedConcurrently) {
		return true, nil
	}
	return alreadyClosed, err
}

func (r *CashRegisterRepository) closeCashRegister(ctx context.Context, cashRegisterID string, merchantID string, req *models.CloseCashRegisterRequest) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	alreadyClosed, err := r.isCashRegisterClosedForMerchant(ctx, cashRegisterID, merchantID)
	if err != nil {
		return false, err
	}
	if alreadyClosed {
		return true, nil
	}

	// 1. Vérifier commandes encore ouvertes
	var tmp string
	err = db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT o.order_id
		FROM orders o
		WHERE o.cash_register_id = ?
		AND o.state NOT IN ('CLOSED','DONE')
		AND (o.scheduled = false OR (o.scheduled = true AND %s > o.estimated_ready))
		LIMIT 1
	`, dbx.UTCNow()), cashRegisterID).Scan(&tmp)

	if err == nil {
		log.Warn("Orders still opened, cannot close cash register", zap.String("cash_register_id", cashRegisterID))
		return false, models.ErrOrdersStillOpened
	} else if err != sql.ErrNoRows {
		return false, err
	}

	// 2. Associer paiements (STRIPE)
	_, err = db.ExecContext(ctx, paymentsRequalifySQL(
		`p.mop = 'STRIPE' AND p.cash_register_id = 'SCANNORDER'`,
	), cashRegisterID, merchantID)
	if err != nil {
		return false, err
	}

	// 3. Associer paiements Uber / Deliveroo
	_, err = db.ExecContext(ctx, paymentsRequalifySQL(
		`p.mop IN ('UBER_EATS','DELIVEROO')
		  AND (p.cash_register_id IS NULL OR p.cash_register_id IN ('UBER_EATS','DELIVEROO'))`,
	), cashRegisterID, merchantID)
	if err != nil {
		return false, err
	}

	// 3bis. Associer paiements borne Kiosk (Stripe Terminal, MOP 'KIOSK' —
	// recordTerminalPayment, docs/KIOSK_DECISIONS.md) : tout paiement 'KIOSK'
	// d'une commande clôturée sans registre associé est rattaché à la première
	// caisse du merchant qui se ferme ensuite, pour qu'il apparaisse dans son
	// rapport Z.
	_, err = db.ExecContext(ctx, paymentsRequalifySQL(
		`p.mop = 'KIOSK' AND (p.cash_register_id IS NULL OR p.cash_register_id = 'KIOSK')`,
	), cashRegisterID, merchantID)
	if err != nil {
		return false, err
	}

	// 4. Récupérer rapport caisse
	report, err := r.GetCashRegisterReport(ctx, cashRegisterID)
	if err != nil {
		return false, err
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
		return false, fmt.Errorf("erreur récupération fond de caisse initial: %w", err)
	}

	// B. Isoler les ventes en espèces depuis le rapport et insérer les items
	var cashSales int
	for _, mopLine := range report.MOP {
		_, err := db.ExecContext(ctx, `
			INSERT INTO cash_registers_items (cash_register_id, mop, amount)
			VALUES (?, ?, ?)
		`, cashRegisterID, mopLine.MOP, mopLine.Amount)
		if err != nil {
			return false, err
		}

		if mopLine.MOP == "ES" {
			cashSales += mopLine.Amount
		}
	}

	// C. Calcul du fond de caisse final
	calculatedFinalCash := initialCashFund + cashSales
	// -------------------------------------------------------------

	// 6. LOGIQUE FISCALE : verrou de l'établissement, puis dernière fermeture
	// chaînée (les fermetures sans empreinte, antérieures à la chaîne, sont
	// ignorées).
	if err := fiscal.LockChain(ctx, fiscal.ChainCashRegisters, merchantID); err != nil {
		return false, err
	}
	var prevHash sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT hash FROM cash_registers
		WHERE merchant_id = ? AND hash IS NOT NULL AND cash_register_id != ?
		ORDER BY end_date DESC LIMIT 1
	`, merchantID, cashRegisterID).Scan(&prevHash)

	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("erreur récupération prev_hash: %w", err)
	}
	actualPrevHash := fiscal.PrevOrGenesis(prevHash.String)

	// 7. LOGIQUE FISCALE : empreinte v2 de la fermeture (dates, fonds de caisse
	// et lignes du Z ; l'ancienne formule ne couvrait que le fond final).
	closedAt := fiscal.Now()
	payload, err := fiscal.LoadCashRegisterClosure(ctx, db, cashRegisterID, closedAt, calculatedFinalCash)
	if err != nil {
		return false, err
	}
	newHash, signature, err := fiscal.Seal(fiscal.ChainCashRegisters, actualPrevHash, payload)
	if err != nil {
		return false, err
	}

	// 8. Fermer le registre (avec MAJ des infos fiscales et du calculatedFinalCash)
	res, err := db.ExecContext(ctx, `
		UPDATE cash_registers
		SET end_date = ?,
			closed = true,
			final_cash_fund = ?,
			previous_hash = ?,
			hash = ?,
			signature = ?,
			hash_version = ?
		WHERE cash_register_id = ?
			AND closed = false
	`, closedAt, calculatedFinalCash, actualPrevHash, newHash, signature, fiscal.HashVersion, cashRegisterID)

	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, err
	} else if n == 0 {
		return false, errCashRegisterClosedConcurrently
	}

	return false, nil
}

// isCashRegisterClosedForMerchant indique si le registre est déjà fermé, pour
// l'établissement donné. Un registre introuvable pour cet établissement
// (inexistant, ou d'un autre établissement) compte comme déjà fermé : la
// fermeture est alors un no-op, au lieu de fermer le registre d'un autre
// établissement et d'y rattacher les paiements de l'appelant (comportement
// d'avant 2026-10-04, cf. CashRegisterBelongsToMerchant).
func (r *CashRegisterRepository) isCashRegisterClosedForMerchant(ctx context.Context, cashRegisterID, merchantID string) (bool, error) {
	owned, err := r.CashRegisterBelongsToMerchant(ctx, cashRegisterID, merchantID)
	if err != nil {
		return false, err
	}
	if !owned {
		return true, nil
	}

	db := dbx.GetDB(ctx, r.database)

	// FOR UPDATE : appelée en tête de la fermeture, verrouille le registre
	// jusqu'au commit. Une annulation de paiement concurrente
	// (fiscal.CancelPayments, qui le verrouille en partage) attend la fin de
	// la fermeture puis est refusée, ou la fermeture attend l'annulation et
	// calcule son Z sans le paiement annulé (lot C conformité caisse).
	var closed bool
	err = db.QueryRowContext(ctx, `
		SELECT closed
		FROM cash_registers
		WHERE cash_register_id = ?
		LIMIT 1
		FOR UPDATE
	`, cashRegisterID).Scan(&closed)

	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	return closed, nil
}

func (r *CashRegisterRepository) GetCashRegisterSummary(ctx context.Context, cashRegisterID string, merchantID string) (*models.CashRegisterSummaryResponse, error) {
	db := dbx.GetDB(ctx, r.database)
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
			cr.closed, cr.enclosed, cr.closure_comment,
			COALESCE(cr.closing_mode, 'MANUAL'),
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
	var closed, enclosed bool

	err := row.Scan(
		&cr.CashRegisterID,
		&start_date_temp,
		&end_date_temp,
		&cr.CashDesk.CashDeskID,
		&cr.CashDesk.CashDeskName,
		&cr.CashFund,
		&cr.FinalCashFund,
		&closed,
		&enclosed,
		&cr.ClosureComment,
		&cr.ClosingMode,
		&cr.OpenedBy.UserID,
		&cr.OpenedBy.FirstName,
		&cr.OpenedBy.LastName,
		&cr.Currency,
		&cr.ClosedBy.UserID,
		&cr.ClosedBy.FirstName,
		&cr.ClosedBy.LastName,
	)
	cr.Closed = closed
	cr.Enclosed = enclosed
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
		// Remises de caisse : figées dans l'instantané (contrôle de dérive de
		// l'export comptable) mais ce ne sont pas des encaissements — hors du
		// théorique, totalisées à part (cf. register_vat.go).
		if models.IsDiscountMOP(it.MOP) {
			cr.Discounts += int(math.Round(it.Amount))
			continue
		}
		cr.Items = append(cr.Items, it)
	}

	// ----------------------------------------------------------------
	// CUSTOM ITEMS
	// ----------------------------------------------------------------
	customRows, err := db.QueryContext(ctx, `
		SELECT crci.id,
		       crci.label AS mop,
		       COALESCE(NULLIF(TRIM(l.label), ''), crci.label) AS label,
		       crci.amount
		FROM cash_registers_custom_items crci
		LEFT JOIN labels l ON l.label_value = crci.label
			AND l.lang = 'FR'
		WHERE cash_register_id = ?
		  AND enabled = TRUE
		ORDER BY id ASC
	`, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer customRows.Close()

	for customRows.Next() {
		var ci models.CRCustomItem
		err := customRows.Scan(&ci.ItemID, &ci.MOP, &ci.Label, &ci.Amount)
		if err != nil {
			return nil, err
		}
		ci.Currency = cr.Currency
		// Une remise recopiée dans le relevé (avant la phase 4, le théorique
		// la proposait) n'est pas de l'argent compté : hors du réel, comme
		// elle est hors du théorique, l'écart reste le même.
		if models.IsDiscountPaymentLabel(ci.MOP) || models.IsDiscountPaymentLabel(ci.Label) {
			continue
		}
		cr.CustomItems = append(cr.CustomItems, ci)
	}

	// ----------------------------------------------------------------
	// ORDERS
	// ----------------------------------------------------------------
	ordersRows, err := db.QueryContext(ctx, `
		SELECT o.order_id, o.order_num, o.creation_date, o.price, o.status,
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
	// p.enabled est boolean en Postgres mais scanné dans un int Go (CRPayment.Enabled) :
	// CASE 1/0 valide dans les deux dialectes (MySQL évalue le tinyint comme booléen).
	payRows, err := db.QueryContext(ctx, `
		SELECT p.order_id, o.order_num, p.payment_id, p.amount, p.mop,
		       CASE WHEN p.enabled THEN 1 ELSE 0 END AS enabled,
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
		if models.IsDiscountMOP(p.MOP) {
			continue
		}
		cr.Payments = append(cr.Payments, p)
	}

	return &models.CashRegisterSummaryResponse{
		Status:       "success",
		CashRegister: &cr,
	}, nil
}

func (r *CashRegisterRepository) AddCustomItem(ctx context.Context, cashRegisterID string, req *models.AddCustomItemRequest, user *auth.UserLoginRow) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if !r.isCashRegisterEditable(ctx, cashRegisterID) {
		return "", models.ErrCashRegisterStillOpen
	}

	// Insert custom item
	insertID, err := db.InsertReturningID(ctx, `
		INSERT INTO cash_registers_custom_items (label, amount, cash_register_id, merchant_id, created_by)
		VALUES (?, ?, ?, ?, ?)
	`, "id", req.Label, req.Value, cashRegisterID, user.MerchantID, user.UserID)

	if err != nil {
		log.Error(err.Error())
		return "", err
	}

	return fmt.Sprintf("%d", insertID), nil
}

func (r *CashRegisterRepository) isCashRegisterClosed(ctx context.Context, cashRegisterID string) bool {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Check if cash register is closed (using the closed column)
	var closed bool
	err := db.QueryRowContext(ctx, `
		SELECT closed
		FROM cash_registers
		WHERE cash_register_id = ?
	`, cashRegisterID).Scan(&closed)

	if err != nil {
		log.Error(err.Error())
		return false // If error, consider not closed
	}
	return closed
}

// isCashRegisterEditable indique si les custom items du registre peuvent
// encore être modifiés : clôturé (closed) mais pas encore verrouillé
// définitivement (enclosed). Distinct de isCashRegisterClosed — celui-ci
// reste utilisé tel quel par GetCashRegisterSummary (une commande doit
// pouvoir consulter un registre déjà enclosed) et EncloseCashRegister (ne
// doit pas se comporter différemment pour un registre déjà enclosed). Une
// fois enclosed, cash_registers_items/cash_registers_custom_items doivent
// rester immuables — c'est l'hypothèse dont dépend le "réel" du rapport
// comptable (accounting.GetRealPaymentsData).
func (r *CashRegisterRepository) isCashRegisterEditable(ctx context.Context, cashRegisterID string) bool {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var closed, enclosed bool
	err := db.QueryRowContext(ctx, `
		SELECT closed, enclosed
		FROM cash_registers
		WHERE cash_register_id = ?
	`, cashRegisterID).Scan(&closed, &enclosed)

	if err != nil {
		log.Error(err.Error())
		return false // If error, consider not editable
	}
	return closed && !enclosed
}

func (r *CashRegisterRepository) DeleteCustomItem(ctx context.Context, cashRegisterID string, itemID string, user *auth.UserLoginRow) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if !r.isCashRegisterEditable(ctx, cashRegisterID) {
		return models.ErrCashRegisterStillOpen
	}

	_, err := db.ExecContext(ctx, `
		UPDATE cash_registers_custom_items
		SET enabled = FALSE
		WHERE cash_register_id = ? AND id = ? AND merchant_id = ?
	`, cashRegisterID, itemID, user.MerchantID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	return nil
}

func (r *CashRegisterRepository) EncloseCashRegister(ctx context.Context, userID, cashRegisterID, comment string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	if !r.isCashRegisterClosed(ctx, cashRegisterID) {
		return models.ErrCashRegisterStillOpen
	}

	// Update: set enclosed (not closed!). Cibles SET non qualifiées : Postgres
	// refuse `SET alias.colonne` (valide aussi en MySQL sans alias).
	_, err := db.ExecContext(ctx, `
		UPDATE cash_registers
		SET enclosed = true,
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

func (r *CashRegisterRepository) GetCashRegisterHistory(ctx context.Context, merchantID string, userID string, req CashRegisterHistoryRequest) (*CashRegisterHistoryResult, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// ==========================================
	// 1️⃣ CONSTRUCTION DU WHERE + ARGUMENTS
	// ==========================================
	// On garde ta logique complexe de droits (Admin ou propriétaire)
	where := ` 
        WHERE cd.merchant_id = ? 
          AND (
                cr.user_id = ? 
                OR EXISTS (
                    SELECT 1 
                    FROM users u 
                    INNER JOIN users_rights ur ON ur.user_id = u.user_id 
                    WHERE u.user_id = ? 
                      AND ur.merchant_id = cd.merchant_id 
                      AND ur.admin = TRUE
                )
          ) `

	args := []interface{}{merchantID, userID, userID}

	// Ajout des filtres par date si présents dans la requête.
	if req.DateFrom != nil && strings.TrimSpace(*req.DateFrom) != "" {
		where += " AND cr.start_date >= ? "
		args = append(args, strings.TrimSpace(*req.DateFrom))
	}
	if req.DateTo != nil && strings.TrimSpace(*req.DateTo) != "" {
		where += " AND cr.start_date <= ? "
		args = append(args, strings.TrimSpace(*req.DateTo))
	}
	// Filtre par cash_register_id si fourni
	if req.CashRegisterID != nil && strings.TrimSpace(*req.CashRegisterID) != "" {
		where += " AND cr.cash_register_id = ? "
		args = append(args, strings.TrimSpace(*req.CashRegisterID))
	}

	// ==========================================
	// 2️⃣ CALCUL DE LA PAGINATION
	// ==========================================
	page, limit := req.NormalizedPagination()

	offset := (page - 1) * limit

	countQuery := `
		SELECT COUNT(*)
		FROM cash_registers cr
		INNER JOIN cash_desks cd ON cd.cash_desk_id = cr.cash_desk_id
	` + where

	var totalItems int
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&totalItems); err != nil {
		return nil, err
	}

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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + limit - 1) / limit
	}

	result := &CashRegisterHistoryResult{
		CashRegisters: []CashRegisterHistoryItem{},
		Metadata: models.PaginationMetadata{
			TotalItems:  totalItems,
			TotalPages:  totalPages,
			CurrentPage: page,
			Limit:       limit,
		},
	}

	if len(registerIDs) == 0 {
		return result, nil
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
	// payments.cash_register_id est varchar (colonne hybride id/sentinelles)
	// face au PK integer de cash_registers : MySQL coerçait la jointure,
	// Postgres exige un cast explicite (syntaxe CHAR/TEXT selon dialecte).
	crIDCast := "CAST(cr.cash_register_id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		crIDCast = "CAST(cr.cash_register_id AS TEXT)"
	}
	fullQuery := fmt.Sprintf(`
		SELECT cr.cash_register_id,
		       cr.start_date,
		       cr.end_date,
		 COALESCE(cr.cash_fund, 0) AS cash_fund,
		 COALESCE(cr.final_cash_fund, 0) AS final_cash_fund,
		 cr.closure_comment,
		 NULLIF(TRIM(CONCAT_WS(' ', cb.first_name, cb.last_name)), '') AS closed_by_name,
	       cd.cash_desk_id,
	       cd.name,
		 cr.enclosed,
		 cr.closed,
		 COALESCE(cr.closing_mode, 'MANUAL') AS closing_mode,
		 cr.hash,
		 COALESCE(pstats.transaction_count, 0) AS transaction_count,
		 COALESCE(pstats.total_revenu, 0) AS total_revenu
        FROM cash_registers cr
        INNER JOIN cash_desks cd ON cd.cash_desk_id = cr.cash_desk_id
		LEFT JOIN users cb ON cb.user_id = cr.closed_by
		LEFT JOIN (
			SELECT p.cash_register_id,
			       COUNT(*) AS transaction_count,
			       COALESCE(SUM(p.amount), 0) AS total_revenu
			FROM payments p
			WHERE p.enabled = TRUE
			  AND p.mop NOT IN `+models.DiscountMOPsSQL+`
			GROUP BY p.cash_register_id
		) pstats ON pstats.cash_register_id = %s
        WHERE cr.cash_register_id IN (%s)
        ORDER BY cr.start_date DESC
    `, crIDCast, strings.Join(inParts, ","))

	fullRows, err := db.QueryContext(ctx, fullQuery)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer fullRows.Close()

	history := make([]CashRegisterHistoryItem, 0, len(registerIDs))
	for fullRows.Next() {
		var h CashRegisterHistoryItem
		var rawStartDate, rawEndDate sql.NullTime
		var cashFund, finalCashFund sql.NullInt64
		var closureComment, closedByName sql.NullString
		var hash sql.NullString
		var transactionCount sql.NullInt64
		var totalRevenu sql.NullInt64

		err := fullRows.Scan(
			&h.CashRegisterID,
			&rawStartDate,
			&rawEndDate,
			&cashFund,
			&finalCashFund,
			&closureComment,
			&closedByName,
			&h.CashDesk.CashDeskID,
			&h.CashDesk.CashDeskName,
			&h.Enclosed,
			&h.Closed,
			&h.ClosingMode,
			&hash,
			&transactionCount,
			&totalRevenu,
		)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}

		if rawStartDate.Valid {
			h.StartDate = rawStartDate.Time.UTC().Format(time.RFC3339)
		}
		if rawEndDate.Valid {
			h.EndDate = rawEndDate.Time.UTC().Format(time.RFC3339)
		}
		if cashFund.Valid {
			h.CashFund = int(cashFund.Int64)
		}
		if finalCashFund.Valid {
			h.FinalCashFund = int(finalCashFund.Int64)
		}
		if closureComment.Valid {
			comment := strings.TrimSpace(closureComment.String)
			if comment != "" {
				h.ClosureComment = &comment
			}
		}
		if closedByName.Valid {
			name := strings.TrimSpace(closedByName.String)
			if name != "" {
				h.ClosedByName = &name
			}
		}
		h.PaymentMethods = []MOPLine{}
		if transactionCount.Valid {
			h.TransactionCount = int(transactionCount.Int64)
		}
		if totalRevenu.Valid {
			h.TotalRevenu = int(totalRevenu.Int64)
		}

		if hash.Valid {
			trimmedHash := strings.TrimSpace(hash.String)
			if trimmedHash != "" {
				prefix := trimmedHash
				if len(prefix) > 10 {
					prefix = prefix[:10]
				}
				h.HashPrefix = &prefix
			}
		}

		history = append(history, h)
	}

	if err := fullRows.Err(); err != nil {
		log.Error(err.Error())
		return nil, err
	}

	pmQuery := fmt.Sprintf(`
		SELECT cri.cash_register_id,
		       cri.mop,
		       COALESCE(l.label, cri.mop) AS label,
		       cri.amount
		FROM cash_registers_items cri
		LEFT JOIN labels l ON l.label_value = cri.mop
		  AND l.lang = 'FR'
		  AND l.label_type = 'mop'
		WHERE cri.cash_register_id IN (%s)
		  AND cri.mop NOT IN `+models.DiscountMOPsSQL+`
		ORDER BY cri.id ASC
	`, strings.Join(inParts, ","))

	pmRows, err := db.QueryContext(ctx, pmQuery)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer pmRows.Close()

	historyIndex := make(map[string]int, len(history))
	for i, item := range history {
		historyIndex[item.CashRegisterID] = i
	}

	for pmRows.Next() {
		var cashRegisterID string
		var method MOPLine
		if err := pmRows.Scan(&cashRegisterID, &method.MOP, &method.Label, &method.Amount); err != nil {
			log.Error(err.Error())
			return nil, err
		}

		idx, ok := historyIndex[cashRegisterID]
		if !ok {
			continue
		}
		history[idx].PaymentMethods = append(history[idx].PaymentMethods, method)
	}

	if err := pmRows.Err(); err != nil {
		log.Error(err.Error())
		return nil, err
	}

	result.CashRegisters = history
	return result, nil
}

func (r *CashRegisterRepository) GetCashRegisterTVADetails(ctx context.Context, merchantID, cashRegisterID string) (*models.CashRegisterDetails, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	regID, err := strconv.Atoi(cashRegisterID)
	if err != nil {
		return nil, fmt.Errorf("cash_register_id non numérique %q: %w", cashRegisterID, err)
	}

	// 1. Retrieve header info
	var header struct {
		StartDate string
		EndDate   string
		CashFund  int
	}

	err = db.QueryRowContext(ctx, `
        SELECT start_date, end_date, cash_fund
        FROM cash_registers
        WHERE cash_register_id = ?
        AND merchant_id = ?
    `, regID, merchantID).Scan(
		&header.StartDate, &header.EndDate, &header.CashFund,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// 2. Ventilation TVA, remises déduites (cf. register_vat.go)
	vat, err := r.computeRegisterVAT(ctx, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	items := vat.Lines

	var totalHT, totalTTC, totalTVA int
	for _, line := range items {
		totalHT += line.HT
		totalTTC += line.TTC
		totalTVA += line.TVA
	}

	// 3. Ventilation MOP (ex CALL GET_CASH_REGISTER_REPORT_MOP), remises
	// exclues : ce ne sont pas des moyens de paiement.
	allMops, err := r.queryCashRegisterReportMOP(ctx, cashRegisterID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	mops := make([]models.MOPLine, 0, len(allMops))
	for _, m := range allMops {
		if !models.IsDiscountMOP(m.MOP) {
			mops = append(mops, m)
		}
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
			Rate:     line.Rate,
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
		GrossTTC:       vat.GrossTTC,
		Discounts:      vat.Discounts,
		CashReport:     report,
		MOP:            mops,
		CashReportType: "Z",
	}, nil
}

func (r *CashRegisterRepository) IsCircularDeviceLink(ctx context.Context, deviceID, onBehalfOf string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Auto-liaison : un appareil qui se lie à lui-même est un cycle de longueur
	// 1. Le cas se produit quand l'app propose de « rejoindre » une caisse déjà
	// ouverte sur ce même appareil ; sans ce garde-fou la liaison est écrite et
	// l'appareil se retrouve durablement marqué comme lié à un autre poste.
	if strings.TrimSpace(deviceID) == strings.TrimSpace(onBehalfOf) {
		return true, nil
	}

	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1 FROM device_link WHERE device_id = ? AND on_behalf_of = ?
	`, onBehalfOf, deviceID).Scan(&exists)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		log.Error(err.Error())
		return false, err
	}

	return true, nil
}

func (r *CashRegisterRepository) UpsertDeviceLink(ctx context.Context, deviceID, userID, onBehalfOf string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Upsert par dialecte (PK device_link.device_id) — mêmes paramètres.
	var query string
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
			INSERT INTO device_link (device_id, user_id, on_behalf_of, creation_date)
			VALUES (?, ?, ?, now())
			ON CONFLICT (device_id) DO UPDATE SET
				on_behalf_of = EXCLUDED.on_behalf_of,
				user_id = EXCLUDED.user_id,
				creation_date = now()`
	} else {
		query = `
			INSERT INTO device_link (device_id, user_id, on_behalf_of, creation_date)
			VALUES (?, ?, ?, UTC_TIMESTAMP())
			ON DUPLICATE KEY UPDATE
				on_behalf_of = VALUES(on_behalf_of),
				user_id = VALUES(user_id),
				creation_date = UTC_TIMESTAMP()`
	}

	_, err := db.ExecContext(ctx, query, deviceID, userID, onBehalfOf)
	if err != nil {
		log.Error(err.Error())
	}
	return err
}

func (r *CashRegisterRepository) DeleteDeviceLink(ctx context.Context, deviceID string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	res, err := db.ExecContext(ctx, `DELETE FROM device_link WHERE device_id = ?`, deviceID)
	if err != nil {
		log.Error(err.Error())
		return 0, err
	}

	return res.RowsAffected()
}
