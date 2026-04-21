package accounting

import (
	"context"
	"database/sql"
	"fmt"
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
			mp.currency
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
	)

	if err == sql.ErrNoRows {
		log.Error("Merchant not found, returning defaults")
		return &MerchantHeader{
			MerchantName: "Nom Établissement",
			SIRET:        "000 000 000 00000",
			VATNumber:    "FR00000000000",
			Address:      "Adresse inconnue",
			Currency:     "EUR",
			Phone:        "N/A",
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
		WHERE o.creation_date >= DATE_FORMAT(?, '%Y-%m-%d 00:00:00')
		  AND o.creation_date <= DATE_FORMAT(?, '%Y-%m-%d 23:59:59')
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
		INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = -1
		WHERE o_fees.creation_date >= DATE_FORMAT(?, '%Y-%m-%d 00:00:00')
		  AND o_fees.creation_date <= DATE_FORMAT(?, '%Y-%m-%d 23:59:59')
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

		// Conversion centimes → euros (division par 100)
		row.TTC = row.TTC / 100
		row.HT = ht / 100
		row.TVA = tva / 100

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
		  AND o.creation_date >= DATE_FORMAT(?, '%Y-%m-%d 00:00:00')
		  AND o.creation_date <= DATE_FORMAT(?, '%Y-%m-%d 23:59:59')
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
			Amount: float64(amount) / 100,
		})
	}

	if err = rows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating payment rows: %v", err))
		return nil, err
	}

	return result, nil
}
