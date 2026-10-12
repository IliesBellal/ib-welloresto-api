package stripeclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/stripe/stripe-go/v84"
)

// InvoiceInfo est la vue d'une facture d'abonnement de la plateforme (Stripe
// Billing, compte plateforme — jamais d'en-tête StripeAccount), réduite à ce
// dont l'envoi au restaurateur a besoin.
type InvoiceInfo struct {
	ID             string
	Number         string
	Status         string
	AmountPaid     int64 // centimes
	Currency       string
	PDFURL         string
	HostedURL      string
	SubscriptionID string
	PeriodStart    time.Time
	PeriodEnd      time.Time
	PaidAt         time.Time
}

// GetInvoice lit une facture d'abonnement (l'URL du PDF y est renouvelée à
// chaque lecture, d'où la relecture plutôt que de se fier à l'événement).
func (s *StripeManager) GetInvoice(ctx context.Context, invoiceID string) (*InvoiceInfo, error) {
	params := &stripe.InvoiceParams{}
	params.Context = ctx

	inv, err := s.client.Invoices.Get(invoiceID, params)
	if err != nil {
		return nil, fmt.Errorf("stripe: get invoice %s: %w", invoiceID, err)
	}
	return invoiceInfoFrom(inv), nil
}

// ListPaidInvoices liste les factures payées créées depuis `since` (rattrapage).
func (s *StripeManager) ListPaidInvoices(ctx context.Context, since time.Time) ([]InvoiceInfo, error) {
	params := &stripe.InvoiceListParams{
		Status:       stripe.String("paid"),
		CreatedRange: &stripe.RangeQueryParams{GreaterThanOrEqual: since.Unix()},
	}
	params.Context = ctx
	params.Limit = stripe.Int64(100)

	var out []InvoiceInfo
	iter := s.client.Invoices.List(params)
	for iter.Next() {
		out = append(out, *invoiceInfoFrom(iter.Invoice()))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("stripe: list paid invoices: %w", err)
	}
	return out, nil
}

// FetchInvoicePDF télécharge le PDF d'une facture depuis l'URL publique que
// Stripe fournit (invoice_pdf). C'est la facture légale, numérotée par Stripe :
// on la transmet telle quelle, sans la regénérer.
func (s *StripeManager) FetchInvoicePDF(ctx context.Context, pdfURL string) ([]byte, error) {
	if pdfURL == "" {
		return nil, fmt.Errorf("stripe: la facture n'a pas (encore) de PDF")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe: download invoice pdf: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stripe: download invoice pdf: status %d", resp.StatusCode)
	}

	// Une facture fait quelques dizaines de Ko : 10 Mo est une borne de sécurité.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("stripe: read invoice pdf: %w", err)
	}
	if len(body) < 4 || string(body[:4]) != "%PDF" {
		return nil, fmt.Errorf("stripe: l'URL de la facture ne renvoie pas un PDF")
	}
	return body, nil
}

func invoiceInfoFrom(inv *stripe.Invoice) *InvoiceInfo {
	info := &InvoiceInfo{
		ID:          inv.ID,
		Number:      inv.Number,
		Status:      string(inv.Status),
		AmountPaid:  inv.AmountPaid,
		Currency:    string(inv.Currency),
		PDFURL:      inv.InvoicePDF,
		HostedURL:   inv.HostedInvoiceURL,
		PeriodStart: time.Unix(inv.PeriodStart, 0),
		PeriodEnd:   time.Unix(inv.PeriodEnd, 0),
	}
	// Pour un abonnement, period_start/period_end de la facture valent la date
	// d'émission : la vraie période de service est portée par ses lignes.
	if inv.Lines != nil {
		var start, end int64
		for _, line := range inv.Lines.Data {
			if line == nil || line.Period == nil || line.Period.End <= line.Period.Start {
				continue
			}
			if start == 0 || line.Period.Start < start {
				start = line.Period.Start
			}
			if line.Period.End > end {
				end = line.Period.End
			}
		}
		if end > start {
			info.PeriodStart, info.PeriodEnd = time.Unix(start, 0), time.Unix(end, 0)
		}
	}
	if inv.StatusTransitions != nil && inv.StatusTransitions.PaidAt > 0 {
		info.PaidAt = time.Unix(inv.StatusTransitions.PaidAt, 0)
	}
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil && inv.Parent.SubscriptionDetails.Subscription != nil {
		info.SubscriptionID = inv.Parent.SubscriptionDetails.Subscription.ID
	}
	return info
}
