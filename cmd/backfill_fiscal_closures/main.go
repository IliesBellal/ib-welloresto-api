// Command backfill_fiscal_closures rattrape les clôtures fiscales journalières
// (et leurs clôtures de mois et d'année) d'établissements qui n'en ont encore
// aucune, depuis une date donnée jusqu'au dernier jour échu — avant de laisser
// la main à la tâche horaire RunFiscalClosures (lot B conformité caisse,
// docs/attestation-conformite-02-lot-B-brief.md).
//
// La tâche horaire, elle, fait commencer un établissement sans clôture au
// dernier jour échu : les jours antérieurs ne seraient alors jamais clos
// (seulement comptés dans la valeur d'ouverture). Ce rattrapage doit donc
// tourner AVANT le premier passage de la tâche — juste après les migrations
// 170/171, avant (ou immédiatement après) le déploiement du code. Un
// établissement qui a déjà une clôture n'est jamais rattrapé en arrière : la
// chaîne ne peut que s'allonger ; il reprend simplement au lendemain de sa
// dernière clôture.
//
// Même calcul que la tâche (fiscal.CloseDueDays) : idempotent, une
// transaction par jour, sûr à relancer et en parallèle de la tâche.
//
// Autonome comme cmd/backfill_customer_stats : n'exige que POSTGRES_URL et
// FISCAL_SIGNING_KEY (la clé qui signe les clôtures — la même que l'API ;
// refusé sans elle, une clôture signée avec une clé vide serait invérifiable).
//
// Usage :
//
//	# Toujours d'abord — n'écrit rien, liste les jours qui seraient clos :
//	POSTGRES_URL="postgres://..." FISCAL_SIGNING_KEY=... go run ./cmd/backfill_fiscal_closures --from=2026-03-21
//
//	# Pour de bon, tous les établissements actifs :
//	POSTGRES_URL="postgres://..." FISCAL_SIGNING_KEY=... go run ./cmd/backfill_fiscal_closures --from=2026-03-21 --apply
//
//	# Un seul établissement :
//	... go run ./cmd/backfill_fiscal_closures --from=2026-03-21 --merchant=212 --apply
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"welloresto-api/internal/fiscal"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	fromRaw := flag.String("from", "", "premier jour à clôturer (AAAA-MM-JJ, jour local de l'établissement) — obligatoire")
	merchant := flag.String("merchant", "", "un seul établissement (merchant.id) ; défaut : tous les établissements actifs")
	apply := flag.Bool("apply", false, "écrire les clôtures (défaut : n'écrit rien, liste les jours)")
	flag.Parse()

	if *fromRaw == "" {
		log.Fatal("--from est obligatoire (AAAA-MM-JJ)")
	}
	from, err := time.Parse("2006-01-02", *fromRaw)
	if err != nil {
		log.Fatalf("--from invalide : %v", err)
	}
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		log.Fatal("POSTGRES_URL n'est pas défini")
	}
	if *apply && os.Getenv("FISCAL_SIGNING_KEY") == "" {
		log.Fatal("FISCAL_SIGNING_KEY n'est pas défini : les clôtures doivent être signées avec la clé de l'API")
	}
	os.Setenv("DB_DIALECT", "postgres")

	db, err := sql.Open("pgx", url)
	if err != nil {
		log.Fatalf("connexion : %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	query := `SELECT id::text, COALESCE(timezone, ''), creation_date FROM merchant WHERE is_active ORDER BY id`
	args := []any{}
	if *merchant != "" {
		query = `SELECT id::text, COALESCE(timezone, ''), creation_date FROM merchant WHERE id::text = $1`
		args = append(args, *merchant)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Fatalf("lecture des établissements : %v", err)
	}
	type merchantRef struct {
		id, timezone string
		created      time.Time
	}
	var merchants []merchantRef
	for rows.Next() {
		var m merchantRef
		if err := rows.Scan(&m.id, &m.timezone, &m.created); err != nil {
			log.Fatalf("lecture des établissements : %v", err)
		}
		merchants = append(merchants, m)
	}
	rows.Close()
	if len(merchants) == 0 {
		log.Fatal("aucun établissement à traiter")
	}

	now := time.Now()
	total, failures := 0, 0
	for _, m := range merchants {
		first, last, ok, err := fiscal.DueRange(ctx, db, m.id, m.timezone, &from, m.created, now)
		switch {
		case err != nil:
			failures++
			fmt.Printf("établissement %s : ERREUR %v\n", m.id, err)
			continue
		case !ok:
			fmt.Printf("établissement %s : rien à clôturer\n", m.id)
			continue
		}
		days := int(last.Sub(first).Hours()/24) + 1
		if !*apply {
			fmt.Printf("établissement %s : %d jour(s) à clôturer, du %s au %s\n", m.id, days, first.Format("2006-01-02"), last.Format("2006-01-02"))
			total += days
			continue
		}
		start := time.Now()
		n, err := fiscal.CloseDueDays(ctx, db, m.id, m.timezone, &from, m.created, now)
		total += n
		if err != nil {
			failures++
			fmt.Printf("établissement %s : %d jour(s) clos, puis ERREUR %v\n", m.id, n, err)
			continue
		}
		fmt.Printf("établissement %s : %d jour(s) clos (%s → %s) en %v\n", m.id, n, first.Format("2006-01-02"), last.Format("2006-01-02"), time.Since(start).Round(time.Millisecond))
	}

	mode := "simulation (rien écrit — relancer avec --apply)"
	if *apply {
		mode = "écrit"
	}
	fmt.Printf("\nTotal : %d jour(s), %d établissement(s) en erreur — %s\n", total, failures, mode)
	if failures > 0 {
		os.Exit(1)
	}
}
