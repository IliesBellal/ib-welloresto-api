package tasks

import (
	"context"
	"encoding/json"
	"time"
	"welloresto-api/internal/database/dbx"

	upsellModule "welloresto-api/internal/modules/upsell"

	"go.uber.org/zap"
)

const (
	upsellPatternWindow  = 90 // days of order history to analyse
	upsellMinCoOccur     = 5  // minimum co-occurrences to include a pair
	upsellMinLift        = 1.0
	upsellMinConfidence  = 0.1
	upsellMaxPairsStored = 10 // top N patterns stored per product
	upsellPatternTTL     = 36 * time.Hour
	upsellCleanupMonths  = 8
)

// RecomputeUpsellPatterns runs a market basket analysis for every active merchant
// and stores the results as JSON in Redis.
// Each merchant is processed independently — an error on one does not stop others.
func (tm *TasksManager) RecomputeUpsellPatterns() {
	ctx := context.Background()

	if tm.AICache == nil || !tm.AICache.IsAvailable() {
		tm.logWarn("[CRON] RecomputeUpsellPatterns: redis indisponible, batch ignoré")
		return
	}

	tm.logInfo("[CRON] RecomputeUpsellPatterns: démarrage")
	start := time.Now()

	// ── 1. Fetch active merchants (lecture complète : 1 connexion max) ───────
	merchants, err := tm.collectIDs(ctx,
		"SELECT m.id FROM merchant m INNER JOIN subscriptions s ON s.merchant_id = "+tskMerchantJoinCast())
	if err != nil {
		tm.logError("[CRON] RecomputeUpsellPatterns: liste marchands échouée", zap.Error(err))
		return
	}

	merchantsProcessed := 0
	merchantsFailed := 0
	totalPairs := 0

	for _, merchantID := range merchants {
		pairs, processErr := tm.processUpsellPatternsForMerchant(ctx, merchantID)
		if processErr != nil {
			tm.logError("[CRON] RecomputeUpsellPatterns: marchand en échec",
				zap.String("merchant_id", merchantID), zap.Error(processErr))
			merchantsFailed++
			continue
		}
		merchantsProcessed++
		totalPairs += pairs
	}

	tm.logInfo("[CRON] RecomputeUpsellPatterns: terminé",
		zap.Int64("duration_ms", time.Since(start).Milliseconds()),
		zap.Int("merchants_processed", merchantsProcessed),
		zap.Int("merchants_failed", merchantsFailed),
		zap.Int("total_patterns", totalPairs))
}

// processUpsellPatternsForMerchant computes market basket patterns for a single merchant
// and writes them to Redis. Returns the number of (directed) pattern pairs written.
func (tm *TasksManager) processUpsellPatternsForMerchant(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, tm.DB)

	// ── Step 1: Total closed orders in window ────────────────────────────────
	var totalOrders int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT order_id)
		FROM orders
		WHERE merchant_id = ?
		  AND state       = 'CLOSED'
		  AND creation_date >= `+tskNowMinusDays()+`
	`, merchantID, upsellPatternWindow).Scan(&totalOrders)
	if err != nil || totalOrders == 0 {
		return 0, err
	}

	// ── Step 2: Per-product support count ────────────────────────────────────
	suppRows, err := db.QueryContext(ctx, `
		SELECT oi.product_id, COUNT(DISTINCT oi.order_id) AS cnt
		FROM orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		WHERE o.merchant_id   = ?
		  AND o.state         = 'CLOSED'
		  AND o.creation_date >= `+tskNowMinusDays()+`
		GROUP BY oi.product_id
	`, merchantID, upsellPatternWindow)
	if err != nil {
		return 0, err
	}
	defer suppRows.Close()

	productCount := make(map[string]int)
	for suppRows.Next() {
		var pid string
		var cnt int
		if scanErr := suppRows.Scan(&pid, &cnt); scanErr == nil {
			productCount[pid] = cnt
		}
	}

	// ── Step 3: Co-occurrence matrix ─────────────────────────────────────────
	pairRows, err := db.QueryContext(ctx, `
		SELECT
			a.product_id AS product_a,
			b.product_id AS product_b,
			COUNT(DISTINCT a.order_id) AS count_ab
		FROM (
			SELECT DISTINCT oi.order_id, oi.product_id
			FROM orderitems oi
			INNER JOIN orders o ON o.order_id = oi.order_id
			WHERE o.merchant_id   = ?
			  AND o.state         = 'CLOSED'
			  AND o.creation_date >= `+tskNowMinusDays()+`
		) a
		INNER JOIN (
			SELECT DISTINCT oi.order_id, oi.product_id
			FROM orderitems oi
			INNER JOIN orders o ON o.order_id = oi.order_id
			WHERE o.merchant_id   = ?
			  AND o.state         = 'CLOSED'
			  AND o.creation_date >= `+tskNowMinusDays()+`
		) b ON a.order_id = b.order_id AND a.product_id < b.product_id
		GROUP BY a.product_id, b.product_id
		HAVING COUNT(DISTINCT a.order_id) >= ?
	`, merchantID, upsellPatternWindow,
		merchantID, upsellPatternWindow,
		upsellMinCoOccur)
	if err != nil {
		return 0, err
	}
	defer pairRows.Close()

	// ── Step 4: Compute metrics and accumulate per-product patterns ───────────
	// Keyed by source product → list of pattern entries to suggest.
	type entry = upsellModule.PatternEntry
	perProduct := make(map[string][]entry)

	for pairRows.Next() {
		var pidA, pidB string
		var countAB int
		if scanErr := pairRows.Scan(&pidA, &pidB, &countAB); scanErr != nil {
			continue
		}

		countA := productCount[pidA]
		countB := productCount[pidB]
		if countA == 0 || countB == 0 {
			continue
		}

		support := float64(countAB) / float64(totalOrders)
		confAB := float64(countAB) / float64(countA)
		confBA := float64(countAB) / float64(countB)
		lift := float64(countAB*totalOrders) / float64(countA*countB)

		if lift < upsellMinLift {
			continue
		}
		if confAB < upsellMinConfidence && confBA < upsellMinConfidence {
			continue
		}

		// A → B
		if confAB >= upsellMinConfidence {
			perProduct[pidA] = append(perProduct[pidA], entry{
				ProductID:  pidB,
				Lift:       lift,
				Confidence: confAB,
				Support:    support,
			})
		}
		// B → A
		if confBA >= upsellMinConfidence {
			perProduct[pidB] = append(perProduct[pidB], entry{
				ProductID:  pidA,
				Lift:       lift,
				Confidence: confBA,
				Support:    support,
			})
		}
	}

	// ── Step 5: Sort and store in Redis ──────────────────────────────────────
	totalWritten := 0
	for sourcePID, entries := range perProduct {
		// Sort by lift DESC, keep top N.
		sortedEntries := entries
		if len(sortedEntries) > upsellMaxPairsStored {
			for i := 0; i < upsellMaxPairsStored; i++ {
				maxIdx := i
				for j := i + 1; j < len(sortedEntries); j++ {
					if sortedEntries[j].Lift > sortedEntries[maxIdx].Lift {
						maxIdx = j
					}
				}
				sortedEntries[i], sortedEntries[maxIdx] = sortedEntries[maxIdx], sortedEntries[i]
			}
			sortedEntries = sortedEntries[:upsellMaxPairsStored]
		}

		raw, marshalErr := json.Marshal(sortedEntries)
		if marshalErr != nil {
			continue
		}

		// Key without "ai:" prefix — aiCache.Set will add it automatically.
		key := "upsell:patterns:" + merchantID + ":" + sourcePID
		_ = tm.AICache.Set(ctx, key, string(raw), upsellPatternTTL)
		totalWritten += len(sortedEntries)
	}

	// ── Step 6: Write metadata ────────────────────────────────────────────────
	meta := map[string]interface{}{
		"computed_at":         time.Now().UTC().Format(time.RFC3339),
		"orders_analyzed":     totalOrders,
		"items_with_patterns": len(perProduct),
		"total_pairs":         totalWritten,
	}
	if metaRaw, err := json.Marshal(meta); err == nil {
		metaKey := "upsell:patterns:" + merchantID + ":_meta"
		_ = tm.AICache.Set(ctx, metaKey, string(metaRaw), upsellPatternTTL)
	}

	return totalWritten, nil
}

// CleanupOldUpsellSuggestions deletes suggestion rows older than upsellCleanupMonths.
func (tm *TasksManager) CleanupOldUpsellSuggestions() {
	if tm.UpsellRepo == nil {
		tm.logWarn("[CRON] CleanupOldUpsellSuggestions: repo indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()

	deleted, err := tm.UpsellRepo.DeleteOldSuggestions(ctx, upsellCleanupMonths)
	if err != nil {
		tm.logError("[CRON] CleanupOldUpsellSuggestions: échec", zap.Error(err))
		return
	}

	tm.logInfo("[CRON] CleanupOldUpsellSuggestions: terminé",
		zap.Int64("deleted", deleted),
		zap.Int("older_than_months", upsellCleanupMonths))
}
