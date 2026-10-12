// Command backfill_kiosk_order_registers rattache les commandes de borne au
// registre de caisse qui a encaissé leurs paiements.
//
// Une commande de borne est créée avec orders.cash_register_id = 'KIOSK' et n'en
// change jamais, alors que son paiement est rattaché à un registre à la clôture
// de celui-ci (étape 3bis de CloseCashRegister, ou paiement du caissier après un
// « payer en caisse »). Le ticket Z et le PDF du registre lisent désormais ce
// lien côté paiements (cash_registers.RegisterOrdersSQL) ; cet outil écrit le
// même rattachement dans orders pour que les autres lecteurs de
// orders.cash_register_id (liste des commandes, archives fiscales à venir)
// s'accordent avec lui. Voir docs/ECART_REGISTRES_BORNE.md.
//
// Règle, identique à celle de la lecture : le registre est le plus petit
// identifiant numérique parmi les paiements actifs de la commande. Une commande
// dont les paiements actifs sont sur plusieurs registres est rattachée au plus
// petit et signalée. Les commandes sans paiement rattaché (borne en attente, paiement
// annulé, registre pas encore fermé) sont laissées à 'KIOSK'.
//
// Ce qui n'est PAS touché : orders.last_update (les tablettes ne doivent pas
// resynchroniser des milliers de commandes), le scellement fiscal
// (orders.cash_register_id n'entre dans aucune empreinte — fiscal.OrderClosurePayload),
// les archives fiscales déjà émises (ZIP immuables : leur colonne « registre »
// garde 'KIOSK').
//
// Idempotent : le UPDATE ne cible que cash_register_id = 'KIOSK'; une seconde
// exécution rapporte 0 commande. Par lots, une petite transaction par lot et par
// registre, reprenable avec --start-after.
//
// Usage :
//
//	# Toujours d'abord : ne modifie rien, rapporte ce qui serait rattaché.
//	POSTGRES_URL="postgres://..." go run ./cmd/backfill_kiosk_order_registers
//
//	# Une seule enseigne :
//	POSTGRES_URL="postgres://..." go run ./cmd/backfill_kiosk_order_registers --merchant=212
//
//	# Appliquer, en gardant la liste des commandes modifiées (pour un retour arrière) :
//	POSTGRES_URL="postgres://..." go run ./cmd/backfill_kiosk_order_registers --apply --out=kiosk_orders.csv
//
//	# Reprendre après la commande 48210 :
//	POSTGRES_URL="postgres://..." go run ./cmd/backfill_kiosk_order_registers --apply --start-after=48210
//
// Retour arrière : UPDATE orders SET cash_register_id = 'KIOSK' WHERE order_id IN (<colonne
// order_id du CSV>). La lecture retrouve alors le même résultat par les paiements.
//
// Outil volontairement autonome (comme cmd/backfill_customer_stats) : pas de
// internal/config.Load(), il n'a besoin que de POSTGRES_URL.
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	kioskSentinel = "KIOSK"
	defaultBatch  = 500
)

type candidate struct {
	orderID    int64
	merchantID string
	price      int64
	register   int64
	registers  int64 // nombre de registres distincts parmi les paiements actifs
}

type registerTotals struct {
	orders int64
	cents  int64
}

func main() {
	apply := flag.Bool("apply", false, "écrit réellement les rattachements (défaut : simulation, rien n'est écrit)")
	merchant := flag.String("merchant", "", "limite à cet établissement (merchant_id)")
	startAfter := flag.Int64("start-after", 0, "reprend après cet order_id (exclu)")
	batch := flag.Int("batch", defaultBatch, "commandes lues par lot")
	out := flag.String("out", "", "écrit en CSV les commandes (order_id, merchant_id, ancien, nouveau registre)")
	flag.Parse()

	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		log.Fatal("POSTGRES_URL n'est pas défini — pointer vers staging ou la production puis relancer")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping: %v", err)
	}

	var dbName string
	_ = db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbName)
	mode := "SIMULATION (rien n'est écrit)"
	if *apply {
		mode = "APPLICATION (écriture)"
	}
	fmt.Printf("backfill_kiosk_order_registers — base %q — %s\n", dbName, mode)
	if *merchant != "" {
		fmt.Printf("établissement : %s\n", *merchant)
	}

	var csvw *csv.Writer
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatalf("création de %s: %v", *out, err)
		}
		defer f.Close()
		csvw = csv.NewWriter(f)
		defer csvw.Flush()
		_ = csvw.Write([]string{"order_id", "merchant_id", "ancien_registre", "nouveau_registre"})
	}

	started := time.Now()
	lastID := *startAfter
	var scanned, attachable, multiRegister, updated int64
	perRegister := map[int64]*registerTotals{}
	var multiIDs []int64

	for {
		rows, err := fetchBatch(ctx, db, lastID, *merchant, *batch)
		if err != nil {
			log.Fatalf("lecture après order_id=%d: %v", lastID, err)
		}
		if len(rows) == 0 {
			break
		}

		byRegister := map[int64][]int64{}
		for _, c := range rows {
			scanned++
			lastID = c.orderID
			if c.register == 0 {
				continue // pas de paiement rattaché : reste à 'KIOSK'
			}
			attachable++
			if c.registers > 1 {
				multiRegister++
				multiIDs = append(multiIDs, c.orderID)
			}
			t := perRegister[c.register]
			if t == nil {
				t = &registerTotals{}
				perRegister[c.register] = t
			}
			t.orders++
			t.cents += c.price
			byRegister[c.register] = append(byRegister[c.register], c.orderID)
			if csvw != nil {
				_ = csvw.Write([]string{strconv.FormatInt(c.orderID, 10), c.merchantID, kioskSentinel, strconv.FormatInt(c.register, 10)})
			}
		}

		if *apply {
			for register, ids := range byRegister {
				n, err := applyBatch(ctx, db, register, ids)
				if err != nil {
					log.Fatalf("application registre=%d (dernière commande lue %d): %v", register, lastID, err)
				}
				updated += n
			}
		}
		fmt.Printf("... %d commande(s) lues, dernière order_id=%d (%d rattachables)\n", scanned, lastID, attachable)
	}

	fmt.Println("---")
	fmt.Printf("commandes borne lues=%d rattachables=%d dont multi-registres=%d sans paiement rattaché=%d\n",
		scanned, attachable, multiRegister, scanned-attachable)
	ids := make([]int64, 0, len(perRegister))
	for id := range perRegister {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	fmt.Printf("%-12s %10s %14s\n", "registre", "commandes", "TTC (euros)")
	for _, id := range ids {
		t := perRegister[id]
		fmt.Printf("%-12d %10d %14.2f\n", id, t.orders, float64(t.cents)/100)
	}
	if len(multiIDs) > 0 {
		shown := multiIDs
		if len(shown) > 20 {
			shown = shown[:20]
		}
		fmt.Printf("commandes à paiements sur plusieurs registres (rattachées au plus petit), à vérifier : %v\n", shown)
	}
	fmt.Printf("durée=%s\n", time.Since(started).Round(time.Second))
	if *apply {
		fmt.Printf("Appliqué : %d commande(s) modifiée(s). Une seconde exécution doit en rapporter 0.\n", updated)
	} else {
		fmt.Println("SIMULATION — rien n'a été écrit. Relancer avec --apply pour rattacher ces commandes.")
	}
}

// fetchBatch lit un lot de commandes de borne (order_id croissant) avec leur
// registre cible. Lecture seule. Le registre cible est le plus petit
// identifiant numérique des paiements actifs, à condition que ce registre
// existe ; 0 = rien à rattacher.
func fetchBatch(ctx context.Context, db *sql.DB, after int64, merchant string, limit int) ([]candidate, error) {
	rows, err := db.QueryContext(ctx, `
		WITH page AS (
			SELECT order_id, merchant_id, COALESCE(price, 0) AS price
			FROM orders
			WHERE cash_register_id = $1
			  AND order_id > $2
			  AND ($3 = '' OR merchant_id = $3)
			ORDER BY order_id
			LIMIT $4
		),
		pay AS (
			SELECT p.order_id,
			       MIN(CASE WHEN p.cash_register_id ~ '^[0-9]+$' THEN p.cash_register_id::bigint END) AS register,
			       COUNT(DISTINCT CASE WHEN p.cash_register_id ~ '^[0-9]+$' THEN p.cash_register_id::bigint END) AS registers
			FROM payments p
			WHERE p.order_id IN (SELECT order_id FROM page) AND p.enabled IS TRUE
			GROUP BY p.order_id
		)
		SELECT pg.order_id, pg.merchant_id, pg.price,
		       COALESCE((SELECT cr.cash_register_id FROM cash_registers cr WHERE cr.cash_register_id = pay.register), 0),
		       COALESCE(pay.registers, 0)
		FROM page pg
		LEFT JOIN pay ON pay.order_id = pg.order_id
		ORDER BY pg.order_id`, kioskSentinel, after, merchant, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.orderID, &c.merchantID, &c.price, &c.register, &c.registers); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// applyBatch rattache les commandes au registre, dans une transaction. La
// condition cash_register_id = 'KIOSK' rend l'écriture idempotente et empêche
// d'écraser une commande rattachée entre-temps.
func applyBatch(ctx context.Context, db *sql.DB, register int64, orderIDs []int64) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE orders SET cash_register_id = $1
		WHERE order_id = ANY($2) AND cash_register_id = $3`,
		strconv.FormatInt(register, 10), orderIDs, kioskSentinel)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}
