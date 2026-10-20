// Package subscriptioninvoices envoie aux restaurateurs, par mail, la facture
// Stripe de chacun de leurs paiements d'abonnement, et la met à disposition dans
// le back-office (docs/payouts-justificatifs.md, section « Factures d'abonnement »).
//
// La facture envoyée est le PDF que Stripe a émis (numérotation et mentions
// légales de Stripe Billing), jamais une regénération.
package subscriptioninvoices

import "time"

// Ref identifie une facture d'abonnement payée à envoyer.
type Ref struct {
	InvoiceID   string
	MerchantID  string
	Number      string
	AmountPaid  int64 // centimes
	Currency    string
	PeriodStart time.Time
	PeriodEnd   time.Time
	PaidAt      time.Time
}

// Pending est une ligne de subscription_invoice_documents à traiter.
type Pending struct {
	Ref
	Attempts int
}

// Merchant est le destinataire de la facture (table merchant).
type Merchant struct {
	ID      string
	Name    string
	Address string
	Email   string // adresse de l'établissement (merchant.email)
	// OwnerEmail : adresse du responsable (premier admin actif), destinataire
	// prioritaire de la facture (docsmode.Recipient).
	OwnerEmail string
}

// Listed est une facture envoyée, vue du back-office.
type Listed struct {
	InvoiceID   string    `json:"invoice_id"`
	Number      string    `json:"number"`
	AmountPaid  int64     `json:"amount_paid"`
	Currency    string    `json:"currency"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	PaidAt      time.Time `json:"paid_at"`
	SentAt      time.Time `json:"sent_at"`
	HasPDF      bool      `json:"has_pdf"`

	PDFKey string `json:"-"`
}

// DownloadLink est le lien signé d'une facture.
type DownloadLink struct {
	InvoiceID   string `json:"invoice_id"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
}
