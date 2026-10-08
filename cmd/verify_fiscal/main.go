// Command verify_fiscal contrôle l'intégrité des données fiscales d'un ou de
// tous les établissements (conformité caisse, lot E, constat C4 ;
// docs/attestation-conformite-06-lot-E-brief.md) : chaînes (empreinte,
// signature, chaînage), numérotation des tickets, clôtures recalculées depuis
// les données, commandes recoupées avec leurs tickets, archives.
//
// Lecture seule (une transaction en lecture seule par établissement). Exige
// POSTGRES_URL et FISCAL_SIGNING_KEY (la clé de l'API, pour vérifier les
// signatures). Avec les variables R2_* du bucket privé, les fichiers
// d'archive sont aussi relus et contrôlés ; sans elles, seule leur ligne
// scellée l'est.
//
// Code de sortie : 0 sans erreur (avertissements possibles), 1 si une erreur
// est relevée, 2 si la vérification n'a pas pu se dérouler.
//
// Usage :
//
//	POSTGRES_URL=... FISCAL_SIGNING_KEY=... go run ./cmd/verify_fiscal --merchant=212
//	... go run ./cmd/verify_fiscal --merchant=212 --from=2026-09-01 --to=2026-09-30 --json=rapport.json
//	... go run ./cmd/verify_fiscal --all            # tous les établissements actifs, un résumé chacun
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"welloresto-api/internal/fiscalverify"
	"welloresto-api/internal/infrastructure/r2"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	merchant := flag.String("merchant", "", "établissement (merchant.id)")
	all := flag.Bool("all", false, "tous les établissements actifs (résumé par établissement)")
	fromRaw := flag.String("from", "", "premier jour (AAAA-MM-JJ, jour local) ; défaut : depuis le début")
	toRaw := flag.String("to", "", "dernier jour inclus (AAAA-MM-JJ) ; défaut : aujourd'hui")
	jsonPath := flag.String("json", "", "écrit aussi le rapport JSON dans ce fichier (un seul établissement)")
	skipFiles := flag.Bool("skip-archive-files", false, "ne relit pas les fichiers d'archive, même si R2 est configuré")
	flag.Parse()

	if (*merchant == "") == !*all {
		fail("indiquer --merchant=<id> ou --all")
	}
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		fail("POSTGRES_URL n'est pas défini")
	}
	if os.Getenv("FISCAL_SIGNING_KEY") == "" {
		fail("FISCAL_SIGNING_KEY n'est pas défini : les signatures ne peuvent pas être vérifiées")
	}
	os.Setenv("DB_DIALECT", "postgres")
	opts := fiscalverify.Options{}
	var err error
	if *fromRaw != "" {
		if opts.From, err = time.Parse("2006-01-02", *fromRaw); err != nil {
			fail("--from invalide : %v", err)
		}
	}
	if *toRaw != "" {
		if opts.To, err = time.Parse("2006-01-02", *toRaw); err != nil {
			fail("--to invalide : %v", err)
		}
	}
	if !*skipFiles {
		if store := privateStore(); store != nil {
			opts.Archives = store
		} else {
			log.Print("R2 privé non configuré : fichiers d'archive non relus (lignes scellées seulement)")
		}
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		fail("connexion : %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if !*all {
		opts.MerchantID = *merchant
		rep, err := fiscalverify.Run(ctx, db, opts)
		if err != nil {
			fail("%v", err)
		}
		rep.WriteText(os.Stdout)
		if *jsonPath != "" {
			b, _ := json.MarshalIndent(rep, "", "  ")
			if err := os.WriteFile(*jsonPath, b, 0o644); err != nil {
				fail("écriture %s : %v", *jsonPath, err)
			}
		}
		if !rep.OK() {
			os.Exit(1)
		}
		return
	}

	rows, err := db.QueryContext(ctx, `SELECT id::text, COALESCE(fullname, '') FROM merchant WHERE is_active ORDER BY id`)
	if err != nil {
		fail("établissements : %v", err)
	}
	type m struct{ id, name string }
	var merchants []m
	for rows.Next() {
		var x m
		if err := rows.Scan(&x.id, &x.name); err != nil {
			fail("établissements : %v", err)
		}
		merchants = append(merchants, x)
	}
	rows.Close()
	exit := 0
	for _, x := range merchants {
		opts.MerchantID = x.id
		rep, err := fiscalverify.Run(ctx, db, opts)
		if err != nil {
			fmt.Printf("%-6s %-30s ÉCHEC : %v\n", x.id, x.name, err)
			exit = 2
			continue
		}
		verdict := "CONFORME"
		if !rep.OK() {
			verdict = "NON CONFORME"
			if exit == 0 {
				exit = 1
			}
		}
		fmt.Printf("%-6s %-30s %-13s %5d erreur(s) %6d avertissement(s)  %s\n", x.id, x.name, verdict, rep.Errors, rep.Warnings, rep.Duration)
	}
	os.Exit(exit)
}

func privateStore() *r2.Client {
	cfg := r2.UploadConfig{
		AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		Endpoint:        os.Getenv("R2_ENDPOINT"),
		Bucket:          os.Getenv("R2_PRIVATE_BUCKET"),
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil
	}
	c, err := r2.NewClient(cfg)
	if err != nil {
		log.Printf("R2 privé : %v", err)
		return nil
	}
	return c
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
