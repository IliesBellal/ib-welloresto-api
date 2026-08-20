package stripe

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/infrastructure/sms"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/utils/dbutils"

	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/balancetransaction"
)

type StripeWebhookService struct {
	repo           Repository
	stripeKey      string
	email          mailer.Service
	smsService     sms.Service
	orderlifecycle *order_life_cycle.OrdersLifeCycleService
	notification   *notification.NotificationService
	redis          *redis.Client
	db             *sql.DB
}

func NewStripeWebhookService(repo Repository, stripeKey string, email mailer.Service, smsService sms.Service, lifecycle *order_life_cycle.OrdersLifeCycleService, notification *notification.NotificationService, redis *redis.Client, db *sql.DB) *StripeWebhookService {
	stripe.Key = stripeKey
	return &StripeWebhookService{
		repo:           repo,
		stripeKey:      stripeKey,
		email:          email,
		smsService:     smsService,
		orderlifecycle: lifecycle,
		notification:   notification,
		redis:          redis,
		db:             db,
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
		return s.HandleRetrieveFees(ctx, event.Data.Object, event.Account)

	case "payment_intent.canceled":
		return s.HandlePaymentIntentUpdated(ctx, event.Data.Object, "CANCELED", event.Account)

	case "payment_intent.succeeded":
		return s.HandlePaymentIntentSucceeded(ctx, event.Data.Object, event.Account)

	case "payment_intent.payment_failed":
		return s.HandlePaymentIntentFailed(ctx, event.Data.Object, event.Account)

	case "payout.paid":
		return s.HandlePayoutPaid(ctx, event.Data.Object, event.Account)

	case "invoice.created":
		return s.HandleInvoiceCreated(ctx, event.Data.Object)

	case "invoice.paid":
		return s.HandleInvoicePaid(ctx, event.Data.Object)

	case "account.updated":
		return s.HandleAccountUpdated(ctx, event.Data.Object)

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

	if merchantID == "" || orderID == "" {
		return errors.New("missing metadata in stripe session")
	}

	piID := ""
	if session.PaymentIntent != nil {
		piID = session.PaymentIntent.ID
	}

	var isAppQRCode bool
	var shouldAutoAccept bool

	// Toutes les écritures liées au paiement s'exécutent dans une seule transaction :
	// l'invalidation du cache Redis et la mise à jour du statut de la commande doivent
	// être atomiques, sinon un GetOrder concurrent peut recacher un statut périmé
	// (ex: ONLINE_PAYMENT_PENDING) pour la durée du TTL.
	err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		key := helpers.GetRedisOrderKey(merchantID, orderID)
		if s.redis != nil {
			s.redis.Delete(txCtx, key)
		}

		// A. Insertion Payment
		if err := s.orderlifecycle.CreatePaymentNoNotification(txCtx, models.Payment{
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
		}); err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}

		// B. Update Order Creation Date to current time upon successful payment
		if err := s.repo.UpdateOrderCreationDate(txCtx, orderID); err != nil {
			return fmt.Errorf("update order creation date: %w", err)
		}

		// C. Update Order Status
		if err := s.repo.UpdateOrderPaymentStatus(txCtx, orderID); err != nil {
			return fmt.Errorf("update order payment status: %w", err)
		}

		// D. Cas Spécial: App QR Code
		if session.Metadata["checkout_session_type"] == "app_qr_code" {
			isAppQRCode = true
			if err := s.handleCustomerUpdate(txCtx, &session, orderID, merchantID); err != nil {
				log.Printf("Warning: failed to update customer: %v", err)
			}
			return nil
		}

		// E. Flow Standard
		if err := s.repo.UpdateOrderDetails(txCtx, session.ID, orderID); err != nil {
			return fmt.Errorf("update order details: %w", err)
		}
		if err := s.repo.UpdateOrderItemsPaid(txCtx, session.ID, orderID); err != nil {
			return fmt.Errorf("update items paid: %w", err)
		}

		// F. Auto Accept Logic
		orderType, merchantParams, err := s.repo.GetAutoAcceptSettings(txCtx, orderID, merchantID)
		if err == nil {
			shouldAutoAccept = (merchantParams.AutoAcceptDelivery && orderType == "DELIVERY") ||
				(merchantParams.AutoAcceptTakeaway && orderType == "TAKE_AWAY")
		}

		// G. Update Customer
		if err := s.handleCustomerUpdate(txCtx, &session, orderID, merchantID); err != nil {
			log.Printf("Warning: customer update failed: %v", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// --- ACTIONS POST-COMMIT (effets de bord) ---
	// Tout ce qui suit ne s'exécute qu'une fois la transaction validée, pour ne jamais
	// notifier/envoyer un email pour un paiement qui aurait finalement été annulé (rollback).

	// Invalidation finale du cache Redis. Le snapshot avant/après pris par
	// ExecuteOrderMutation (déclenché par CreatePaymentNoNotification ci-dessus)
	// relit et recache la commande via ComputeGetOrder pendant la transaction,
	// avant que UpdateOrderDetails ne pose brand_status='PENDING_APPROVAL' plus
	// bas dans cette même transaction — le cache se retrouve donc figé sur le
	// statut pré-paiement ('ONLINE_PAYMENT_PENDING') pour la durée du TTL (10
	// min), après un commit pourtant réussi. On invalide une dernière fois ici,
	// après le commit réel, pour garantir que la prochaine lecture retombe sur
	// la DB à jour plutôt que sur ce cache périmé.
	if s.redis != nil {
		s.redis.Delete(ctx, helpers.GetRedisOrderKey(merchantID, orderID))
	}

	if isAppQRCode {
		go s.notification.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
		return nil
	}

	go s.notification.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)

	if shouldAutoAccept {
		go s.orderlifecycle.SetOrderAccepted(context.Background(), "SYSTEM", merchantID, orderID)
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
				TrackingURL:  "https://wello-resto-scannorder-prod.onrender.com/restaurant/" + merchant.Code + "/order/" + order.OrderID,
				SupportEmail: "contact@welloresto.fr",
			}
			go s.email.SendOrderConfirmationToCustomer(session.CustomerDetails.Email, emailPayload)

			if session.CustomerDetails != nil && session.CustomerDetails.Phone != "" {
				smsData := sms.OrderConfirmationSMSData{
					MerchantName: merchant.BusinessName,
					OrderID:      order.OrderID,
					OrderTotal:   fmt.Sprintf("%.2f", float64(order.Price)/100) + merchant.Currency,
					TrackingURL:  "https://wello-resto-scannorder-prod.onrender.com/restaurant/" + merchant.Code + "/order/" + order.OrderID,
				}
				go s.smsService.SendOrderConfirmationSMS("Wello", session.CustomerDetails.Phone, smsData)
			}
		}
	}

	return nil
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

	err := s.orderlifecycle.SetOrderDenied(ctx, orderID, models.DenyOrderRequest{
		MerchantID:       merchantID,
		UserID:           models.StripeWebhookUserID,
		DeletionReasonID: "43",
		DeletionComment:  "Session de paiement expirée ou annulée",
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
func (s *StripeWebhookService) HandleRetrieveFees(ctx context.Context, data json.RawMessage, connectedAccountID string) error {
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

	// 2. Call Stripe API pour récupérer les détails des frais
	params := &stripe.BalanceTransactionParams{}
	params.SetStripeAccount(connectedAccountID)
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

// 4. HandlePaymentIntentUpdated. accountID vient de StripeEvent.Account : vide
// pour un événement plateforme, rempli (acct_...) pour un événement émis côté
// compte connecté (cas des PaymentIntents Terminal/Checkout créés en direct
// charge via SetStripeAccount — voir docs/KIOSK_DECISIONS.md, section Connect
// webhooks). Pas utilisé pour scoper un appel API ici (UpdatePaymentIntentStatus
// est un simple UPDATE SQL par payment_intent_id, un identifiant Stripe global,
// jamais ambigu entre comptes) — uniquement loggé pour pouvoir confirmer, côté
// Render, sur quelle scope (plateforme/connect) cet event est réellement arrivé.
func (s *StripeWebhookService) HandlePaymentIntentUpdated(ctx context.Context, data json.RawMessage, status, accountID string) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(data, &pi); err != nil {
		return fmt.Errorf("unmarshal payment intent: %w", err)
	}

	logger.FromContext(ctx).Info("[stripe webhook] payment_intent." + strings.ToLower(status) + " pi=" + pi.ID + " connect_account=" + accountID)
	return s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, status)
}

// HandlePaymentIntentSucceeded traite payment_intent.succeeded. Un paiement
// Stripe Terminal (card_present) créé par une borne Kiosk est reconnu par
// pi.Metadata["channel"] == "kiosk" — cette metadata est écrite dès la
// création du PaymentIntent (stripeclient.CreateTerminalPaymentIntent) et n'a
// pas de TTL (contrairement à l'ancien mapping Redis qu'elle remplace, voir
// docs/KIOSK_DECISIONS.md, "Retrait de Redis du mapping
// order_id/payment_intent_id"). order_id/merchant_id sont lus directement
// depuis cette même metadata, sans aucune requête DB/Redis supplémentaire. À
// défaut de channel=kiosk, on retombe sur le comportement existant du flux
// Checkout en ligne (statut CAPTURED en base), strictement inchangé.
//
// accountID (StripeEvent.Account) est loggé mais pas utilisé pour la logique :
// le PaymentIntent Terminal comme le Checkout web sont tous deux créés en
// direct charge (SetStripeAccount), donc tous deux émis comme événements
// Connect (account rempli) — aucun appel API Stripe n'est fait plus bas dans
// cette chaîne (SQL uniquement), donc aucun scoping par compte n'est requis
// pour la correction fonctionnelle. Le log sert uniquement à confirmer que
// l'endpoint reçoit bien ces events avec le bon compte une fois la
// configuration Stripe Dashboard corrigée (voir docs/KIOSK_DECISIONS.md).
func (s *StripeWebhookService) HandlePaymentIntentSucceeded(ctx context.Context, data json.RawMessage, accountID string) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(data, &pi); err != nil {
		return fmt.Errorf("unmarshal payment intent: %w", err)
	}

	logger.FromContext(ctx).Info("[stripe webhook] payment_intent.succeeded pi=" + pi.ID + " connect_account=" + accountID)

	if handled, err := s.handleTerminalPaymentSucceeded(ctx, &pi); handled || err != nil {
		return err
	}

	return s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, "CAPTURED")
}

// HandlePaymentIntentFailed traite payment_intent.payment_failed. Seuls les
// paiements Terminal Kiosk (metadata channel=kiosk) sont concernés : la
// commande reste en pending_card_payment (le client peut réessayer ou
// basculer vers la caisse) — on ne touche pas au statut serveur, on marque
// juste la ligne stripe_payments comme 'FAILED' (pour qu'elle sorte de
// l'ensemble "actif" lu par CancelActivePaymentIntentForOrder) et on informe
// un éventuel écran de suivi via une notification order_updated. Tout autre
// payment_intent.payment_failed (paiement en ligne) est ignoré, comme avant.
// accountID : voir HandlePaymentIntentSucceeded.
func (s *StripeWebhookService) HandlePaymentIntentFailed(ctx context.Context, data json.RawMessage, accountID string) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(data, &pi); err != nil {
		return fmt.Errorf("unmarshal payment intent: %w", err)
	}

	logger.FromContext(ctx).Info("[stripe webhook] payment_intent.payment_failed pi=" + pi.ID + " connect_account=" + accountID)

	orderID, merchantID, ok := kioskTerminalMetadata(&pi)
	if !ok {
		return nil
	}

	if err := s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, "FAILED"); err != nil {
		logger.FromContext(ctx).Warn("[stripe terminal] UpdatePaymentIntentStatus(FAILED) failed for pi=" + pi.ID + ": " + err.Error())
	}

	go s.notification.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
	return nil
}

// pi.Metadata["channel"] est le point d'extension prévu pour tout futur canal
// de paiement carte présente (ex: TPE caisse POS -> "pos_till", acompte de
// réservation de table -> "reservation_deposit") : chaque nouveau canal doit
// écrire sa propre valeur distincte à la création du PaymentIntent, jamais
// "kiosk" ni une valeur déjà utilisée par un autre canal, pour rester
// filtrable ici sans ambiguïté avec le Terminal Kiosk (voir
// docs/KIOSK_DECISIONS.md, audit du filtre channel).
//
// kioskTerminalMetadata lit order_id/merchant_id depuis la metadata Stripe du
// PaymentIntent, reconnue comme un paiement Terminal Kiosk via
// channel=="kiosk" (écrite à la création, voir
// stripeclient.CreateTerminalPaymentIntent). ok=false si channel n'est pas
// "kiosk", ou si order_id/merchant_id sont absents malgré channel=kiosk (cas
// anormal, logué).
func kioskTerminalMetadata(pi *stripe.PaymentIntent) (orderID, merchantID string, ok bool) {
	if pi.Metadata["channel"] != "kiosk" {
		return "", "", false
	}
	orderID = pi.Metadata["order_id"]
	merchantID = pi.Metadata["merchant_id"]
	return orderID, merchantID, orderID != "" && merchantID != ""
}

// handleTerminalPaymentSucceeded confirme la commande liée à un PaymentIntent
// Terminal. Retourne (true, err) quand le PaymentIntent est bien un paiement
// Terminal Kiosk (metadata channel=kiosk), (false, nil) sinon.
func (s *StripeWebhookService) handleTerminalPaymentSucceeded(ctx context.Context, pi *stripe.PaymentIntent) (bool, error) {
	log := logger.FromContext(ctx)

	orderID, merchantID, ok := kioskTerminalMetadata(pi)
	if !ok {
		return false, nil
	}
	if orderID == "" || merchantID == "" {
		log.Warn("[stripe terminal] payment_intent.succeeded pi=" + pi.ID + " has channel=kiosk metadata but missing order_id/merchant_id")
		return true, fmt.Errorf("stripe terminal: missing order_id/merchant_id metadata for pi=%s", pi.ID)
	}

	// brand_status: PENDING_CARD_PAYMENT -> PENDING. merchant_approval reste
	// "ACCEPTED" (déjà posé à la création côté Kiosk, jamais touché ici) — le
	// kiosk n'a pas d'étape d'acceptation restaurateur, contrairement au
	// paiement comptoir ScanNOrder/POS. Guard côté SQL (WHERE brand_status =
	// 'PENDING_CARD_PAYMENT') : un replay du webhook Stripe est un no-op.
	// Voir docs/KIOSK_DECISIONS.md.
	confirmed, err := s.repo.ConfirmKioskCardPayment(ctx, merchantID, orderID)
	if err != nil {
		log.Error("[stripe terminal] ConfirmKioskCardPayment failed for pi=" + pi.ID + " order=" + orderID + " merchant=" + merchantID + ": " + err.Error())
		return true, err
	}
	if !confirmed {
		// Guard WHERE brand_status = 'PENDING_CARD_PAYMENT' n'a matché aucune
		// ligne : soit un replay (déjà transitionné), soit la commande n'était
		// plus dans cet état pour une autre raison (annulée, basculée caisse).
		// Sans ce log, ce cas est indiscernable d'un succès silencieux.
		log.Warn("[stripe terminal] ConfirmKioskCardPayment: no row matched (order not in PENDING_CARD_PAYMENT) for pi=" + pi.ID + " order=" + orderID + " merchant=" + merchantID)
	}

	if s.redis != nil {
		s.redis.Delete(ctx, helpers.GetRedisOrderKey(merchantID, orderID))
	}

	// Enregistrement du paiement Terminal via l'UNIQUE point d'insertion du
	// projet (order_life_cycle : AddPaymentAndReturnID), le même que le Checkout
	// en ligne — cohérence multi-canal du reporting payments.mop. En best-effort :
	// la commande est déjà confirmée (action métier critique déjà faite) ; un échec
	// d'insertion ici est un trou de reporting, pas un échec fonctionnel, et ne
	// doit pas provoquer un retour d'erreur qui ferait rejouer le webhook Stripe
	// (transition brand_status déjà passée + re-insertion = doublon rejeté par le
	// garde fiscal de montant).
	s.recordTerminalPayment(ctx, orderID, merchantID, pi)

	if confirmed {
		go s.notification.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)
	}

	// Marque la ligne stripe_payments comme capturée : sans ça,
	// CancelActivePaymentIntentForOrder pourrait encore la considérer comme
	// "active" après coup (best-effort, un échec ici n'affecte pas la
	// confirmation déjà faite ci-dessus).
	if err := s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, "CAPTURED"); err != nil {
		log.Warn("[stripe terminal] UpdatePaymentIntentStatus(CAPTURED) failed for pi=" + pi.ID + ": " + err.Error())
	}

	return true, nil
}

// recordTerminalPayment insère la ligne payments d'un encaissement Terminal
// (card_present) via l'unique fonction d'insertion du projet
// (CreatePaymentNoNotification -> AddPaymentAndReturnID), la même que le
// Checkout en ligne. Champs :
//   - amount   : montant du PaymentIntent en centimes ;
//   - mop      : models.CardMOP ('CB', identique aux paiements carte POS —
//     rattachable à la clôture de caisse comme n'importe quel paiement carte) ;
//   - fee      : 0 initialement, net_amount initialisé à amount par l'INSERT —
//     tous deux mis à jour par le webhook charge.captured (UpdateFees) ;
//   - user_id  : "KIOSK" (created_by des commandes borne) ;
//   - cash_register_id : laissé vide -> NULL (une borne n'a pas de caisse ; le
//     paiement est rattaché à la prochaine clôture de caisse du merchant).
//
// AddPaymentAndReturnID complète la ligne stripe_payments déjà pré-créée à la
// création du PaymentIntent (stripeclient.TerminalPaymentStore.CreateMapping)
// plutôt que d'en insérer une seconde — voir docs/KIOSK_DECISIONS.md.
func (s *StripeWebhookService) recordTerminalPayment(ctx context.Context, orderID, merchantID string, pi *stripe.PaymentIntent) {
	log := logger.FromContext(ctx)
	piID := pi.ID
	if err := s.orderlifecycle.CreatePaymentNoNotification(ctx, models.Payment{
		OrderID:         orderID,
		MerchantID:      merchantID,
		MOP:             models.CardMOP,
		Amount:          int(pi.Amount),
		UserID:          "KIOSK",
		OperationType:   models.OperationTypeSale,
		PaymentIntentID: &piID,
	}); err != nil {
		log.Info("[stripe webhook] terminal payment record failed for order " + orderID + ":" + err.Error())
	}
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

	cardLast4 := ""
	cardBrand := ""
	paymentMethod := "Carte bancaire"
	paymentDetail := ""
	if refundedCharge.PaymentMethodDetails != nil {
		if refundedCharge.PaymentMethodDetails.Link != nil {
			paymentMethod = "Link"
			if refundedCharge.PaymentMethodDetails.Card != nil {
				cardLast4 = refundedCharge.PaymentMethodDetails.Card.Last4
				cardBrand = string(refundedCharge.PaymentMethodDetails.Card.Brand)
			}
			if cardLast4 != "" {
				paymentDetail = "•••• " + cardLast4
			} else if refundedCharge.PaymentMethodDetails.Link.Country != "" {
				paymentDetail = strings.ToUpper(refundedCharge.PaymentMethodDetails.Link.Country)
			}
		} else if refundedCharge.PaymentMethodDetails.Card != nil {
			cardLast4 = refundedCharge.PaymentMethodDetails.Card.Last4
			cardBrand = string(refundedCharge.PaymentMethodDetails.Card.Brand)
			if cardBrand != "" {
				paymentMethod = cardBrand
			}
			if cardLast4 != "" {
				paymentDetail = "•••• " + cardLast4
			}
		}
	}

	// Préparation des données pour le mail
	refundData := mailer.RefundData{
		Amount:        fmt.Sprintf("%.2f", float64(refundedCharge.Amount)/100) + " " + string(refundedCharge.Currency),
		PaymentMethod: paymentMethod,
		PaymentDetail: paymentDetail,
		CardLast4:     cardLast4,
		CardBrand:     cardBrand,
		MerchantLogo:  "http://storage.welloresto.fr/img/defaults/wr_logo_invoice.png",
		RefundReason:  "Remboursement",
		SupportEmail:  "contact@welloresto.fr",
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

// HandleAccountUpdated caches the Connect account verification status in stripe_accounts.
// Triggered by the "account.updated" Stripe webhook event.
func (s *StripeWebhookService) HandleAccountUpdated(ctx context.Context, data json.RawMessage) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal raw account payload: %w", err)
	}

	accountID, _ := raw["id"].(string)
	if accountID == "" {
		return errors.New("missing account id in account.updated payload")
	}

	var acc stripe.Account
	if err := json.Unmarshal(data, &acc); err != nil {
		return fmt.Errorf("unmarshal account: %w", err)
	}

	status := "action_required"
	if acc.ChargesEnabled && acc.PayoutsEnabled {
		status = "verified"
	}

	if err := s.repo.UpdateStripeAccountVerificationStatus(ctx, accountID, status); err != nil {
		log.Printf("[stripe webhook] failed to update verification status for account %s: %v", accountID, err)
		return err
	}

	merchantID, err := s.repo.GetMerchantIDByStripeAccountID(ctx, accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("[stripe webhook] no merchant found for account %s", accountID)
			return nil
		}
		return fmt.Errorf("resolve merchant by stripe account id: %w", err)
	}

	if acc.DetailsSubmitted && acc.ChargesEnabled {
		if err := s.repo.SetScanNOrderActivated(ctx, merchantID, true); err != nil {
			return fmt.Errorf("activate scannorder for merchant %s: %w", merchantID, err)
		}
	}

	log.Printf("[stripe webhook] account %s verification status updated to %s", accountID, status)
	return nil
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
