package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/order_life_cycle"

	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/balancetransaction"
)

type StripeWebhookService struct {
	repo           Repository
	stripeKey      string
	email          mailer.Service
	smsService     sms.Service
	orderlifecycle *order_life_cycle.OrdersLifeCycleService
	notification   *notification.NotificationService
	redis          *redis.Client
}

func NewStripeWebhookService(repo Repository, stripeKey string, email mailer.Service, smsService sms.Service, lifecycle *order_life_cycle.OrdersLifeCycleService, notification *notification.NotificationService, redis *redis.Client) *StripeWebhookService {
	stripe.Key = stripeKey
	return &StripeWebhookService{
		repo:           repo,
		stripeKey:      stripeKey,
		email:          email,
		smsService:     smsService,
		orderlifecycle: lifecycle,
		notification:   notification,
		redis:          redis,
	}
}

// ProcessEvent est le point d'entrée unique. Il dispatche vers les handlers spécifiques.
func (s *StripeWebhookService) ProcessEvent(ctx context.Context, event StripeEvent) error {
	switch event.Type {

	case "checkout.session.completed":
		return s.HandleCheckoutSessionCompleted(ctx, event.Data.Object)

	case "checkout.session.expired":
		return s.HandleCheckoutSessionCanceled(ctx, event.Data.Object)

	case "charge.refunded":
		return s.HandleRefund(ctx, event.Data.Object)

	case "charge.captured":
		// En PHP c'était retrieveFees. On gère les frais ici.
		return s.HandleRetrieveFees(ctx, event.Data.Object)

	case "payment_intent.canceled":
		return s.HandlePaymentIntentUpdated(ctx, event.Data.Object, "CANCELED")

	case "payment_intent.succeeded":
		return s.HandlePaymentIntentUpdated(ctx, event.Data.Object, "CAPTURED")

	case "payout.paid":
		return s.HandlePayoutPaid(ctx, event.Data.Object, event.Account)

	case "invoice.created":
		return s.HandleInvoiceCreated(ctx, event.Data.Object)

	case "invoice.paid":
		return s.HandleInvoicePaid(ctx, event.Data.Object)

	default:
		return nil
	}
}

// 1. HandleCheckoutSessionCompleted
func (s *StripeWebhookService) HandleCheckoutSessionCompleted(ctx context.Context, data json.RawMessage) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(data, &session); err != nil {
		return fmt.Errorf("unmarshal session: %w", err)
	}

	merchantID := session.Metadata["merchant_id"]
	orderID := session.Metadata["order_id"]

	key := helpers.GetRedisOrderKey(merchantID, orderID)
	s.redis.Delete(ctx, key)

	if merchantID == "" || orderID == "" {
		return errors.New("missing metadata in stripe session")
	}

	// On utilise dbutils.RunInTx pour ouvrir la transaction.
	// Toutes les fonctions à l'intérieur utiliseront 'txCtx'.
	piID := ""
	if session.PaymentIntent != nil {
		piID = session.PaymentIntent.ID
	}

	// A. Insertion Payment
	// Le repo de orderlifecycle utilisera GetDB(txCtx) en interne
	err := s.orderlifecycle.CreatePayment(ctx, models.Payment{
		CashRegisterID:    models.ScanNOrderCashRegisterID,
		MOP:               models.StripeMOP,
		Amount:            int(session.AmountTotal),
		OrderID:           orderID,
		MerchantID:        merchantID,
		UserID:            models.StripeWebhookUserID,
		OperationType:     models.OperationTypeSale,
		PaymentIntentID:   &piID,
		CheckoutSessionID: &session.ID,
		CustomerEmail:     &session.CustomerDetails.Email,
	})
	if err != nil {
		return fmt.Errorf("insert payment: %w", err)
	}

	// C. Update Order Status
	if err := s.repo.UpdateOrderPaymentStatus(ctx, orderID); err != nil {
		return fmt.Errorf("update order payment status: %w", err)
	}

	// D. Cas Spécial: App QR Code
	if session.Metadata["checkout_session_type"] == "app_qr_code" {
		if err := s.handleCustomerUpdate(ctx, &session, orderID, merchantID); err != nil {
			log.Printf("Warning: failed to update customer: %v", err)
		}
		// Async : On utilise context.Background() car la transaction va se fermer
		go s.notification.SendNotificationAsync(merchantID, orderID, "ORDER_UPDATE")
		return nil // Succès -> Commit
	}

	// E. Flow Standard
	if err := s.repo.UpdateOrderDetails(ctx, session.ID, orderID); err != nil {
		return fmt.Errorf("update order details: %w", err)
	}
	if err := s.repo.UpdateOrderItemsPaid(ctx, session.ID, orderID); err != nil {
		return fmt.Errorf("update items paid: %w", err)
	}

	// F. Auto Accept Logic
	orderType, merchantParams, err := s.repo.GetAutoAcceptSettings(ctx, orderID, merchantID)
	if err == nil {
		go s.notification.SendNotificationAsync(merchantID, orderID, "UPDATE_ORDER")

		shouldAccept := (merchantParams.AutoAcceptDelivery && orderType == "DELIVERY") ||
			(merchantParams.AutoAcceptTakeaway && orderType == "TAKE_AWAY")

		if shouldAccept {
			// IMPORTANT: context.Background() pour les appels asynchrones hors transaction
			go s.orderlifecycle.SetOrderAccepted(context.Background(), "SYSTEM", merchantID, orderID)
		}
	}

	// G. Update Customer
	if err := s.handleCustomerUpdate(ctx, &session, orderID, merchantID); err != nil {
		log.Printf("Warning: customer update failed: %v", err)
	}

	// Récupération des infos pour notifications
	order, _ := s.repo.GetOrder(ctx, orderID)
	if order != nil {
		merchant, err := s.repo.GetMerchant(ctx, merchantID)
		if err == nil {
			// Préparation mail/SMS
			emailPayload := mailer.ScanNOrderConfirmationData{
				OrderTotal:   fmt.Sprintf("%.2f", float64(order.Price)/100) + merchant.Currency,
				MerchantLogo: merchant.LogoURL,
				MerchantName: merchant.BusinessName,
				OrderDate:    order.CreationDate.String(),
				TrackingURL:  "https://scannorder.welloresto.fr/restaurant/" + merchant.Code + "/" + order.OrderID,
				SupportEmail: "contact@welloresto.fr",
			}
			go s.email.SendOrderConfirmationToCustomer(session.CustomerDetails.Email, emailPayload)

			if session.CustomerDetails != nil && session.CustomerDetails.Phone != "" {
				smsData := sms.OrderConfirmationSMSData{
					MerchantName: merchant.BusinessName,
					OrderID:      order.OrderID,
					OrderTotal:   fmt.Sprintf("%.2f", float64(order.Price)/100) + merchant.Currency,
					TrackingURL:  "https://scannorder.welloresto.fr/restaurant/" + merchant.Code + "/" + order.OrderID,
				}
				go s.smsService.SendOrderConfirmationSMS("Wello", session.CustomerDetails.Phone, smsData)
			}
		}
	}

	go s.notification.SendNotificationAsync(merchantID, orderID, "ORDER_UPDATE")

	return nil // Fin du bloc : Commit automatique
}

// 2. HandleCheckoutSessionCanceled
func (s *StripeWebhookService) HandleCheckoutSessionCanceled(ctx context.Context, data json.RawMessage) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(data, &session); err != nil {
		return fmt.Errorf("unmarshal session: %w", err)
	}

	merchantID := session.Metadata["merchant_id"]
	orderID := session.Metadata["order_id"]

	if merchantID == "" || orderID == "" {
		return errors.New("missing metadata in stripe session")
	}

	err := s.orderlifecycle.DenyOrder(ctx, orderID, models.DenyOrderRequest{
		MerchantID:       merchantID,
		UserID:           models.StripeWebhookUserID,
		DeletionReasonID: "43",
	})
	if errors.Is(err, models.ErrOrderClosed) {
		return nil
	}

	return err
	/*
		// Suppression de la commande via le orderlifecycle
		return s.orderlifecycle.DeleteOrder(ctx, models.DenyOrderInput{
			MerchantID:       merchantID,
			OrderID:          orderID,
			UserID:           "SYSTEM",
			DeletionReasonID: "43",
		})*/
}

// 3. HandleRetrieveFees (Charge Captured)
func (s *StripeWebhookService) HandleRetrieveFees(ctx context.Context, data json.RawMessage) error {
	var charge stripe.Charge
	if err := json.Unmarshal(data, &charge); err != nil {
		return fmt.Errorf("unmarshal charge: %w", err)
	}

	// Sécurité si PaymentIntent est null
	piID := ""
	if charge.PaymentIntent != nil {
		piID = charge.PaymentIntent.ID
	}

	// Sécurité si BalanceTransaction est null (string ou struct)
	btID := ""
	if charge.BalanceTransaction != nil {
		btID = charge.BalanceTransaction.ID
	}

	if piID == "" || btID == "" {
		return fmt.Errorf("missing payment_intent or balance_transaction in charge %s", charge.ID)
	}

	// 1. Get Connected Account ID via le PI ID
	accountID, err := s.repo.GetAccountIDByPaymentIntent(ctx, piID)
	if err != nil {
		return fmt.Errorf("account not found for intent %s: %w", piID, err)
	}

	// 2. Call Stripe API pour récupérer les détails des frais
	params := &stripe.BalanceTransactionParams{}
	params.SetStripeAccount(accountID)
	bt, err := balancetransaction.Get(btID, params)
	if err != nil {
		return fmt.Errorf("stripe api error: %w", err)
	}

	// 3. Calculate Fees
	var wrFees, stripeFees int64
	for _, f := range bt.FeeDetails {
		if f.Type == "application_fee" {
			wrFees += f.Amount
		} else if f.Type == "stripe_fee" {
			stripeFees += f.Amount
		}
	}

	// 4. Update DB
	if err := s.repo.UpdateFees(ctx, piID, wrFees, stripeFees, bt.Fee); err != nil {
		return fmt.Errorf("update fees db: %w", err)
	}

	return nil
}

// 4. HandlePaymentIntentUpdated
func (s *StripeWebhookService) HandlePaymentIntentUpdated(ctx context.Context, data json.RawMessage, status string) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(data, &pi); err != nil {
		return fmt.Errorf("unmarshal payment intent: %w", err)
	}

	return s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, status)
}

// 5. HandleRefund
func (s *StripeWebhookService) HandleRefund(ctx context.Context, data json.RawMessage) error {
	var refundedCharge stripe.Charge
	if err := json.Unmarshal(data, &refundedCharge); err != nil {
		return fmt.Errorf("unmarshal refund: %w", err)
	}

	// On a besoin de l'ID du PaymentIntent pour désactiver le paiement en base.
	// Le refund object contient payment_intent ID (string).
	piID := ""
	if refundedCharge.PaymentIntent != nil {
		piID = refundedCharge.PaymentIntent.ID
	}

	if err := s.repo.DisablePayment(ctx, piID); err != nil {
		return err
	}

	// Préparation des données pour le mail
	refundData := mailer.RefundData{
		Amount:       fmt.Sprintf("%.2f", float64(refundedCharge.Amount)/100) + " " + string(refundedCharge.Currency),
		CardLast4:    refundedCharge.PaymentMethodDetails.Card.Last4,
		CardBrand:    string(refundedCharge.PaymentMethodDetails.Card.Brand),
		MerchantLogo: "http://storage.welloresto.fr/img/defaults/wr_logo_invoice.png",
		RefundReason: "Remboursement",
		SupportEmail: "contact@welloresto.fr",
	}
	go s.email.SendRefundNotification(refundedCharge.BillingDetails.Email, refundData)
	return nil
}

// 6. HandlePayoutPaid
func (s *StripeWebhookService) HandlePayoutPaid(ctx context.Context, data json.RawMessage, connectedAccountID string) error {
	// Utilisation de la struct Payout définie localement ou dans models
	var payout Payout
	if err := json.Unmarshal(data, &payout); err != nil {
		return fmt.Errorf("failed to unmarshal payout: %w", err)
	}

	// Récupérer le Marchand
	merchant, err := s.repo.GetMerchantByStripeAccountID(ctx, connectedAccountID)
	if err != nil {
		return fmt.Errorf("failed to get merchant for account %s: %w", connectedAccountID, err)
	}

	if merchant == nil {
		return nil
	}

	// Conversion des données pour le mailer
	// On formate la date ici (Unix timestamp -> string lisible)
	arrivalDate := time.Unix(payout.ArrivalDate, 0).Format("02/01/2006")
	amount := fmt.Sprintf("%.2f %s", float64(payout.Amount)/100.0, "€") // Assumant EUR pour simplifier, sinon mapper currency

	emailData := mailer.PayoutData{
		PayoutID:     payout.ID,
		Amount:       amount,
		ArrivalDate:  arrivalDate,
		Status:       payout.Status,
		MerchantLogo: "http://storage.welloresto.fr/img/defaults/wr_logo_invoice.png",
	}

	go s.email.SendPayoutPaidNotification(merchant.Email, merchant.BusinessName, emailData)

	return nil
}

// 7. Invoices (Subscription)
func (s *StripeWebhookService) HandleInvoiceCreated(ctx context.Context, data json.RawMessage) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(data, &invoice); err != nil {
		return fmt.Errorf("unmarshal invoice: %w", err)
	}

	merchantID := invoice.Metadata["merchant_id"]
	if merchantID == "" {
		return nil
	}

	return s.repo.CreateInvoice(ctx, merchantID, invoice.ID, invoice.AmountDue, invoice.Created, invoice.Customer.ID)
}

func (s *StripeWebhookService) HandleInvoicePaid(ctx context.Context, data json.RawMessage) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(data, &invoice); err != nil {
		return fmt.Errorf("unmarshal invoice: %w", err)
	}

	return s.repo.PayInvoice(ctx, invoice.ID, invoice.StatusTransitions.PaidAt)
}

func (s *StripeWebhookService) VerifySignature(ctx context.Context, header http.Header, body []byte) {
	// A implémenter avec webhook.ConstructEvent de la lib stripe-go
}

// --- Private Helpers ---

// handleCustomerUpdate reste inchangé dans sa logique, mais prend un pointeur typé car appelé après unmarshal
func (s *StripeWebhookService) handleCustomerUpdate(ctx context.Context, session *stripe.CheckoutSession, orderID, merchantID string) error {
	if session.CustomerDetails == nil {
		return nil
	}

	/*
		Il faudra ici mettre à jour le client en s'assurant que l'adresse email soit conservée et que l'adresse postale du client ne soit pas perdue
			details := session.CustomerDetails
			var address string
			if details.Address != nil {
				address = fmt.Sprintf("%s, %s %s", details.Address.Line1, details.Address.PostalCode, details.Address.City)
			}
			existing, err := s.repo.FindCustomer(ctx, details.Email, merchantID)
			if err != nil {
				return err
			}

			var customerID int64
			if existing != nil {
				customerID = existing.ID
				existing.Name = details.Name
				if address != "" {
					existing.Address = address
				}
				if err := s.repo.UpdateCustomer(ctx, *existing); err != nil {
					return err
				}
			} else {
				newC := Customer{
					Name:    details.Name,
					Email:   details.Email,
					Address: address,
				}
				id, err := s.repo.CreateCustomer(ctx, newC, merchantID)
				if err != nil {
					return err
				}
				customerID = id
			}

			return s.repo.UpdateOrderCustomer(ctx, orderID, customerID)
	*/
	return nil
}
