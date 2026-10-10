package payouts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"

	"go.uber.org/zap"
)

const (
	// pendingBatch : payouts traités par passage horaire.
	pendingBatch = 50
	// listLimit : justificatifs renvoyés au back-office (5 ans de payouts mensuels).
	listLimit = 60
	// linkTTL : durée de validité d'un lien de téléchargement.
	linkTTL = time.Hour
	// maxAttempts : tentatives horaires avant d'abandonner un payout (3 jours —
	// largement le temps que Stripe rapproche les lignes d'un payout).
	maxAttempts = 72
)

// StripeReader est la partie de l'API Stripe dont le module a besoin
// (implémentée par *stripeclient.StripeManager).
type StripeReader interface {
	GetPayout(ctx context.Context, accountID, payoutID string) (*stripeclient.PayoutInfo, error)
	ListPaidPayouts(ctx context.Context, accountID string, since time.Time) ([]stripeclient.PayoutInfo, error)
	ListPayoutTransactions(ctx context.Context, accountID, payoutID string) ([]stripeclient.PayoutTransaction, error)
}

// Store archive les PDF dans le bucket R2 privé et en donne des liens
// temporaires (nil : pas d'archivage).
type Store interface {
	UploadPrivateFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
	GenerateSignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Mailer envoie les justificatifs (implémenté par mailer.Service).
type Mailer interface {
	SendPayoutDocuments(to string, data mailer.PayoutDocumentsData, attachments []mailer.Attachment) error
}

// Service produit et envoie le relevé de versement et la facture de commission
// de chaque payout.
type Service struct {
	repo   Repository
	stripe StripeReader
	store  Store
	mail   Mailer
	log    *zap.Logger
	now    func() time.Time
}

// NewService construit le service. store et log peuvent être nil.
func NewService(repo Repository, stripe StripeReader, store Store, mail Mailer, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{repo: repo, stripe: stripe, store: store, mail: mail, log: log, now: time.Now}
}

// RecordPaidPayout met un payout payé en file d'attente (appelé par le webhook
// payout.paid). Idempotent : un événement rejoué ne crée pas de doublon.
func (s *Service) RecordPaidPayout(ctx context.Context, ref Ref) error {
	return s.repo.InsertPending(ctx, ref)
}

// Result résume un passage de ProcessPending.
type Result struct {
	Done, Retry, Failed int
}

// ProcessPending traite les payouts en attente. Une erreur sur un payout ne
// bloque pas les suivants : il est retenté au passage suivant, puis abandonné
// (et signalé) après maxAttempts échecs.
func (s *Service) ProcessPending(ctx context.Context) (Result, error) {
	var res Result
	pending, err := s.repo.ListPending(ctx, pendingBatch)
	if err != nil {
		return res, fmt.Errorf("lecture des payouts en attente: %w", err)
	}

	for _, p := range pending {
		err := s.Process(ctx, p)
		if err == nil {
			res.Done++
			continue
		}

		abandon := p.Attempts+1 >= maxAttempts
		if recErr := s.repo.RecordFailure(ctx, p.PayoutID, err.Error(), abandon); recErr != nil {
			s.log.Error("[payouts] enregistrement de l'échec impossible", zap.String("payout_id", p.PayoutID), zap.Error(recErr))
		}

		var notReconciled *ErrNotReconciled
		switch {
		case abandon:
			res.Failed++
			s.log.Error("[payouts] justificatifs abandonnés après trop de tentatives",
				zap.String("payout_id", p.PayoutID), zap.String("account_id", p.AccountID), zap.Error(err))
		case errors.As(err, &notReconciled):
			res.Retry++
			s.log.Info("[payouts] payout pas encore rapproché par Stripe, nouvel essai à la prochaine heure",
				zap.String("payout_id", p.PayoutID), zap.Error(err))
		default:
			res.Retry++
			s.log.Warn("[payouts] justificatifs en échec, nouvel essai à la prochaine heure",
				zap.String("payout_id", p.PayoutID), zap.Error(err))
		}
	}
	return res, nil
}

// Process produit et envoie les justificatifs d'un payout. Sûr à rejouer : la
// facture est émise une seule fois par payout et les PDF sont régénérés.
func (s *Service) Process(ctx context.Context, p Pending) error {
	merchant, err := s.repo.MerchantByStripeAccount(ctx, p.AccountID)
	if err != nil {
		return fmt.Errorf("lecture du restaurateur: %w", err)
	}
	if merchant == nil {
		// Les justificatifs partent quand même : identifiés par le compte Stripe,
		// ils arrivent chez testRecipient.
		s.log.Error("[payouts] aucun restaurateur pour ce compte Stripe",
			zap.String("payout_id", p.PayoutID), zap.String("account_id", p.AccountID))
		merchant = &Merchant{Name: p.AccountID}
	}

	txs, err := s.stripe.ListPayoutTransactions(ctx, p.AccountID, p.PayoutID)
	if err != nil {
		return err
	}
	channels, err := s.repo.ChannelsByPaymentIntent(ctx, SalePaymentIntents(txs))
	if err != nil {
		return fmt.Errorf("rattachement des paiements aux commandes: %w", err)
	}
	summary := Aggregate(txs, channels, p.Amount)
	if !summary.Reconciled() {
		// Stripe rapproche en général en quelques minutes : on retente d'abord.
		// Passé reconcileRetries essais, le relevé part avec son écart signalé,
		// plutôt que de ne jamais partir.
		if p.Attempts < reconcileRetries {
			return &ErrNotReconciled{PayoutAmount: p.Amount, Discrepancy: summary.Discrepancy}
		}
		s.log.Error("[payouts] payout envoyé avec un écart non rapproché",
			zap.String("payout_id", p.PayoutID), zap.Int64("ecart_centimes", summary.Discrepancy))
	}

	invoice, err := s.invoiceFor(ctx, merchant, p.Ref, summary)
	if err != nil {
		return err
	}

	statementPDF, err := buildStatementPDF(merchant, p.Ref, summary, invoice)
	if err != nil {
		return fmt.Errorf("PDF du relevé: %w", err)
	}
	statementName := "releve-versement-" + p.ArrivalDate.In(parisLocation).Format("2006-01-02") + ".pdf"
	attachments := []mailer.Attachment{{Name: statementName, Content: statementPDF}}

	var invoicePDF []byte
	var invoiceName string
	if invoice != nil {
		if invoicePDF, err = buildInvoicePDF(merchant, p.Ref, invoice); err != nil {
			return fmt.Errorf("PDF de la facture: %w", err)
		}
		invoiceName = "facture-" + invoice.Number + ".pdf"
		attachments = append(attachments, mailer.Attachment{Name: invoiceName, Content: invoicePDF})
	}

	// L'archivage est un plus : son échec n'empêche pas d'envoyer les documents.
	recipient, notice := recipientFor(merchant)
	done := Done{
		MerchantID:   merchant.ID,
		Recipient:    recipient,
		StatementKey: s.archive(ctx, merchant.ID, p.PayoutID, statementName, statementPDF),
	}
	if invoice != nil {
		done.InvoiceKey = s.archive(ctx, merchant.ID, p.PayoutID, invoiceName, invoicePDF)
	}

	data := mailer.PayoutDocumentsData{
		MerchantName: merchant.Name,
		PayoutID:     p.PayoutID,
		Amount:       formatEUR(p.Amount),
		ArrivalDate:  formatDate(p.ArrivalDate),
		SupportEmail: mailer.SupportEmail,
	}
	if invoice != nil {
		data.InvoiceNumber = invoice.Number
	}
	data.TestNotice = notice

	if err := s.mail.SendPayoutDocuments(done.Recipient, data, attachments); err != nil {
		return fmt.Errorf("envoi du mail: %w", err)
	}
	return s.repo.MarkDone(ctx, p.PayoutID, done)
}

// invoiceFor renvoie la facture de commission du payout, en l'émettant si
// besoin ; nil quand il n'y a rien à facturer.
func (s *Service) invoiceFor(ctx context.Context, merchant *Merchant, ref Ref, summary *Summary) (*Invoice, error) {
	switch {
	case summary.Commission < 0:
		// Ne devrait pas arriver (une commission négative suppose plus de
		// remboursements de commission que de commission) : on signale et le
		// relevé part seul.
		s.log.Error("[payouts] commission négative, aucune facture émise",
			zap.String("payout_id", ref.PayoutID), zap.String("merchant_id", merchant.ID), zap.Int64("commission", summary.Commission))
		return nil, nil
	case summary.Commission == 0:
		return nil, nil
	}

	existing, err := s.repo.InvoiceForPayout(ctx, ref.PayoutID)
	if err != nil {
		return nil, fmt.Errorf("lecture de la facture: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	invoice, err := s.repo.CreateInvoice(ctx, NewInvoice{
		Series:      invoiceSeries(),
		MerchantID:  merchant.ID,
		PayoutID:    ref.PayoutID,
		AmountTTC:   summary.Commission,
		PeriodStart: summary.PeriodStart,
		PeriodEnd:   summary.PeriodEnd,
		IssuedAt:    s.now(),
	})
	if err != nil {
		return nil, fmt.Errorf("émission de la facture: %w", err)
	}
	return invoice, nil
}

// archive dépose un PDF dans R2 et renvoie sa clé ("" si l'archivage a échoué
// ou n'est pas configuré).
func (s *Service) archive(ctx context.Context, merchantID, payoutID, filename string, content []byte) string {
	if s.store == nil {
		return ""
	}
	key := fmt.Sprintf("payouts/%s/%s/%s", merchantID, payoutID, filename)
	if _, err := s.store.UploadPrivateFile(ctx, key, bytes.NewReader(content), "application/pdf"); err != nil {
		s.log.Error("[payouts] archivage R2 impossible : ce document ne sera pas téléchargeable depuis le back-office", zap.String("key", key), zap.Error(err))
		return ""
	}
	return key
}

// ErrDocumentUnavailable : le document demandé n'existe pas (payout inconnu,
// d'un autre établissement, sans facture) ou n'a pas pu être archivé.
var ErrDocumentUnavailable = errors.New("payout document unavailable")

// ListForMerchant renvoie les justificatifs envoyés d'un établissement, du plus récent au plus ancien.
func (s *Service) ListForMerchant(ctx context.Context, merchantID string) ([]Listed, error) {
	return s.repo.ListForMerchant(ctx, merchantID, listLimit)
}

// DownloadLink renvoie un lien signé d'une heure vers le relevé ("statement") ou
// la facture ("invoice") d'un payout de l'établissement.
func (s *Service) DownloadLink(ctx context.Context, merchantID, payoutID, kind string) (*DownloadLink, error) {
	if s.store == nil {
		return nil, ErrDocumentUnavailable
	}
	doc, err := s.repo.GetForMerchant(ctx, merchantID, payoutID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, ErrDocumentUnavailable
	}

	key := doc.StatementKey
	if kind == KindInvoice {
		key = doc.InvoiceKey
	}
	if key == "" {
		return nil, ErrDocumentUnavailable
	}
	url, err := s.store.GenerateSignedURL(ctx, key, linkTTL)
	if err != nil {
		return nil, fmt.Errorf("lien de téléchargement: %w", err)
	}
	return &DownloadLink{PayoutID: payoutID, Kind: kind, Filename: path.Base(key), DownloadURL: url}, nil
}

// Enqueue met en file un payout précis (rattrapage). accountID peut être vide :
// le payout est alors cherché sur tous les comptes connectés.
func (s *Service) Enqueue(ctx context.Context, accountID, payoutID string) (*stripeclient.PayoutInfo, error) {
	accounts := []string{accountID}
	if accountID == "" {
		var err error
		if accounts, err = s.repo.ConnectedAccounts(ctx); err != nil {
			return nil, err
		}
	}

	for _, account := range accounts {
		info, err := s.stripe.GetPayout(ctx, account, payoutID)
		if err != nil {
			continue // payout d'un autre compte
		}
		if info.Status != "paid" {
			return info, fmt.Errorf("payout %s au statut %q : seuls les payouts payés ont des justificatifs", payoutID, info.Status)
		}
		return info, s.repo.InsertPending(ctx, refOf(info))
	}
	return nil, fmt.Errorf("payout %s introuvable sur les comptes connectés", payoutID)
}

// EnqueueSince met en file tous les payouts payés depuis `since` (rattrapage).
func (s *Service) EnqueueSince(ctx context.Context, since time.Time) (int, error) {
	accounts, err := s.repo.ConnectedAccounts(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, account := range accounts {
		payouts, err := s.stripe.ListPaidPayouts(ctx, account, since)
		if err != nil {
			s.log.Warn("[payouts] lecture des payouts impossible", zap.String("account_id", account), zap.Error(err))
			continue
		}
		for i := range payouts {
			if err := s.repo.InsertPending(ctx, refOf(&payouts[i])); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

func refOf(info *stripeclient.PayoutInfo) Ref {
	return Ref{
		PayoutID:    info.ID,
		AccountID:   info.AccountID,
		Amount:      info.Amount,
		Currency:    info.Currency,
		ArrivalDate: info.ArrivalDate,
	}
}
