package stripeclient

import (
	"context"
	"fmt"
	"time"

	"github.com/stripe/stripe-go/v84"
)

// PayoutInfo est la vue d'un payout (virement vers la banque) d'un compte
// connecté, réduite à ce dont les justificatifs de versement ont besoin.
type PayoutInfo struct {
	ID          string
	AccountID   string
	Amount      int64 // centimes
	Currency    string
	Status      string
	ArrivalDate time.Time
}

// PayoutTransaction est une ligne de balance incluse dans un payout. Les frais
// sont déjà séparés : sur un compte connecté, la commission de la plateforme
// (application_fee_amount) et les frais Stripe sont deux entrées du même
// fee_details de la ligne de paiement.
type PayoutTransaction struct {
	ID                string
	Type              string // charge, refund, adjustment, ...
	ReportingCategory string // charge, refund, dispute, ...
	Amount            int64  // brut, signé
	CommissionFee     int64  // part de Fee en application_fee (commission Wello Resto, TTC)
	StripeFee         int64  // le reste de Fee (frais de traitement Stripe)
	Net               int64  // Amount - CommissionFee - StripeFee
	Created           time.Time
	PaymentIntentID   string // vide si la ligne ne se rattache à aucun paiement
}

// GetPayout lit un payout d'un compte connecté.
func (s *StripeManager) GetPayout(ctx context.Context, accountID, payoutID string) (*PayoutInfo, error) {
	params := &stripe.PayoutParams{}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	p, err := s.client.Payouts.Get(payoutID, params)
	if err != nil {
		return nil, fmt.Errorf("stripe: get payout %s: %w", payoutID, err)
	}
	return payoutInfoFrom(accountID, p), nil
}

// ListPaidPayouts liste les payouts payés d'un compte connecté créés depuis
// `since` (rattrapage des justificatifs).
func (s *StripeManager) ListPaidPayouts(ctx context.Context, accountID string, since time.Time) ([]PayoutInfo, error) {
	params := &stripe.PayoutListParams{
		Status:       stripe.String("paid"),
		CreatedRange: &stripe.RangeQueryParams{GreaterThanOrEqual: since.Unix()},
	}
	params.Context = ctx
	params.Limit = stripe.Int64(100)
	params.SetStripeAccount(accountID)

	var out []PayoutInfo
	iter := s.client.Payouts.List(params)
	for iter.Next() {
		out = append(out, *payoutInfoFrom(accountID, iter.Payout()))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("stripe: list payouts of %s: %w", accountID, err)
	}
	return out, nil
}

// ListPayoutTransactions liste toutes les lignes de balance d'un payout, avec
// la source dépliée pour remonter au PaymentIntent. Ne marche que pour les
// payouts automatiques (c'est le cas des comptes Express de la plateforme).
func (s *StripeManager) ListPayoutTransactions(ctx context.Context, accountID, payoutID string) ([]PayoutTransaction, error) {
	params := &stripe.BalanceTransactionListParams{Payout: stripe.String(payoutID)}
	params.Context = ctx
	params.Limit = stripe.Int64(100)
	params.AddExpand("data.source")
	params.SetStripeAccount(accountID)

	var out []PayoutTransaction
	iter := s.client.BalanceTransactions.List(params)
	for iter.Next() {
		out = append(out, payoutTransactionFrom(iter.BalanceTransaction()))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("stripe: list balance transactions of payout %s: %w", payoutID, err)
	}
	return out, nil
}

func payoutInfoFrom(accountID string, p *stripe.Payout) *PayoutInfo {
	return &PayoutInfo{
		ID:          p.ID,
		AccountID:   accountID,
		Amount:      p.Amount,
		Currency:    string(p.Currency),
		Status:      string(p.Status),
		ArrivalDate: time.Unix(p.ArrivalDate, 0),
	}
}

func payoutTransactionFrom(bt *stripe.BalanceTransaction) PayoutTransaction {
	var commission int64
	for _, detail := range bt.FeeDetails {
		if detail != nil && detail.Type == "application_fee" {
			commission += detail.Amount
		}
	}

	return PayoutTransaction{
		ID:                bt.ID,
		Type:              string(bt.Type),
		ReportingCategory: string(bt.ReportingCategory),
		Amount:            bt.Amount,
		CommissionFee:     commission,
		StripeFee:         bt.Fee - commission,
		Net:               bt.Net,
		Created:           time.Unix(bt.Created, 0),
		PaymentIntentID:   paymentIntentOf(bt.Source),
	}
}

// paymentIntentOf remonte de la source d'une ligne de balance (paiement ou
// remboursement) à son PaymentIntent ; vide pour les autres sources.
func paymentIntentOf(src *stripe.BalanceTransactionSource) string {
	if src == nil {
		return ""
	}
	switch {
	case src.Charge != nil && src.Charge.PaymentIntent != nil:
		return src.Charge.PaymentIntent.ID
	case src.Refund != nil && src.Refund.PaymentIntent != nil:
		return src.Refund.PaymentIntent.ID
	}
	return ""
}
