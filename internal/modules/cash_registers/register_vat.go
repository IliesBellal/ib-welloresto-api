package cash_registers

import (
	"context"
	"fmt"
	"math"
	"sort"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// Ventilation TVA d'un registre de caisse (ticket Z), présentée comme Square
// ou Lightspeed : ventes brutes − remises = ventes nettes, TVA sur le net
// (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md, phase 4). Remplace la requête SQL
// héritée de la procédure GET_CASH_REGISTER_REPORT, qui :
//   - comptait la TVA avant remise (les remises de caisse sont enregistrées
//     comme des paiements, models.DiscountMOPs) ;
//   - oubliait les suppléments (extra), que l'export comptable compte ;
//   - calculait le HT des frais de livraison en TTC × (100 − taux) / 100
//     (300 € à 20 % → HT 240 € au lieu de 250 €).
//
// Commandes du registre (orders.cash_register_id), closes et vendues
// (models.VoidOrderBrandStatusesSQL) ; catégorie et taux lus en priorité sur la
// ligne (TVA figée à la vente, migration 164).

// registerVATReport est la ventilation TVA d'un registre.
type registerVATReport struct {
	// Lines : une ligne par catégorie affichée (show_in_report, plus la
	// catégorie -1 des frais de livraison), montants nets de remises, même
	// forme que l'ancienne requête (catégories sans vente comprises, à 0).
	Lines []models.CashReportLine
	// GrossTTC : ventes brutes TTC des catégories affichées ; Discounts : remises
	// qui leur sont imputées. Somme des Lines.TTC = GrossTTC − Discounts.
	GrossTTC  int
	Discounts int
}

type registerVATBucket struct {
	tvaID int64
	rate  float64
	ttc   int64
}

type registerVATCategory struct {
	tvaID        int64
	deliveryType string
	label        string
	title        string
	rate         float64
}

// computeRegisterVAT calcule la ventilation TVA du registre, remises de caisse
// déduites au prorata des taux de chaque commande (plus grand reste, remise
// plafonnée au total de la commande, répartie sur toutes ses parts y compris
// les catégories masquées — même règle que l'export comptable,
// pos/accounting.buildManualVAT).
func (r *CashRegisterRepository) computeRegisterVAT(ctx context.Context, cashRegisterID string) (*registerVATReport, error) {
	db := dbx.GetDB(ctx, r.database)

	// 1) Parts TTC par commande et par (catégorie, taux).
	lineRows, err := db.QueryContext(ctx, `
		SELECT o.order_id,
		       tva.tva_id,
		       `+models.OrderItemTVARateSQL("oi", "tva")+` AS rate,
		       (oi.price + COALESCE(e.extra_price, 0)) * oi.quantity AS ttc
		FROM orders o
		INNER JOIN orderitems oi ON oi.order_id = o.order_id
		INNER JOIN products p ON p.product_id = oi.product_id
		INNER JOIN tva_categories tva ON tva.tva_id = `+models.OrderItemTVAIDSQL("oi", "o", "p")+`
		LEFT JOIN (
			SELECT order_item_id, SUM(extra.price) AS extra_price
			FROM extra
			GROUP BY order_item_id
		) e ON e.order_item_id = oi.order_item_id
		WHERE o.cash_register_id = ?
		  AND o.state = 'CLOSED'
		  AND upper(o.brand_status) NOT IN `+models.VoidOrderBrandStatusesSQL+`
		UNION ALL
		SELECT o_fees.order_id,
		       tva_fees.tva_id,
		       `+models.DeliveryFeesTVARateSQL("o_fees", "tva_fees")+` AS rate,
		       o_fees.delivery_fees AS ttc
		FROM orders o_fees
		INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = -1
		WHERE o_fees.cash_register_id = ?
		  AND o_fees.state = 'CLOSED'
		  AND o_fees.delivery_fees <> 0
		  AND upper(o_fees.brand_status) NOT IN `+models.VoidOrderBrandStatusesSQL+`
	`, cashRegisterID, cashRegisterID)
	if err != nil {
		return nil, fmt.Errorf("register VAT lines: %w", err)
	}
	defer lineRows.Close()

	bucketsByOrder := map[int64][]registerVATBucket{}
	var orderIDs []int64
	for lineRows.Next() {
		var orderID int64
		var b registerVATBucket
		if err := lineRows.Scan(&orderID, &b.tvaID, &b.rate, &b.ttc); err != nil {
			return nil, fmt.Errorf("scan register VAT line: %w", err)
		}
		buckets, seen := bucketsByOrder[orderID]
		if !seen {
			orderIDs = append(orderIDs, orderID)
		}
		merged := false
		for i := range buckets {
			if buckets[i].tvaID == b.tvaID && buckets[i].rate == b.rate {
				buckets[i].ttc += b.ttc
				merged = true
				break
			}
		}
		if !merged {
			buckets = append(buckets, b)
		}
		bucketsByOrder[orderID] = buckets
	}
	if err := lineRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate register VAT lines: %w", err)
	}

	// 2) Remises de caisse par commande.
	discountRows, err := db.QueryContext(ctx, `
		SELECT p.order_id, SUM(p.amount)
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
		WHERE o.cash_register_id = ?
		  AND o.state = 'CLOSED'
		  AND upper(o.brand_status) NOT IN `+models.VoidOrderBrandStatusesSQL+`
		  AND p.enabled = TRUE
		  AND p.mop IN `+models.DiscountMOPsSQL+`
		GROUP BY p.order_id
	`, cashRegisterID)
	if err != nil {
		return nil, fmt.Errorf("register discounts: %w", err)
	}
	defer discountRows.Close()
	discountByOrder := map[int64]int64{}
	for discountRows.Next() {
		var orderID, amount int64
		if err := discountRows.Scan(&orderID, &amount); err != nil {
			return nil, fmt.Errorf("scan register discount: %w", err)
		}
		discountByOrder[orderID] = amount
	}
	if err := discountRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate register discounts: %w", err)
	}

	// 3) Catégories affichées.
	categoryRows, err := db.QueryContext(ctx, `
		SELECT all_tva.tva_id, all_tva.delivery_type, COALESCE(l.label, ''), all_tva.tva_title, all_tva.tva_rate
		FROM tva_categories all_tva
		LEFT JOIN labels l ON l.label_value = all_tva.delivery_type
			AND l.lang = 'FR'
			AND l.label_type = 'delivery_type'
		WHERE all_tva.show_in_report IS TRUE OR all_tva.tva_id = -1
	`)
	if err != nil {
		return nil, fmt.Errorf("register VAT categories: %w", err)
	}
	defer categoryRows.Close()
	var categories []registerVATCategory
	displayed := map[int64]bool{}
	for categoryRows.Next() {
		var c registerVATCategory
		if err := categoryRows.Scan(&c.tvaID, &c.deliveryType, &c.label, &c.title, &c.rate); err != nil {
			return nil, fmt.Errorf("scan register VAT category: %w", err)
		}
		if displayed[c.tvaID] {
			continue
		}
		displayed[c.tvaID] = true
		categories = append(categories, c)
	}
	if err := categoryRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate register VAT categories: %w", err)
	}

	return buildRegisterVATReport(bucketsByOrder, orderIDs, discountByOrder, categories), nil
}

// buildRegisterVATReport assemble la ventilation à partir des parts TTC par
// commande, des remises par commande et des catégories affichées (sans accès
// base, testable unitairement).
func buildRegisterVATReport(bucketsByOrder map[int64][]registerVATBucket, orderIDs []int64, discountByOrder map[int64]int64, categories []registerVATCategory) *registerVATReport {
	displayed := map[int64]bool{}
	for _, c := range categories {
		displayed[c.tvaID] = true
	}

	type rateKey struct {
		tvaID int64
		rate  float64
	}
	netByRate := map[rateKey]int64{}
	report := &registerVATReport{}
	sort.Slice(orderIDs, func(a, b int) bool { return orderIDs[a] < orderIDs[b] })
	for _, orderID := range orderIDs {
		buckets := bucketsByOrder[orderID]
		var alloc []int64
		if discount := discountByOrder[orderID]; discount > 0 {
			weights := make([]int64, len(buckets))
			var orderTTC int64
			for i, b := range buckets {
				weights[i] = b.ttc
				if b.ttc > 0 {
					orderTTC += b.ttc
				}
			}
			if discount > orderTTC {
				discount = orderTTC
			}
			alloc = helpers.AllocateLargestRemainder(discount, weights)
		}
		for i, b := range buckets {
			share := int64(0)
			if alloc != nil {
				share = alloc[i]
			}
			if !displayed[b.tvaID] {
				continue
			}
			netByRate[rateKey{tvaID: b.tvaID, rate: b.rate}] += b.ttc - share
			report.GrossTTC += int(b.ttc)
			report.Discounts += int(share)
		}
	}

	// Une ligne par catégorie affichée ; HT arrondi par taux réellement
	// appliqué (un taux de catégorie peut avoir changé depuis la vente), TVA =
	// TTC − HT pour que HT + TVA = TTC au centime.
	type categoryTotals struct{ ht, ttc int }
	totals := map[int64]*categoryTotals{}
	for key, net := range netByRate {
		t := totals[key.tvaID]
		if t == nil {
			t = &categoryTotals{}
			totals[key.tvaID] = t
		}
		t.ttc += int(net)
		if key.rate == 0 {
			t.ht += int(net)
		} else {
			t.ht += int(math.Round(float64(net) * 100.0 / (100.0 + key.rate)))
		}
	}

	sort.SliceStable(categories, func(a, b int) bool {
		// Frais de livraison (-1) en dernier, comme l'ancienne requête (UNION).
		if (categories[a].tvaID == -1) != (categories[b].tvaID == -1) {
			return categories[b].tvaID == -1
		}
		if categories[a].deliveryType != categories[b].deliveryType {
			return categories[a].deliveryType < categories[b].deliveryType
		}
		return categories[a].title < categories[b].title
	})
	for _, c := range categories {
		line := models.CashReportLine{DeliveryType: c.deliveryType, Label: c.label, TVATitle: c.title, Rate: c.rate}
		if t := totals[c.tvaID]; t != nil {
			line.TTC = t.ttc
			line.HT = t.ht
			line.TVA = t.ttc - t.ht
		}
		report.Lines = append(report.Lines, line)
	}
	return report
}
