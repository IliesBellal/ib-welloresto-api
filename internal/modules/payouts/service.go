package payouts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	"welloresto-api/internal/docsmode"
	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"

	"go.uber.org/zap"
)

const (
	// pendingBatch : payouts traités par passage horaire.
	pendingBatch = 50
	// invoiceBatch : factures mensuelles traitées par passage horaire.
	invoiceBatch = 50
	// invoiceMinDay : jour du mois (heure de Paris) à partir duquel la facture du
	// mois précédent est émise, pour laisser aux derniers virements le temps
	// d'être traités.
	invoiceMinDay = 3
	// listLimit : éléments renvoyés au back-office (5 ans de mois).
	listLimit = 60
	// linkTTL : durée de validité d'un lien de téléchargement.
	linkTTL = time.Hour
	// maxAttempts : tentatives horaires avant d'abandonner un payout ou l'envoi
	// d'une facture (3 jours — largement le temps que Stripe rapproche les
	// lignes d'un payout ou qu'une panne de mail passe).
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
	SendCommissionInvoice(to string, data mailer.CommissionInvoiceData, attachments []mailer.Attachment) error
}

// Service produit et envoie le relevé de chaque virement, puis la facture de
// commission et le récapitulatif de chaque mois. Rien n'y dépend du rythme des
// virements : un relevé par virement quel qu'il soit, une facture par mois.
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

// Process produit et envoie le relevé d'un payout. Sûr à rejouer : le PDF est regénéré.
func (s *Service) Process(ctx context.Context, p Pending) error {
	merchant, err := s.repo.MerchantByStripeAccount(ctx, p.AccountID)
	if err != nil {
		return fmt.Errorf("lecture du restaurateur: %w", err)
	}
	if merchant == nil {
		// Le relevé part quand même : identifié par le compte Stripe, il arrive
		// chez testRecipient.
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
	if summary.Commission < 0 {
		// Ne devrait pas arriver ; la facture mensuelle ignorera ce virement si le
		// total du mois reste négatif.
		s.log.Error("[payouts] commission négative sur un virement",
			zap.String("payout_id", p.PayoutID), zap.Int64("commission", summary.Commission))
	}

	statementPDF, err := buildStatementPDF(merchant, p.Ref, summary)
	if err != nil {
		return fmt.Errorf("PDF du relevé: %w", err)
	}
	statementName := "releve-versement-" + p.ArrivalDate.In(parisLocation).Format("2006-01-02") + ".pdf"

	// L'archivage est un plus : son échec n'empêche pas d'envoyer le relevé.
	recipient, notice := recipientFor(merchant)
	done := Done{
		MerchantID:   merchant.ID,
		Recipient:    recipient,
		StatementKey: s.archive(ctx, merchant.ID, p.PayoutID, statementName, statementPDF),
		Totals: Line{
			ScanNOrder:  summary.channelAmount(ChannelScanNOrder),
			Kiosk:       summary.channelAmount(ChannelKiosk),
			Other:       summary.channelAmount(ChannelOther),
			Refunds:     summary.Refunds,
			Adjustments: summary.Adjustments,
			Commission:  summary.Commission,
			StripeFees:  summary.StripeFees,
			Discrepancy: summary.Discrepancy,
		},
	}

	data := mailer.PayoutDocumentsData{
		MerchantName:  merchant.Name,
		PayoutID:      p.PayoutID,
		Amount:        formatEUR(p.Amount),
		ArrivalDate:   formatDate(p.ArrivalDate),
		BackOfficeURL: docsmode.BackOfficeURL + docsmode.TabVirementsPath,
		SupportEmail:  mailer.SupportEmail,
		TestNotice:    notice,
	}
	if err := s.mail.SendPayoutDocuments(recipient, data, []mailer.Attachment{{Name: statementName, Content: statementPDF}}); err != nil {
		return fmt.Errorf("envoi du mail: %w", err)
	}
	return s.repo.MarkDone(ctx, p.PayoutID, done)
}

// InvoiceResult résume un passage de RunMonthlyInvoices.
type InvoiceResult struct {
	Issued, Sent, Retry, Failed int
}

// RunMonthlyInvoices émet, pour chaque établissement, la facture de commission
// du mois écoulé, puis envoie toutes les factures émises dont le mail n'est pas
// encore parti.
//
// Émission : à partir du invoiceMinDay du mois (heure de Paris), pour chaque
// établissement qui a des virements traités et non facturés arrivés avant le
// début du mois courant, sans virement encore en cours de traitement et sans
// facture déjà émise pour le mois. Un virement traité en retard est donc repris
// le mois suivant, sans trou ni doublon. Le rythme des virements (quotidien,
// hebdomadaire, mensuel) n'y change rien.
func (s *Service) RunMonthlyInvoices(ctx context.Context) (InvoiceResult, error) {
	var res InvoiceResult
	now := s.now().In(parisLocation)

	if now.Day() >= invoiceMinDay {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, parisLocation)
		periodMonth := monthStart.AddDate(0, -1, 0)

		merchants, err := s.repo.InvoiceCandidates(ctx, monthStart, periodMonth)
		if err != nil {
			return res, fmt.Errorf("lecture des établissements à facturer: %w", err)
		}
		for _, merchantID := range merchants {
			issued, err := s.issueInvoice(ctx, merchantID, monthStart, periodMonth)
			if err != nil {
				s.log.Error("[payouts] émission de la facture mensuelle impossible",
					zap.String("merchant_id", merchantID), zap.Error(err))
				continue
			}
			if issued {
				res.Issued++
			}
		}
	}

	unsent, err := s.repo.UnsentInvoices(ctx, invoiceBatch)
	if err != nil {
		return res, fmt.Errorf("lecture des factures à envoyer: %w", err)
	}
	for _, inv := range unsent {
		err := s.deliverInvoice(ctx, inv)
		if err == nil {
			res.Sent++
			continue
		}

		abandon := inv.Attempts+1 >= maxAttempts
		if recErr := s.repo.RecordInvoiceFailure(ctx, inv.ID, err.Error(), abandon); recErr != nil {
			s.log.Error("[payouts] enregistrement de l'échec d'envoi impossible", zap.String("invoice", inv.Number), zap.Error(recErr))
		}
		if abandon {
			res.Failed++
			s.log.Error("[payouts] facture abandonnée après trop de tentatives d'envoi",
				zap.String("invoice", inv.Number), zap.String("merchant_id", inv.MerchantID), zap.Error(err))
			continue
		}
		res.Retry++
		s.log.Warn("[payouts] envoi de la facture en échec, nouvel essai à la prochaine heure",
			zap.String("invoice", inv.Number), zap.Error(err))
	}
	return res, nil
}

// issueInvoice émet la facture mensuelle d'un établissement ; false quand il n'y
// a finalement rien à facturer.
func (s *Service) issueInvoice(ctx context.Context, merchantID string, before, periodMonth time.Time) (bool, error) {
	lines, err := s.repo.PayoutsToInvoice(ctx, merchantID, before)
	if err != nil {
		return false, fmt.Errorf("lecture des virements: %w", err)
	}
	if len(lines) == 0 {
		return false, nil
	}

	ids := make([]string, 0, len(lines))
	var commission int64
	first, last := lines[0].ArrivalDate, lines[0].ArrivalDate
	for _, l := range lines {
		ids = append(ids, l.PayoutID)
		commission += l.Commission
		if l.ArrivalDate.Before(first) {
			first = l.ArrivalDate
		}
		if l.ArrivalDate.After(last) {
			last = l.ArrivalDate
		}
	}

	switch {
	case commission < 0:
		// Ne devrait pas arriver : on signale, sans facture.
		s.log.Error("[payouts] commission mensuelle négative, aucune facture émise",
			zap.String("merchant_id", merchantID), zap.Int64("commission", commission), zap.Int("virements", len(lines)))
		return false, s.repo.SkipInvoicing(ctx, ids)
	case commission == 0:
		return false, s.repo.SkipInvoicing(ctx, ids)
	}

	inv, err := s.repo.IssueInvoice(ctx, NewInvoice{
		Series:      invoiceSeries(),
		MerchantID:  merchantID,
		PeriodMonth: periodMonth,
		PayoutIDs:   ids,
		AmountTTC:   commission,
		PeriodStart: first,
		PeriodEnd:   last,
		IssuedAt:    s.now(),
	})
	if err != nil {
		return false, err
	}
	return inv != nil, nil
}

// deliverInvoice produit les deux PDF d'une facture émise (facture et
// récapitulatif), les archive et les envoie. Sûr à rejouer : les PDF sont
// regénérés depuis les données de la facture et de ses virements.
func (s *Service) deliverInvoice(ctx context.Context, inv Invoice) error {
	merchant, err := s.repo.MerchantByID(ctx, inv.MerchantID)
	if err != nil {
		return fmt.Errorf("lecture du restaurateur: %w", err)
	}
	if merchant == nil {
		s.log.Error("[payouts] établissement introuvable pour une facture", zap.String("invoice", inv.Number), zap.String("merchant_id", inv.MerchantID))
		merchant = &Merchant{ID: inv.MerchantID, Name: "établissement " + inv.MerchantID}
	}
	lines, err := s.repo.InvoiceLines(ctx, inv.ID)
	if err != nil {
		return fmt.Errorf("lecture des virements de la facture: %w", err)
	}

	invoicePDF, err := buildInvoicePDF(merchant, &inv)
	if err != nil {
		return fmt.Errorf("PDF de la facture: %w", err)
	}
	recapPDF, err := buildRecapPDF(merchant, &inv, lines)
	if err != nil {
		return fmt.Errorf("PDF du récapitulatif: %w", err)
	}
	invoiceName := "facture-" + inv.Number + ".pdf"
	recapName := "recapitulatif-commissions-" + inv.PeriodMonth.Format("2006-01") + ".pdf"

	recipient, notice := recipientFor(merchant)
	sent := InvoiceSent{
		Recipient:  recipient,
		InvoiceKey: s.archive(ctx, merchant.ID, "invoices/"+inv.Number, invoiceName, invoicePDF),
		RecapKey:   s.archive(ctx, merchant.ID, "invoices/"+inv.Number, recapName, recapPDF),
	}

	data := mailer.CommissionInvoiceData{
		MerchantName:  merchant.Name,
		InvoiceNumber: inv.Number,
		Month:         monthLabel(inv.PeriodMonth),
		Amount:        formatEUR(inv.AmountTTC),
		PayoutCount:   inv.PayoutCount,
		BackOfficeURL: docsmode.BackOfficeURL + docsmode.TabFacturesPath,
		SupportEmail:  mailer.SupportEmail,
		TestNotice:    notice,
	}
	attachments := []mailer.Attachment{{Name: invoiceName, Content: invoicePDF}, {Name: recapName, Content: recapPDF}}
	if err := s.mail.SendCommissionInvoice(recipient, data, attachments); err != nil {
		return fmt.Errorf("envoi du mail: %w", err)
	}
	return s.repo.MarkInvoiceSent(ctx, inv.ID, sent)
}

// archive dépose un PDF dans R2 et renvoie sa clé ("" si l'archivage a échoué
// ou n'est pas configuré).
func (s *Service) archive(ctx context.Context, merchantID, folder, filename string, content []byte) string {
	if s.store == nil {
		return ""
	}
	key := fmt.Sprintf("payouts/%s/%s/%s", merchantID, folder, filename)
	if _, err := s.store.UploadPrivateFile(ctx, key, bytes.NewReader(content), "application/pdf"); err != nil {
		s.log.Error("[payouts] archivage R2 impossible : ce document ne sera pas téléchargeable depuis le back-office", zap.String("key", key), zap.Error(err))
		return ""
	}
	return key
}

// ErrDocumentUnavailable : le document demandé n'existe pas (inconnu, d'un
// autre établissement) ou n'a pas pu être archivé.
var ErrDocumentUnavailable = errors.New("payout document unavailable")

// ListForMerchant renvoie les relevés envoyés d'un établissement, du plus récent au plus ancien.
func (s *Service) ListForMerchant(ctx context.Context, merchantID string) ([]Listed, error) {
	return s.repo.ListForMerchant(ctx, merchantID, listLimit)
}

// ListInvoicesForMerchant renvoie les factures de commission envoyées d'un établissement.
func (s *Service) ListInvoicesForMerchant(ctx context.Context, merchantID string) ([]Invoice, error) {
	return s.repo.ListInvoicesForMerchant(ctx, merchantID, listLimit)
}

// DownloadLink renvoie un lien signé d'une heure vers le relevé d'un virement de l'établissement.
func (s *Service) DownloadLink(ctx context.Context, merchantID, payoutID string) (*DownloadLink, error) {
	if s.store == nil {
		return nil, ErrDocumentUnavailable
	}
	doc, err := s.repo.GetForMerchant(ctx, merchantID, payoutID)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.StatementKey == "" {
		return nil, ErrDocumentUnavailable
	}
	return s.signed(ctx, payoutID, KindStatement, doc.StatementKey)
}

// InvoiceLink renvoie un lien signé d'une heure vers la facture (KindInvoice)
// ou le récapitulatif (KindRecap) d'une facture mensuelle de l'établissement.
func (s *Service) InvoiceLink(ctx context.Context, merchantID, number, kind string) (*DownloadLink, error) {
	if s.store == nil {
		return nil, ErrDocumentUnavailable
	}
	inv, err := s.repo.GetInvoiceForMerchant(ctx, merchantID, number)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, ErrDocumentUnavailable
	}
	key := inv.InvoiceKey
	if kind == KindRecap {
		key = inv.RecapKey
	}
	if key == "" {
		return nil, ErrDocumentUnavailable
	}
	return s.signed(ctx, number, kind, key)
}

func (s *Service) signed(ctx context.Context, ref, kind, key string) (*DownloadLink, error) {
	url, err := s.store.GenerateSignedURL(ctx, key, linkTTL)
	if err != nil {
		return nil, fmt.Errorf("lien de téléchargement: %w", err)
	}
	return &DownloadLink{Ref: ref, Kind: kind, Filename: path.Base(key), DownloadURL: url}, nil
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
