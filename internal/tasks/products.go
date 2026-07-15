package tasks

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

const (
	minProductSoldQty             = 5
	numPopularProductsPerMerchant = 10
)

// UpdatePopularProducts : Mise à jour des produits populaires (30 derniers jours).
//
// Contrainte pool MySQL (1 connexion max) : pas de transaction globale qui
// monopoliserait l'unique connexion pendant toute la boucle marchands. Les
// SELECT lourds tournent hors transaction, puis chaque marchand est mis à jour
// dans une transaction courte (reset + set). Une erreur sur un marchand
// n'interrompt pas les autres.
func (tm *TasksManager) UpdatePopularProducts() {
	ctx := context.Background()
	tm.logInfo("[CRON] UpdatePopularProducts: démarrage")

	merchants, err := tm.collectIDs(ctx,
		"SELECT m.id FROM merchant m INNER JOIN subscriptions s ON s.merchant_id = m.id")
	if err != nil {
		tm.logError("[CRON] UpdatePopularProducts: liste marchands échouée", zap.Error(err))
		return
	}

	ok, failed := 0, 0
	for _, merchantID := range merchants {
		if err := tm.updateMerchantPopularProducts(ctx, merchantID); err != nil {
			tm.logError("[CRON] UpdatePopularProducts: marchand en échec",
				zap.String("merchant_id", merchantID), zap.Error(err))
			failed++
			continue
		}
		ok++
	}

	tm.logInfo("[CRON] UpdatePopularProducts: terminé",
		zap.Int("merchants_ok", ok), zap.Int("merchants_failed", failed))
}

// updateMerchantPopularProducts calcule puis applique les flags is_popular
// d'un seul marchand : top 1 par catégorie + top N global (30 derniers jours).
func (tm *TasksManager) updateMerchantPopularProducts(ctx context.Context, merchantID string) error {
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
	topCat, err := tm.collectIDs(ctx, topCatQuery, merchantID, minProductSoldQty, merchantID)
	if err != nil {
		return fmt.Errorf("top par catégorie: %w", err)
	}

	// --- LOGIQUE TOP X GLOBAL ---
	topGlobalQuery := `
		SELECT oi.product_id FROM orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		WHERE o.merchant_id = ? AND o.creation_date >= NOW() - INTERVAL 30 DAY
		GROUP BY oi.product_id ORDER BY COUNT(*) DESC LIMIT ?`
	topGlobal, err := tm.collectIDs(ctx, topGlobalQuery, merchantID, numPopularProductsPerMerchant)
	if err != nil {
		return fmt.Errorf("top global: %w", err)
	}

	// Union dédupliquée des deux listes.
	seen := make(map[string]bool, len(topCat)+len(topGlobal))
	var popularIDs []string
	for _, id := range append(topCat, topGlobal...) {
		if !seen[id] {
			seen[id] = true
			popularIDs = append(popularIDs, id)
		}
	}

	// Transaction courte scopée au marchand : reset puis set.
	tx, err := tm.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE products SET is_popular = 0 WHERE merchant_id = ? AND is_popular = 1",
		merchantID); err != nil {
		tx.Rollback()
		return fmt.Errorf("reset is_popular: %w", err)
	}

	if len(popularIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(popularIDs)), ",")
		args := make([]interface{}, 0, len(popularIDs)+1)
		args = append(args, merchantID)
		for _, id := range popularIDs {
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			"UPDATE products SET is_popular = 1 WHERE merchant_id = ? AND product_id IN (%s)",
			placeholders), args...); err != nil {
			tx.Rollback()
			return fmt.Errorf("set is_popular: %w", err)
		}
	}

	return tx.Commit()
}

// collectIDs exécute une requête retournant une colonne d'IDs et la lit
// intégralement en mémoire avant de rendre la main, pour libérer l'unique
// connexion du pool au plus tôt.
func (tm *TasksManager) collectIDs(ctx context.Context, query string, args ...interface{}) ([]string, error) {
	rows, err := tm.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
