package tasks

import (
	"context"
	"database/sql"
	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// Fenêtre et bornes calquées sur UpdateAverageDistributionTime (distribution.go)
// pour rester cohérent avec le seul autre job de moyenne glissante du repo.
const (
	avgDeliveryCalcIntervalMinutes = 1440 // fenêtre d'historique analysée (24h)
	avgDeliveryMinOrders           = 3    // commandes livraison minimum pour produire une moyenne
	avgDeliveryFloorSec            = 60   // borne de sécurité basse du résultat (s)
	avgDeliveryCeilSec             = 3600 // borne de sécurité haute du résultat (s)
)

// UpdateAverageDeliveryTime met à jour, par marchand, la moyenne glissante du
// temps de trajet livraison (average_delivery_time), à partir des valeurs
// orders.delivery_travel_seconds déjà capturées côté client (Google Maps sur
// le POS, OSRM sur ScanNOrder) au moment de la création/mise à jour de la
// commande. Sert de filet de sécurité (resolveDeliveryTravelSeconds côté
// order_life_cycle) et d'estimation affichée avant checkout sur ScanNOrder.
//
// Contrainte pool MySQL (1 connexion max) : la lecture est faite intégralement
// en mémoire avant l'upsert, comme UpdateAverageDistributionTime.
func (tm *TasksManager) UpdateAverageDeliveryTime() {
	ctx := context.Background()
	tm.logInfo("[CRON] UpdateAverageDeliveryTime: démarrage")
	db := dbx.GetDB(ctx, tm.DB)

	query := `
		SELECT o.merchant_id, AVG(o.delivery_travel_seconds), COUNT(*)
		FROM orders o
		WHERE o.order_type = 'DELIVERY'
			AND o.delivery_travel_seconds IS NOT NULL
			AND o.creation_date >= ` + tskNowMinusMinutes() + `
		GROUP BY o.merchant_id`

	rows, err := db.QueryContext(ctx, query, avgDeliveryCalcIntervalMinutes)
	if err != nil {
		tm.logError("[CRON] UpdateAverageDeliveryTime: lecture échouée", zap.Error(err))
		return
	}

	type merchantAverage struct {
		id      string
		seconds int64
		orders  int64
	}
	var averages []merchantAverage
	for rows.Next() {
		var id string
		var avgSeconds sql.NullFloat64
		var count int64
		if err := rows.Scan(&id, &avgSeconds, &count); err != nil {
			tm.logError("[CRON] UpdateAverageDeliveryTime: scan échoué", zap.Error(err))
			continue
		}
		if !avgSeconds.Valid || count < avgDeliveryMinOrders {
			continue // pas assez de données, la valeur précédente reste en place
		}
		seconds := int64(avgSeconds.Float64 + 0.5)
		if seconds < avgDeliveryFloorSec {
			seconds = avgDeliveryFloorSec
		}
		if seconds > avgDeliveryCeilSec {
			seconds = avgDeliveryCeilSec
		}
		averages = append(averages, merchantAverage{id: id, seconds: seconds, orders: count})
	}
	if err := rows.Err(); err != nil {
		tm.logError("[CRON] UpdateAverageDeliveryTime: itération interrompue", zap.Error(err))
	}
	rows.Close()

	updated := 0
	for _, m := range averages {
		upsertQuery := `
			INSERT INTO average_delivery_time (merchant_id, delivery_time_seconds)
			VALUES (?, ?)
			ON DUPLICATE KEY UPDATE delivery_time_seconds = ?`
		upsertArgs := []interface{}{m.id, m.seconds, m.seconds}
		if dbx.ActiveDialect() == dbx.Postgres {
			upsertQuery = `
			INSERT INTO average_delivery_time (merchant_id, delivery_time_seconds)
			VALUES (?, ?)
			ON CONFLICT (merchant_id) DO UPDATE SET delivery_time_seconds = EXCLUDED.delivery_time_seconds`
			upsertArgs = []interface{}{m.id, m.seconds}
		}
		if _, err := db.ExecContext(ctx, upsertQuery, upsertArgs...); err != nil {
			tm.logError("[CRON] UpdateAverageDeliveryTime: upsert échoué",
				zap.String("merchant_id", m.id), zap.Error(err))
			continue
		}
		updated++
	}

	tm.logInfo("[CRON] UpdateAverageDeliveryTime: terminé",
		zap.Int("marchands_analyses", len(averages)),
		zap.Int("marchands_mis_a_jour", updated))
}
