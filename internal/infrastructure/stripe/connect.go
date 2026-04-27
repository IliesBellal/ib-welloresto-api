package stripeclient

import (
	"strings"

	"github.com/stripe/stripe-go/v84"
)

// BankAccountInfo represents a bank account linked to a Stripe Connect account.
type BankAccountInfo struct {
	ID                string `json:"id"`
	BankName          string `json:"bank_name"`
	Last4             string `json:"last4"`
	Currency          string `json:"currency"`
	Status            string `json:"status"` // "verified" | "pending" | "errored"
	AccountHolderName string `json:"account_holder_name,omitempty"`
}

// BalanceAmount holds a single currency entry inside a Stripe Balance.
type BalanceAmount struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// BalanceInfo holds available and pending balance slices.
type BalanceInfo struct {
	Available []BalanceAmount `json:"available"`
	Pending   []BalanceAmount `json:"pending"`
}

// GetConnectAccountStatus retrieves the live status for a Stripe Connect account.
// Returns "verified" when both charges and payouts are enabled, "action_required" otherwise.
func (s *StripeManager) GetConnectAccountStatus(accountID string) (string, error) {
	acc, err := s.client.Accounts.GetByID(accountID, nil)
	if err != nil {
		return "", err
	}
	return connectAccountStatus(acc), nil
}

// CreateOnboardingLink generates a Stripe AccountLink (type: account_onboarding) for the connected account.
func (s *StripeManager) CreateOnboardingLink(accountID, returnURL, refreshURL string) (string, error) {
	params := &stripe.AccountLinkParams{
		Account:    stripe.String(accountID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String("account_update"),
	}
	link, err := s.client.AccountLinks.New(params)
	if err != nil {
		return "", err
	}
	return link.URL, nil
}

// GetBankAccounts lists all external bank accounts for a Stripe Connect account.
func (s *StripeManager) GetBankAccounts(accountID string) ([]BankAccountInfo, error) {
	params := &stripe.BankAccountListParams{
		Account: stripe.String(accountID),
	}
	iter := s.client.BankAccounts.List(params)

	var accounts []BankAccountInfo
	for iter.Next() {
		ba := iter.BankAccount()

		status := "pending"
		switch ba.Status {
		case stripe.BankAccountStatusVerified:
			status = "verified"
		case stripe.BankAccountStatusErrored, stripe.BankAccountStatusVerificationFailed:
			status = "errored"
		}

		accounts = append(accounts, BankAccountInfo{
			ID:                ba.ID,
			BankName:          ba.BankName,
			Last4:             ba.Last4,
			Currency:          strings.ToUpper(string(ba.Currency)),
			Status:            status,
			AccountHolderName: ba.AccountHolderName,
		})
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

// CreateBankAccountLink generates an AccountLink (type: account_update) to allow the merchant
// to configure their IBAN/bank account on the Stripe Connect dashboard.
func (s *StripeManager) CreateBankAccountLink(accountID, returnURL, refreshURL string) (string, error) {
	params := &stripe.AccountLinkParams{
		Account:    stripe.String(accountID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String("account_onboarding"),
		CollectionOptions: &stripe.AccountLinkCollectionOptionsParams{
			Fields:             stripe.String("eventually_due"),
			FutureRequirements: stripe.String("include"),
		},
	}
	link, err := s.client.AccountLinks.New(params)
	if err != nil {
		return "", err
	}
	return link.URL, nil
}

// GetConnectBalance returns the balance for a Stripe Connect account.
// Amounts are in the smallest currency unit (centimes for EUR).
func (s *StripeManager) GetConnectBalance(accountID string) (*BalanceInfo, error) {
	params := &stripe.BalanceParams{}
	params.SetStripeAccount(accountID)

	bal, err := s.client.Balance.Get(params)
	if err != nil {
		return nil, err
	}

	info := &BalanceInfo{}
	for _, a := range bal.Available {
		info.Available = append(info.Available, BalanceAmount{
			Amount:   a.Amount,
			Currency: strings.ToUpper(string(a.Currency)),
		})
	}
	for _, p := range bal.Pending {
		info.Pending = append(info.Pending, BalanceAmount{
			Amount:   p.Amount,
			Currency: strings.ToUpper(string(p.Currency)),
		})
	}
	return info, nil
}

// connectAccountStatus maps a live stripe.Account to our status string.
// Exported so the webhook can reuse it without an extra API call.
func connectAccountStatus(acc *stripe.Account) string {
	// 1. Si les paiements ou les virements sont déjà désactivés
	if !acc.ChargesEnabled || !acc.PayoutsEnabled {
		return "action_required"
	}

	// 2. Détection du "Prochainement limité" (Soon to be restricted)
	// On vérifie si Stripe a des exigences en attente (CurrentlyDue)
	// ou en retard (PastDue).
	if len(acc.Requirements.CurrentlyDue) > 0 || len(acc.Requirements.PastDue) > 0 {
		return "action_required"
	}

	// 3. Optionnel : vérifier si un motif de désactivation est déjà renseigné
	if acc.Requirements.DisabledReason != "" {
		return "action_required"
	}

	return "verified"
}

// ConnectAccountStatusFromAccount is the public facade over connectAccountStatus,
// called by the webhook service when processing an account.updated event.
func ConnectAccountStatusFromAccount(acc *stripe.Account) string {
	return connectAccountStatus(acc)
}
