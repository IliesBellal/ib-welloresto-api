// Command renumber_users renumérote les utilisateurs "user-<uuid>" en ids
// numériques (docs/ids-entiers-users-discounts-chantier.md, étape 3).
//
// Sans --apply : liste les utilisateurs concernés et, table par table, le
// nombre de lignes qui seraient réécrites ou conservées (lignes fiscales
// signées). Rien n'est écrit. À lancer en production avant le créneau pour
// savoir exactement ce qui va bouger.
//
// Avec --apply : une seule transaction (userids.Apply), puis vidage du cache
// Redis des sessions (models.UserCachePrefix), qui porte encore l'ancien
// UserID pendant 60 minutes. Sans REDIS_URL, la commande s'arrête AVANT
// d'écrire quoi que ce soit : une session en cache écrirait l'ancien id dans
// des paiements, qui seraient ensuite signés avec.
//
// Avec --audit : cherche les anciens identifiants dans TOUTES les colonnes texte de la
// base qui ne figurent pas dans l'inventaire (userids.Columns). À lancer en
// production avant --apply, et après : l'inventaire vient d'un environnement de
// test, une colonne propre à la production lui échapperait. Lecture seule mais
// longue (elle parcourt chaque colonne texte) : hors des heures de service.
//
// Prérequis : migrations 186 (séquence) et 187 (user_id_remap) appliquées, et
// le code qui lit user_id_remap déployé. Rejouable.
//
// Usage :
//
//	POSTGRES_URL=... go run ./cmd/renumber_users
//	POSTGRES_URL=... go run ./cmd/renumber_users --audit
//	POSTGRES_URL=... REDIS_URL=... go run ./cmd/renumber_users --apply
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/models"
	"welloresto-api/internal/userids"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	apply := flag.Bool("apply", false, "renuméroter (défaut : aperçu seulement)")
	audit := flag.Bool("audit", false, "chercher les anciens ids dans toutes les colonnes texte hors inventaire (lecture seule, long)")
	flag.Parse()

	postgresURL := os.Getenv("POSTGRES_URL")
	if postgresURL == "" {
		log.Fatal("POSTGRES_URL doit être défini")
	}
	// dbx.Rebind lit le dialecte actif (DB_DIALECT) : les requêtes de userids
	// sont écrites avec des "?" comme le reste des repositories.
	if err := os.Setenv(dbx.EnvDialect, string(dbx.Postgres)); err != nil {
		log.Fatalf("DB_DIALECT : %v", err)
	}

	db, err := sql.Open("pgx", postgresURL)
	if err != nil {
		log.Fatalf("connexion à la base : %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if *audit {
		left, err := userids.Audit(ctx, db)
		if err != nil {
			log.Fatalf("audit : %v", err)
		}
		if len(left) == 0 {
			fmt.Println("Aucune colonne texte hors inventaire ne contient d'ancien identifiant.")
			return
		}
		fmt.Println("Colonnes HORS inventaire qui contiennent encore un ancien identifiant (à ajouter à userids.Columns) :")
		for _, l := range left {
			fmt.Printf("  %-40s %-28s %d ligne(s)\n", l.Table, l.Column, l.Rows)
		}
		os.Exit(1)
	}

	if !*apply {
		rep, err := userids.Plan(ctx, db)
		if err != nil {
			log.Fatalf("aperçu : %v", err)
		}
		printReport(rep, false)
		fmt.Println("Aucune écriture. Relancer avec --apply (et REDIS_URL) pour renuméroter.")
		return
	}

	cache, err := redis.New()
	if err != nil {
		log.Fatalf("Redis indisponible, rien n'a été écrit : %v", err)
	}

	rep, err := userids.Apply(ctx, db)
	if err != nil {
		log.Fatalf("renumérotation annulée (rollback) : %v", err)
	}
	printReport(rep, true)

	n, err := cache.ScanDeleteByPattern(ctx, models.UserCachePrefix+"*")
	if err != nil {
		log.Fatalf("renumérotation FAITE, mais vidage du cache des sessions incomplet (%d clés supprimées) : %v — relancer la commande, elle est rejouable", n, err)
	}
	fmt.Printf("Cache des sessions vidé : %d clé(s) supprimée(s).\n", n)
}

func printReport(rep userids.Report, applied bool) {
	legacy := make([]string, 0, len(rep.Users))
	for id := range rep.Users {
		legacy = append(legacy, id)
	}
	sort.Strings(legacy)

	fmt.Printf("Utilisateurs à renuméroter : %d\n", len(legacy))
	for _, id := range legacy {
		next := rep.Users[id]
		switch {
		case next != "":
			fmt.Printf("  %s -> %s\n", id, next)
		case applied:
			fmt.Printf("  %s -> ?\n", id)
		default:
			fmt.Printf("  %s (nouvel id tiré à l'application)\n", id)
		}
	}

	verb := "à réécrire"
	if applied {
		verb = "réécrites"
	}
	fmt.Printf("\n%-48s %12s %16s\n", "colonne", verb, "signées gardées")
	var total, kept int64
	for _, c := range rep.Columns {
		if c.Rewrite == 0 && c.KeptSealed == 0 {
			continue
		}
		fmt.Printf("%-48s %12d %16d\n", c.Column, c.Rewrite, c.KeptSealed)
		total += c.Rewrite
		kept += c.KeptSealed
	}
	fmt.Printf("%-48s %12d %16d\n", "TOTAL", total, kept)
	for _, c := range rep.Missing {
		fmt.Printf("colonne absente de cette base, ignorée : %s\n", c)
	}
	fmt.Println()
}
