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

// seedUpsellBaskets crée une commande CLOSED récente par panier, avec une
// ligne par produit du panier.
func seedUpsellBaskets(t *testing.T, db *sql.DB, ctx context.Context, merchantID string, firstOrderNum int, baskets [][]int64) {
	t.Helper()
	now := time.Now().UTC()
	for i, basket := range baskets {
		var orderID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand_status, state, price, tva, ht, created_by, creation_date)
			VALUES ($1, $2, 'ACCEPTED', 'CLOSED', 800, 0, 800, 'itest', $3)
			RETURNING order_id`, merchantID, firstOrderNum+i, now.Add(-time.Duration(i)*time.Hour)).Scan(&orderID); err != nil {
			t.Fatalf("seed order %d: %v", i, err)
		}
		for _, pid := range basket {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
				VALUES ($1, $2, $3, 1, 300)`, orderID, pid, merchantID); err != nil {
				t.Fatalf("seed orderitem order=%d product=%d: %v", orderID, pid, err)
			}
		}
	}
}

// repeatBasket renvoie n fois le même panier.
func repeatBasket(n int, basket ...int64) [][]int64 {
	baskets := make([][]int64, n)
	for i := range baskets {
		baskets[i] = basket
	}
	return baskets
}

// 8 commandes A+B (upsellMinCoOccur = 8) et 4 commandes C seul, sur 12 :
// lift A-B = 8×12 / (8×8) = 1,5 (≥ upsellMinLift = 1,2), P(B | A) lissée ≈ 0,81.
// Les deux sens passent. Sans les commandes C, le lift vaudrait 1,0 : A et B
// seraient simplement dans toutes les commandes, sans lien spécifique.
func TestProcessUpsellPatternsForMerchant_Postgres(t *testing.T) {
	rawDB := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTaskMerchant(t, rawDB, ctx, 1)

	seedProduct := func(name string) int64 {
		var id int64
		if err := rawDB.QueryRowContext(ctx, `
			INSERT INTO products (merchant_id, name, price, category)
			VALUES ($1, $2, 300, 'itest')
			RETURNING product_id`, merchantID, name).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return id
	}
	productA := seedProduct("itest product A")
	productB := seedProduct("itest product B")
	productC := seedProduct("itest product C")

	baskets := append(repeatBasket(8, productA, productB), repeatBasket(4, productC)...)
	seedUpsellBaskets(t, rawDB, ctx, merchantID, 200, baskets)

	// AICache nil : Cache.Set/Get sont nil-safe (internal/ai/cache/redis.go),
	// donc processUpsellPatternsForMerchant reste testable sans Redis réel.
	tm := &TasksManager{DB: rawDB}
	pairs, err := tm.processUpsellPatternsForMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("processUpsellPatternsForMerchant failed against postgres: %v", err)
	}
	if pairs != 2 {
		t.Fatalf("expected 2 directed patterns (A<->B), got %d", pairs)
	}
}

// Les variantes (by_product_of) sont comptées sous leur produit groupe
// (docs/UPSELL_COMPLETION.md, D1). A est commandé 4 fois avec la variante V1
// et 4 fois avec la variante V2 du groupe G : aucune paire A+Vn n'atteint
// upsellMinCoOccur = 8, mais la paire A+G (8 commandes) l'atteint une fois les
// variantes rattachées. Sans ce rattachement, aucun pattern n'est produit.
// 4 commandes C seul portent le lift à 1,5.
func TestProcessUpsellPatternsForMerchant_VariantsRolledUp_Postgres(t *testing.T) {
	rawDB := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTaskMerchant(t, rawDB, ctx, 1)

	seedProduct := func(name string, groupID any) int64 {
		var id int64
		if err := rawDB.QueryRowContext(ctx, `
			INSERT INTO products (merchant_id, name, price, category, by_product_of)
			VALUES ($1, $2, 300, 'itest', $3)
			RETURNING product_id`, merchantID, name, groupID).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return id
	}
	productA := seedProduct("itest product A", nil)
	productC := seedProduct("itest product C", nil)
	group := seedProduct("itest group G", nil)
	variant1 := seedProduct("itest variant G1", group)
	variant2 := seedProduct("itest variant G2", group)

	baskets := append(repeatBasket(4, productA, variant1), repeatBasket(4, productA, variant2)...)
	baskets = append(baskets, repeatBasket(4, productC)...)
	seedUpsellBaskets(t, rawDB, ctx, merchantID, 300, baskets)

	tm := &TasksManager{DB: rawDB}
	pairs, err := tm.processUpsellPatternsForMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("processUpsellPatternsForMerchant failed against postgres: %v", err)
	}
	// A→G et G→A.
	if pairs != 2 {
		t.Fatalf("expected 2 directed patterns (A<->G) once variants are rolled up, got %d", pairs)
	}
}

// Liste « petits prix » (docs/UPSELL_COMPLETION.md, D10 et D13) : jamais de
// produit groupe, mais ses variantes, chacune avec son prix et ses ventes ;
// produits à 0 € et indisponibles écartés ; seuil = médiane / 3.
func TestComputeUpsellLowPriceList_Postgres(t *testing.T) {
	rawDB := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedTaskMerchant(t, rawDB, ctx, 1)

	seed := func(name string, price int, isGroup bool, groupID any, available bool) int64 {
		var id int64
		if err := rawDB.QueryRowContext(ctx, `
			INSERT INTO products (merchant_id, name, price, category, is_product_group, by_product_of, available, enabled, status)
			VALUES ($1, $2, $3, 'itest', $4, $5, $6, TRUE, 'available')
			RETURNING product_id`, merchantID, name, price, isGroup, groupID, available).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return id
	}
	coca := seed("itest Coca", 0, true, nil, true)
	coca33 := seed("itest Coca 33cl", 190, false, coca, true)
	coca125 := seed("itest Coca 1.25L", 400, false, coca, true)
	eau := seed("itest Eau", 140, false, nil, true)
	sauce := seed("itest Sauce", 0, false, nil, true)
	retired := seed("itest Tiramisu retiré", 300, false, nil, false)
	var mains []int64
	for i := 0; i < 5; i++ {
		mains = append(mains, seed("itest Pizza "+strconv.Itoa(i), 990, false, nil, true))
	}

	// Ventes : Eau 3, Coca 33cl 2, Coca 1.25L 1 (au-dessus du seuil), sauce et
	// tiramisu retiré 1 chacun, une pizza 1.
	baskets := [][]int64{
		{eau, mains[0]}, {eau, coca33}, {eau, sauce}, {coca33}, {coca125}, {retired},
	}
	seedUpsellBaskets(t, rawDB, ctx, merchantID, 500, baskets)

	tm := &TasksManager{DB: rawDB}
	entries, median, err := tm.computeUpsellLowPriceList(ctx, merchantID)
	if err != nil {
		t.Fatalf("computeUpsellLowPriceList failed against postgres: %v", err)
	}
	// Prix > 0 des produits proposables (le groupe n'en fait pas partie) :
	// 140, 190, 400, 990 ×5 → médiane 990, seuil 330.
	if median != 990 {
		t.Fatalf("median = %v, want 990", median)
	}
	want := []string{strconv.FormatInt(eau, 10), strconv.FormatInt(coca33, 10)}
	if len(entries) != 2 || entries[0].ProductID != want[0] || entries[1].ProductID != want[1] ||
		entries[0].Orders != 3 || entries[1].Orders != 2 || entries[1].Price != 190 {
		t.Fatalf("entries = %+v, want [Eau (3 ventes), Coca 33cl (2 ventes, 190)] et jamais le groupe %d", entries, coca)
	}
}

// TestCleanupExpiredPasswordResets_Postgres exercises the exported cron entry
// point directly — unlike the other tasks in this file.
//
// It is safe here precisely because password_resets is a table introduced by
// migration 078: it holds no real data, only what tests put in it. The task
// also deletes strictly on age, so seeded recent rows are provably untouched.
//
// See docs/PASSWORD_RESET.md.
func TestCleanupExpiredPasswordResets_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const userID = "itest-purge-user-1"

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID)
	})
	if _, err := db.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}

	now := time.Now().UTC()
	seed := func(id string, createdAt time.Time) {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO password_resets (id, user_id, token_hash, expires_at, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			id, userID, id+"-hash", createdAt.Add(30*time.Minute), createdAt); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// Straddle the 7-day retention boundary on both sides.
	seed("itest-purge-old-8d", now.AddDate(0, 0, -8))
	seed("itest-purge-old-30d", now.AddDate(0, 0, -30))
	seed("itest-purge-edge-6d", now.AddDate(0, 0, -6))
	seed("itest-purge-fresh", now.Add(-time.Minute))

	tm := &TasksManager{DB: db}
	tm.CleanupExpiredPasswordResets()

	rows, err := db.QueryContext(ctx,
		`SELECT id FROM password_resets WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer rows.Close()

	survivors := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		survivors[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if len(survivors) != 2 {
		t.Fatalf("got %d surviving rows %v, want 2 (the ones under 7 days old)", len(survivors), survivors)
	}
	if !survivors["itest-purge-edge-6d"] {
		t.Error("a 6-day-old row was purged — the retention window is too aggressive")
	}
	if !survivors["itest-purge-fresh"] {
		t.Error("a row created a minute ago was purged")
	}
	if survivors["itest-purge-old-8d"] || survivors["itest-purge-old-30d"] {
		t.Error("rows older than the retention window were not purged")
	}
}
