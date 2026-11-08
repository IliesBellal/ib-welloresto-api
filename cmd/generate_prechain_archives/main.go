// Command generate_prechain_archives produit, pour chaque établissement dont la
// chaîne de clôtures a commencé en cours de mois, l'archive fiscale des jours
// qui précèdent sa première clôture journalière (par exemple du 1er au 8
// octobre quand la mise en service date du 9).
//
// Elle n'écrit aucune clôture et ne touche à aucune empreinte existante : elle
// ajoute seulement une archive à la demande (ZIP de CSV dans le bucket R2
// privé, ligne chaînée dans fiscal_archives), marquée « antérieure à la
// première clôture » dans son manifeste. Voir docs/archives-premier-mois.md.
//
// Sûre à relancer : un établissement dont l'archive de cette période existe
// déjà est ignoré.
//
// Usage :
//
//	# Toujours d'abord — n'écrit rien, liste les périodes :
//	POSTGRES_URL="postgres://..." go run ./cmd/generate_prechain_archives --from=2026-10-01
//
//	# Pour de bon (clé de signature et bucket R2 privé de l'API) :
//	POSTGRES_URL=... FISCAL_SIGNING_KEY=... R2_ACCESS_KEY_ID=... R2_SECRET_ACCESS_KEY=... \
//	  R2_ENDPOINT=... R2_PRIVATE_BUCKET=... go run ./cmd/generate_prechain_archives --from=2026-10-01 --apply
//
//	# Un seul établissement :
//	... go run ./cmd/generate_prechain_archives --from=2026-10-01 --merchant=236 --apply
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscalarchive"
	"welloresto-api/internal/infrastructure/r2"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	fromRaw := flag.String("from", "", "premier jour à archiver (AAAA-MM-JJ, jour local) — obligatoire")
	merchant := flag.String("merchant", "", "un seul établissement (merchant.id) ; défaut : tous les établissements actifs")
	apply := flag.Bool("apply", false, "générer les archives (défaut : liste les périodes)")
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
		log.Fatal("FISCAL_SIGNING_KEY n'est pas défini : les archives doivent être signées avec la clé de l'API")
	}
	os.Setenv("DB_DIALECT", "postgres")

	db, err := sql.Open("pgx", url)
	if err != nil {
		log.Fatalf("connexion : %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	var store fiscalarchive.Store
	if *apply {
		client, err := r2.NewClient(r2.UploadConfig{
			AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
			Endpoint:        os.Getenv("R2_ENDPOINT"),
			Bucket:          os.Getenv("R2_PRIVATE_BUCKET"),
			PublicBaseURL:   os.Getenv("R2_PUBLIC_BASE_URL"),
		})
		if err != nil {
			log.Fatalf("bucket R2 privé indisponible : %v", err)
		}
		store = client
	}

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

	generated, failures := 0, 0
	for _, m := range merchants {
		created := m.created
		if loc, err := time.LoadLocation(m.timezone); err == nil {
			created = created.In(loc)
		}
		start, end, done, ok, err := fiscalarchive.PreChainPeriod(ctx, dbx.GetDB(ctx, db), m.id, from, created)
		switch {
		case err != nil:
			failures++
			fmt.Printf("établissement %s : ERREUR %v\n", m.id, err)
			continue
		case !ok:
			fmt.Printf("établissement %s : rien à archiver\n", m.id)
			continue
		case done:
			fmt.Printf("établissement %s : archive du %s au %s déjà générée\n", m.id, start.Format("2006-01-02"), end.Format("2006-01-02"))
			continue
		case !*apply:
			fmt.Printf("établissement %s : archive à générer, du %s au %s\n", m.id, start.Format("2006-01-02"), end.Format("2006-01-02"))
			generated++
			continue
		}
		a, err := fiscalarchive.Generate(ctx, db, store, fiscalarchive.Request{
			MerchantID: m.id, Kind: fiscalarchive.KindPeriod, Start: start, End: end, GeneratedBy: fiscalarchive.GeneratedBySystem,
		})
		if err != nil {
			failures++
			fmt.Printf("établissement %s : ERREUR %v\n", m.id, err)
			continue
		}
		generated++
		anomalies := 0
		if a.Check != nil {
			anomalies = a.Check.Anomalies
		}
		fmt.Printf("établissement %s : archive %d (%s), %d octets, %d anomalie(s) au contrôle croisé\n", m.id, a.ID, a.Filename, a.SizeBytes, anomalies)
	}

	mode := "simulation (rien écrit — relancer avec --apply)"
	if *apply {
		mode = "écrit"
	}
	fmt.Printf("\nTotal : %d archive(s), %d établissement(s) en erreur — %s\n", generated, failures, mode)
	if failures > 0 {
		os.Exit(1)
	}
}
