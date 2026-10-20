package subscriptioninvoices

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"welloresto-api/internal/docsmode"
	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"

	"go.uber.org/zap"
)

const (
	// pendingBatch : factures traitées par passage horaire.
	pendingBatch = 50
	// maxAttempts : tentatives horaires avant d'abandonner une facture (3 jours).
	maxAttempts = 72
	// listLimit : factures renvoyées au back-office (5 ans de facturation mensuelle).
	listLimit = 60
	// linkTTL : durée de validité d'un lien de téléchargement.
	linkTTL = time.Hour
)

// StripeReader est la partie de l'API Stripe dont le module a besoin
// (implémentée par *stripeclient.StripeManager).
type StripeReader interface {
	GetInvoice(ctx context.Context, invoiceID string) (*stripeclient.InvoiceInfo, error)
	ListPaidInvoices(ctx context.Context, since time.Time) ([]stripeclient.InvoiceInfo, error)
	FetchInvoicePDF(ctx context.Context, pdfURL string) ([]byte, error)
}

// Store archive les PDF dans le bucket R2 privé et en donne des liens
// temporaires (nil : pas d'archivage).
type Store interface {
	UploadPrivateFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
	GenerateSignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Mailer envoie la facture (implémenté par mailer.Service).
type Mailer interface {
	SendSubscriptionInvoice(to string, data mailer.SubscriptionInvoiceData, attachment mailer.Attachment) error
}

// Service envoie la facture de chaque paiement d'abonnement et la met à
// disposition du back-office.
type Service struct {
	repo   Repository
	stripe StripeReader
	store  Store
	mail   Mailer
	log    *zap.Logger
}

// NewService construit le service. store et log peuvent être nil.
func NewService(repo Repository, stripe StripeReader, store Store, mail Mailer, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{repo: repo, stripe: stripe, store: store, mail: mail, log: log}
}

// RecordPaidInvoice met une facture payée en file d'attente (appelé par le
// webhook invoice.paid). Idempotent ; une facture à zéro (période d'essai) n'a
// rien à justifier et est ignorée.
func (s *Service) RecordPaidInvoice(ctx context.Context, ref Ref) error {
	if ref.AmountPaid <= 0 || ref.MerchantID == "" {
		return nil
	}
	return s.repo.InsertPending(ctx, ref)
}

// Result résume un passage de ProcessPending.
type Result struct {
	Done, Retry, Failed int
}

// ProcessPending traite les factures en attente. Une erreur sur une facture ne
// bloque pas les suivantes : elle est retentée au passage suivant, puis
// abandonnée (et signalée) après maxAttempts échecs.
func (s *Service) ProcessPending(ctx context.Context) (Result, error) {
	var res Result
	pending, err := s.repo.ListPending(ctx, pendingBatch)
	if err != nil {
		return res, fmt.Errorf("lecture des factures en attente: %w", err)
	}

	for _, p := range pending {
		err := s.Process(ctx, p)
		if err == nil {
			res.Done++
			continue
		}

		abandon := p.Attempts+1 >= maxAttempts
		if recErr := s.repo.RecordFailure(ctx, p.InvoiceID, err.Error(), abandon); recErr != nil {
			s.log.Error("[subscription-invoices] enregistrement de l'échec impossible", zap.String("invoice_id", p.InvoiceID), zap.Error(recErr))
		}
		if abandon {
			res.Failed++
			s.log.Error("[subscription-invoices] facture abandonnée après trop de tentatives",
				zap.String("invoice_id", p.InvoiceID), zap.String("merchant_id", p.MerchantID), zap.Error(err))
			continue
		}
		res.Retry++
		s.log.Warn("[subscription-invoices] facture en échec, nouvel essai à la prochaine heure",
			zap.String("invoice_id", p.InvoiceID), zap.Error(err))
	}
	return res, nil
}

// Process envoie la facture d'un paiement d'abonnement. Sûr à rejouer.
func (s *Service) Process(ctx context.Context, p Pending) error {
	info, err := s.stripe.GetInvoice(ctx, p.InvoiceID)
	if err != nil {
		return err
	}
	pdf, err := s.stripe.FetchInvoicePDF(ctx, info.PDFURL)
	if err != nil {
		return err
	}

	merchant, err := s.repo.Merchant(ctx, p.MerchantID)
	if err != nil {
		return fmt.Errorf("lecture du restaurateur: %w", err)
	}
	if merchant == nil {
		// La facture part quand même, identifiée par l'établissement.
		s.log.Error("[subscription-invoices] établissement introuvable",
			zap.String("invoice_id", p.InvoiceID), zap.String("merchant_id", p.MerchantID))
		merchant = &Merchant{ID: p.MerchantID, Name: "établissement " + p.MerchantID}
	}

	number := firstNonEmpty(info.Number, p.Number, p.InvoiceID)
	filename := "facture-abonnement-" + strings.NewReplacer("/", "-", " ", "-").Replace(number) + ".pdf"
	recipient, notice := recipientFor(merchant)

	// L'archivage est un plus : son échec n'empêche pas d'envoyer la facture.
	done := Done{Number: number, Recipient: recipient, PDFKey: s.archive(ctx, p.MerchantID, p.InvoiceID, filename, pdf)}

	data := mailer.SubscriptionInvoiceData{
		// Une même adresse peut recevoir les factures de plusieurs établissements.
		Establishment: mailer.Establishment{ID: merchant.ID, Name: merchant.Name, Address: merchant.Address},
		MerchantName:  merchant.Name,
		InvoiceNumber: number,
		Amount:        formatEUR(p.AmountPaid),
		Period:        formatPeriod(servicePeriod(p.Ref, info)),
		HostedURL:     info.HostedURL,
		SupportEmail:  mailer.SupportEmail,
		TestNotice:    notice,
	}
	if err := s.mail.SendSubscriptionInvoice(recipient, data, mailer.Attachment{Name: filename, Content: pdf}); err != nil {
		return fmt.Errorf("envoi du mail: %w", err)
	}
	return s.repo.MarkDone(ctx, p.InvoiceID, done)
}

// archive dépose le PDF dans R2 et renvoie sa clé ("" si l'archivage a échoué
// ou n'est pas configuré).
func (s *Service) archive(ctx context.Context, merchantID, invoiceID, filename string, content []byte) string {
	if s.store == nil {
		return ""
	}
	key := fmt.Sprintf("subscription-invoices/%s/%s/%s", merchantID, invoiceID, filename)
	if _, err := s.store.UploadPrivateFile(ctx, key, bytes.NewReader(content), "application/pdf"); err != nil {
		s.log.Error("[subscription-invoices] archivage R2 impossible : cette facture ne sera pas téléchargeable depuis le back-office",
			zap.String("key", key), zap.Error(err))
		return ""
	}
	return key
}

// ErrDocumentUnavailable : la facture demandée n'existe pas (inconnue, d'un
// autre établissement) ou n'a pas pu être archivée.
var ErrDocumentUnavailable = errors.New("subscription invoice unavailable")

// ListForMerchant renvoie les factures envoyées d'un établissement, de la plus récente à la plus ancienne.
func (s *Service) ListForMerchant(ctx context.Context, merchantID string) ([]Listed, error) {
	return s.repo.ListForMerchant(ctx, merchantID, listLimit)
}

// DownloadLink renvoie un lien signé d'une heure vers la facture d'un établissement.
func (s *Service) DownloadLink(ctx context.Context, merchantID, invoiceID string) (*DownloadLink, error) {
	if s.store == nil {
		return nil, ErrDocumentUnavailable
	}
	doc, err := s.repo.GetForMerchant(ctx, merchantID, invoiceID)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.PDFKey == "" {
		return nil, ErrDocumentUnavailable
	}
	url, err := s.store.GenerateSignedURL(ctx, doc.PDFKey, linkTTL)
	if err != nil {
		return nil, fmt.Errorf("lien de téléchargement: %w", err)
	}
	return &DownloadLink{InvoiceID: invoiceID, Filename: path.Base(doc.PDFKey), DownloadURL: url}, nil
}

// EnqueueSince met en file toutes les factures d'abonnement payées depuis
// `since` (rattrapage). Les factures sans établissement connu ou à zéro sont ignorées.
func (s *Service) EnqueueSince(ctx context.Context, since time.Time) (int, error) {
	invoices, err := s.stripe.ListPaidInvoices(ctx, since)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, inv := range invoices {
		if inv.SubscriptionID == "" || inv.AmountPaid <= 0 {
			continue
		}
		merchantID, err := s.repo.MerchantBySubscription(ctx, inv.SubscriptionID)
		if err != nil {
			return count, err
		}
		if merchantID == "" {
			s.log.Warn("[subscription-invoices] abonnement inconnu, facture ignorée",
				zap.String("invoice_id", inv.ID), zap.String("subscription_id", inv.SubscriptionID))
			continue
		}
		ref := Ref{
			InvoiceID: inv.ID, MerchantID: merchantID, Number: inv.Number, AmountPaid: inv.AmountPaid,
			Currency: inv.Currency, PeriodStart: inv.PeriodStart, PeriodEnd: inv.PeriodEnd, PaidAt: inv.PaidAt,
		}
		if err := s.repo.InsertPending(ctx, ref); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// recipientFor renvoie l'adresse qui reçoit la facture d'un marchand (son
// responsable, voir docsmode.Recipient), et une note à afficher dans le mail
// quand ce n'est pas lui qui la reçoit.
func recipientFor(m *Merchant) (to, notice string) {
	return docsmode.Recipient(docsmode.TestMode, m.Name, m.OwnerEmail, m.Email)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// formatEUR formate des centimes à la française : 7900 -> "79,00 €".
func formatEUR(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := fmt.Sprintf("%d", cents/100)
	var groups []string
	for len(whole) > 3 {
		groups = append([]string{whole[len(whole)-3:]}, groups...)
		whole = whole[:len(whole)-3]
	}
	groups = append([]string{whole}, groups...)
	return fmt.Sprintf("%s%s,%02d €", sign, strings.Join(groups, " "), cents%100)
}

// servicePeriod préfère la période lue sur les lignes de la facture (relue chez
// Stripe) à celle enregistrée à la réception du webhook, qui peut se réduire à
// la date d'émission.
func servicePeriod(ref Ref, info *stripeclient.InvoiceInfo) (time.Time, time.Time) {
	if info.PeriodEnd.After(info.PeriodStart) {
		return info.PeriodStart, info.PeriodEnd
	}
	return ref.PeriodStart, ref.PeriodEnd
}

// formatPeriod : "du 01/10/2026 au 31/10/2026" (vide si la période est inconnue).
func formatPeriod(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || start.Unix() <= 0 || !end.After(start) {
		return ""
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		loc = time.UTC
	}
	return "du " + start.In(loc).Format("02/01/2006") + " au " + end.In(loc).Format("02/01/2006")
}
