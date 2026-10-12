// Command backfill_subscription_invoices envoie par mail les factures Stripe
// d'abonnement déjà payées et les rend consultables dans le back-office
// (docs/payouts-justificatifs.md, « Factures d'abonnement »).
//
// Le webhook invoice.paid ne couvre que les paiements à venir : cette commande
// rattrape ceux qui sont déjà passés, et sert aux premiers essais sans attendre
// le prochain prélèvement. Elle met les factures en file (table
// subscription_invoice_documents) puis les traite comme la tâche horaire
// RunSubscriptionInvoiceDocuments ; idempotente : une facture déjà envoyée ne
// l'est jamais deux fois.
//
// ATTENTION : hors phase de test (docsmode.TestMode = false dans
// internal/docsmode), les mails partent chez les restaurateurs. Lancer d'abord
// sans --apply : la commande liste ce qu'elle traiterait.
//
// Usage (la clé STRIPE_API_KEY et la base doivent être celles de l'environnement visé) :
//
//	POSTGRES_URL=... STRIPE_API_KEY=... BREVO_API_KEY=... \
//	  R2_ACCESS_KEY_ID=... R2_SECRET_ACCESS_KEY=... R2_ENDPOINT=... R2_PRIVATE_BUCKET=... \
//	  go run ./cmd/backfill_subscription_invoices --since-days=90 [--apply]
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
	"welloresto-api/internal/modules/subscriptioninvoices"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

func main() {
	sinceDays := flag.Int("since-days", 0, "toutes les factures d'abonnement payées depuis N jours")
	apply := flag.Bool("apply", false, "envoyer (défaut : liste seulement)")
	flag.Parse()

	if *sinceDays <= 0 {
		log.Fatal("indiquer --since-days=N")
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
	repo := subscriptioninvoices.NewRepository(db)
	ctx := context.Background()
	since := time.Now().AddDate(0, 0, -*sinceDays)

	if !*apply {
		dryRun(ctx, repo, stripeMgr, since)
		return
	}

	// Archivage R2 optionnel : sans lui, les factures partent quand même par mail.
	var store subscriptioninvoices.Store
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
	svc := subscriptioninvoices.NewService(repo, stripeMgr, store, mail, zap.NewExample())

	n, err := svc.EnqueueSince(ctx, since)
	if err != nil {
		log.Fatalf("mise en file : %v", err)
	}
	fmt.Printf("%d facture(s) mise(s) en file (les déjà envoyées sont ignorées)\n", n)

	for {
		res, err := svc.ProcessPending(ctx)
		if err != nil {
			log.Fatalf("traitement : %v", err)
		}
		fmt.Printf("envoyées : %d — à retenter : %d — abandonnées : %d\n", res.Done, res.Retry, res.Failed)
		if res.Done == 0 {
			if res.Retry > 0 {
				fmt.Println("des factures sont en erreur (voir les logs) : relancer plus tard, ou laisser la tâche horaire s'en charger")
			}
			return
		}
	}
}

// dryRun liste les factures que la commande traiterait, sans rien écrire.
func dryRun(ctx context.Context, repo subscriptioninvoices.Repository, stripeMgr *stripeclient.StripeManager, since time.Time) {
	invoices, err := stripeMgr.ListPaidInvoices(ctx, since)
	if err != nil {
		log.Fatalf("lecture des factures : %v", err)
	}

	found := 0
	for _, inv := range invoices {
		if inv.SubscriptionID == "" || inv.AmountPaid <= 0 {
			continue
		}
		merchantID, err := repo.MerchantBySubscription(ctx, inv.SubscriptionID)
		if err != nil {
			log.Fatalf("lecture de l'abonnement %s : %v", inv.SubscriptionID, err)
		}
		if merchantID == "" {
			fmt.Printf("%s  %s  %.2f %s  ABONNEMENT INCONNU (%s) : ignorée\n", inv.ID, inv.Number, float64(inv.AmountPaid)/100, inv.Currency, inv.SubscriptionID)
			continue
		}
		fmt.Printf("%s  %s  établissement %s  %.2f %s  payée le %s\n", inv.ID, inv.Number, merchantID, float64(inv.AmountPaid)/100, inv.Currency, inv.PaidAt.Format("02/01/2006"))
		found++
	}
	fmt.Printf("%d facture(s) à envoyer. Relancer avec --apply pour les envoyer.\n", found)
}
