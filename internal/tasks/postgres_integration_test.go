//go:build postgres_integration

package tasks

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
)

// Ces tests ciblent délibérément les fonctions PAR MARCHAND (non exportées :
// computeAverageDistributionTime, updateMerchantPopularProducts,
// processUpsellPatternsForMerchant), jamais les points d'entrée cron exportés
// (UpdateAverageDistributionTime, UpdatePopularProducts, RecomputeUpsellPatterns,
// CloseOrders, DenyOrders, CapturePayments, CancelPayments).
//
// Raison : ces points d'entrée bouclent sur TOUS les marchands / commandes /
// paiements de la base sans filtre — et le Postgres Docker de dev utilisé ici
// contient une copie chargée de données réelles (rapports 36-43, 48-51), pas
// seulement des fixtures synthétiques. Un test qui appellerait
// tm.CapturePayments()/tm.CancelPayments() réel y trouverait de vraies lignes
// stripe_payments (vérifié en lecture seule avant d'écrire ce fichier : 94
// lignes REQUIRES_CONFIRMATION) et risquerait un paiement/remboursement Stripe
// réel si StripeService avait été renseigné, ou un panic sur service nil dans
// le cas contraire. Les fonctions par marchand, elles, sont scopées par
// `WHERE merchant_id = ?` et ne touchent jamais que les données du marchand
// sentinelle créé par ce test. La portabilité des requêtes globales
// (CloseOrders/DenyOrders/CapturePayments/CancelPayments) est vérifiée par
// TestSQLCompatFragments_Postgres ci-dessous, qui exerce les mêmes fragments
// SQL (tskMinutesSince, tskMerchantJoinCast, ...) sur des tables dérivées
// isolées, sans toucher aux tables réelles orders/payments/merchant.

// seedTaskMerchant insère un marchand sentinelle minimal (+ merchant_parameters)
// et retourne son id (chaîne, comme le voit le code applicatif) et une
// fonction de nettoyage.
func seedTaskMerchant(t *testing.T, db *sql.DB, ctx context.Context, capacity int) string {
	t.Helper()

	var merchantIntID int64
	// token est varchar(20) en cible : rester court.
	token := "it" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullName, address, street_number, street, zip_code, city, SIRET, web_site, merchantTel, token)
		VALUES ('itest-tasks', 'addr', '1', 'street', '75001', 'Paris', $1, 'https://itest.example', '0000000000', $1)
		RETURNING id`, token).Scan(&merchantIntID)
	if err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, concurrent_preparation_capacity)
		VALUES ($1, now(), $2)`, merchantID, capacity); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = db.ExecContext(cctx, `DELETE FROM orderitems WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(cctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(cctx, `DELETE FROM products WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(cctx, `DELETE FROM average_distribution_time WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(cctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(cctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	return merchantID
}

// --- Fragments SQL portables (internal/tasks/sqlcompat.go) --------------

func TestSQLCompatFragments_Postgres(t *testing.T) {
	rawDB := pgtest.Open(t)
	ctx := context.Background()
	db := dbx.GetDB(ctx, rawDB)

	t.Run("tskMinutesSince", func(t *testing.T) {
		past := time.Now().Add(-42 * time.Minute)
		var minutes float64
		q := "SELECT " + tskMinutesSince("?")
		if err := db.QueryRowContext(ctx, q, past).Scan(&minutes); err != nil {
			t.Fatalf("tskMinutesSince query failed against postgres: %v", err)
		}
		if minutes < 41 || minutes > 43 {
			t.Fatalf("expected ~42 minutes, got %v", minutes)
		}
	})

	t.Run("tskSecondsBetween", func(t *testing.T) {
		from := time.Now().Add(-90 * time.Second)
		to := time.Now()
		var seconds int64
		// ::timestamptz nécessaire ici seulement parce que le test lie deux
		// paramètres nus sans colonne de contexte ("operator is not unique") ;
		// en production `col` est toujours une vraie colonne typée.
		// tskSecondsBetween écrit "to - from" dans le texte SQL : les deux
		// placeholders étant des chaînes identiques, l'ordre de liaison suit
		// l'ordre d'apparition dans le texte généré (to en premier).
		q := "SELECT " + tskSecondsBetween("?::timestamptz", "?::timestamptz")
		if err := db.QueryRowContext(ctx, q, to, from).Scan(&seconds); err != nil {
			t.Fatalf("tskSecondsBetween query failed against postgres: %v", err)
		}
		if seconds < 89 || seconds > 91 {
			t.Fatalf("expected ~90 seconds, got %d", seconds)
		}
	})

	t.Run("tskUnixTimestamp", func(t *testing.T) {
		ts := time.Now().Truncate(time.Second)
		var unix int64
		q := "SELECT " + tskUnixTimestamp("?::timestamptz")
		if err := db.QueryRowContext(ctx, q, ts).Scan(&unix); err != nil {
			t.Fatalf("tskUnixTimestamp query failed against postgres: %v", err)
		}
		if unix != ts.Unix() {
			t.Fatalf("expected unix %d, got %d", ts.Unix(), unix)
		}
	})

	t.Run("tskNowMinusMinutes", func(t *testing.T) {
		q := "SELECT COUNT(*) FROM (SELECT ?::timestamptz AS ts) x WHERE x.ts <= " + tskNowMinusMinutes()
		var count int
		past := time.Now().Add(-100 * time.Minute)
		if err := db.QueryRowContext(ctx, q, past, 90).Scan(&count); err != nil {
			t.Fatalf("tskNowMinusMinutes query failed against postgres: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected timestamp 100min old to satisfy now-90min bound, count=%d", count)
		}
		recent := time.Now().Add(-10 * time.Minute)
		if err := db.QueryRowContext(ctx, q, recent, 90).Scan(&count); err != nil {
			t.Fatalf("tskNowMinusMinutes query failed against postgres: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected timestamp 10min old to NOT satisfy now-90min bound, count=%d", count)
		}
	})

	t.Run("tskNowMinusDays", func(t *testing.T) {
		q := "SELECT COUNT(*) FROM (SELECT ?::timestamptz AS ts) x WHERE x.ts >= " + tskNowMinusDays()
		var count int
		within := time.Now().Add(-10 * 24 * time.Hour)
		if err := db.QueryRowContext(ctx, q, within, 90).Scan(&count); err != nil {
			t.Fatalf("tskNowMinusDays query failed against postgres: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected timestamp 10 days old to satisfy now-90days window, count=%d", count)
		}
		outside := time.Now().Add(-100 * 24 * time.Hour)
		if err := db.QueryRowContext(ctx, q, outside, 90).Scan(&count); err != nil {
			t.Fatalf("tskNowMinusDays query failed against postgres: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected timestamp 100 days old to NOT satisfy now-90days window, count=%d", count)
		}
	})

	t.Run("tskNowMinus30Days", func(t *testing.T) {
		q := "SELECT COUNT(*) FROM (SELECT ?::timestamptz AS ts) x WHERE x.ts >= " + tskNowMinus30Days()
		var count int
		within := time.Now().Add(-10 * 24 * time.Hour)
		if err := db.QueryRowContext(ctx, q, within).Scan(&count); err != nil {
			t.Fatalf("tskNowMinus30Days query failed against postgres: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected timestamp 10 days old to satisfy 30-day window, count=%d", count)
		}
		outside := time.Now().Add(-40 * 24 * time.Hour)
		if err := db.QueryRowContext(ctx, q, outside).Scan(&count); err != nil {
			t.Fatalf("tskNowMinus30Days query failed against postgres: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected timestamp 40 days old to NOT satisfy 30-day window, count=%d", count)
		}
	})

	t.Run("tskMerchantJoinCast", func(t *testing.T) {
		// Table dérivée aliasée `m` (comme dans le code réel) avec un id
		// integer, comparée à un merchant_id texte — reproduit exactement le
		// problème "operator does not exist: integer = character varying"
		// observé sur orders/merchant_parameters/subscriptions.
		q := "SELECT 1 FROM (SELECT 42 AS id) m WHERE " + tskMerchantJoinCast() + " = ?"
		var one int
		if err := db.QueryRowContext(ctx, q, "42").Scan(&one); err != nil {
			t.Fatalf("tskMerchantJoinCast query failed against postgres: %v", err)
		}
		if one != 1 {
			t.Fatalf("expected match, got %d", one)
		}
	})
}

// --- UpdateAverageDistributionTime (par marchand) ------------------------

func TestComputeAndStoreAverageDistributionTime_Postgres(t *testing.T) {
	rawDB := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTaskMerchant(t, rawDB, ctx, 2)

	var productID int64
	if err := rawDB.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'itest product', 500, 'itest')
		RETURNING product_id`, merchantID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	var orderID int64
	if err := rawDB.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, price, tva, ht, created_by)
		VALUES ($1, 1, 'PENDING_APPROVAL', 500, 0, 500, 'itest')
		RETURNING order_id`, merchantID).Scan(&orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	// 5 orderitems, turnaround identique 100s chacun -> moyenne pondérée
	// attendue = 100s quelle que soit la capacité (cf. distribution_test.go
	// TestSimulateAverageDistributionTime_CapacityDoesNotPanic).
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		orderedOn := now.Add(-time.Duration(300-i*10) * time.Second)
		distributedOn := orderedOn.Add(100 * time.Second)
		if _, err := rawDB.ExecContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, distributed_quantity, price, ordered_on, distributed_on)
			VALUES ($1, $2, $3, 1, 1, 500, $4, $5)`,
			orderID, productID, merchantID, orderedOn, distributedOn); err != nil {
			t.Fatalf("seed orderitem %d: %v", i, err)
		}
	}

	tm := &TasksManager{DB: rawDB}
	avgTime, items, err := tm.computeAverageDistributionTime(ctx, merchantID, 2)
	if err != nil {
		t.Fatalf("computeAverageDistributionTime failed against postgres: %v", err)
	}
	if items != 5 {
		t.Fatalf("expected 5 items processed, got %d", items)
	}
	if avgTime != 100 {
		t.Fatalf("expected avgTime=100, got %d", avgTime)
	}

	// Upsert : réplique exacte de la branche Postgres de UpdateAverageDistributionTime
	// (distribution.go), scopée au seul merchantID sentinelle.
	upsert := func(value int64) {
		if _, err := dbx.GetDB(ctx, rawDB).ExecContext(ctx, `
			INSERT INTO average_distribution_time (merchant_id, distribution_time)
			VALUES (?, ?)
			ON CONFLICT (merchant_id) DO UPDATE SET distribution_time = EXCLUDED.distribution_time`,
			merchantID, value); err != nil {
			t.Fatalf("upsert average_distribution_time failed: %v", err)
		}
	}

	upsert(avgTime)
	var stored int64
	if err := rawDB.QueryRowContext(ctx, `SELECT distribution_time FROM average_distribution_time WHERE merchant_id = $1`, merchantID).Scan(&stored); err != nil {
		t.Fatalf("read back average_distribution_time: %v", err)
	}
	if stored != 100 {
		t.Fatalf("expected stored=100 after insert, got %d", stored)
	}

	// Rejoue avec une valeur différente : vérifie le chemin ON CONFLICT DO UPDATE.
	upsert(200)
	if err := rawDB.QueryRowContext(ctx, `SELECT distribution_time FROM average_distribution_time WHERE merchant_id = $1`, merchantID).Scan(&stored); err != nil {
		t.Fatalf("read back average_distribution_time after update: %v", err)
	}
	if stored != 200 {
		t.Fatalf("expected stored=200 after ON CONFLICT update, got %d", stored)
	}
	var rowCount int
	if err := rawDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM average_distribution_time WHERE merchant_id = $1`, merchantID).Scan(&rowCount); err != nil {
		t.Fatalf("count average_distribution_time rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly 1 row (upsert, not duplicate insert), got %d", rowCount)
	}
}

// --- UpdatePopularProducts (par marchand) --------------------------------

func TestUpdateMerchantPopularProducts_Postgres(t *testing.T) {
	rawDB := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTaskMerchant(t, rawDB, ctx, 1)

	var popularProductID, staleProductID int64
	if err := rawDB.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'itest popular', 500, 'itest-cat')
		RETURNING product_id`, merchantID).Scan(&popularProductID); err != nil {
		t.Fatalf("seed popular product: %v", err)
	}
	if err := rawDB.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category, is_popular)
		VALUES ($1, 'itest stale', 500, 'itest-cat-2', TRUE)
		RETURNING product_id`, merchantID).Scan(&staleProductID); err != nil {
		t.Fatalf("seed stale product: %v", err)
	}

	// 6 commandes récentes (< 30 jours) avec 1 orderitem chacune sur le
	// produit "popular" -> doit ressortir en top catégorie ET top global.
	// Le produit "stale" n'a aucune commande récente -> is_popular doit être
	// remis à FALSE par le reset.
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		var orderID int64
		if err := rawDB.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand_status, price, tva, ht, created_by, creation_date)
			VALUES ($1, $2, 'PENDING_APPROVAL', 500, 0, 500, 'itest', $3)
			RETURNING order_id`, merchantID, 100+i, now.Add(-time.Duration(i)*time.Hour)).Scan(&orderID); err != nil {
			t.Fatalf("seed order %d: %v", i, err)
		}
		if _, err := rawDB.ExecContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
			VALUES ($1, $2, $3, 1, 500)`, orderID, popularProductID, merchantID); err != nil {
			t.Fatalf("seed orderitem %d: %v", i, err)
		}
	}

	tm := &TasksManager{DB: rawDB}
	if err := tm.updateMerchantPopularProducts(ctx, merchantID); err != nil {
		t.Fatalf("updateMerchantPopularProducts failed against postgres: %v", err)
	}

	var popularFlag, staleFlag sql.NullBool
	if err := rawDB.QueryRowContext(ctx, `SELECT is_popular FROM products WHERE product_id = $1 AND merchant_id = $2`, popularProductID, merchantID).Scan(&popularFlag); err != nil {
		t.Fatalf("read back popular product: %v", err)
	}
	if !popularFlag.Valid || !popularFlag.Bool {
		t.Fatalf("expected popular product is_popular=TRUE, got %+v", popularFlag)
	}
	if err := rawDB.QueryRowContext(ctx, `SELECT is_popular FROM products WHERE product_id = $1 AND merchant_id = $2`, staleProductID, merchantID).Scan(&staleFlag); err != nil {
		t.Fatalf("read back stale product: %v", err)
	}
	if !staleFlag.Valid || staleFlag.Bool {
		t.Fatalf("expected stale product is_popular reset to FALSE, got %+v", staleFlag)
	}
}

// --- RecomputeUpsellPatterns (par marchand) ------------------------------

func TestProcessUpsellPatternsForMerchant_Postgres(t *testing.T) {
	rawDB := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTaskMerchant(t, rawDB, ctx, 1)

	var productA, productB int64
	if err := rawDB.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'itest product A', 500, 'itest')
		RETURNING product_id`, merchantID).Scan(&productA); err != nil {
		t.Fatalf("seed product A: %v", err)
	}
	if err := rawDB.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'itest product B', 300, 'itest')
		RETURNING product_id`, merchantID).Scan(&productB); err != nil {
		t.Fatalf("seed product B: %v", err)
	}

	// 6 commandes CLOSED co-occurrant A+B (>= upsellMinCoOccur=5), dans la
	// fenêtre upsellPatternWindow=90 jours.
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		var orderID int64
		if err := rawDB.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand_status, state, price, tva, ht, created_by, creation_date)
			VALUES ($1, $2, 'ACCEPTED', 'CLOSED', 800, 0, 800, 'itest', $3)
			RETURNING order_id`, merchantID, 200+i, now.Add(-time.Duration(i)*time.Hour)).Scan(&orderID); err != nil {
			t.Fatalf("seed order %d: %v", i, err)
		}
		for _, pid := range []int64{productA, productB} {
			if _, err := rawDB.ExecContext(ctx, `
				INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
				VALUES ($1, $2, $3, 1, 500)`, orderID, pid, merchantID); err != nil {
				t.Fatalf("seed orderitem order=%d product=%d: %v", orderID, pid, err)
			}
		}
	}

	// AICache nil : Cache.Set/Get sont nil-safe (internal/ai/cache/redis.go),
	// donc processUpsellPatternsForMerchant reste testable sans Redis réel.
	tm := &TasksManager{DB: rawDB}
	pairs, err := tm.processUpsellPatternsForMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("processUpsellPatternsForMerchant failed against postgres: %v", err)
	}
	if pairs == 0 {
		t.Fatalf("expected at least one co-occurrence pattern (A<->B), got 0")
	}
}
