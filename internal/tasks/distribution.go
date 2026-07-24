package tasks

import (
	"context"
	"database/sql"
	"math"
	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// Constantes héritées du script PHP updateAverageDistributionTime.
const (
	avgDistCalcIntervalMinutes = 1440 // fenêtre d'historique analysée (24h)
	avgDistTurnaroundMinSec    = 10   // filtre outliers : turnaround minimum (s)
	avgDistTurnaroundMaxSec    = 1200 // filtre outliers : turnaround maximum (s)
	avgDistMinItems            = 5    // items minimum pour produire une moyenne
	avgDistFloorSec            = 10   // borne de sécurité basse du résultat (s)
	avgDistCeilSec             = 1200 // borne de sécurité haute du résultat (s)
)

type distributionItem struct {
	quantity    int64
	orderedTS   int64 // UNIX_TIMESTAMP(oi.ordered_on)
	turnaroundS int64 // secondes entre ordered_on et distributed_on
}

// UpdateAverageDistributionTime : met à jour le temps de préparation moyen en
// simulant la capacité de production parallèle de la cuisine
// (concurrent_preparation_capacity). Portage direct du script PHP.
//
// Contrainte pool MySQL (1 connexion max) : chaque requête est lue
// intégralement en mémoire avant la suivante, et l'upsert final est un
// statement autocommit unique — aucune transaction longue.
func (tm *TasksManager) UpdateAverageDistributionTime() {
	ctx := context.Background()
	tm.logInfo("[CRON] UpdateAverageDistributionTime: démarrage")
	db := dbx.GetDB(ctx, tm.DB)

	type merchantCapacity struct {
		id       string
		capacity int64
	}

	merchantsQuery := `
		SELECT mp.merchant_id, mp.concurrent_preparation_capacity
		FROM merchant m
		INNER JOIN merchant_parameters mp ON mp.merchant_id = ` + tskMerchantJoinCast()

	rows, err := db.QueryContext(ctx, merchantsQuery)
	if err != nil {
		tm.logError("[CRON] UpdateAverageDistributionTime: liste marchands échouée", zap.Error(err))
		return
	}

	var merchants []merchantCapacity
	for rows.Next() {
		var id string
		var capacity sql.NullInt64
		if err := rows.Scan(&id, &capacity); err != nil {
			tm.logError("[CRON] UpdateAverageDistributionTime: scan marchand échoué", zap.Error(err))
			continue
		}
		merchants = append(merchants, merchantCapacity{id: id, capacity: capacity.Int64})
	}
	if err := rows.Err(); err != nil {
		tm.logError("[CRON] UpdateAverageDistributionTime: itération marchands interrompue", zap.Error(err))
	}
	rows.Close()

	updated := 0
	for _, m := range merchants {
		// Comme en PHP : sans capacité configurée (NULL ou 0), le marchand
		// n'est pas traité.
		if m.capacity < 1 {
			continue
		}

		avgTime, items, err := tm.computeAverageDistributionTime(ctx, m.id, int(m.capacity))
		if err != nil {
			tm.logError("[CRON] UpdateAverageDistributionTime: marchand en échec",
				zap.String("merchant_id", m.id), zap.Error(err))
			continue
		}
		if items < avgDistMinItems {
			continue // pas assez de données, la valeur précédente reste en place
		}

		upsertQuery := `
			INSERT INTO average_distribution_time (merchant_id, distribution_time)
			VALUES (?, ?)
			ON DUPLICATE KEY UPDATE distribution_time = ?`
		upsertArgs := []interface{}{m.id, avgTime, avgTime}
		if dbx.ActiveDialect() == dbx.Postgres {
			// Pas de syntaxe commune pour l'upsert (ON DUPLICATE KEY UPDATE vs
			// ON CONFLICT) : average_distribution_time.merchant_id est la PK,
			// donc ON CONFLICT (merchant_id) cible bien la même contrainte.
			upsertQuery = `
			INSERT INTO average_distribution_time (merchant_id, distribution_time)
			VALUES (?, ?)
			ON CONFLICT (merchant_id) DO UPDATE SET distribution_time = EXCLUDED.distribution_time`
			upsertArgs = []interface{}{m.id, avgTime}
		}
		if _, err := db.ExecContext(ctx, upsertQuery, upsertArgs...); err != nil {
			tm.logError("[CRON] UpdateAverageDistributionTime: upsert échoué",
				zap.String("merchant_id", m.id), zap.Error(err))
			continue
		}
		updated++

		tm.logInfo("[CRON] UpdateAverageDistributionTime: marchand mis à jour",
			zap.String("merchant_id", m.id),
			zap.Int64("distribution_time_s", avgTime),
			zap.Int64("capacite", m.capacity),
			zap.Int("items", items))
	}

	tm.logInfo("[CRON] UpdateAverageDistributionTime: terminé",
		zap.Int("merchants_analyses", len(merchants)),
		zap.Int("merchants_mis_a_jour", updated))
}

// computeAverageDistributionTime lit les orderitems distribués de la fenêtre,
// simule la file de production sur `capacity` slots parallèles et retourne le
// temps moyen borné [avgDistFloorSec, avgDistCeilSec] ainsi que le nombre
// d'items traités. Retourne (0, 0, nil) si pas assez de données.
func (tm *TasksManager) computeAverageDistributionTime(ctx context.Context, merchantID string, capacity int) (int64, int, error) {
	db := dbx.GetDB(ctx, tm.DB)
	query := `
		SELECT
			oi.quantity,
			` + tskUnixTimestamp("oi.ordered_on") + `,
			` + tskSecondsBetween("oi.ordered_on", "oi.distributed_on") + `
		FROM orders o
		INNER JOIN orderitems oi ON o.order_id = oi.order_id
		INNER JOIN products p ON oi.product_id = p.product_id
		WHERE
			p.merchant_id = ?
			AND oi.distributed_quantity > 0 AND oi.distributed_on IS NOT NULL
			AND oi.ordered_on >= ` + tskNowMinusMinutes() + `
			AND ` + tskSecondsBetween("oi.ordered_on", "oi.distributed_on") + ` BETWEEN ? AND ?
		ORDER BY oi.ordered_on ASC`

	rows, err := db.QueryContext(ctx, query,
		merchantID, avgDistCalcIntervalMinutes, avgDistTurnaroundMinSec, avgDistTurnaroundMaxSec)
	if err != nil {
		return 0, 0, err
	}

	// Lecture complète avant tout autre accès DB (1 connexion max).
	var items []distributionItem
	for rows.Next() {
		var quantity, orderedTS, turnaround sql.NullInt64
		if err := rows.Scan(&quantity, &orderedTS, &turnaround); err != nil {
			continue
		}
		if !quantity.Valid || !orderedTS.Valid || !turnaround.Valid {
			continue
		}
		items = append(items, distributionItem{
			quantity:    quantity.Int64,
			orderedTS:   orderedTS.Int64,
			turnaroundS: turnaround.Int64,
		})
	}
	iterErr := rows.Err()
	rows.Close()
	if iterErr != nil {
		return 0, 0, iterErr
	}

	avgTime, processed := simulateAverageDistributionTime(items, capacity)
	return avgTime, processed, nil
}

// simulateAverageDistributionTime simule la cuisine avec `capacity` slots de
// production parallèles et retourne le temps moyen borné ainsi que le nombre
// d'items traités. Retourne (0, 0) si moins de avgDistMinItems items.
// `capacity` doit être >= 1 (garanti par l'appelant).
func simulateAverageDistributionTime(items []distributionItem, capacity int) (int64, int) {
	if len(items) < avgDistMinItems {
		return 0, 0
	}
	if capacity < 1 {
		capacity = 1
	}

	// Chaque slot mémorise le timestamp auquel il se libère.
	productionSlots := make([]int64, capacity)
	var totalProductionSeconds int64
	totalItemsProcessed := 0

	for _, item := range items {
		if item.quantity <= 0 {
			continue
		}
		// Temps de préparation estimé pour un article de cette ligne.
		timePerSingleItem := int64(math.Round(float64(item.turnaroundS) / float64(item.quantity)))
		if timePerSingleItem <= 0 {
			continue
		}

		for q := int64(0); q < item.quantity; q++ {
			// a. Trouver le slot qui se libère le plus tôt.
			slotIdx := 0
			for i := 1; i < len(productionSlots); i++ {
				if productionSlots[i] < productionSlots[slotIdx] {
					slotIdx = i
				}
			}

			// b. La préparation démarre à l'heure de commande ou à la
			// libération du slot, selon le plus tardif.
			startTS := item.orderedTS
			if productionSlots[slotIdx] > startTS {
				startTS = productionSlots[slotIdx]
			}

			// c./d. Occupation du slot jusqu'à la fin de cet item.
			productionSlots[slotIdx] = startTS + timePerSingleItem

			// e. Accumulation du temps de production.
			totalProductionSeconds += timePerSingleItem
			totalItemsProcessed++
		}
	}

	if totalItemsProcessed < avgDistMinItems {
		return 0, 0
	}

	avgTime := int64(math.Round(float64(totalProductionSeconds) / float64(totalItemsProcessed)))
	if avgTime < avgDistFloorSec {
		avgTime = avgDistFloorSec
	}
	if avgTime > avgDistCeilSec {
		avgTime = avgDistCeilSec
	}

	return avgTime, totalItemsProcessed
}
