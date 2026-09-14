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
//
// Plancher FIFO (ajout ultérieur, hors traduction de la procédure d'origine) :
// sans lui, une commande créée juste après une mise à jour CRON de
// average_distribution_time peut obtenir un estimated_ready antérieur à celui
// d'une commande OPEN déjà en file, non planifiée — visible en cuisine/côté
// client comme une commande qui "double" une commande plus ancienne. Le
// plancher force estimated_ready(nouvelle) >= MAX(estimated_ready) des
// commandes OPEN non planifiées déjà enregistrées + son propre temps de
// préparation (nb produits × temps par produit). Il ne peut que retarder
// l'estimation, jamais l'avancer, et ne coûte aucun aller-retour DB
// supplémentaire (sous-requête scalaire dans la même requête).
const estimateQueryFmt = `
	SELECT ROUND(
		GREATEST(
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
			),
			CASE WHEN MAX(fifo.max_ready) IS NULL THEN 0
				ELSE GREATEST(0, %s) + (? * LEAST(adt.distribution_time, 180))
			END
		), 0) AS estimated_distribution_time
	FROM average_distribution_time adt
	INNER JOIN merchant_parameters mp ON mp.merchant_id = adt.merchant_id
	LEFT JOIN orders o ON adt.merchant_id = o.merchant_id
		AND o.state = 'OPEN'
		AND (o.scheduled = FALSE OR (o.scheduled = TRUE AND %s + INTERVAL '90' MINUTE >= o.estimated_ready))
	LEFT JOIN orderitems oi ON o.order_id = oi.order_id AND oi.isDistributed = FALSE
	LEFT JOIN (
		SELECT o2.merchant_id, MAX(o2.estimated_ready) AS max_ready
		FROM orders o2
		WHERE o2.merchant_id = ? AND o2.state = 'OPEN' AND o2.scheduled = FALSE
		GROUP BY o2.merchant_id
	) fifo ON fifo.merchant_id = adt.merchant_id
	WHERE adt.merchant_id = ?
	GROUP BY adt.merchant_id, mp.merchant_id`

// secondsBetweenExpr : équivalent portable de TIMESTAMPDIFF(SECOND, from, to)
// (MySQL) / EXTRACT(EPOCH FROM (to - from)) (Postgres), positif quand `to` est
// postérieur à `from`. Même pattern que tskSecondsBetween
// (internal/tasks/sqlcompat.go), dupliqué ici faute de helper partagé entre
// packages pour ces fragments SQL.
func secondsBetweenExpr(from, to string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "EXTRACT(EPOCH FROM (" + to + " - " + from + "))"
	}
	return "TIMESTAMPDIFF(SECOND, " + from + ", " + to + ")"
}

// EstimatedSeconds retourne le temps de distribution estimé en secondes pour un
// merchant, en tenant compte du nombre de produits de la commande en cours de
// création. found=false quand le merchant n'a pas de ligne
// average_distribution_time/merchant_parameters (la procédure ne renvoyait
// alors aucune ligne — chaque appelant garde sa valeur par défaut).
func EstimatedSeconds(ctx context.Context, database *sql.DB, merchantID string, nbProductsCurrentOrder int) (int, bool, error) {
	db := dbx.GetDB(ctx, database)

	fifoSecondsExpr := secondsBetweenExpr(dbx.UTCNow(), "MAX(fifo.max_ready)")
	query := fmt.Sprintf(estimateQueryFmt, fifoSecondsExpr, dbx.UTCNow())

	var seconds sql.NullFloat64
	err := db.QueryRowContext(ctx, query,
		nbProductsCurrentOrder, nbProductsCurrentOrder, merchantID, merchantID).Scan(&seconds)
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
