package customer_accounts

import (
	"context"

	"welloresto-api/internal/infrastructure/mailer"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
)

// Mailer envoie un email à partir d'un modèle (mailer.Service).
type Mailer interface {
	SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{})
	SendAsyncWithAttachment(fromName, fromEmail, to, subject, templateName string, data interface{}, attachmentBytes []byte, attachmentName string)
}

// SMSSender envoie un SMS au nom d'un établissement, compté sur sa
// consommation (merchantsms.Service).
type SMSSender interface {
	SendTransactional(ctx context.Context, merchantID, phone, message string) error
}

// BrandResolver donne l'identité visuelle des emails d'un établissement
// (merchantbrand.Repository).
type BrandResolver interface {
	Resolve(ctx context.Context, merchantID, name string) mailer.MerchantBrand
}

// CheckoutParams : session de paiement en ligne d'un compte client, sur le
// compte Stripe Connect de l'établissement.
type CheckoutParams struct {
	StripeAccountID string
	Amount          int64
	ApplicationFee  int64
	Description     string
	SuccessURL      string
	CancelURL       string
	Metadata        map[string]string
}

// OnlinePayments crée une session de paiement Stripe et renvoie son adresse.
type OnlinePayments interface {
	CreateCustomerAccountCheckout(p CheckoutParams) (url string, err error)
}

// StripeCheckout : OnlinePayments sur le client Stripe de l'API.
type StripeCheckout struct {
	Manager *stripeclient.StripeManager
}

func (s StripeCheckout) CreateCustomerAccountCheckout(p CheckoutParams) (string, error) {
	return s.Manager.CreateCustomerAccountCheckout(stripeclient.CustomerAccountCheckout{
		StripeAccountID: p.StripeAccountID, Amount: p.Amount, ApplicationFee: p.ApplicationFee,
		Description: p.Description, SuccessURL: p.SuccessURL, CancelURL: p.CancelURL, Metadata: p.Metadata,
	})
}
