// Command backfill_payout_documents génère et envoie le relevé de versement et
// la facture de commission de payouts Stripe déjà payés (docs/payouts-justificatifs.md).
//
// Le webhook payout.paid ne couvre que les payouts à venir : cette commande
// rattrape ceux qui sont déjà passés, et sert aux premiers essais sans attendre
// le prochain payout du 7. Elle met les payouts en file (table
// payout_documents) puis les traite comme la tâche horaire RunPayoutDocuments ;
// idempotente : un payout déjà traité n'est jamais renvoyé, et la facture d'un
// payout n'est émise qu'une fois.
//
// ATTENTION : hors phase de test (docsmode.TestMode = false dans
// internal/docsmode), les mails partent chez les restaurateurs.
// Lancer d'abord sans --apply : la commande liste ce qu'elle traiterait.
//
// Usage (la clé STRIPE_API_KEY et la base doivent être celles de l'environnement visé) :
//
//	# Un payout précis (le compte connecté est cherché automatiquement) :
//	POSTGRES_URL=... STRIPE_API_KEY=... BREVO_API_KEY=... \
//	  R2_ACCESS_KEY_ID=... R2_SECRET_ACCESS_KEY=... R2_ENDPOINT=... R2_PRIVATE_BUCKET=... \
//	  go run ./cmd/backfill_payout_documents --payout=po_xxx [--apply]
//
//	# Tous les payouts payés des 60 derniers jours :
//	... go run ./cmd/backfill_payout_documents --since-days=60 [--apply]
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"welloresto-api/internal/infrastructure/brevo_mailer"
	"welloresto-api/internal/infrastructure/r2"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/modules/payouts"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

func main() {
	payoutID := flag.String("payout", "", "un payout précis (po_...)")
	sinceDays := flag.Int("since-days", 0, "tous les payouts payés depuis N jours")
	apply := flag.Bool("apply", false, "générer et envoyer (défaut : liste seulement)")
	flag.Parse()

	if (*payoutID == "") == (*sinceDays <= 0) {
		log.Fatal("indiquer soit --payout=po_..., soit --since-days=N")
	}

	stripeKey, postgresURL := os.Getenv("STRIPE_API_KEY"), os.Getenv("POSTGRES_URL")
	if stripeKey == "" || postgresURL == "" {
		log.Fatal("STRIPE_API_KEY et POSTGRES_URL doivent être définis")
	}

	db, err := sql.Open("pgx", postgresURL)
	if err != nil {
		log.Fatalf("connexion à la base : %v", err)
	}
	defer db.Close()

	stripeMgr := stripeclient.NewStripeManager(stripeKey)
	repo := payouts.NewRepository(db)
	ctx := context.Background()

	if !*apply {
		dryRun(ctx, repo, stripeMgr, *payoutID, *sinceDays)
		return
	}

	// Archivage R2 optionnel : sans lui, les PDF partent quand même par mail.
	var store payouts.Store
	if r2Client, err := r2.NewClient(r2.UploadConfig{
		AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		Endpoint:        os.Getenv("R2_ENDPOINT"),
		Bucket:          os.Getenv("R2_PRIVATE_BUCKET"),
		PublicBaseURL:   os.Getenv("R2_PUBLIC_BASE_URL"),
	}); err != nil {
		log.Printf("R2 privé indisponible, PDF non archivés : %v", err)
	} else {
		store = r2Client
	}

	mail := brevo_mailer.NewBrevoMailer(brevo_mailer.Config{APIKey: os.Getenv("BREVO_API_KEY")})
	svc := payouts.NewService(repo, stripeMgr, store, mail, zap.NewExample())

	if *payoutID != "" {
		info, err := svc.Enqueue(ctx, "", *payoutID)
		if err != nil {
			log.Fatalf("mise en file : %v", err)
		}
		fmt.Printf("en file : %s (%s, %.2f %s)\n", info.ID, info.AccountID, float64(info.Amount)/100, info.Currency)
	} else {
		n, err := svc.EnqueueSince(ctx, time.Now().AddDate(0, 0, -*sinceDays))
		if err != nil {
			log.Fatalf("mise en file : %v", err)
		}
		fmt.Printf("%d payout(s) mis en file (les déjà traités sont ignorés)\n", n)
	}

	for {
		res, err := svc.ProcessPending(ctx)
		if err != nil {
			log.Fatalf("traitement : %v", err)
		}
		fmt.Printf("envoyés : %d — à retenter : %d — abandonnés : %d\n", res.Done, res.Retry, res.Failed)
		if res.Done == 0 {
			if res.Retry > 0 {
				fmt.Println("des payouts ne sont pas encore rapprochés par Stripe (ou ont une erreur, voir les logs) : relancer plus tard, ou laisser la tâche horaire s'en charger")
			}
			return
		}
	}
}

// dryRun liste les payouts que la commande traiterait, sans rien écrire.
func dryRun(ctx context.Context, repo payouts.Repository, stripeMgr *stripeclient.StripeManager, payoutID string, sinceDays int) {
	accounts, err := repo.ConnectedAccounts(ctx)
	if err != nil {
		log.Fatalf("comptes connectés : %v", err)
	}

	found := 0
	for _, account := range accounts {
		if payoutID != "" {
			if info, err := stripeMgr.GetPayout(ctx, account, payoutID); err == nil {
				fmt.Printf("%s  %s  %s  %.2f %s  arrivée le %s\n", info.ID, info.AccountID, info.Status, float64(info.Amount)/100, info.Currency, info.ArrivalDate.Format("02/01/2006"))
				found++
			}
			continue
		}
		list, err := stripeMgr.ListPaidPayouts(ctx, account, time.Now().AddDate(0, 0, -sinceDays))
		if err != nil {
			log.Printf("%s : %v", account, err)
			continue
		}
		for _, info := range list {
			fmt.Printf("%s  %s  %.2f %s  arrivée le %s\n", info.ID, info.AccountID, float64(info.Amount)/100, info.Currency, info.ArrivalDate.Format("02/01/2006"))
			found++
		}
	}
	fmt.Printf("%d payout(s) trouvé(s). Relancer avec --apply pour générer et envoyer les justificatifs.\n", found)
}
