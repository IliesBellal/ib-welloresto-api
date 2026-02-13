package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
)

const (
	minProductSoldQty             = 5
	numPopularProductsPerMerchant = 10
)

// UpdateAverageDistributionTime : Calcul complexe avec simulation de slots cuisine
func (tm *TasksManager) UpdateAverageDistributionTime() {
	log.Println("[CRON] Démarrage: UpdateAverageDistributionTime")

	// TODO: Implémenter la logique de slots parallèles
	// Récupérer les marchands
	// Simuler la file d'attente
	// Mettre à jour la DB

	log.Println("[CRON] Terminé: UpdateAverageDistributionTime")
}

// UpdateAverageDistributionTimeV1 : Calcul simplifié par commande
func (tm *TasksManager) UpdateAverageDistributionTimeV1() {
	log.Println("[CRON] Démarrage: UpdateAverageDistributionTimeV1")

	// TODO: Implémenter la logique séquentielle

	log.Println("[CRON] Terminé: UpdateAverageDistributionTimeV1")
}

// UpdatePopularProducts : Mise à jour des produits populaires (30 derniers jours)
func (tm *TasksManager) UpdatePopularProducts() {
	log.Println("[CRON] Démarrage: UpdatePopularProducts")

	ctx := context.Background()
	tx, err := tm.DB.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Erreur transaction: %v", err)
		return
	}

	// Étape 1 : Reset
	_, err = tx.ExecContext(ctx, "UPDATE products SET is_popular = 0")
	if err != nil {
		tx.Rollback()
		return
	}

	// Étape 2 : Liste des marchands actifs
	rows, err := tx.QueryContext(ctx, "SELECT m.id FROM merchant m INNER JOIN subscriptions s on s.merchant_id = m.id")
	if err != nil {
		tx.Rollback()
		return
	}
	defer rows.Close()

	var merchants []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		merchants = append(merchants, id)
	}

	for _, merchantID := range merchants {
		// --- LOGIQUE TOP 1 PAR CATEGORIE ---
		topCatQuery := `
			SELECT p.product_id FROM orderitems oi
			INNER JOIN orders o ON o.order_id = oi.order_id
			INNER JOIN products p ON p.product_id = oi.product_id
			WHERE o.merchant_id = ? AND o.creation_date >= NOW() - INTERVAL 30 DAY
			GROUP BY p.category, p.product_id
			HAVING COUNT(*) >= ?
			AND p.product_id = (
				SELECT oi2.product_id FROM orderitems oi2
				INNER JOIN orders o2 ON o2.order_id = oi2.order_id
				INNER JOIN products p2 ON p2.product_id = oi2.product_id
				WHERE o2.merchant_id = ? AND o2.creation_date >= NOW() - INTERVAL 30 DAY
				AND p2.category = p.category
				GROUP BY oi2.product_id ORDER BY COUNT(*) DESC LIMIT 1
			)`

		tm.updateMerchantPopularity(ctx, tx, topCatQuery, merchantID, minProductSoldQty)

		// --- LOGIQUE TOP X GLOBAL ---
		topGlobalQuery := `
			SELECT oi.product_id FROM orderitems oi
			INNER JOIN orders o ON o.order_id = oi.order_id
			WHERE o.merchant_id = ? AND o.creation_date >= NOW() - INTERVAL 30 DAY
			GROUP BY oi.product_id ORDER BY COUNT(*) DESC LIMIT ?`

		tm.updateMerchantPopularity(ctx, tx, topGlobalQuery, merchantID, numPopularProductsPerMerchant)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Erreur commit: %v", err)
	}
	log.Println("[CRON] Terminé: UpdatePopularProducts")
}

// Helper pour l'update massif par marchand
func (tm *TasksManager) updateMerchantPopularity(ctx context.Context, tx *sql.Tx, selectQuery string, merchantID string, limitOrMin int) {
	rows, err := tx.QueryContext(ctx, selectQuery, merchantID, limitOrMin)
	if err != nil {
		return
	}
	defer rows.Close()

	var ids []interface{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	if len(ids) > 0 {
		placeholders := make([]string, len(ids))
		for i := range ids {
			placeholders[i] = "?"
		}
		updateQuery := fmt.Sprintf(
			"UPDATE products SET is_popular = 1 WHERE merchant_id = ? AND product_id IN (%s)",
			strings.Join(placeholders, ","),
		)
		args := append([]interface{}{merchantID}, ids...)
		tx.ExecContext(ctx, updateQuery, args...)
	}
}
