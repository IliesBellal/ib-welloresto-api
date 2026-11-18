package customer_accounts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/customeraccount"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

// Lien de paiement en ligne d'une ardoise (établissements avec un compte
// Stripe). Le lien porte un jeton public, sans montant : la somme due est
// relue à chaque clic, une session Stripe Checkout est créée sur le compte
// Connect de l'établissement, et le webhook checkout.session.completed
// enregistre un versement (moyen STRIPE, sans registre de caisse).

// paymentLinkTTL : durée de validité d'un lien (un lien encore valable une
// semaine est réutilisé).
const paymentLinkTTL = 60 * 24 * time.Hour

// OnlinePaymentChannel : métadonnée « channel » des sessions et paiements
// Stripe d'un compte client (le webhook les reconnaît ainsi).
const OnlinePaymentChannel = "customer_account"

func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) linkURL(token string) string {
	return strings.TrimRight(s.deps.PublicBaseURL, "/") + "/pay/account/" + token
}

// paymentURL renvoie le lien de paiement d'un client (créé au besoin).
func (s *Service) paymentURL(ctx context.Context, merchantID string, customerID int64) (string, error) {
	if s.deps.Payments == nil || strings.TrimSpace(s.deps.PublicBaseURL) == "" {
		return "", models.ErrAccountOnlinePaymentUnavailable
	}
	if _, ok, err := s.repo.Stripe(ctx, merchantID); err != nil {
		return "", err
	} else if !ok {
		return "", models.ErrAccountOnlinePaymentUnavailable
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	token, err = s.repo.PaymentLinkToken(ctx, merchantID, customerID, token, paymentLinkTTL)
	if err != nil {
		return "", err
	}
	return s.linkURL(token), nil
}

// paymentURLIfAvailable : lien de paiement pour un email, vide si
// l'établissement n'a pas de compte Stripe.
func (s *Service) paymentURLIfAvailable(ctx context.Context, merchantID string, customerID int64) string {
	url, err := s.paymentURL(ctx, merchantID, customerID)
	if err != nil {
		return ""
	}
	return url
}

// PaymentLink renvoie le lien de paiement en ligne d'un client endetté.
func (s *Service) PaymentLink(ctx context.Context, customerID int64) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", models.ErrUnauthorized
	}
	if err := s.requireCustomer(ctx, user.MerchantID, customerID); err != nil {
		return "", err
	}
	balance, err := customeraccount.Balance(ctx, s.repo.DB(ctx), user.MerchantID, customerID)
	if err != nil {
		return "", err
	}
	if balance >= 0 {
		return "", models.ErrAccountNothingDue
	}
	return s.paymentURL(ctx, user.MerchantID, customerID)
}

// applicationFee : frais ScanNOrder de l'établissement sur un paiement en
// ligne (jamais plus que le montant).
func applicationFee(amount int64, variable float64, fixed int64) int64 {
	if variable <= 0 && fixed <= 0 {
		return 0
	}
	fee := int64(math.Floor(float64(amount)*variable + float64(fixed) + 0.5))
	if fee < 0 {
		return 0
	}
	if fee >= amount {
		return amount - 1
	}
	return fee
}

// StartOnlinePayment (public) : relit la dette du client du lien et renvoie
// l'adresse de la page de paiement Stripe.
func (s *Service) StartOnlinePayment(ctx context.Context, token string) (string, error) {
	if s.deps.Payments == nil {
		return "", models.ErrAccountOnlinePaymentUnavailable
	}
	merchantID, customerID, err := s.repo.ResolvePaymentLink(ctx, strings.TrimSpace(token))
	if err != nil {
		return "", err
	}
	db := s.repo.DB(ctx)
	balance, err := customeraccount.Balance(ctx, db, merchantID, customerID)
	if err != nil {
		return "", err
	}
	if balance >= 0 {
		return "", models.ErrAccountNothingDue
	}
	account, ok, err := s.repo.Stripe(ctx, merchantID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", models.ErrAccountOnlinePaymentUnavailable
	}
	m, err := s.repo.Merchant(ctx, merchantID)
	if err != nil {
		return "", err
	}
	amount := -balance
	done := s.linkURL(token) + "/done"
	return s.deps.Payments.CreateCustomerAccountCheckout(CheckoutParams{
		StripeAccountID: account.AccountID,
		Amount:          amount,
		ApplicationFee:  applicationFee(amount, account.VariableFees, account.FixedFees),
		Description:     "Règlement de votre compte · " + m.Name,
		SuccessURL:      done + "?status=success",
		CancelURL:       done + "?status=cancel",
		Metadata: map[string]string{
			"channel":     OnlinePaymentChannel,
			"merchant_id": merchantID,
			"customer_id": strconv.FormatInt(customerID, 10),
		},
	})
}

// OnlinePaymentMerchantName (public) : nom de l'établissement d'un lien, pour
// la page de retour.
func (s *Service) OnlinePaymentMerchantName(ctx context.Context, token string) string {
	merchantID, _, err := s.repo.ResolvePaymentLink(ctx, strings.TrimSpace(token))
	if err != nil {
		return ""
	}
	m, err := s.repo.Merchant(ctx, merchantID)
	if err != nil {
		return ""
	}
	return m.Name
}

// HandleOnlinePaymentCompleted enregistre le versement d'une session Stripe
// payée (webhook). Rejouable : une session n'est comptée qu'une fois.
func (s *Service) HandleOnlinePaymentCompleted(ctx context.Context, sessionID, paymentIntentID string, metadata map[string]string, amount int64, paid bool) error {
	if !paid || amount <= 0 || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	merchantID := strings.TrimSpace(metadata["merchant_id"])
	customerID, err := strconv.ParseInt(strings.TrimSpace(metadata["customer_id"]), 10, 64)
	if merchantID == "" || err != nil || customerID <= 0 {
		return fmt.Errorf("customer_accounts: online payment %s: missing metadata", sessionID)
	}
	return dbutils.RunInTx(ctx, s.database, func(txCtx context.Context) error {
		claimed, err := s.repo.ClaimOnlinePayment(txCtx, sessionID, paymentIntentID, merchantID, customerID, amount)
		if err != nil || !claimed {
			return err
		}
		mop := models.AccountOnlineMOP
		comment := "Paiement en ligne (réf. " + sessionID + ")"
		if len(comment) > 250 {
			comment = comment[:250]
		}
		op := customeraccount.Operation{
			MerchantID: merchantID, CustomerID: customerID, Type: models.AccountOperationDeposit, Amount: amount,
			MOP: &mop, Comment: &comment, UserID: models.StripeWebhookUserID,
		}
		if err := customeraccount.ValidateOperation(op); err != nil {
			return err
		}
		if err := customeraccount.Lock(txCtx, merchantID, customerID); err != nil {
			return err
		}
		id, _, err := customeraccount.Insert(txCtx, s.repo.DB(txCtx), op)
		if err != nil {
			return err
		}
		logger.FromContext(txCtx).Info("customer account: online payment recorded",
			zap.String("merchant_id", merchantID), zap.Int64("customer_id", customerID),
			zap.Int64("amount", amount), zap.Int64("operation_id", id))
		return s.repo.SetOnlinePaymentOperation(txCtx, sessionID, id)
	})
}
