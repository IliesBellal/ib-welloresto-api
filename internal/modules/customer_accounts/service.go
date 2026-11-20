// Package customer_accounts expose le compte client (docs/customer-accounts.md) :
// consultation, versement, remboursement du solde, abandon de créance,
// réglages (plafond d'ardoise, espèces, relances), balance âgée, totaux
// mensuels, relevé, relances et lien de paiement en ligne.
package customer_accounts

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/customeraccount"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// maxCreditLimit : plafond d'ardoise maximal réglable (100 000 €), garde-fou
// contre une faute de frappe.
const maxCreditLimit = 10_000_000

// maxOperationAmount : montant maximal d'une opération (100 000 €).
const maxOperationAmount = 10_000_000

// ActiveRegisterFunc renvoie le registre ouvert d'un appareil (celui de
// order_life_cycle : appareil, ou appareil principal auquel il est lié).
type ActiveRegisterFunc func(ctx context.Context, merchantID, deviceID string) (string, error)

// Deps : services externes du module (envoi d'emails et de SMS, Stripe,
// identité visuelle des emails). Chacun peut manquer (tests, configuration
// incomplète) : la fonction qui en dépend renvoie alors une erreur.
type Deps struct {
	Mailer   Mailer
	SMS      SMSSender
	Brand    BrandResolver
	Payments OnlinePayments
	// PublicBaseURL : adresse publique de l'API, base des liens de paiement.
	PublicBaseURL string
	// Now : horloge (tests). Nil : time.Now.
	Now func() time.Time
}

type Service struct {
	database       *sql.DB
	repo           *Repository
	activeRegister ActiveRegisterFunc
	deps           Deps
}

func NewService(database *sql.DB, repo *Repository, activeRegister ActiveRegisterFunc, deps ...Deps) *Service {
	s := &Service{database: database, repo: repo, activeRegister: activeRegister}
	if len(deps) > 0 {
		s.deps = deps[0]
	}
	if s.deps.Now == nil {
		s.deps.Now = time.Now
	}
	return s
}

// OperationResult : le mouvement écrit et l'état du compte après lui.
type OperationResult struct {
	Operation models.CustomerAccountOperation `json:"operation"`
	Account   *models.CustomerAccount         `json:"account"`
}

// GetAccount renvoie le solde, le plafond et l'historique du compte d'un client.
func (s *Service) GetAccount(ctx context.Context, customerID int64) (*models.CustomerAccount, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if err := s.requireCustomer(ctx, user.MerchantID, customerID); err != nil {
		return nil, err
	}
	return s.account(ctx, user.MerchantID, customerID)
}

func (s *Service) account(ctx context.Context, merchantID string, customerID int64) (*models.CustomerAccount, error) {
	db := s.repo.DB(ctx)
	balance, err := customeraccount.Balance(ctx, db, merchantID, customerID)
	if err != nil {
		return nil, err
	}
	settings, err := customeraccount.LoadMerchantSettings(ctx, db, merchantID)
	if err != nil {
		return nil, err
	}
	cs, err := customeraccount.LoadCustomerSettings(ctx, db, merchantID, customerID)
	if err != nil {
		return nil, err
	}
	limit := customeraccount.EffectiveCreditLimit(settings.CreditLimit, cs)
	entries, err := s.repo.Entries(ctx, merchantID, customerID)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []models.CustomerAccountEntry{}
	}
	reminders, err := s.repo.Reminders(ctx, merchantID, customerID)
	if err != nil {
		return nil, err
	}
	return &models.CustomerAccount{
		CustomerID: customerID, Balance: balance, CreditLimit: limit,
		Available: customeraccount.Available(balance, limit), Entries: entries,
		MerchantCreditLimit: settings.CreditLimit, CustomerSettings: cs, CashMode: settings.CashMode,
		OnlinePaymentAvailable: settings.OnlinePaymentAvailable, Reminders: reminders,
	}, nil
}

// ListAccounts liste les comptes non soldés de l'établissement.
func (s *Service) ListAccounts(ctx context.Context) ([]models.CustomerAccountSummary, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.NonZeroAccounts(ctx, user.MerchantID)
}

// Deposit enregistre un versement (avance, ou règlement de l'ardoise).
func (s *Service) Deposit(ctx context.Context, customerID int64, req models.CustomerAccountOperationRequest) (*OperationResult, error) {
	return s.apply(ctx, customerID, req, models.AccountOperationDeposit)
}

// Refund rend au client tout ou partie de son solde créditeur.
func (s *Service) Refund(ctx context.Context, customerID int64, req models.CustomerAccountOperationRequest) (*OperationResult, error) {
	return s.apply(ctx, customerID, req, models.AccountOperationRefund)
}

// WriteOff abandonne tout ou partie de l'ardoise d'un client. Réservé aux
// administrateurs (rôle administrateur de l'établissement) : renoncer à une
// créance est une décision de gestion, pas un geste de caisse. Le droit
// pos.refund de la route ne suffit pas.
func (s *Service) WriteOff(ctx context.Context, customerID int64, req models.CustomerAccountOperationRequest) (*OperationResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if !user.HasAdminRole() {
		return nil, models.ErrForbidden
	}
	return s.apply(ctx, customerID, req, models.AccountOperationWriteOff)
}

// buildOperation traduit une requête en mouvement (signe, moyen, motif), sans
// accès à la base.
func buildOperation(merchantID, userID string, customerID int64, req models.CustomerAccountOperationRequest, opType string) (customeraccount.Operation, error) {
	if req.Amount <= 0 || req.Amount > maxOperationAmount {
		return customeraccount.Operation{}, models.ErrAccountInvalidAmount
	}
	op := customeraccount.Operation{
		MerchantID: merchantID, CustomerID: customerID, Type: opType, Amount: req.Amount, UserID: userID,
	}
	if opType == models.AccountOperationRefund {
		op.Amount = -req.Amount
	}
	if opType != models.AccountOperationWriteOff {
		mop := strings.ToUpper(strings.TrimSpace(req.MOP))
		// Depuis la caisse : argent remis en main propre seulement (le paiement
		// en ligne passe par le lien de paiement).
		if !models.IsAccountMoneyMOP(mop) {
			return customeraccount.Operation{}, models.ErrAccountInvalidMOP
		}
		op.MOP = &mop
	}
	if c := strings.TrimSpace(req.Comment); c != "" {
		if len(c) > 250 {
			c = c[:250]
		}
		op.Comment = &c
	}
	return op, customeraccount.ValidateOperation(op)
}

func (s *Service) apply(ctx context.Context, customerID int64, req models.CustomerAccountOperationRequest, opType string) (*OperationResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	op, err := buildOperation(user.MerchantID, user.UserID, customerID, req, opType)
	if err != nil {
		return nil, err
	}
	if err := s.requireCustomer(ctx, user.MerchantID, customerID); err != nil {
		return nil, err
	}
	settings, err := customeraccount.LoadMerchantSettings(ctx, s.repo.DB(ctx), user.MerchantID)
	if err != nil {
		return nil, err
	}
	if err := customeraccount.CheckCashDeposit(&op, settings.CashMode, req.NonResidentConfirmed); err != nil {
		return nil, err
	}

	// L'argent passe par le registre ouvert de l'appareil (il apparaît dans
	// son Z) ; un abandon de créance n'en a pas besoin.
	if op.MOP != nil {
		registerID, err := s.activeRegister(ctx, user.MerchantID, req.DeviceID)
		if err != nil {
			return nil, err
		}
		op.CashRegisterID = &registerID
	}

	var result *OperationResult
	err = dbutils.RunInTx(ctx, s.database, func(txCtx context.Context) error {
		db := s.repo.DB(txCtx)
		if op.CashRegisterID != nil {
			open, err := s.repo.RegisterIsOpen(txCtx, *op.CashRegisterID)
			if err != nil {
				return err
			}
			if !open {
				return models.ErrNoCashRegisterOpen
			}
		}
		if err := customeraccount.Lock(txCtx, op.MerchantID, customerID); err != nil {
			return err
		}
		balance, err := customeraccount.Balance(txCtx, db, op.MerchantID, customerID)
		if err != nil {
			return err
		}
		if err := checkBalance(opType, op.Amount, balance); err != nil {
			return err
		}
		id, at, err := customeraccount.Insert(txCtx, db, op)
		if err != nil {
			return err
		}
		result = &OperationResult{Operation: models.CustomerAccountOperation{
			OperationID: id, CustomerID: customerID, OperationType: op.Type, Amount: op.Amount, MOP: op.MOP,
			Comment: op.Comment, UserID: op.UserID, CashRegisterID: op.CashRegisterID, OperationDate: at.Unix(),
		}}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Account, err = s.account(ctx, user.MerchantID, customerID); err != nil {
		return nil, err
	}
	return result, nil
}

// checkBalance : un remboursement ne dépasse pas le solde créditeur, un
// abandon ne dépasse pas la dette. Un versement est toujours possible.
func checkBalance(opType string, amount, balance int64) error {
	switch opType {
	case models.AccountOperationRefund:
		if -amount > balance {
			return models.ErrAccountRefundExceedsBalance
		}
	case models.AccountOperationWriteOff:
		if amount > -balance {
			return models.ErrAccountWriteOffExceedsDebt
		}
	}
	return nil
}

func (s *Service) requireCustomer(ctx context.Context, merchantID string, customerID int64) error {
	ok, err := s.repo.CustomerBelongsToMerchant(ctx, merchantID, customerID)
	if err != nil {
		return err
	}
	if !ok {
		return models.ErrNotFound
	}
	return nil
}

// GetSettings renvoie les réglages du compte client de l'établissement.
func (s *Service) GetSettings(ctx context.Context) (*models.CustomerAccountSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	settings, err := customeraccount.LoadMerchantSettings(ctx, s.repo.DB(ctx), user.MerchantID)
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// UpdateSettings écrit les réglages (plafond d'ardoise, espèces, relances).
// Les ardoises existantes au-delà d'un plafond baissé restent dues ; seul un
// nouveau débit est refusé.
func (s *Service) UpdateSettings(ctx context.Context, req models.CustomerAccountSettings) (*models.CustomerAccountSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	req.CashMode = strings.ToUpper(strings.TrimSpace(req.CashMode))
	if req.CashMode == "" {
		req.CashMode = models.AccountCashModeBlock
	}
	if req.ReminderDays == 0 {
		req.ReminderDays = 30
	}
	if req.ReminderMax == 0 {
		req.ReminderMax = 3
	}
	if err := customeraccount.ValidateMerchantSettings(req, maxCreditLimit); err != nil {
		return nil, err
	}
	db := s.repo.DB(ctx)
	if err := customeraccount.SaveMerchantSettings(ctx, db, user.MerchantID, req); err != nil {
		return nil, err
	}
	saved, err := customeraccount.LoadMerchantSettings(ctx, db, user.MerchantID)
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// UpdateCustomerSettings écrit les réglages propres à un client (plafond
// propre, ardoise interdite) et renvoie son compte.
func (s *Service) UpdateCustomerSettings(ctx context.Context, customerID int64, req models.CustomerAccountCustomerSettings) (*models.CustomerAccount, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if err := s.requireCustomer(ctx, user.MerchantID, customerID); err != nil {
		return nil, err
	}
	if req.CreditLimit != nil && (*req.CreditLimit < 0 || *req.CreditLimit > maxCreditLimit) {
		return nil, models.ErrAccountInvalidAmount
	}
	if err := customeraccount.SaveCustomerSettings(ctx, s.repo.DB(ctx), user.MerchantID, customerID, req, user.UserID); err != nil {
		return nil, err
	}
	return s.account(ctx, user.MerchantID, customerID)
}

// Aging renvoie la balance âgée des ardoises, plus anciennes d'abord.
func (s *Service) Aging(ctx context.Context) ([]models.CustomerAccountAging, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	ledger, err := customeraccount.LoadLedger(ctx, s.repo.DB(ctx), user.MerchantID, 0)
	if err != nil {
		return nil, err
	}
	aging := customeraccount.AgeDebts(ledger, s.deps.Now())
	if len(aging) == 0 {
		return []models.CustomerAccountAging{}, nil
	}
	names, err := s.repo.CustomerSummaries(ctx, user.MerchantID, agingIDs(aging))
	if err != nil {
		return nil, err
	}
	for i := range aging {
		if n, ok := names[aging[i].CustomerSummary.CustomerID]; ok {
			n.Balance = -aging[i].Debt
			aging[i].CustomerSummary = n
		}
	}
	return aging, nil
}

func agingIDs(aging []models.CustomerAccountAging) []int64 {
	ids := make([]int64, len(aging))
	for i, a := range aging {
		ids[i] = a.CustomerSummary.CustomerID
	}
	return ids
}

// Monthly renvoie les mouvements et soldes de fin de mois d'une année.
func (s *Service) Monthly(ctx context.Context, year int) ([]models.CustomerAccountMonth, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	db := s.repo.DB(ctx)
	ledger, err := customeraccount.LoadLedger(ctx, db, user.MerchantID, 0)
	if err != nil {
		return nil, err
	}
	loc, err := s.repo.MerchantLocation(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if year < 2000 || year > 2100 {
		year = s.deps.Now().In(loc).Year()
	}
	return customeraccount.MonthlyTotals(ledger, year, loc), nil
}
