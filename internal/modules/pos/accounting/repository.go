package accounting

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

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
	db := dbutils.GetDB(ctx, r.database)
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
		INNER JOIN merchant_parameters mp ON mp.merchant_id = m.id
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

// IsMonthClosed vérifie si le mois est clôturé
func (r *AccountingRepository) IsMonthClosed(ctx context.Context, merchantID, year, month string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Vérifier que la période est dans le passé
	monthPad := fmt.Sprintf("%02s", month)

	// Calculate last day of month
	var y, m int
	fmt.Sscanf(year, "%d", &y)
	fmt.Sscanf(month, "%d", &m)

	lastDay := time.Date(y, time.Month(m+1), 0, 23, 59, 59, 0, time.UTC).Day()
	endDate := fmt.Sprintf("%s-%s-%02d 23:59:59", year, monthPad, lastDay)

	now := time.Now().UTC()
	endTime, _ := time.Parse("2006-01-02 15:04:05", endDate)

	if now.Before(endTime) {
		log.Error(fmt.Sprintf("Month not finished yet: %s", endDate))
		return false, nil
	}

	// Vérifier que toutes les commandes sont CLOSED
	startDate := fmt.Sprintf("%s-%s-01 00:00:00", year, monthPad)
	sqlQuery := `
		SELECT COUNT(*) as not_closed_count
		FROM orders
		WHERE merchant_id = ?
		  AND creation_date >= ?
		  AND creation_date <= ?
		  AND state IS NOT NULL
		  AND state <> 'CLOSED'
	`

	row := db.QueryRowContext(ctx, sqlQuery, merchantID, startDate, endDate)
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

// GetTVAData récupère les données de TVA groupées par taux
func (r *AccountingRepository) GetTVAData(ctx context.Context, merchantID, dateFrom, dateTo string) ([]TVARow, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	sqlQuery := `
		SELECT 
			tva.tva_title AS title,
			tva.tva_rate AS rate,
			((oi.price + IFNULL(e.extra_price, 0)) * oi.quantity) AS TTC
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
		  AND o.creation_date <= ?
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
		  AND o_fees.creation_date <= ?
		  AND o_fees.merchant_id = ?
		  AND o_fees.brand = 'WELLO_RESTO'
		  AND o_fees.created_by NOT IN ('-1', 'SCANNORDER')
		  AND o_fees.brand_status NOT IN ('DELETED', 'CANCELED')
		  AND o_fees.state = 'CLOSED'
	`

	rows, err := db.QueryContext(ctx, sqlQuery, dateFrom, dateTo, merchantID, dateFrom, dateTo, merchantID)
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

// GetPaymentsData récupère les données de paiements groupées par moyen
func (r *AccountingRepository) GetPaymentsData(ctx context.Context, merchantID, dateFrom, dateTo string) ([]PaymentRow, error) {
	db := dbutils.GetDB(ctx, r.database)
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
		  AND p.enabled = 1
		  AND o.creation_date >= ?
		  AND o.creation_date <= ?
		  AND o.created_by NOT IN ('-1', 'SCANNORDER')
		  AND o.state = 'CLOSED'
		  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
		  AND o.brand = 'WELLO_RESTO'
		GROUP BY l.label
		ORDER BY l.label
	`

	rows, err := db.QueryContext(ctx, sqlQuery, merchantID, dateFrom, dateTo)
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
	db := dbutils.GetDB(ctx, r.database)
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

	query := fmt.Sprintf(`
		SELECT month_key, channel, order_type, rate,
		       SUM(ttc_cents) AS ttc_cents,
		       SUM(ht_cents) AS ht_cents,
		       SUM(vat_cents) AS vat_cents
		FROM (
			SELECT
				DATE_FORMAT(o.creation_date, '%%Y-%%m') AS month_key,
				CASE
					WHEN o.brand = 'UBER_EATS' THEN 'ubereats'
					WHEN o.brand = 'DELIVEROO' THEN 'deliveroo'
					WHEN o.brand = 'WELLO_RESTO' AND o.created_by = 'SCANNORDER' THEN 'scannorder'
					ELSE 'restaurant'
				END AS channel,
				LOWER(o.order_type) AS order_type,
				tva.tva_rate AS rate,
				((oi.price + IFNULL(e.extra_price, 0)) * oi.quantity) AS ttc_cents,
				ROUND(((oi.price + IFNULL(e.extra_price, 0)) * oi.quantity) * 100.0 / (100.0 + tva.tva_rate)) AS ht_cents,
				((oi.price + IFNULL(e.extra_price, 0)) * oi.quantity) - ROUND(((oi.price + IFNULL(e.extra_price, 0)) * oi.quantity) * 100.0 / (100.0 + tva.tva_rate)) AS vat_cents
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
			WHERE o.creation_date >= DATE_FORMAT(?, '%%Y-%%m-%%d 00:00:00')
			  AND o.creation_date <= DATE_FORMAT(?, '%%Y-%%m-%%d 23:59:59')
			  AND o.merchant_id = ?
			  AND o.state = 'CLOSED'
			  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
			  AND tva.show_in_report
			  %s
			  %s

			UNION ALL

			SELECT
				DATE_FORMAT(o_fees.creation_date, '%%Y-%%m') AS month_key,
				CASE
					WHEN o_fees.brand = 'UBER_EATS' THEN 'ubereats'
					WHEN o_fees.brand = 'DELIVEROO' THEN 'deliveroo'
					WHEN o_fees.brand = 'WELLO_RESTO' AND o_fees.created_by = 'SCANNORDER' THEN 'scannorder'
					ELSE 'restaurant'
				END AS channel,
				LOWER(o_fees.order_type) AS order_type,
				tva_fees.tva_rate AS rate,
				o_fees.delivery_fees AS ttc_cents,
				ROUND(o_fees.delivery_fees * 100.0 / (100.0 + tva_fees.tva_rate)) AS ht_cents,
				o_fees.delivery_fees - ROUND(o_fees.delivery_fees * 100.0 / (100.0 + tva_fees.tva_rate)) AS vat_cents
			FROM orders o_fees
			INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = -1
			WHERE o_fees.creation_date >= DATE_FORMAT(?, '%%Y-%%m-%%d 00:00:00')
			  AND o_fees.creation_date <= DATE_FORMAT(?, '%%Y-%%m-%%d 23:59:59')
			  AND o_fees.merchant_id = ?
			  AND o_fees.state = 'CLOSED'
			  AND o_fees.brand_status NOT IN ('DELETED', 'CANCELED')
			  AND o_fees.delivery_fees > 0
			  %s
			  %s
		) agg
		GROUP BY month_key, channel, order_type, rate
		ORDER BY month_key, channel, order_type, rate
	`, channelClauseItems, orderTypeClauseItems, channelClauseFees, orderTypeClauseFees)

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
