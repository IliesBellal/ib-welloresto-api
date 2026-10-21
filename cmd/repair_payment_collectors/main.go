// Command repair_payment_collectors rétablit les paiements signés dont
// payments.user_id a été réécrit à la livraison d'un arrêt (conformité caisse ;
// docs/ids-entiers-users-discounts-chantier.md, §9).
//
// Avant la migration 188, la livraison réattribuait au livreur tous les
// paiements de la commande en réécrivant user_id, qui fait partie de la charge
// utile signée : leur empreinte ne se recalculait plus et cmd/verify_fiscal
// les signalait. Cette commande retrouve l'ancien user_id (le seul qui redonne
// l'empreinte stockée), le remet, et inscrit le livreur dans collected_by pour
// les paiements encaissés en main propre.
//
// Sans --apply : liste ce qui serait réparé, sans rien écrire. Avec --apply :
// une seule transaction ; un paiement dont l'ancien id n'est pas retrouvé n'est
// pas modifié (il reste signalé par verify_fiscal). Rejouable.
//
// Prérequis : migration 188. Avant ou après cmd/renumber_users, sans importance :
// les lignes signées gardent leur ancien id dans les deux cas. Seule l'empreinte
// (SHA-256) est recalculée, pas la signature : la clé FISCAL_SIGNING_KEY n'est
// pas nécessaire.
//
// Usage :
//
//	POSTGRES_URL=... go run ./cmd/repair_payment_collectors
//	POSTGRES_URL=... go run ./cmd/repair_payment_collectors --apply
//
// Contrôler ensuite avec : FISCAL_SIGNING_KEY=... go run ./cmd/verify_fiscal --all
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	apply := flag.Bool("apply", false, "réparer (défaut : aperçu seulement)")
	flag.Parse()

	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		log.Fatal("POSTGRES_URL doit être défini")
	}
	if err := os.Setenv(dbx.EnvDialect, string(dbx.Postgres)); err != nil {
		log.Fatalf("DB_DIALECT : %v", err)
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		log.Fatalf("connexion à la base : %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	repairs, err := fiscal.FindReassignedPayments(ctx, db)
	if err != nil {
		log.Fatalf("recherche : %v", err)
	}

	resolved := 0
	fmt.Printf("%-10s %-10s %-10s %-16s %-16s %-16s %s\n", "paiement", "établiss.", "commande", "mop", "user_id actuel", "user_id d'origine", "collected_by")
	for _, r := range repairs {
		original := r.OriginalUserID
		if original == "" {
			original = "(introuvable)"
		} else {
			resolved++
		}
		fmt.Printf("%-10s %-10s %-10s %-16s %-16s %-16s %s\n", r.PaymentID, r.MerchantID, r.OrderID, r.MOP, r.CurrentUserID, original, r.CollectedBy)
	}
	fmt.Printf("\n%d paiement(s) à réparer, dont %d dont l'ancien user_id est retrouvé, %d introuvable(s).\n", len(repairs), resolved, len(repairs)-resolved)

	if !*apply {
		fmt.Println("Aucune écriture. Relancer avec --apply pour réparer.")
		return
	}
	fixed, err := fiscal.ApplyCollectorRepairs(ctx, db, repairs)
	if err != nil {
		log.Fatalf("réparation annulée (rollback) : %v", err)
	}
	fmt.Printf("%d paiement(s) réparé(s). Contrôler avec : go run ./cmd/verify_fiscal --all\n", fixed)
}
