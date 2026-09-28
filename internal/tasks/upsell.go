package tasks

import (
	"context"
	"encoding/json"
	"sort"
	"time"
	"welloresto-api/internal/database/dbx"

	upsellModule "welloresto-api/internal/modules/upsell"

	"go.uber.org/zap"
)

// Association rules (docs/UPSELL_COMPLETION.md, §8, D12). B is kept as a
// suggestion for A when the pair was ordered together at least
// upsellMinCoOccur times, is more frequent than chance by upsellMinLift, and
// the smoothed share of A's orders that also hold B reaches
// upsellMinConfidence. Suggestions are ranked by that smoothed share.
const (
	upsellPatternWindow  = 90 // days of order history to analyse
	upsellMinCoOccur     = 8  // minimum co-occurrences to include a pair
	upsellMinLift        = 1.2
	upsellMinConfidence  = 0.10 // on the smoothed P(B | A)
	upsellConfidenceBeta = 10.0 // smoothing weight, in orders (see upsellSmoothedConfidence)
	upsellMaxPairsStored = 10   // top N patterns stored per product
	upsellPatternTTL     = 36 * time.Hour
	upsellCleanupMonths  = 8
	// Low-price best sellers: products priced at most a third of the median
	// catalogue price, best sellers first (docs/UPSELL_COMPLETION.md, D10).
	upsellLowPriceDivisor  = 3
	upsellLowPriceMaxItems = 30
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

// upsellBasketLinesSQL lists the distinct (order, product) lines of a
// merchant's closed orders in the analysis window.
// A variant (products.by_product_of) is counted under its product group:
// pooling the variants of a group strengthens its statistics. At suggestion
// time a pattern pointing to a group is turned into one of its variants, as a
// group itself is never suggested (D13). A line whose product no longer
// exists keeps its own id.
// Placeholders: merchant_id, window in days. See docs/UPSELL_COMPLETION.md (D1).
func upsellBasketLinesSQL() string {
	return `
			SELECT DISTINCT oi.order_id,
			       COALESCE(NULLIF(p.by_product_of, 0), oi.product_id) AS product_id
			FROM orderitems oi
			INNER JOIN orders o ON o.order_id = oi.order_id
			LEFT JOIN products p ON p.product_id = oi.product_id
			WHERE o.merchant_id   = ?
			  AND o.state         = 'CLOSED'
			  AND o.creation_date >= ` + tskNowMinusDays()
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
		SELECT l.product_id, COUNT(*) AS cnt
		FROM (`+upsellBasketLinesSQL()+`) l
		GROUP BY l.product_id
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

	// ── Step 2b: Low-price best sellers ─────────────────────────────────────
	// Independent from the patterns: a failure is logged and the patterns are
	// still computed.
	lowPrice, lowPriceMedian, lpErr := tm.computeUpsellLowPriceList(ctx, merchantID)
	if lpErr != nil {
		tm.logWarn("[CRON] RecomputeUpsellPatterns: liste petits prix en échec",
			zap.String("merchant_id", merchantID), zap.Error(lpErr))
	} else if raw, marshalErr := json.Marshal(lowPrice); marshalErr == nil {
		// Key without "ai:" prefix — aiCache.Set will add it automatically.
		_ = tm.AICache.Set(ctx, "upsell:lowprice:"+merchantID, string(raw), upsellPatternTTL)
	}

	// ── Step 3: Co-occurrence matrix ─────────────────────────────────────────
	pairRows, err := db.QueryContext(ctx, `
		SELECT
			a.product_id AS product_a,
			b.product_id AS product_b,
			COUNT(*)     AS count_ab
		FROM (`+upsellBasketLinesSQL()+`) a
		INNER JOIN (`+upsellBasketLinesSQL()+`) b
			ON a.order_id = b.order_id AND a.product_id < b.product_id
		GROUP BY a.product_id, b.product_id
		HAVING COUNT(*) >= ?
	`, merchantID, upsellPatternWindow,
		merchantID, upsellPatternWindow,
		upsellMinCoOccur)
	if err != nil {
		return 0, err
	}
	defer pairRows.Close()

	// ── Step 4: Compute metrics and accumulate per-product patterns ───────────
	// Keyed by source product → list of pattern entries to suggest.
	perProduct := make(map[string][]upsellModule.PatternEntry)

	for pairRows.Next() {
		var pidA, pidB string
		var countAB int
		if scanErr := pairRows.Scan(&pidA, &pidB, &countAB); scanErr != nil {
			continue
		}

		ab, ba := upsellPairPatterns(pidA, pidB, countAB, productCount[pidA], productCount[pidB], totalOrders)
		if ab != nil {
			perProduct[pidA] = append(perProduct[pidA], *ab)
		}
		if ba != nil {
			perProduct[pidB] = append(perProduct[pidB], *ba)
		}
	}

	// ── Step 5: Sort and store in Redis ──────────────────────────────────────
	totalWritten := 0
	for sourcePID, entries := range perProduct {
		// Best smoothed confidence first, keep top N.
		sortedEntries := sortUpsellPatterns(entries, upsellMaxPairsStored)

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
		"low_price_median":    lowPriceMedian,
		"low_price_threshold": lowPriceMedian / upsellLowPriceDivisor,
		"low_price_items":     len(lowPrice),
	}
	if metaRaw, err := json.Marshal(meta); err == nil {
		metaKey := "upsell:patterns:" + merchantID + ":_meta"
		_ = tm.AICache.Set(ctx, metaKey, string(metaRaw), upsellPatternTTL)
	}

	return totalWritten, nil
}

// upsellSmoothedConfidence is P(target | source) pulled towards the overall
// share of orders holding the target: (countBoth + beta × countTarget/total) /
// (countSource + beta). On a large sample it matches the raw share; on a small
// one (6 orders out of 6) it no longer passes for a certainty.
func upsellSmoothedConfidence(countBoth, countSource, countTarget, totalOrders int) float64 {
	prior := float64(countTarget) / float64(totalOrders)
	return (float64(countBoth) + upsellConfidenceBeta*prior) / (float64(countSource) + upsellConfidenceBeta)
}

// upsellPairPatterns applies the association rules to a pair of products
// ordered together countAB times, and returns the A→B and B→A patterns that
// pass (nil otherwise). Confidence holds the smoothed P(B | A) (resp. P(A | B)).
func upsellPairPatterns(pidA, pidB string, countAB, countA, countB, totalOrders int) (ab, ba *upsellModule.PatternEntry) {
	if countA == 0 || countB == 0 || totalOrders == 0 || countAB < upsellMinCoOccur {
		return nil, nil
	}
	lift := float64(countAB) * float64(totalOrders) / (float64(countA) * float64(countB))
	if lift < upsellMinLift {
		return nil, nil
	}
	support := float64(countAB) / float64(totalOrders)

	if conf := upsellSmoothedConfidence(countAB, countA, countB, totalOrders); conf >= upsellMinConfidence {
		ab = &upsellModule.PatternEntry{ProductID: pidB, Lift: lift, Confidence: conf, Support: support}
	}
	if conf := upsellSmoothedConfidence(countAB, countB, countA, totalOrders); conf >= upsellMinConfidence {
		ba = &upsellModule.PatternEntry{ProductID: pidA, Lift: lift, Confidence: conf, Support: support}
	}
	return ab, ba
}

// sortUpsellPatterns orders entries by confidence descending (product id on
// ties, for a stable result) and keeps at most limit of them.
func sortUpsellPatterns(entries []upsellModule.PatternEntry, limit int) []upsellModule.PatternEntry {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Confidence != entries[j].Confidence {
			return entries[i].Confidence > entries[j].Confidence
		}
		return entries[i].ProductID < entries[j].ProductID
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

// upsellCatalogProduct is a product that can be suggested as such, with its
// own price: a standalone product or a variant, never a product group.
type upsellCatalogProduct struct {
	ProductID string
	Price     int64
}

// computeUpsellLowPriceList returns the merchant's low-price best sellers (see
// selectUpsellLowPrice) along with the median catalogue price it was computed
// from. Unlike the patterns, sales are counted per product and not per group:
// the list holds variants, which are what gets suggested (D13).
func (tm *TasksManager) computeUpsellLowPriceList(ctx context.Context, merchantID string) ([]upsellModule.LowPriceEntry, float64, error) {
	db := dbx.GetDB(ctx, tm.DB)

	// Same filters as menu.ListAvailableProductsForUpsell, so that the list
	// only holds products the suggestion step can offer.
	rows, err := db.QueryContext(ctx, `
		SELECT p.product_id, COALESCE(p.price, 0)
		FROM products p
		LEFT JOIN products g
			ON g.product_id = p.by_product_of
			AND g.merchant_id = p.merchant_id
		WHERE p.merchant_id = ?
		  AND p.available   = TRUE
		  AND p.enabled     = TRUE
		  AND p.status      IN ('available', '1')
		  AND COALESCE(p.is_product_group, FALSE) = FALSE
		  AND (
		        p.by_product_of IS NULL OR p.by_product_of = 0
		        OR (g.is_product_group = TRUE
		            AND g.available = TRUE
		            AND g.enabled   = TRUE
		            AND g.status    IN ('available', '1'))
		  )
	`, merchantID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var catalog []upsellCatalogProduct
	for rows.Next() {
		var cp upsellCatalogProduct
		if err := rows.Scan(&cp.ProductID, &cp.Price); err != nil {
			return nil, 0, err
		}
		catalog = append(catalog, cp)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	salesRows, err := db.QueryContext(ctx, `
		SELECT oi.product_id, COUNT(DISTINCT oi.order_id)
		FROM orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		WHERE o.merchant_id   = ?
		  AND o.state         = 'CLOSED'
		  AND o.creation_date >= `+tskNowMinusDays()+`
		GROUP BY oi.product_id
	`, merchantID, upsellPatternWindow)
	if err != nil {
		return nil, 0, err
	}
	defer salesRows.Close()

	sales := make(map[string]int)
	for salesRows.Next() {
		var pid string
		var orders int
		if err := salesRows.Scan(&pid, &orders); err != nil {
			return nil, 0, err
		}
		sales[pid] = orders
	}
	if err := salesRows.Err(); err != nil {
		return nil, 0, err
	}

	entries, median := selectUpsellLowPrice(catalog, sales, upsellLowPriceMaxItems)
	return entries, median, nil
}

// selectUpsellLowPrice keeps the products that sold at least once and whose
// price is above 0 and at most a third of the median catalogue price (median of
// the prices above 0). They are sorted by orders descending, then price
// ascending, then product id for a stable order, and capped at limit.
// Products at 0 € are left out on purpose: sorted by price they would come
// first, and they are only worth offering when a pattern backs them (e.g. a
// house sauce). Returns the median alongside for logging.
func selectUpsellLowPrice(catalog []upsellCatalogProduct, sales map[string]int, limit int) ([]upsellModule.LowPriceEntry, float64) {
	prices := make([]int64, 0, len(catalog))
	for _, cp := range catalog {
		if cp.Price > 0 {
			prices = append(prices, cp.Price)
		}
	}
	if len(prices) == 0 {
		return []upsellModule.LowPriceEntry{}, 0
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
	mid := len(prices) / 2
	median := float64(prices[mid])
	if len(prices)%2 == 0 {
		median = float64(prices[mid-1]+prices[mid]) / 2
	}
	threshold := median / upsellLowPriceDivisor

	entries := make([]upsellModule.LowPriceEntry, 0)
	for _, cp := range catalog {
		orders := sales[cp.ProductID]
		if cp.Price <= 0 || float64(cp.Price) > threshold || orders == 0 {
			continue
		}
		entries = append(entries, upsellModule.LowPriceEntry{ProductID: cp.ProductID, Price: cp.Price, Orders: orders})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Orders != entries[j].Orders {
			return entries[i].Orders > entries[j].Orders
		}
		if entries[i].Price != entries[j].Price {
			return entries[i].Price < entries[j].Price
		}
		return entries[i].ProductID < entries[j].ProductID
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, median
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
