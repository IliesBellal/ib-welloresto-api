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
