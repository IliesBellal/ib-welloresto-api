package stripe

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/order_life_cycle"

	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/balancetransaction"
)

type StripeWebhookService struct {
	repo         Repository
	stripeKey    string
	email        mailer.Service
	lifecycle    *order_life_cycle.OrdersLifeCycleService
	notification *notification.NotificationService
}

func NewStripeWebhookService(repo Repository, stripeKey string, email mailer.Service, lifecycle *order_life_cycle.OrdersLifeCycleService, notification *notification.NotificationService) *StripeWebhookService {
	// Configure Stripe globally
	stripe.Key = stripeKey
	return &StripeWebhookService{
		repo:         repo,
		stripeKey:    stripeKey,
		email:        email,
		lifecycle:    lifecycle,
		notification: notification,
	}
}

// 1. HandleCheckoutSessionCompleted (remplace setStripePaid)
func (s *StripeWebhookService) HandleCheckoutSessionCompleted(ctx context.Context, session *stripe.CheckoutSession) error {

	// Extraction des métadonnées
	merchantID := session.Metadata["merchant_id"]
	orderID := session.Metadata["order_id"]

	if merchantID == "" || orderID == "" {
		return errors.New("missing metadata in stripe session")
	}

	return s.repo.WithTx(ctx, func(tx *sql.Tx) error {
		// A. Insertion Payment & StripePayment
		paymentID, err := s.repo.InsertPayment(ctx, tx, Payment{
			MerchantID: merchantID,
			OrderID:    orderID,
			Amount:     session.AmountTotal,
		})
		if err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}

		err = s.repo.InsertStripePayment(ctx, tx, StripePayment{
			OrderID:           orderID,
			PaymentID:         paymentID,
			PaymentIntentID:   session.PaymentIntent.ID,
			CheckoutSessionID: session.ID,
			CustomerEmail:     session.CustomerDetails.Email,
		})
		if err != nil {
			return fmt.Errorf("insert stripe payment: %w", err)
		}

		// B. Update Order Status (isPaid calculation)
		if err := s.repo.UpdateOrderPaymentStatus(ctx, tx, orderID); err != nil {
			return fmt.Errorf("update order payment status: %w", err)
		}

		// C. Cas Spécial: App QR Code
		if session.Metadata["checkout_session_type"] == "app_qr_code" {
			// Update customer, notify mobile, return.
			if err := s.handleCustomerUpdate(ctx, tx, session, orderID, merchantID); err != nil {
				log.Printf("Warning: failed to update customer: %v", err)
			}
			// Notifications (Note: In pure architecture, do this AFTER commit, but for simplicity here:)
			go s.notification.SendNotificationAsync(merchantID, orderID, "ORDER_UPDATE")
			return nil // On s'arrête là comme dans le script PHP
		}

		// D. Flow Standard: Update Order & Items
		if err := s.repo.UpdateOrderDetails(ctx, tx, session.ID, orderID); err != nil {
			return fmt.Errorf("update order details: %w", err)
		}
		if err := s.repo.UpdateOrderItemsPaid(ctx, tx, session.ID, orderID); err != nil {
			return fmt.Errorf("update items paid: %w", err)
		}

		// E. Auto Accept Logic
		orderType, merchantParams, err := s.repo.GetAutoAcceptSettings(ctx, tx, orderID, merchantID)
		if err == nil {
			// Trigger notification
			go s.notification.SendNotificationAsync(merchantID, orderID, "UPDATE_ORDER")

			shouldAccept := (merchantParams.AutoAcceptDelivery && orderType == "DELIVERY") ||
				(merchantParams.AutoAcceptTakeaway && orderType == "TAKE_AWAY")

			if shouldAccept {
				go s.lifecycle.SetOrderAccepted(ctx, "SYSTEM", merchantID, orderID)
			}
		}

		// F. Update Customer Info
		if err := s.handleCustomerUpdate(ctx, tx, session, orderID, merchantID); err != nil {
			log.Printf("Warning: customer update failed: %v", err)
		}

		// G. Send Emails (Fetch data first)
		order, _ := s.repo.GetOrder(ctx, tx, orderID)
		merchant, _ := s.repo.GetMerchant(ctx, tx, merchantID)

		if order != nil && merchant != nil {
			go s.email.SendOrderConfirmationToCustomer(session.CustomerDetails.Email, order)
		}
		go s.notification.SendNotificationAsync(merchantID, orderID, "ORDER_UPDATE")

		return nil
	})
}

// 2. HandlePayoutPaid / Fees (remplace retrieveFees)
func (s *StripeWebhookService) HandleRetrieveFees(ctx context.Context, intentID string, balanceTxID string) error {
	return s.repo.WithTx(ctx, func(tx *sql.Tx) error {
		// 1. Get Connected Account ID
		accountID, err := s.repo.GetAccountIDByPaymentIntent(ctx, tx, intentID)
		if err != nil {
			return fmt.Errorf("account not found for intent %s: %w", intentID, err)
		}

		// 2. Call Stripe API
		params := &stripe.BalanceTransactionParams{}
		params.SetStripeAccount(accountID)
		bt, err := balancetransaction.Get(balanceTxID, params)
		if err != nil {
			return fmt.Errorf("stripe api error: %w", err)
		}

		// 3. Calculate Fees
		var wrFees, stripeFees int64
		for _, f := range bt.FeeDetails {
			//if f.Type == stripe.BalanceTransactionFeeDetailsTypeApplicationFee {
			if f.Type == "application_fee" {
				wrFees += f.Amount
				//} else if f.Type == stripe.BalanceTransactionFeeDetailsTypeStripeFee {
			} else if f.Type == "stripe_fee" {
				stripeFees += f.Amount
			}
		}

		// 4. Update DB
		if err := s.repo.UpdateFees(ctx, tx, intentID, wrFees, stripeFees, bt.Fee); err != nil {
			return fmt.Errorf("update fees db: %w", err)
		}

		return nil
	})
}

// 3. Intents (Captured/Canceled)
func (s *StripeWebhookService) HandlePaymentIntentUpdated(ctx context.Context, intentID string, status string) error {
	// Status can be "CAPTURED" or "CANCELED"
	return s.repo.WithTx(ctx, func(tx *sql.Tx) error {
		return s.repo.UpdatePaymentIntentStatus(ctx, tx, intentID, status)
	})
}

// 4. Refund
func (s *StripeWebhookService) HandleRefund(ctx context.Context, intentID string, amount int64, currency string) error {
	return s.repo.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.repo.DisablePayment(ctx, tx, intentID); err != nil {
			return err
		}
		// Conversion centimes -> float pour l'email
		data := mailer.RefundMailData{float64(amount / 100), currency}
		go s.email.SendRefundNotification("customer-email-placeholder", data)
		return nil
	})
}

// 5. Invoices (Subscription)
func (s *StripeWebhookService) HandleInvoiceCreated(ctx context.Context, invoice *stripe.Invoice) error {
	merchantID := invoice.Metadata["merchant_id"] // Assuming it's in metadata or you query it via customer
	if merchantID == "" {
		// Fallback logic to find merchant by stripe_customer_id if needed
		return nil
	}
	return s.repo.WithTx(ctx, func(tx *sql.Tx) error {
		return s.repo.CreateInvoice(ctx, tx, merchantID, invoice.ID, invoice.AmountDue, invoice.Created, invoice.Customer.ID)
	})
}

func (s *StripeWebhookService) HandleInvoicePaid(ctx context.Context, invoice *stripe.Invoice) error {
	return s.repo.WithTx(ctx, func(tx *sql.Tx) error {
		return s.repo.PayInvoice(ctx, tx, invoice.ID, invoice.StatusTransitions.PaidAt)
	})
}

// --- Private Helpers ---

func (s *StripeWebhookService) handleCustomerUpdate(ctx context.Context, tx *sql.Tx, session *stripe.CheckoutSession, orderID, merchantID string) error {
	if session.CustomerDetails == nil {
		return nil
	}

	details := session.CustomerDetails
	var address string
	if details.Address != nil {
		address = fmt.Sprintf("%s, %s %s", details.Address.Line1, details.Address.PostalCode, details.Address.City)
	}

	// 1. Check existing customer
	existing, err := s.repo.FindCustomer(ctx, tx, details.Email, merchantID)
	if err != nil {
		return err
	}

	var customerID int64

	if existing != nil {
		customerID = existing.ID
		// Update existing
		existing.Name = details.Name // Coalesce logic logic handled in SQL or here
		if address != "" {
			existing.Address = address
		}
		if err := s.repo.UpdateCustomer(ctx, tx, *existing); err != nil {
			return err
		}
	} else {
		// Create new
		newC := Customer{
			Name:    details.Name,
			Email:   details.Email,
			Address: address,
		}
		id, err := s.repo.CreateCustomer(ctx, tx, newC, merchantID)
		if err != nil {
			return err
		}
		customerID = id
	}

	// Link to Order
	return s.repo.UpdateOrderCustomer(ctx, tx, orderID, customerID)
}
