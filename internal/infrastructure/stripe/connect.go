package stripeclient

import (
	"context"
	"io"
	"strings"
	"unicode"

	"github.com/stripe/stripe-go/v84"
	"golang.org/x/text/unicode/norm"
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
		Type:       stripe.String("account_onboarding"),
	}
	link, err := s.client.AccountLinks.New(params)
	if err != nil {
		return "", err
	}
	return link.URL, nil
}

// mccRestaurants est le code d'activité Stripe "Eating Places, Restaurants".
const mccRestaurants = "5812"

// ConnectAccountProfile regroupe les informations de l'établissement
// pré-remplies sur son compte Stripe Connect : le nom affiché du compte (dans
// le dashboard Connect, sur Checkout et sur les relevés bancaires des clients)
// est celui de l'établissement, pas la raison sociale saisie à l'onboarding.
type ConnectAccountProfile struct {
	MerchantID   string
	BusinessName string // nom de l'établissement (merchant.fullName)
	Email        string // utilisé seulement à la création : c'est l'identifiant du dashboard Express
	URL          string // boutique ScanNOrder publique, "" si inconnue
}

func (p ConnectAccountProfile) businessProfileParams() *stripe.AccountBusinessProfileParams {
	bp := &stripe.AccountBusinessProfileParams{MCC: stripe.String(mccRestaurants)}
	if name := strings.TrimSpace(p.BusinessName); name != "" {
		bp.Name = stripe.String(name)
	}
	if p.URL != "" {
		bp.URL = stripe.String(p.URL)
	}
	return bp
}

// settingsParams renvoie nil quand le nom ne donne pas de libellé de relevé
// valide : Stripe le demandera alors au marchand pendant l'onboarding.
func (p ConnectAccountProfile) settingsParams() *stripe.AccountSettingsParams {
	descriptor := StatementDescriptor(p.BusinessName)
	if descriptor == "" {
		return nil
	}
	return &stripe.AccountSettingsParams{
		Payments: &stripe.AccountSettingsPaymentsParams{StatementDescriptor: stripe.String(descriptor)},
	}
}

// CreateExpressAccount creates a Stripe Connect Express account with required capabilities,
// pre-filled with the establishment's profile.
//
// Le type d'entreprise n'est pas imposé : le marchand le choisit à
// l'onboarding (société, ou entreprise individuelle pour un auto-entrepreneur).
func (s *StripeManager) CreateExpressAccount(ctx context.Context, profile ConnectAccountProfile) (string, error) {
	params := &stripe.AccountParams{
		Type:    stripe.String(string(stripe.AccountTypeExpress)),
		Country: stripe.String("FR"),
		Capabilities: &stripe.AccountCapabilitiesParams{
			CardPayments: &stripe.AccountCapabilitiesCardPaymentsParams{Requested: stripe.Bool(true)},
			Transfers:    &stripe.AccountCapabilitiesTransfersParams{Requested: stripe.Bool(true)},
		},
		BusinessProfile: profile.businessProfileParams(),
		Settings:        profile.settingsParams(),
	}
	if email := strings.TrimSpace(profile.Email); email != "" {
		params.Email = stripe.String(email)
	}
	params.AddMetadata("merchant_id", profile.MerchantID)
	params.Context = ctx

	acc, err := s.client.Accounts.New(params)
	if err != nil {
		return "", err
	}

	return acc.ID, nil
}

// UpdateAccountProfile réaligne le nom, l'activité, l'URL et le libellé de
// relevé d'un compte existant sur l'établissement (comptes créés avant le
// pré-remplissage, ou nom modifié par le marchand). L'email n'est pas touché.
func (s *StripeManager) UpdateAccountProfile(ctx context.Context, accountID string, profile ConnectAccountProfile) error {
	params := &stripe.AccountParams{
		BusinessProfile: profile.businessProfileParams(),
		Settings:        profile.settingsParams(),
	}
	params.Context = ctx

	_, err := s.client.Accounts.Update(accountID, params)
	return err
}

// statementDescriptorReplacer replie les ligatures que la décomposition
// Unicode ne traite pas.
var statementDescriptorReplacer = strings.NewReplacer("œ", "oe", "Œ", "OE", "æ", "ae", "Æ", "AE", "ß", "ss")

// StatementDescriptor dérive du nom de l'établissement un libellé de relevé
// bancaire conforme à Stripe : 5 à 22 caractères latins, au moins une lettre,
// sans < > \ ' " *. Accents retirés, majuscules, ponctuation réduite à
// l'espace, au point et au tiret. Renvoie "" si le résultat n'est pas valide.
func StatementDescriptor(name string) string {
	decomposed := norm.NFD.String(statementDescriptorReplacer.Replace(name))

	var b strings.Builder
	pendingSpace := false
	hasLetter := false
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue // accent détaché par NFD
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-'):
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			hasLetter = hasLetter || unicode.IsLetter(r)
			b.WriteRune(unicode.ToUpper(r))
		default:
			pendingSpace = true
		}
	}

	descriptor := b.String()
	if len(descriptor) > 22 {
		descriptor = strings.TrimSpace(descriptor[:22])
	}
	if len(descriptor) < 5 || !hasLetter {
		return ""
	}
	return descriptor
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

// CreateBankAccountLink returns the URL where the merchant manages their IBAN/bank account.
//
// Un AccountLink account_update n'est accepté que si la plateforme collecte
// elle-même les informations (requirement_collection = application, comptes
// Custom). Les comptes Express créés par CreateExpressAccount sont collectés
// par Stripe : le compte bancaire s'y gère depuis le dashboard Express (login
// link), qui n'existe qu'une fois l'onboarding soumis — avant, c'est
// l'onboarding lui-même qui demande l'IBAN.
func (s *StripeManager) CreateBankAccountLink(accountID, returnURL, refreshURL string) (string, error) {
	acc, err := s.client.Accounts.GetByID(accountID, nil)
	if err != nil {
		return "", err
	}

	if acc.Controller == nil || acc.Controller.RequirementCollection != stripe.AccountControllerRequirementCollectionApplication {
		if !acc.DetailsSubmitted {
			return s.CreateOnboardingLink(accountID, returnURL, refreshURL)
		}
		login, err := s.client.LoginLinks.New(&stripe.LoginLinkParams{Account: stripe.String(accountID)})
		if err != nil {
			return "", err
		}
		return login.URL, nil
	}

	params := &stripe.AccountLinkParams{
		Account:    stripe.String(accountID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String("account_update"),
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

// UploadBrandingFile uploads an image to Stripe Files with purpose business_logo.
// The caller is responsible for closing the reader.
// Returns the Stripe File ID to be used in UpdateAccountBranding.
func (s *StripeManager) UploadBrandingFile(ctx context.Context, reader io.Reader, filename string) (string, error) {
	params := &stripe.FileParams{
		FileReader: reader,
		Filename:   stripe.String(filename),
		Purpose:    stripe.String(string(stripe.FilePurposeBusinessLogo)),
	}
	params.Context = ctx

	f, err := s.client.Files.New(params)
	if err != nil {
		return "", err
	}
	return f.ID, nil
}

// UpdateAccountBranding updates the branding settings for a Stripe connected account.
// logoFileID must be a Stripe File ID returned by UploadBrandingFile.
// primaryColor must be a hex string (e.g. "#FF5733"); pass "" to leave the color unchanged.
func (s *StripeManager) UpdateAccountBranding(ctx context.Context, accountID, logoFileID, primaryColor string) error {
	branding := &stripe.AccountSettingsBrandingParams{
		Logo: stripe.String(logoFileID),
		Icon: stripe.String(logoFileID),
	}
	if primaryColor != "" {
		branding.PrimaryColor = stripe.String(primaryColor)
	}

	params := &stripe.AccountParams{
		Settings: &stripe.AccountSettingsParams{
			Branding: branding,
		},
	}
	params.Context = ctx

	_, err := s.client.Accounts.Update(accountID, params)
	return err
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
