package reports

import (
	"context"
	"database/sql"
	"fmt"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

// ReportsRepository handles data access for reports module
type ReportsRepository struct {
	database *sql.DB
}

// NewReportsRepository creates a new instance of ReportsRepository
func NewReportsRepository(db *sql.DB) *ReportsRepository {
	return &ReportsRepository{database: db}
}

// GetTVAReportData récupère les données de TVA par jour et par type de livraison
func (r *ReportsRepository) GetTVAReportData(ctx context.Context, merchantID, dateFrom, dateTo string) ([]TVADayReport, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	sqlQuery := `
		SELECT 
			DATE_FORMAT(o.creation_date, '%Y-%m-%d') AS report_date,
			o.order_type,
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
			DATE_FORMAT(o_fees.creation_date, '%Y-%m-%d') AS report_date,
			o_fees.order_type,
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
		ORDER BY report_date, order_type, title
	`

	rows, err := db.QueryContext(ctx, sqlQuery, dateFrom, dateTo, merchantID, dateFrom, dateTo, merchantID)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching TVA report data: %v", err))
		return nil, err
	}
	defer rows.Close()

	// Map structure: date -> order_type -> TVATitle -> aggregated data
	dayMap := make(map[string]map[string]map[string]*TVADayData)

	for rows.Next() {
		var date string
		var orderType string
		var title string
		var rate float64
		var ttcCent int64

		if err := rows.Scan(&date, &orderType, &title, &rate, &ttcCent); err != nil {
			log.Error(fmt.Sprintf("Error scanning TVA report row: %v", err))
			return nil, err
		}

		if dayMap[date] == nil {
			dayMap[date] = make(map[string]map[string]*TVADayData)
		}
		if dayMap[date][orderType] == nil {
			dayMap[date][orderType] = make(map[string]*TVADayData)
		}

		if _, exists := dayMap[date][orderType][title]; !exists {
			dayMap[date][orderType][title] = &TVADayData{
				TVATitle: title,
				Rate:     rate,
				TTC:      0,
				HT:       0,
				TVA:      0,
			}
		}

		dayMap[date][orderType][title].TTC += float64(ttcCent)
	}

	if err = rows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating TVA report rows: %v", err))
		return nil, err
	}

	// Convert map to sorted slice
	var result []TVADayReport
	for date := range dayMap {
		dayReport := TVADayReport{
			Date:    date,
			TTCSum:  0,
			HTSum:   0,
			TVASum:  0,
			VATData: []VATDataItem{},
		}

		// Process each order_type for this day
		orderTypeMap := map[string]string{
			"DELIVERY":  "Livraison",
			"TAKE_AWAY": "À emporter",
			"IN":        "Sur place",
		}

		for orderType, tvaMap := range dayMap[date] {
			label := orderTypeMap[orderType]
			if label == "" {
				label = orderType
			}

			for _, data := range tvaMap {
				// Calculate HT and TVA from TTC
				rate := data.Rate
				var ht, tva float64

				if rate == 0 {
					ht = data.TTC
					tva = 0
				} else {
					ht = data.TTC * (100.0 / (100.0 + rate))
					tva = data.TTC - ht
				}

				// Convert centimes to centimes (keep as int64 for storage)
				ttcCent := int64(data.TTC)
				htCent := int64(ht)
				tvaCent := int64(tva)

				dayReport.VATData = append(dayReport.VATData, VATDataItem{
					TVATitle:             data.TVATitle,
					TVADeliveryTypeLabel: label,
					TTC:                  ttcCent,
					HT:                   htCent,
					TVA:                  tvaCent,
				})

				dayReport.TTCSum += ttcCent
				dayReport.HTSum += htCent
				dayReport.TVASum += tvaCent
			}
		}

		result = append(result, dayReport)
	}

	return result, nil
}

// GetPaymentsReportData récupère les données de paiements par jour et par MOP
func (r *ReportsRepository) GetPaymentsReportData(ctx context.Context, merchantID, dateFrom, dateTo string) ([]PaymentsDayReport, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	sqlQuery := `
		SELECT 
			DATE_FORMAT(o.creation_date, '%Y-%m-%d') AS report_date,
			p.mop AS payment_code,
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
		GROUP BY report_date, payment_code, payment_label
		ORDER BY report_date, payment_code
	`

	rows, err := db.QueryContext(ctx, sqlQuery, merchantID, dateFrom, dateTo)
	if err != nil {
		log.Error(fmt.Sprintf("Error fetching payments report data: %v", err))
		return nil, err
	}
	defer rows.Close()

	// Map structure: date -> [PaymentItem]
	dayMap := make(map[string][]PaymentItem)

	for rows.Next() {
		var date string
		var mopCode string
		var label string
		var amount int64

		if err := rows.Scan(&date, &mopCode, &label, &amount); err != nil {
			log.Error(fmt.Sprintf("Error scanning payments report row: %v", err))
			return nil, err
		}

		dayMap[date] = append(dayMap[date], PaymentItem{
			MOP:    mopCode,
			Label:  label,
			Amount: amount, // Already in centimes from DB
		})
	}

	if err = rows.Err(); err != nil {
		log.Error(fmt.Sprintf("Error iterating payments report rows: %v", err))
		return nil, err
	}

	// Convert map to sorted slice
	var result []PaymentsDayReport
	for date := range dayMap {
		result = append(result, PaymentsDayReport{
			Date:     date,
			Payments: dayMap[date],
		})
	}

	return result, nil
}

// Helper struct for internal use
type TVADayData struct {
	TVATitle string
	Rate     float64
	TTC      float64
	HT       float64
	TVA      float64
}
