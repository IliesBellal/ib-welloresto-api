// Package distributiontime remplace la procédure stockée MySQL
// GET_AVERAGE_DISTRIBUTION_TIME par une requête SQL directe, exécutable
// telle quelle sur MySQL et PostgreSQL via dbx. Partagé par les modules
// orders, order_life_cycle et ubereats (seuls appelants historiques du CALL).
package distributiontime

import (
	"context"
	"database/sql"
	"fmt"

	"welloresto-api/internal/database/dbx"
)

// Traduction du corps de la procédure — écarts voulus vis-à-vis de l'original :
//
//   - IFNULL → COALESCE (portable, même sémantique).
//   - DATE_ADD(UTC_TIMESTAMP, INTERVAL 90 MINUTE) → `%s + INTERVAL '90' MINUTE`
//     avec dbx.UTCNow() : la syntaxe INTERVAL '90' MINUTE est acceptée par les
//     deux dialectes, seul le « maintenant » diffère (UTC_TIMESTAMP() vs now()).
//     L'horloge reste celle de la base (comme dans la procédure et le reste du
//     repo) plutôt qu'un time.Time passé en paramètre, dont l'encodage dépend
//     du driver (paramètre `loc` du DSN MySQL) — un seul point de branchement
//     de dialecte, déjà existant, et aucune dépendance à la config du driver.
//   - o.scheduled = 0/1 → = FALSE / = TRUE (boolean en cible Postgres,
//     littéraux valides sur TINYINT(1) MySQL — cf. pattern 14-tier1 §3).
//   - CAST(numérateur AS DECIMAL(20,4)) : sans lui, Postgres ferait une
//     division entière (int/int tronqué) là où MySQL produit un décimal.
//   - NULLIF(capacity, 0) : MySQL retourne NULL sur division par zéro (rattrapé
//     par le IFNULL de la procédure) ; Postgres lèverait division_by_zero.
//     NULLIF reproduit le comportement MySQL sur les deux dialectes.
//   - GROUP BY étendu à mp.merchant_id (PK de merchant_parameters) : le GROUP BY
//     original (adt.merchant_id, mp.minimum_preparation_time) laissait des
//     colonnes non agrégées hors dépendance fonctionnelle — rejeté par Postgres
//     et par MySQL en mode ONLY_FULL_GROUP_BY. Grouper par les deux PK rend
//     toutes les colonnes sélectionnées fonctionnellement dépendantes, sans
//     changer le résultat (au plus une ligne par merchant).
const estimateQueryFmt = `
	SELECT ROUND(
		LEAST(
			GREATEST(
				COALESCE(
					CAST((COALESCE(SUM(oi.quantity - oi.distributed_quantity), 0) + ?) * LEAST(adt.distribution_time, 180) AS DECIMAL(20,4))
						/ NULLIF(mp.concurrent_preparation_capacity, 0),
					mp.minimum_preparation_time
				),
				mp.minimum_preparation_time
			),
			mp.maximum_preparation_time
		), 0) AS estimated_distribution_time
	FROM average_distribution_time adt
	INNER JOIN merchant_parameters mp ON mp.merchant_id = adt.merchant_id
	LEFT JOIN orders o ON adt.merchant_id = o.merchant_id
		AND o.state = 'OPEN'
		AND (o.scheduled = FALSE OR (o.scheduled = TRUE AND %s + INTERVAL '90' MINUTE >= o.estimated_ready))
	LEFT JOIN orderitems oi ON o.order_id = oi.order_id AND oi.isDistributed = FALSE
	WHERE adt.merchant_id = ?
	GROUP BY adt.merchant_id, mp.merchant_id`

// EstimatedSeconds retourne le temps de distribution estimé en secondes pour un
// merchant, en tenant compte du nombre de produits de la commande en cours de
// création. found=false quand le merchant n'a pas de ligne
// average_distribution_time/merchant_parameters (la procédure ne renvoyait
// alors aucune ligne — chaque appelant garde sa valeur par défaut).
func EstimatedSeconds(ctx context.Context, database *sql.DB, merchantID string, nbProductsCurrentOrder int) (int, bool, error) {
	db := dbx.GetDB(ctx, database)

	query := fmt.Sprintf(estimateQueryFmt, dbx.UTCNow())

	var seconds sql.NullFloat64
	err := db.QueryRowContext(ctx, query, nbProductsCurrentOrder, merchantID).Scan(&seconds)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if !seconds.Valid {
		return 0, false, nil
	}
	return int(seconds.Float64), true, nil
}
