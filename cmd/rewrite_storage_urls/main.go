// Command rewrite_storage_urls remplace les URLs publiques historiques du
// bucket R2 (https://pub-….r2.dev) par le domaine public courant
// (R2_PUBLIC_BASE_URL, ex. https://storage.welloresto.fr) dans toutes les
// colonnes texte / json de la base. Le chemin et le cache-buster (?v=…) sont
// conservés. Voir docs/images-redimensionnement.md.
//
// Les colonnes sont découvertes dans information_schema : aucune liste à tenir
// à jour. Les tables dont le nom correspond à --exclude (chaînes fiscales,
// audits, journaux, archives) sont listées mais jamais modifiées : leur contenu
// est scellé ou historique.
//
// Sans --apply, la commande ne fait que lister les colonnes concernées.
// Avec --apply, tout est réécrit dans UNE transaction (tout ou rien).
//
// Usage :
//
//	POSTGRES_URL=... R2_PUBLIC_BASE_URL=https://storage.welloresto.fr \
//	  go run ./cmd/rewrite_storage_urls [--apply]
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const defaultFromRegex = `https://pub-[A-Za-z0-9]+\.r2\.dev`

type column struct {
	table, name, dataType string
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func (c column) ident() string { return quoteIdent(c.table) + "." + quoteIdent(c.name) }

// cast renvoie le suffixe de cast pour réécrire la valeur dans son type d'origine.
func (c column) cast() string {
	switch c.dataType {
	case "jsonb":
		return "::jsonb"
	case "json":
		return "::json"
	}
	return ""
}

func main() {
	apply := flag.Bool("apply", false, "réécrire (défaut : liste seulement)")
	fromRegex := flag.String("from", defaultFromRegex, "motif (regex Postgres) des anciennes URLs")
	to := flag.String("to", os.Getenv("R2_PUBLIC_BASE_URL"), "nouveau domaine public (défaut : R2_PUBLIC_BASE_URL)")
	exclude := flag.String("exclude", `fiscal|chain|audit|journal|archive|_log`, "tables ignorées (regex sur le nom)")
	flag.Parse()

	postgresURL := os.Getenv("POSTGRES_URL")
	if postgresURL == "" {
		log.Fatal("POSTGRES_URL doit être défini")
	}
	target := strings.TrimRight(*to, "/")
	if !strings.HasPrefix(target, "https://") {
		log.Fatal("--to / R2_PUBLIC_BASE_URL doit commencer par https://")
	}
	excludeRe, err := regexp.Compile(*exclude)
	if err != nil {
		log.Fatalf("--exclude invalide : %v", err)
	}

	db, err := sql.Open("pgx", postgresURL)
	if err != nil {
		log.Fatalf("connexion à la base : %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	cols, err := listColumns(ctx, db)
	if err != nil {
		log.Fatalf("découverte des colonnes : %v", err)
	}

	var todo []column
	total := 0
	for _, c := range cols {
		var n int
		q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s::text ~ $1`, quoteIdent(c.table), quoteIdent(c.name))
		if err := db.QueryRowContext(ctx, q, *fromRegex).Scan(&n); err != nil {
			log.Fatalf("comptage %s.%s : %v", c.table, c.name, err)
		}
		if n == 0 {
			continue
		}
		if excludeRe.MatchString(c.table) {
			fmt.Printf("IGNORÉ   %-45s %6d ligne(s) (table exclue)\n", c.table+"."+c.name, n)
			continue
		}
		fmt.Printf("À CHANGER %-44s %6d ligne(s) [%s]\n", c.table+"."+c.name, n, c.dataType)
		todo = append(todo, c)
		total += n
	}
	fmt.Printf("\n%d colonne(s), %d valeur(s) à réécrire vers %s\n", len(todo), total, target)

	if !*apply {
		fmt.Println("Simulation : rien n'a été modifié. Relancer avec --apply.")
		return
	}
	if len(todo) == 0 {
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("transaction : %v", err)
	}
	defer tx.Rollback()
	for _, c := range todo {
		q := fmt.Sprintf(`UPDATE %s SET %s = regexp_replace(%s::text, $1, $2, 'g')%s WHERE %s::text ~ $1`,
			quoteIdent(c.table), quoteIdent(c.name), quoteIdent(c.name), c.cast(), quoteIdent(c.name))
		res, err := tx.ExecContext(ctx, q, *fromRegex, target)
		if err != nil {
			log.Fatalf("réécriture %s.%s (rien n'est enregistré) : %v", c.table, c.name, err)
		}
		n, _ := res.RowsAffected()
		fmt.Printf("réécrit  %-45s %6d ligne(s)\n", c.table+"."+c.name, n)
	}
	if err := tx.Commit(); err != nil {
		log.Fatalf("commit : %v", err)
	}
	fmt.Println("Terminé.")
}

func listColumns(ctx context.Context, db *sql.DB) ([]column, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema()
		  AND t.table_type = 'BASE TABLE'
		  AND c.data_type IN ('character varying', 'character', 'text', 'json', 'jsonb')
		ORDER BY c.table_name, c.ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.table, &c.name, &c.dataType); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
