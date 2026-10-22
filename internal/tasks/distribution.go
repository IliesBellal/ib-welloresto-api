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

	// avgDistConfidenceK pondère la moyenne brute du cycle par la taille de
	// l'échantillon : alpha = items / (items + avgDistConfidenceK). Peu
	// d'items (ouverture, matin creux) → alpha faible, la valeur précédente
	// domine ; beaucoup d'items (coup de feu) → alpha → 1, la moyenne suit
	// vite la réalité. Absent du calcul lors du tout premier historique d'un
	// marchand (pas de valeur précédente à pondérer). Plus la constante est
	// grande, plus il faut d'items pour faire bouger significativement la
	// moyenne en un seul cycle.
	avgDistConfidenceK = 25.0

	// Garde-fou de variation maximale par cycle de CRON (ceinture et
	// bretelles au-dessus du lissage par confiance) : la valeur lissée ne
	// peut jamais s'écarter de la valeur précédente de plus de
	// max(avgDistMaxDeltaAbsoluteSec, ancienne_valeur * avgDistMaxDeltaRelative)
	// en un seul cycle. Le max des deux évite qu'un temps de préparation très
	// court (ex: 20s) soit verrouillé par un pourcentage dérisoire en
	// secondes absolues.
	avgDistMaxDeltaRelative    = 0.25 // ±25 % de l'ancienne valeur
	avgDistMaxDeltaAbsoluteSec = 60   // ±60s plancher
)

// UpdateAverageDistributionTime : met à jour le temps de préparation moyen
// par article de chaque marchand, sur les orderitems servis des dernières 24h.
//
// Le portage du script PHP simulait une file de production sur
// concurrent_preparation_capacity postes, mais le résultat n'additionnait que
// les temps unitaires : la simulation et la capacité n'avaient aucun effet.
// Le calcul se fait donc en une seule agrégation SQL pour tous les marchands
// (computeDistributionStats), au lieu d'une requête par marchand qui relisait
// tout l'historique de chacun de ses produits. La capacité ne sert plus qu'à
// choisir les marchands traités (NULL ou 0 : ignoré, comme avant) ; elle reste
// utilisée par l'estimation (distributiontime.EstimatedSeconds).
func (tm *TasksManager) UpdateAverageDistributionTime() {
	ctx := context.Background()
	tm.logInfo("[CRON] UpdateAverageDistributionTime: démarrage")
	db := dbx.GetDB(ctx, tm.DB)

	type merchantCapacity struct {
		id       string
		capacity int64
		previous sql.NullInt64 // valeur actuelle en base, si déjà calculée
	}

	// LEFT JOIN average_distribution_time : récupère la valeur précédente en
	// même temps que la liste des marchands, sans requête supplémentaire —
	// nécessaire au lissage par confiance (avgDistConfidenceK).
	merchantsQuery := `
		SELECT mp.merchant_id, mp.concurrent_preparation_capacity, adt.distribution_time
		FROM merchant m
		INNER JOIN merchant_parameters mp ON mp.merchant_id = ` + tskMerchantJoinCast() + `
		LEFT JOIN average_distribution_time adt ON adt.merchant_id = mp.merchant_id`

	rows, err := db.QueryContext(ctx, merchantsQuery)
	if err != nil {
		tm.logError("[CRON] UpdateAverageDistributionTime: liste marchands échouée", zap.Error(err))
		return
	}

	var merchants []merchantCapacity
	for rows.Next() {
		var id string
		var capacity, previous sql.NullInt64
		if err := rows.Scan(&id, &capacity, &previous); err != nil {
			tm.logError("[CRON] UpdateAverageDistributionTime: scan marchand échoué", zap.Error(err))
			continue
		}
		merchants = append(merchants, merchantCapacity{id: id, capacity: capacity.Int64, previous: previous})
	}
	if err := rows.Err(); err != nil {
		tm.logError("[CRON] UpdateAverageDistributionTime: itération marchands interrompue", zap.Error(err))
	}
	rows.Close()

	stats, err := tm.computeDistributionStats(ctx)
	if err != nil {
		tm.logError("[CRON] UpdateAverageDistributionTime: agrégation des orderitems échouée", zap.Error(err))
		return
	}

	updated := 0
	for _, m := range merchants {
		// Comme en PHP : sans capacité configurée (NULL ou 0), le marchand
		// n'est pas traité.
		if m.capacity < 1 {
			continue
		}

		avgTime, items := averageDistributionTime(stats[m.id])
		if items < avgDistMinItems {
			continue // pas assez de données, la valeur précédente reste en place
		}

		smoothedTime := smoothDistributionTime(m.previous, avgTime, items)

		upsertQuery := `
			INSERT INTO average_distribution_time (merchant_id, distribution_time)
			VALUES (?, ?)
			ON DUPLICATE KEY UPDATE distribution_time = ?`
		upsertArgs := []interface{}{m.id, smoothedTime, smoothedTime}
		if dbx.ActiveDialect() == dbx.Postgres {
			// Pas de syntaxe commune pour l'upsert (ON DUPLICATE KEY UPDATE vs
			// ON CONFLICT) : average_distribution_time.merchant_id est la PK,
			// donc ON CONFLICT (merchant_id) cible bien la même contrainte.
			upsertQuery = `
			INSERT INTO average_distribution_time (merchant_id, distribution_time)
			VALUES (?, ?)
			ON CONFLICT (merchant_id) DO UPDATE SET distribution_time = EXCLUDED.distribution_time`
			upsertArgs = []interface{}{m.id, smoothedTime}
		}
		if _, err := db.ExecContext(ctx, upsertQuery, upsertArgs...); err != nil {
			tm.logError("[CRON] UpdateAverageDistributionTime: upsert échoué",
				zap.String("merchant_id", m.id), zap.Error(err))
			continue
		}
		updated++

		tm.logInfo("[CRON] UpdateAverageDistributionTime: marchand mis à jour",
			zap.String("merchant_id", m.id),
			zap.Int64("distribution_time_s", smoothedTime),
			zap.Int64("distribution_time_brut_s", avgTime),
			zap.Int64("distribution_time_precedent_s", m.previous.Int64),
			zap.Int64("capacite", m.capacity),
			zap.Int("items", items))
	}

	tm.logInfo("[CRON] UpdateAverageDistributionTime: terminé",
		zap.Int("merchants_analyses", len(merchants)),
		zap.Int("merchants_mis_a_jour", updated))
}

// merchantDistributionStats agrège, pour un marchand, les orderitems servis de
// la fenêtre : lines compte les lignes retenues par le SQL, items et
// totalSeconds ne portent que sur les lignes exploitables (quantité > 0 et
// temps unitaire arrondi > 0).
type merchantDistributionStats struct {
	lines        int64
	items        int64
	totalSeconds float64
}

// computeDistributionStats agrège en une seule requête, pour tous les
// marchands, les orderitems servis sur les avgDistCalcIntervalMinutes
// dernières minutes. Le temps unitaire d'une ligne est round(turnaround /
// quantité) ; totalSeconds en est la somme pondérée par la quantité.
//
// Lecture seule. Les marchands sans orderitem dans la fenêtre sont absents
// de la map.
func (tm *TasksManager) computeDistributionStats(ctx context.Context) (map[string]merchantDistributionStats, error) {
	db := dbx.GetDB(ctx, tm.DB)

	turnaround := tskSecondsBetween("oi.ordered_on", "oi.distributed_on")
	perItem := "ROUND(CAST(" + turnaround + " AS DECIMAL(20,4)) / oi.quantity)"
	usable := "oi.quantity > 0 AND " + perItem + " > 0"

	query := `
		SELECT
			p.merchant_id,
			COUNT(oi.quantity),
			COALESCE(SUM(CASE WHEN ` + usable + ` THEN oi.quantity END), 0),
			COALESCE(SUM(CASE WHEN ` + usable + ` THEN oi.quantity * ` + perItem + ` END), 0)
		FROM orderitems oi
		INNER JOIN products p ON oi.product_id = p.product_id
		WHERE
			oi.distributed_quantity > 0 AND oi.distributed_on IS NOT NULL
			AND oi.ordered_on >= ` + tskNowMinusMinutes() + `
			AND ` + turnaround + ` BETWEEN ? AND ?
		GROUP BY p.merchant_id`

	rows, err := db.QueryContext(ctx, query,
		avgDistCalcIntervalMinutes, avgDistTurnaroundMinSec, avgDistTurnaroundMaxSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]merchantDistributionStats)
	for rows.Next() {
		var merchantID string
		var s merchantDistributionStats
		if err := rows.Scan(&merchantID, &s.lines, &s.items, &s.totalSeconds); err != nil {
			return nil, err
		}
		stats[merchantID] = s
	}
	return stats, rows.Err()
}

// averageDistributionTime retourne le temps unitaire moyen borné
// [avgDistFloorSec, avgDistCeilSec] et le nombre d'items pris en compte, ou
// (0, 0) s'il y a moins de avgDistMinItems lignes ou items exploitables.
func averageDistributionTime(s merchantDistributionStats) (int64, int) {
	if s.lines < avgDistMinItems || s.items < avgDistMinItems {
		return 0, 0
	}

	avgTime := int64(math.Round(s.totalSeconds / float64(s.items)))
	if avgTime < avgDistFloorSec {
		avgTime = avgDistFloorSec
	}
	if avgTime > avgDistCeilSec {
		avgTime = avgDistCeilSec
	}

	return avgTime, int(s.items)
}

// smoothDistributionTime pondère la moyenne brute d'un cycle de CRON
// (rawAvgTime, calculée sur `items` échantillons) par la valeur précédente
// stockée en base (previous), puis plafonne la variation autorisée en un
// seul cycle (avgDistMaxDeltaRelative / avgDistMaxDeltaAbsoluteSec).
//
// Si previous n'existe pas encore (tout premier calcul du marchand), la
// valeur brute est retournée telle quelle : il n'y a rien à lisser ni à
// plafonner sans historique.
//
// rawAvgTime est déjà borné à [avgDistFloorSec, avgDistCeilSec] par
// averageDistributionTime, et previous l'était lors de son propre
// calcul : une moyenne pondérée des deux, ensuite resserrée par le
// plafonnement, reste donc toujours dans ces bornes sans reclamp explicite.
func smoothDistributionTime(previous sql.NullInt64, rawAvgTime int64, items int) int64 {
	if !previous.Valid {
		return rawAvgTime
	}
	old := previous.Int64

	alpha := float64(items) / (float64(items) + avgDistConfidenceK)
	smoothed := int64(math.Round(float64(old)*(1-alpha) + float64(rawAvgTime)*alpha))

	maxDelta := int64(math.Round(math.Max(avgDistMaxDeltaAbsoluteSec, float64(old)*avgDistMaxDeltaRelative)))
	if smoothed > old+maxDelta {
		smoothed = old + maxDelta
	} else if smoothed < old-maxDelta {
		smoothed = old - maxDelta
	}

	return smoothed
}
