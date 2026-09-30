package tasks

import (
	"context"
	"fmt"
	"math"
	"sort"
	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// Paramètres du calcul des produits populaires. Chaque valeur est justifiée
// dans docs/POPULAR_PRODUCTS.md (décision entre parenthèses).
const (
	// Fenêtre et pondération du score (P4).
	popularWindowDays      = 28
	popularHalfLifeDays    = 14.0
	popularMinExposureDays = 7.0

	// Sélection par catégorie (P6, seuil calibré en P10). popularMinOrders
	// (P11) : preuve minimale en commandes réelles, pour qu'un produit récent
	// extrapolé sur popularMinExposureDays ne devienne pas populaire sur une
	// seule vente.
	popularMinScore      = 4.0
	popularMinOrders     = 3
	popularLeaderRatio   = 0.5
	popularCategoryShare = 0.25
	popularCategoryMax   = 3

	// Stabilité (P7) : un produit déjà populaire le reste tant qu'il atteint
	// 75 % des seuils, et passe devant à score proche.
	popularKeepRatio      = 0.75
	popularIncumbentBonus = 1.1
)

// UpdatePopularProducts : mise à jour nocturne des produits populaires.
//
// Les SELECT tournent hors transaction, puis chaque marchand est mis à jour
// dans une transaction courte. Une erreur sur un marchand n'interrompt pas
// les autres.
func (tm *TasksManager) UpdatePopularProducts() {
	ctx := context.Background()
	tm.logInfo("[CRON] UpdatePopularProducts: démarrage")

	merchants, err := tm.collectIDs(ctx,
		"SELECT DISTINCT m.id FROM merchant m INNER JOIN subscriptions s ON s.merchant_id = "+tskMerchantJoinCast())
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

// popularCandidate est un produit affichable d'un marchand, avec son score.
type popularCandidate struct {
	ProductID  int64
	Category   string
	Score      float64
	Orders     int
	WasPopular bool
}

// popularCandidatesQuery liste les produits candidats d'un marchand et la
// somme pondérée de leurs commandes (P1 à P5). Syntaxe Postgres uniquement
// (make_interval, extract(epoch ...)) : c'est le seul moteur en production.
//
// Paramètres : merchant_id, fenêtre en jours, demi-vie en jours, merchant_id.
const popularCandidatesQuery = `
	WITH order_lines AS (
		SELECT DISTINCT o.order_id, o.creation_date, oi.product_id
		FROM orders o
		INNER JOIN orderitems oi ON oi.order_id = o.order_id
		WHERE o.merchant_id = ?
		  AND o.creation_date >= now() - make_interval(days => ?)
		  AND o.state IN ('CLOSED', 'DONE')
		  AND upper(o.brand_status) NOT IN ('CANCELED', 'DENIED', 'DELETED')
	),
	sales AS (
		SELECT product_id,
		       SUM(power(0.5, extract(epoch FROM now() - creation_date) / 86400.0
		                      / CAST(? AS double precision))) AS weighted,
		       COUNT(*) AS orders
		FROM order_lines
		GROUP BY product_id
	)
	SELECT p.product_id,
	       COALESCE(NULLIF(p.category, ''), g.category) AS category,
	       COALESCE(p.is_popular, FALSE),
	       CAST(COALESCE(s.weighted, 0) AS double precision),
	       COALESCE(s.orders, 0),
	       CAST(extract(epoch FROM now() - p.creation_date) / 86400.0 AS double precision)
	FROM products p
	LEFT JOIN products g
		ON g.product_id = p.by_product_of AND g.merchant_id = p.merchant_id
	INNER JOIN productcateg pc
		ON pc.merchant_id = p.merchant_id
		AND pc.merchant_categ_id = COALESCE(NULLIF(p.category, ''), g.category)
		AND pc.enabled = TRUE
	LEFT JOIN sales s ON s.product_id = p.product_id
	WHERE p.merchant_id = ?
	  AND p.enabled = TRUE
	  AND p.status IN ('1', 'available')
	  AND COALESCE(p.is_product_group, FALSE) = FALSE
	  AND (
	        p.by_product_of IS NULL OR p.by_product_of = 0
	        OR (g.is_product_group = TRUE AND g.enabled = TRUE AND g.status IN ('1', 'available'))
	  )`

// updateMerchantPopularProducts calcule puis applique les flags is_popular
// d'un seul marchand (docs/POPULAR_PRODUCTS.md).
func (tm *TasksManager) updateMerchantPopularProducts(ctx context.Context, merchantID string) error {
	candidates, err := tm.loadPopularCandidates(ctx, merchantID)
	if err != nil {
		return fmt.Errorf("candidats: %w", err)
	}

	selected := selectPopularProducts(candidates)
	ids := make([]int64, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}

	// Transaction courte scopée au marchand. On n'écrit que les changements :
	// retrait du flag hors sélection (candidats ou non, un produit désactivé
	// perd aussi son flag), puis pose sur les nouveaux élus.
	tx, err := tm.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// dbx.Wrap applique le rebind ?->$N sur la transaction (même pattern que
	// planning/swaps.Approve, rapport 29) — dbx.GetDB seul ne suffit pas ici
	// car la connexion est déjà résolue en *sql.Tx par BeginTx.
	txDB := dbx.Wrap(tx)

	if _, err := txDB.ExecContext(ctx,
		"UPDATE products SET is_popular = FALSE WHERE merchant_id = ? AND is_popular = TRUE AND NOT (product_id = ANY(?))",
		merchantID, ids); err != nil {
		tx.Rollback()
		return fmt.Errorf("retrait is_popular: %w", err)
	}

	if len(ids) > 0 {
		if _, err := txDB.ExecContext(ctx,
			"UPDATE products SET is_popular = TRUE WHERE merchant_id = ? AND product_id = ANY(?) AND is_popular IS DISTINCT FROM TRUE",
			merchantID, ids); err != nil {
			tx.Rollback()
			return fmt.Errorf("pose is_popular: %w", err)
		}
	}

	return tx.Commit()
}

// loadPopularCandidates lit les candidats d'un marchand et calcule leur score.
func (tm *TasksManager) loadPopularCandidates(ctx context.Context, merchantID string) ([]popularCandidate, error) {
	db := dbx.GetDB(ctx, tm.DB)
	rows, err := db.QueryContext(ctx, popularCandidatesQuery,
		merchantID, popularWindowDays, popularHalfLifeDays, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []popularCandidate
	for rows.Next() {
		var c popularCandidate
		var weighted, ageDays float64
		if err := rows.Scan(&c.ProductID, &c.Category, &c.WasPopular, &weighted, &c.Orders, &ageDays); err != nil {
			return nil, err
		}
		c.Score = popularScore(weighted, ageDays)
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// popularScore ramène la somme pondérée des commandes (poids 0.5^(âge/demi-vie))
// en « commandes sur la fenêtre au rythme actuel » : un produit vendu
// régulièrement une fois par jour depuis toujours vaut popularWindowDays.
//
// La somme des poids d'une vente régulière sur d jours vaut
// demi-vie × (1 − 0.5^(d/demi-vie)) / ln 2 par vente quotidienne. Pour un
// produit récent, d est son nombre de jours de présence, borné à
// [popularMinExposureDays, popularWindowDays] : il n'a pas besoin d'une
// fenêtre complète, mais un seul bon jour ne suffit pas.
func popularScore(weighted, ageDays float64) float64 {
	if weighted <= 0 {
		return 0
	}
	d := math.Min(popularWindowDays, math.Max(popularMinExposureDays, ageDays))
	exposure := popularHalfLifeDays * (1 - math.Pow(0.5, d/popularHalfLifeDays)) / math.Ln2
	return weighted * popularWindowDays / exposure
}

// selectPopularProducts applique les règles de sélection par catégorie (P6,
// P7, P11) et retourne les produits élus.
func selectPopularProducts(candidates []popularCandidate) map[int64]bool {
	byCategory := make(map[string][]popularCandidate)
	for _, c := range candidates {
		byCategory[c.Category] = append(byCategory[c.Category], c)
	}

	selected := make(map[int64]bool)
	for _, products := range byCategory {
		limit := popularCategoryLimit(len(products))

		leader := 0.0
		for _, p := range products {
			leader = math.Max(leader, p.Score)
		}

		sort.Slice(products, func(i, j int) bool {
			ki, kj := popularRankKey(products[i]), popularRankKey(products[j])
			if ki != kj {
				return ki > kj
			}
			return products[i].ProductID < products[j].ProductID
		})

		count := 0
		for _, p := range products {
			if count >= limit {
				break
			}
			tolerance := 1.0
			if p.WasPopular {
				tolerance = popularKeepRatio
			}
			if p.Orders >= popularMinOrders &&
				p.Score >= popularMinScore*tolerance &&
				p.Score >= popularLeaderRatio*leader*tolerance {
				selected[p.ProductID] = true
				count++
			}
		}
	}
	return selected
}

// popularCategoryLimit : 25 % des produits de la catégorie, arrondi à
// l'inférieur, au minimum 1 et au maximum popularCategoryMax.
func popularCategoryLimit(size int) int {
	limit := int(math.Floor(float64(size) * popularCategoryShare))
	return min(popularCategoryMax, max(1, limit))
}

// popularRankKey : score de classement, avec l'avantage du produit déjà
// populaire. Les seuils, eux, s'appliquent au score brut.
func popularRankKey(c popularCandidate) float64 {
	if c.WasPopular {
		return c.Score * popularIncumbentBonus
	}
	return c.Score
}

// collectIDs exécute une requête retournant une colonne d'IDs et la lit
// intégralement en mémoire avant de rendre la main, pour libérer la
// connexion au plus tôt.
func (tm *TasksManager) collectIDs(ctx context.Context, query string, args ...interface{}) ([]string, error) {
	db := dbx.GetDB(ctx, tm.DB)
	rows, err := db.QueryContext(ctx, query, args...)
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
