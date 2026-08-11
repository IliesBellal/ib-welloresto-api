package accounting

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
)

// acctCastChar caste une expression en texte selon le dialecte (jointures
// cross-type merchant.id integer vs merchant_id varchar).
func acctCastChar(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(" + expr + " AS TEXT)"
	}
	return "CAST(" + expr + " AS CHAR)"
}

// acctRoundToInt arrondit une expression à l'entier de façon scannable en
// int64 — le ROUND(double precision) de Postgres renvoie un double que
// database/sql refuse de convertir en int64 ; même pattern que
// stats.roundToIntExpr (tva_rate est une colonne real, qui force
// l'arithmétique flottante).
func acctRoundToInt(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "ROUND(CAST(" + expr + " AS numeric), 0)"
	}
	return "ROUND(" + expr + ")"
}

// acctDayStart / acctDayEnd bornent une journée à partir d'un paramètre date
// 'YYYY-MM-DD' (équivalent de DATE_FORMAT(?, '%Y-%m-%d 00:00:00'/'23:59:59')).
func acctDayStart() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(? AS date)"
	}
	return "DATE_FORMAT(?, '%Y-%m-%d 00:00:00')"
}

func acctDayEnd() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(? AS date) + INTERVAL '23:59:59'"
	}
	return "DATE_FORMAT(?, '%Y-%m-%d 23:59:59')"
}

// acctUTCParam formate un instant en littéral SQL UTC ('YYYY-MM-DD HH:MM:SS'),
// directement comparable à orders.creation_date qui est écrit en UTC
// (dbx.UTCNow()). Passer une chaîne plutôt qu'un time.Time évite toute
// réinterprétation de fuseau par le driver, et reste valide dans les deux
// dialectes.
func acctUTCParam(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// acctMonthKey formate un timestamp en 'YYYY-MM' selon le dialecte.
func acctMonthKey(col string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "to_char(" + col + ", 'YYYY-MM')"
	}
	return "DATE_FORMAT(" + col + ", '%Y-%m')"
}

// AccountingRepository handles data access for accounting module
type AccountingRepository struct {
	database *sql.DB
}

// NewAccountingRepository creates a new instance of AccountingRepository
func NewAccountingRepository(db *sql.DB) *AccountingRepository {
	return &AccountingRepository{database: db}
}

// GetMerchantHeader récupère les infos du merchant depuis la BD
func (r *AccountingRepository) GetMerchantHeader(ctx context.Context, merchantID string) (*MerchantHeader, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	sqlQuery := `
		SELECT 
			m.SIRET,
			m.address,
			m.fullName,
			m.merchantTel,
			m.vat_number,
			mp.currency,
			m.timezone
		FROM merchant m
		INNER JOIN merchant_parameters mp ON mp.merchant_id = ` + acctCastChar("m.id") + `
		WHERE m.id = ?
		LIMIT 1
	`

	row := db.QueryRowContext(ctx, sqlQuery, merchantID)

	var header MerchantHeader
	err := row.Scan(
		&header.SIRET,
		&header.Address,
		&header.MerchantName,
		&header.Phone,
		&header.VATNumber,
		&header.Currency,
		&header.Timezone,
	)

	if err == sql.ErrNoRows {
		log.Error("Merchant not found, returning defaults")
		return &MerchantHeader{
			MerchantName: "Nom Établissement",
			SIRET:        "000 000 000 00000",
			VATNumber:    nil,
			Address:      "Adresse inconnue",
			Currency:     "EUR",
			Phone:        "N/A",
			Timezone:     "Europe/Paris",
		}, nil
	}

	if err != nil {
		log.Error(fmt.Sprintf("Error fetching merchant header: %v", err))
		return nil, err
	}

	return &header, nil
}

// IsMonthClosed vérifie si le mois est clôturé. Les bornes sont calculées dans
// le fuseau de l'établissement : un mois n'est terminé qu'une fois minuit passé
// en heure locale, pas en UTC.
func (r *AccountingRepository) IsMonthClosed(ctx context.Context, merchantID string, year, month int, loc *time.Location) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Premier jour du mois 00:00:00 local -> premier jour du mois suivant
	// 00:00:00 local (borne exclusive, gère les mois de 28 à 31 jours et les
	// changements d'heure sans arithmétique manuelle).
	monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, loc)
	monthEnd := monthStart.AddDate(0, 1, 0)

	// Vérifier que la période est dans le passé
	if time.Now().Before(monthEnd) {
		log.Error(fmt.Sprintf("Month not finished yet: %s", monthEnd.Format(time.RFC3339)))
		return false, nil
	}

	// Vérifier que toutes les commandes sont CLOSED
	sqlQuery := `
		SELECT COUNT(*) as not_closed_count
		FROM orders
		WHERE merchant_id = ?
		  AND creation_date >= ?
		  AND creation_date < ?
		  AND state IS NOT NULL
		  AND state <> 'CLOSED'
	`

	row := db.QueryRowContext(ctx, sqlQuery, merchantID, acctUTCParam(monthStart), acctUTCParam(monthEnd))
	var notClosedCount int
	err := row.Scan(&notClosedCount)

	if err != nil {
		log.Error(fmt.Sprintf("Error checking closed orders: %v", err))
		return false, err
	}

	if notClosedCount > 0 {
		log.Error(fmt.Sprintf("Not all orders are closed: %d not closed", notClosedCount))
		return false, nil
	}

	return true, nil
}

// GetTVAData récupère les données de TVA groupées par taux sur [from, toExclusive[.
// Les deux bornes sont des instants absolus (déjà résolus dans le fuseau de
// l'établissement par l'appelant) ; la borne haute est exclusive afin d'inclure
// la dernière seconde du dernier jour et ses fractions.
func (r *AccountingRepository) GetTVAData(ctx context.Context, merchantID string, from, toExclusive time.Time) ([]TVARow, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	fromParam := acctUTCParam(from)
	toParam := acctUTCParam(toExclusive)

	// IFNULL est MySQL-only -> COALESCE (valide dans les deux dialectes)
	sqlQuery := `
		SELECT
			tva.tva_title AS title,
			tva.tva_rate AS rate,
			((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) AS TTC
		FROM orders o
		INNER JOIN orderitems oi ON oi.order_id = o.order_id
		INNER JOIN products p ON p.product_id = oi.product_id
		INNER JOIN tva_categories tva ON tva.tva_id = (
			CASE 
				WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id
				WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id
				ELSE p.tva_in_id
			END
		)
		LEFT JOIN (
			SELECT order_item_id, SUM(extra.price) AS extra_price
			FROM extra
			GROUP BY order_item_id
		) e ON e.order_item_id = oi.order_item_id
		WHERE o.creation_date >= ?
		  AND o.creation_date < ?
		  AND o.merchant_id = ?
		  AND o.state = 'CLOSED'
		  AND o.brand = 'WELLO_RESTO'
		  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
		  AND o.created_by NOT IN ('-1', 'SCANNORDER')
		  AND tva.show_in_report
		UNION ALL
		SELECT
			tva_fees.tva_title AS title,
			tva_fees.tva_rate AS rate,
			o_fees.delivery_fees AS TTC
		FROM orders o_fees
		INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = '-1'
		WHERE o_fees.creation_date >= ?
		  AND o_fees.creation_date < ?
		  AND o_fees.merchant_id = ?
		  AND o_fees.brand = 'WELLO_RESTO'
		  AND o_fees.created_by NOT IN ('-1', 'SCANNORDER')
		  AND o_fees.brand_status NOT IN ('DELETED', 'CANCELED')
		  AND o_fees.state = 'CLOSED'
	`

	rows, err := db.QueryContext(ctx, sqlQuery, fromParam, toParam, merchantID, fromParam, toParam, merchantID)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching TVA data: %v", err))
		return nil, err
	}
	defer rows.Close()

	// Map pour regrouper par titre TVA
	tvaTotals := make(map[string]*TVARow)

	for rows.Next() {
		var title string
		var rate float64
		var ttcCent int64

		if err := rows.Scan(&title, &rate, &ttcCent); err != nil {
			log.Error(fmt.Sprintf("Error scanning TVA row: %v", err))
			return nil, err
		}

		if _, exists := tvaTotals[title]; !exists {
			tvaTotals[title] = &TVARow{
				TVATitle: title,
				Rate:     rate,
				TTC:      0,
				HT:       0,
				TVA:      0,
			}
		}

		tvaTotals[title].TTC += float64(ttcCent)
	}

	if err = rows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating TVA rows: %v", err))
		return nil, err
	}

	// Conversion TTC → HT + TVA
	var result []TVARow
	for _, row := range tvaTotals {
		rate := row.Rate

		var ht, tva float64
		if rate == 0 {
			ht = row.TTC
			tva = 0
		} else {
			ht = row.TTC * (100.0 / (100.0 + rate))
			tva = row.TTC - ht
		}

		row.HT = ht
		row.TVA = tva

		result = append(result, *row)
	}

	return result, nil
}

// GetPaymentsData récupère les données de paiements groupées par moyen sur
// [from, toExclusive[. L'ancrage est la date de création de la commande, pas
// celle du paiement : une commande créée le 31/08 à 23h30 et encaissée le 01/09
// reste rattachée au rapport d'août.
func (r *AccountingRepository) GetPaymentsData(ctx context.Context, merchantID string, from, toExclusive time.Time) ([]PaymentRow, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	sqlQuery := `
		SELECT 
			l.label AS payment_label,
			SUM(p.amount) AS total_amount
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		INNER JOIN labels l 
			ON l.label_type = 'mop' 
			AND l.label_value = p.mop 
			AND l.lang = 'FR'
		WHERE p.merchant_id = ?
		  AND p.enabled = TRUE
		  AND o.creation_date >= ?
		  AND o.creation_date < ?
		  AND o.created_by NOT IN ('-1', 'SCANNORDER')
		  AND o.state = 'CLOSED'
		  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
		  AND o.brand = 'WELLO_RESTO'
		GROUP BY l.label
		ORDER BY l.label
	`

	rows, err := db.QueryContext(ctx, sqlQuery, merchantID, acctUTCParam(from), acctUTCParam(toExclusive))
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching payments data: %v", err))
		return nil, err
	}
	defer rows.Close()

	var result []PaymentRow

	for rows.Next() {
		var label string
		var amount int64

		if err := rows.Scan(&label, &amount); err != nil {
			log.Error(fmt.Sprintf("Error scanning payment row: %v", err))
			return nil, err
		}

		// Conversion centimes → euros (division par 100)
		result = append(result, PaymentRow{
			Label:  label,
			Amount: amount,
		})
	}

	if err = rows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating payment rows: %v", err))
		return nil, err
	}

	return result, nil
}

// accountingExcludedChannelMOPs liste les codes MOP de canaux hors périmètre
// du rapport WELLO_RESTO (Uber Eats/Deliveroo : TVA pas encore gérée par
// notre système ; STRIPE : exclusivement ScanNOrder — commandes déjà exclues
// de GetPaymentsData/GetTVAData via created_by). cash_registers_items /
// cash_registers_custom_items sont déjà agrégés par (cash_register_id, mop) à
// la clôture de caisse et ne portent donc pas nativement le filtre
// brand/created_by des requêtes ci-dessus : cette liste le reproduit pour le
// "réel".
var accountingExcludedChannelMOPs = []string{"STRIPE", "UBER_EATS", "DELIVEROO"}

// GetTrustedEnclosedRegisterIDs retourne les cash_register_id "de confiance"
// pour le réel du rapport comptable : registres enclosed du merchant dont
// start_date tombe dans [from, toExclusive), et dont l'instantané figé à la
// clôture (cash_registers_items) correspond toujours à un recalcul live des
// mêmes paiements. Un écart signale une correction faite après la clôture
// définitive du registre (ex. paiement ajouté a posteriori sur une commande
// dont le registre est déjà enclosed) : le registre entier est alors écarté
// du réel pour ce rapport — pas de repli partiel mélangé, cf. plan.
func (r *AccountingRepository) GetTrustedEnclosedRegisterIDs(ctx context.Context, merchantID string, from, toExclusive time.Time) ([]int64, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	candidateRows, err := db.QueryContext(ctx, `
		SELECT cash_register_id
		FROM cash_registers
		WHERE merchant_id = ?
		  AND enclosed = TRUE
		  AND start_date >= ?
		  AND start_date < ?
	`, merchantID, acctUTCParam(from), acctUTCParam(toExclusive))
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching candidate enclosed registers: %v", err))
		return nil, err
	}
	defer candidateRows.Close()

	var candidateIDs []int64
	for candidateRows.Next() {
		var id int64
		if err := candidateRows.Scan(&id); err != nil {
			log.Error(fmt.Sprintf("Error scanning candidate register id: %v", err))
			return nil, err
		}
		candidateIDs = append(candidateIDs, id)
	}
	if err := candidateRows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating candidate registers: %v", err))
		return nil, err
	}
	if len(candidateIDs) == 0 {
		return nil, nil
	}

	idPlaceholders := make([]string, len(candidateIDs))
	frozenArgs := make([]interface{}, len(candidateIDs))
	liveArgs := make([]interface{}, len(candidateIDs))
	for i, id := range candidateIDs {
		idPlaceholders[i] = "?"
		frozenArgs[i] = id
		liveArgs[i] = strconv.FormatInt(id, 10) // payments.cash_register_id est varchar
	}
	idInClause := strings.Join(idPlaceholders, ",")

	// Instantané figé (cash_registers_items), par (cash_register_id, mop).
	frozenRows, err := db.QueryContext(ctx, `
		SELECT cash_register_id, mop, amount
		FROM cash_registers_items
		WHERE cash_register_id IN (`+idInClause+`)
	`, frozenArgs...)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching frozen cash_registers_items: %v", err))
		return nil, err
	}
	defer frozenRows.Close()

	type registerMOPKey struct {
		registerID int64
		mop        string
	}
	frozen := make(map[registerMOPKey]int64)
	for frozenRows.Next() {
		var key registerMOPKey
		var amount int64
		if err := frozenRows.Scan(&key.registerID, &key.mop, &amount); err != nil {
			log.Error(fmt.Sprintf("Error scanning frozen cash_registers_items row: %v", err))
			return nil, err
		}
		frozen[key] = amount
	}
	if err := frozenRows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating frozen cash_registers_items: %v", err))
		return nil, err
	}

	// Recalcul live des mêmes paiements, mêmes filtres que
	// cashRegisterReportMOPSQL (cash_registers/repository.go) — sans filtre
	// canal/brand, pour rester comparable à ce qui a produit l'instantané.
	liveRows, err := db.QueryContext(ctx, `
		SELECT `+acctCastChar("p.cash_register_id")+` AS cash_register_id, p.mop, SUM(p.amount) AS amount
		FROM orders o
		INNER JOIN payments p ON p.order_id = o.order_id
		WHERE p.cash_register_id IN (`+idInClause+`)
		  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
		  AND p.enabled IS TRUE
		GROUP BY `+acctCastChar("p.cash_register_id")+`, p.mop
	`, liveArgs...)
	if err != nil {
		log.Error(fmt.Sprintf("Error recomputing live cash register totals: %v", err))
		return nil, err
	}
	defer liveRows.Close()

	live := make(map[registerMOPKey]int64)
	for liveRows.Next() {
		var registerIDStr, mop string
		var amount int64
		if err := liveRows.Scan(&registerIDStr, &mop, &amount); err != nil {
			log.Error(fmt.Sprintf("Error scanning live register total row: %v", err))
			return nil, err
		}
		registerID, convErr := strconv.ParseInt(registerIDStr, 10, 64)
		if convErr != nil {
			// cash_register_id non numérique : ne devrait pas arriver pour un
			// registre candidat (sentinelles comme 'SCANNORDER'/'KIOSK' sont
			// requalifiées vers l'id réel avant l'enclose) — ignorer plutôt
			// que planter le rapport.
			continue
		}
		live[registerMOPKey{registerID: registerID, mop: mop}] = amount
	}
	if err := liveRows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating live register totals: %v", err))
		return nil, err
	}

	// Un registre est "de confiance" seulement si toutes ses paires
	// (cash_register_id, mop) — figées comme live — se correspondent
	// exactement. Le moindre écart, ou une clé présente d'un seul côté,
	// écarte le registre entier. On garde le détail par MOP (montant figé vs
	// live) pour que le log d'alerte soit exploitable sans avoir à
	// re-dériver l'écart manuellement.
	type driftDetail struct {
		mop    string
		frozen int64
		live   int64
	}
	driftedDetails := make(map[int64][]driftDetail)
	for key, frozenAmount := range frozen {
		if liveAmount, ok := live[key]; !ok || liveAmount != frozenAmount {
			driftedDetails[key.registerID] = append(driftedDetails[key.registerID], driftDetail{mop: key.mop, frozen: frozenAmount, live: liveAmount})
		}
	}
	for key, liveAmount := range live {
		if _, ok := frozen[key]; !ok {
			driftedDetails[key.registerID] = append(driftedDetails[key.registerID], driftDetail{mop: key.mop, frozen: 0, live: liveAmount})
		}
	}

	trusted := make([]int64, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		if details, ok := driftedDetails[id]; ok {
			parts := make([]string, 0, len(details))
			for _, d := range details {
				parts = append(parts, fmt.Sprintf("mop=%s frozen=%d live=%d", d.mop, d.frozen, d.live))
			}
			log.Warn(fmt.Sprintf("cash register %d (merchant %s) excluded from 'réel' accounting: frozen cash_registers_items no longer matches live payments [%s] — likely a payment corrected after enclose", id, merchantID, strings.Join(parts, ", ")))
			continue
		}
		trusted = append(trusted, id)
	}

	return trusted, nil
}

// GetRealPaymentsData somme le "réel" des registres de caisse passés en
// paramètre (déjà filtrés par GetTrustedEnclosedRegisterIDs) : le montant
// figé par MOP à la clôture (cash_registers_items) plus les ajustements
// manuels actifs (cash_registers_custom_items). Contrairement à
// GetPaymentsData, la jointure vers labels est un LEFT JOIN délibéré : un
// code MOP non libellé ne doit jamais faire disparaître silencieusement un
// montant du total censé être le plus fiable — il apparaît sous son code brut
// à défaut de libellé. Un custom item à texte libre sans correspondance MOP
// apparaît de la même façon comme sa propre ligne (décision produit).
func (r *AccountingRepository) GetRealPaymentsData(ctx context.Context, registerIDs []int64) ([]PaymentRow, error) {
	if len(registerIDs) == 0 {
		return nil, nil
	}

	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	idPlaceholders := make([]string, len(registerIDs))
	for i := range registerIDs {
		idPlaceholders[i] = "?"
	}
	idInClause := strings.Join(idPlaceholders, ",")

	excludedPlaceholders := make([]string, len(accountingExcludedChannelMOPs))
	for i := range accountingExcludedChannelMOPs {
		excludedPlaceholders[i] = "?"
	}
	excludedInClause := strings.Join(excludedPlaceholders, ",")

	sqlQuery := `
		SELECT label, SUM(amount) AS amount
		FROM (
			SELECT COALESCE(NULLIF(l.label, ''), cri.mop) AS label, cri.amount AS amount
			FROM cash_registers_items cri
			LEFT JOIN labels l ON l.label_type = 'mop' AND l.label_value = cri.mop AND l.lang = 'FR'
			WHERE cri.cash_register_id IN (` + idInClause + `)
			  AND cri.mop NOT IN (` + excludedInClause + `)

			UNION ALL

			SELECT COALESCE(NULLIF(l.label, ''), crci.label) AS label, crci.amount AS amount
			FROM cash_registers_custom_items crci
			LEFT JOIN labels l ON l.label_type = 'mop' AND l.label_value = crci.label AND l.lang = 'FR'
			WHERE crci.cash_register_id IN (` + idInClause + `)
			  AND crci.enabled = TRUE
			  AND crci.label NOT IN (` + excludedInClause + `)
		) real_payments
		GROUP BY label
		ORDER BY label
	`

	args := make([]interface{}, 0, 2*(len(registerIDs)+len(accountingExcludedChannelMOPs)))
	for _, id := range registerIDs {
		args = append(args, id)
	}
	for _, mop := range accountingExcludedChannelMOPs {
		args = append(args, mop)
	}
	for _, id := range registerIDs {
		args = append(args, id)
	}
	for _, mop := range accountingExcludedChannelMOPs {
		args = append(args, mop)
	}

	rows, err := db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching real payments data: %v", err))
		return nil, err
	}
	defer rows.Close()

	var result []PaymentRow
	for rows.Next() {
		var row PaymentRow
		if err := rows.Scan(&row.Label, &row.Amount); err != nil {
			log.Error(fmt.Sprintf("Error scanning real payment row: %v", err))
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating real payment rows: %v", err))
		return nil, err
	}

	return result, nil
}

// interpolateQuery replaces query placeholders with actual parameter values for logging/debugging
func interpolateQuery(query string, args []interface{}) string {
	argIndex := 0
	result := ""
	for i := 0; i < len(query); i++ {
		if query[i] == '?' && argIndex < len(args) {
			arg := args[argIndex]
			argIndex++
			switch v := arg.(type) {
			case string:
				result += "'" + strings.ReplaceAll(v, "'", "''") + "'"
			case int, int64:
				result += fmt.Sprintf("%v", v)
			case float64:
				result += strconv.FormatFloat(v, 'f', -1, 64)
			case nil:
				result += "NULL"
			default:
				result += fmt.Sprintf("'%v'", v)
			}
		} else {
			result += string(query[i])
		}
	}
	return result
}

func (r *AccountingRepository) GetVATAggregationRows(
	ctx context.Context,
	merchantID string,
	fromUTC time.Time,
	toUTC time.Time,
	channels []string,
	orderTypes []string,
) ([]VATAggregationRow, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	start := fromUTC.Format("2006-01-02")
	end := toUTC.Format("2006-01-02")

	channelClauseItems, channelArgsItems := buildChannelFilterClause("o", channels)
	orderTypeClauseItems, orderTypeArgsItems := buildOrderTypeFilterClause("o", orderTypes)
	channelClauseFees, channelArgsFees := buildChannelFilterClause("o_fees", channels)
	orderTypeClauseFees, orderTypeArgsFees := buildOrderTypeFilterClause("o_fees", orderTypes)

	baseArgs := []interface{}{start, end, merchantID}
	itemArgs := append([]interface{}{}, baseArgs...)
	itemArgs = append(itemArgs, channelArgsItems...)
	itemArgs = append(itemArgs, orderTypeArgsItems...)

	feesArgs := append([]interface{}{}, baseArgs...)
	feesArgs = append(feesArgs, channelArgsFees...)
	feesArgs = append(feesArgs, orderTypeArgsFees...)

	args := append(itemArgs, feesArgs...)

	// Fragments par dialecte : DATE_FORMAT (clé de mois + bornes de journée)
	// et ROUND scannable en int64 (cf. helpers en tête de fichier).
	itemsTTC := "((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)"
	itemsHT := acctRoundToInt(itemsTTC + " * 100.0 / (100.0 + tva.tva_rate)")
	feesHT := acctRoundToInt("o_fees.delivery_fees * 100.0 / (100.0 + tva_fees.tva_rate)")

	query := fmt.Sprintf(`
		SELECT month_key, channel, order_type, rate,
		       SUM(ttc_cents) AS ttc_cents,
		       SUM(ht_cents) AS ht_cents,
		       SUM(vat_cents) AS vat_cents
		FROM (
			SELECT
				%s AS month_key,
				CASE
					WHEN o.brand = 'UBER_EATS' THEN 'ubereats'
					WHEN o.brand = 'DELIVEROO' THEN 'deliveroo'
					WHEN o.brand = 'WELLO_RESTO' AND o.created_by = 'SCANNORDER' THEN 'scannorder'
					ELSE 'restaurant'
				END AS channel,
				LOWER(o.order_type) AS order_type,
				tva.tva_rate AS rate,
				%s AS ttc_cents,
				%s AS ht_cents,
				%s - %s AS vat_cents
			FROM orders o
			INNER JOIN orderitems oi ON oi.order_id = o.order_id
			INNER JOIN products p ON p.product_id = oi.product_id
			INNER JOIN tva_categories tva ON tva.tva_id = (
				CASE
					WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id
					WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id
					ELSE p.tva_in_id
				END
			)
			LEFT JOIN (
				SELECT order_item_id, SUM(extra.price) AS extra_price
				FROM extra
				GROUP BY order_item_id
			) e ON e.order_item_id = oi.order_item_id
			WHERE o.creation_date >= %s
			  AND o.creation_date <= %s
			  AND o.merchant_id = ?
			  AND o.state = 'CLOSED'
			  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
			  AND tva.show_in_report
			  %s
			  %s

			UNION ALL

			SELECT
				%s AS month_key,
				CASE
					WHEN o_fees.brand = 'UBER_EATS' THEN 'ubereats'
					WHEN o_fees.brand = 'DELIVEROO' THEN 'deliveroo'
					WHEN o_fees.brand = 'WELLO_RESTO' AND o_fees.created_by = 'SCANNORDER' THEN 'scannorder'
					ELSE 'restaurant'
				END AS channel,
				LOWER(o_fees.order_type) AS order_type,
				tva_fees.tva_rate AS rate,
				o_fees.delivery_fees AS ttc_cents,
				%s AS ht_cents,
				o_fees.delivery_fees - %s AS vat_cents
			FROM orders o_fees
			INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = -1
			WHERE o_fees.creation_date >= %s
			  AND o_fees.creation_date <= %s
			  AND o_fees.merchant_id = ?
			  AND o_fees.state = 'CLOSED'
			  AND o_fees.brand_status NOT IN ('DELETED', 'CANCELED')
			  AND o_fees.delivery_fees > 0
			  %s
			  %s
		) agg
		GROUP BY month_key, channel, order_type, rate
		ORDER BY month_key, channel, order_type, rate
	`,
		acctMonthKey("o.creation_date"),
		itemsTTC, itemsHT, itemsTTC, itemsHT,
		acctDayStart(), acctDayEnd(),
		channelClauseItems, orderTypeClauseItems,
		acctMonthKey("o_fees.creation_date"),
		feesHT, feesHT,
		acctDayStart(), acctDayEnd(),
		channelClauseFees, orderTypeClauseFees)

	// Log the fully interpolated query for debugging
	finalQuery := interpolateQuery(query, args)
	log.Info(fmt.Sprintf("VAT Query Executed:\n%s", finalQuery))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching VAT aggregation rows: %v", err))
		return nil, err
	}
	defer rows.Close()

	out := make([]VATAggregationRow, 0)
	for rows.Next() {
		var row VATAggregationRow
		if err := rows.Scan(&row.Month, &row.Channel, &row.OrderType, &row.Rate, &row.TTCCents, &row.HTCents, &row.VATCents); err != nil {
			return nil, err
		}
		out = append(out, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func buildChannelFilterClause(alias string, channels []string) (string, []interface{}) {
	if len(channels) == 0 {
		return "", nil
	}

	placeholders := make([]string, 0, len(channels))
	args := make([]interface{}, 0, len(channels))
	for _, c := range channels {
		placeholders = append(placeholders, "?")
		args = append(args, c)
	}

	caseExpr := fmt.Sprintf(`CASE
		WHEN %s.brand = 'UBER_EATS' THEN 'ubereats'
		WHEN %s.brand = 'DELIVEROO' THEN 'deliveroo'
		WHEN %s.brand = 'WELLO_RESTO' AND %s.created_by = 'SCANNORDER' THEN 'scannorder'
		ELSE 'restaurant'
	END`, alias, alias, alias, alias)

	return " AND " + caseExpr + " IN (" + strings.Join(placeholders, ",") + ")", args
}

func buildOrderTypeFilterClause(alias string, orderTypes []string) (string, []interface{}) {
	if len(orderTypes) == 0 {
		return "", nil
	}

	placeholders := make([]string, 0, len(orderTypes))
	args := make([]interface{}, 0, len(orderTypes))
	for _, t := range orderTypes {
		placeholders = append(placeholders, "?")
		args = append(args, strings.ToUpper(t))
	}

	return " AND " + alias + ".order_type IN (" + strings.Join(placeholders, ",") + ")", args
}
