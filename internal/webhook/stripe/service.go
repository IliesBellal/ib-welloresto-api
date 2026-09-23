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
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/billing"
	"welloresto-api/internal/modules/dunning"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/utils/dbutils"

	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/balancetransaction"
	"github.com/stripe/stripe-go/v78/paymentintent"
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
	// billing handles the LOT B B2a platform-billing events
	// (setup_intent.succeeded, invoice.created/paid) — a separate module
	// from this package's own Connect-account order-payment concerns (see
	// docs/decisions.md, LOT B B2a investigation).
	billing *billing.Service
	// dunning handles the LOT B B2b-1 cascade d'impayé
	// (invoice.payment_failed, and invoice.paid's dunning-clearing side).
	dunning *dunning.Service
	// terminal donne accès à GetPaymentStatus (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md,
	// même chemin que le polling client) pour le recalcul du statut avant tout
	// push WebSocket d'échec/annulation — voir pushTerminalPaymentUpdateRecomputed.
	// Distinct du chantier de vérification de signature webhook, hors scope
	// de cette session.
	terminal *stripeclient.TerminalService
}

func NewStripeWebhookService(repo Repository, stripeKey string, email mailer.Service, smsService sms.Service, lifecycle *order_life_cycle.OrdersLifeCycleService, notification *notification.NotificationService, redis *redis.Client, db *sql.DB, billingSvc *billing.Service, dunningSvc *dunning.Service, terminal *stripeclient.TerminalService) *StripeWebhookService {
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
		dunning:        dunningSvc,
		billing:        billingSvc,
		terminal:       terminal,
	}
}

// ProcessEvent est le point d'entrée unique. Il dispatche vers les handlers
// spécifiques.
//
// Idempotence (audit wello-kiosk/docs/AUDIT_STRIPE_TERMINAL.md §8 points
// 4/8/9, voir docs/KIOSK_DECISIONS.md) : event.ID (evt_...) est marqué
// "traité" dans stripe_webhook_events AVANT le dispatch. Un event rejoué
// (retry automatique Stripe sur une réponse non-200, ou double delivery
// réelle) trouve la marque déjà posée et retourne nil (200) sans retraiter —
// obligatoire dès lors que le webhook devient la seule source de vérité pour
// le paiement carte Kiosk. Si le dispatch échoue ensuite, la marque est
// retirée pour laisser un futur retry Stripe (même event.ID, delivery
// différente) reprocesser réellement l'event plutôt que de rester bloqué
// derrière un faux "déjà traité" issu d'un échec transitoire (DB down, etc.).
func (s *StripeWebhookService) ProcessEvent(ctx context.Context, event StripeEvent) error {
	if event.ID != "" {
		alreadyProcessed, err := s.repo.MarkEventProcessed(ctx, event.ID, event.Type)
		if err != nil {
			return fmt.Errorf("mark webhook event processed: %w", err)
		}
		if alreadyProcessed {
			logger.FromContext(ctx).Info("[stripe webhook] duplicate event, skipping reprocessing: id=" + event.ID + " type=" + event.Type)
			return nil
		}

		if err := s.dispatchEvent(ctx, event); err != nil {
			if delErr := s.repo.DeleteProcessedEvent(ctx, event.ID); delErr != nil {
				logger.FromContext(ctx).Warn("[stripe webhook] failed to unmark event " + event.ID + " after processing error: " + delErr.Error())
			}
			return err
		}
		return nil
	}

	// Pas d'event.ID (ex : appel direct depuis un test) : aucune déduplication
	// possible, on dispatche directement.
	return s.dispatchEvent(ctx, event)
}

func (s *StripeWebhookService) dispatchEvent(ctx context.Context, event StripeEvent) error {
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

	case "payment_intent.amount_capturable_updated":
		return s.HandlePaymentIntentAmountCapturableUpdated(ctx, event.Data.Object, event.Account)

	case "payment_intent.payment_failed":
		return s.HandlePaymentIntentFailed(ctx, event.Data.Object, event.Account)

	case "payout.paid":
		return s.HandlePayoutPaid(ctx, event.Data.Object, event.Account)

	case "invoice.created":
		return s.HandleInvoiceCreated(ctx, event.Data.Object)

	case "invoice.paid":
		return s.HandleInvoicePaid(ctx, event.Data.Object)

	case "invoice.payment_failed":
		// LOT B B2b-1 — cascade d'impayé.
		return s.HandleInvoicePaymentFailed(ctx, event.Data.Object)

	case "setup_intent.succeeded":
		// LOT B B2a — SEPA mandate acceptance. See billing.Service for why
		// this takes the raw event payload rather than a typed object
		// (stripe-go v78/v84 split between this dispatcher and that package).
		return s.billing.HandleSetupIntentSucceeded(ctx, event.Data.Object)

	case "account.updated":
		return s.HandleAccountUpdated(ctx, event.Data.Object)

	case "terminal.reader.action_failed":
		return s.HandleTerminalReaderActionFailed(ctx, event.Data.Object)

	case "terminal.reader.action_succeeded":
		return s.HandleTerminalReaderActionSucceeded(ctx, event.Data.Object)

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
	if err := s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, status); err != nil {
		return err
	}

	// Push server-driven Kiosk uniquement pour "canceled" (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) —
	// les autres statuts génériques gérés ici (par ex. de futurs cas) ne
	// concernent pas le canal Terminal Kiosk. Ignoré silencieusement pour
	// tout PaymentIntent non-Terminal (metadata channel != "kiosk").
	if status == "CANCELED" {
		if _, _, ok := kioskTerminalMetadata(&pi); ok {
			s.pushTerminalPaymentUpdateRecomputed(ctx, pi.ID)
		}
	}
	return nil
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

	// finalLocalStatus="CAPTURED" : ce webhook, pour un PaymentIntent Terminal
	// Kiosk, ne signale plus "commande confirmable" (c'est déjà fait par
	// HandlePaymentIntentAmountCapturableUpdated dès l'autorisation, voir
	// docs/KIOSK_DECISIONS.md, "Capture différée Terminal") mais "capture
	// réelle effectuée" — potentiellement jusqu'à 12h plus tard, via le cron
	// CapturePayments. confirmTerminalPayment gère l'idempotence : si la
	// commande est déjà confirmée, ce passage ne fait plus que marquer CAPTURED.
	if handled, err := s.confirmTerminalPayment(ctx, &pi, accountID, "CAPTURED"); handled || err != nil {
		return err
	}

	return s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, "CAPTURED")
}

// HandlePaymentIntentAmountCapturableUpdated traite
// payment_intent.amount_capturable_updated, émis dès qu'un PaymentIntent en
// capture_method=manual passe en requires_capture — pour la borne (channel
// kiosk), c'est le moment où un tap carte vient de réussir
// (resolveOrCreatePaymentIntentLocked, internal/infrastructure/stripe/terminal.go).
// C'est ce signal, et non la capture réelle différée par le cron
// CapturePayments (tasks/payments.go, jusqu'à 12h plus tard), qui doit
// confirmer la commande en cuisine et enregistrer le paiement — voir
// docs/KIOSK_DECISIONS.md, "Capture différée Terminal (parité ScanNOrder)".
// ScanNOrder (Checkout web) utilise aussi capture_method=manual mais se
// confirme via checkout.session.completed : cet event ne le concerne pas,
// filtré par kioskTerminalMetadata (channel=kiosk uniquement) à l'intérieur
// de confirmTerminalPayment.
func (s *StripeWebhookService) HandlePaymentIntentAmountCapturableUpdated(ctx context.Context, data json.RawMessage, accountID string) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(data, &pi); err != nil {
		return fmt.Errorf("unmarshal payment intent: %w", err)
	}

	logger.FromContext(ctx).Info("[stripe webhook] payment_intent.amount_capturable_updated pi=" + pi.ID + " connect_account=" + accountID)

	// finalLocalStatus="" : ne touche PAS payment_intent_status, qui doit
	// rester REQUIRES_CONFIRMATION (valeur déjà posée par défaut/
	// ProcessPaymentIntentOnReader) pour que le cron CapturePayments/
	// CancelPayments trouve cette ligne exactement comme un paiement ScanNOrder.
	_, err := s.confirmTerminalPayment(ctx, &pi, accountID, "")
	return err
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
	s.pushTerminalPaymentUpdateRecomputed(ctx, pi.ID)
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

// confirmTerminalPayment confirme la commande liée à un PaymentIntent
// Terminal et enregistre le paiement. Retourne (true, err) quand le
// PaymentIntent est bien un paiement Terminal Kiosk (metadata channel=kiosk),
// (false, nil) sinon. Appelée par deux webhooks (voir
// docs/KIOSK_DECISIONS.md, "Capture différée Terminal (parité ScanNOrder)") :
//   - payment_intent.amount_capturable_updated (finalLocalStatus="") : le
//     tap carte vient de réussir, le PI est en requires_capture. C'est ICI
//     que la commande est confirmée en cuisine et le paiement enregistré —
//     payment_intent_status reste REQUIRES_CONFIRMATION pour que le cron
//     CapturePayments/CancelPayments (tasks/payments.go) le trouve, comme un
//     paiement ScanNOrder.
//   - payment_intent.succeeded (finalLocalStatus="CAPTURED") : la capture
//     réelle a eu lieu, potentiellement jusqu'à 12h plus tard via le cron. La
//     commande est déjà confirmée (confirmed=false ci-dessous, cas attendu,
//     pas une anomalie) : seul payment_intent_status passe à CAPTURED.
//
// Guard par PaymentIntent (audit wello-kiosk/docs/AUDIT_STRIPE_TERMINAL.md §8
// point 4, voir docs/KIOSK_DECISIONS.md) : le guard historique
// (`WHERE brand_status = 'PENDING_CARD_PAYMENT'`, par COMMANDE) ne distingue
// pas un replay légitime du même PaymentIntent d'un second PaymentIntent
// concurrent qui aurait déjà capturé la commande — le scénario exact du bug
// timeout/retry documenté côté Kiosk (retry sans annulation du premier PI).
// On ne confirme donc désormais que si aucun AUTRE PaymentIntent n'a déjà
// capturé cette commande ; sinon, ERROR explicite + le PaymentIntent reçu est
// marqué TO_REFUND (exploitable en back-office) — jamais de remboursement
// automatique. Toute la section est verrouillée (FOR UPDATE sur orders) pour
// sérialiser deux deliveries concurrentes du même event/de deux events
// distincts pour la même commande.
//
// Détails carte (server-driven, docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) :
// relus AVANT la transaction, en best-effort — un échec de cette relecture ne
// doit jamais empêcher la confirmation de la commande, qui est l'action
// métier critique. Écrits (SetTerminalCardDetails) DANS la transaction si
// disponibles. Le push WebSocket, lui, part TOUJOURS après le commit, jamais
// pendant.
func (s *StripeWebhookService) confirmTerminalPayment(ctx context.Context, pi *stripe.PaymentIntent, accountID, finalLocalStatus string) (bool, error) {
	log := logger.FromContext(ctx)

	orderID, merchantID, ok := kioskTerminalMetadata(pi)
	if !ok {
		return false, nil
	}
	if orderID == "" || merchantID == "" {
		log.Warn("[stripe terminal] pi=" + pi.ID + " has channel=kiosk metadata but missing order_id/merchant_id")
		return true, fmt.Errorf("stripe terminal: missing order_id/merchant_id metadata for pi=%s", pi.ID)
	}

	var cardDetails *stripeclient.CardPresentDetails
	fetchParams := &stripe.PaymentIntentParams{Expand: []*string{stripe.String("latest_charge")}}
	fetchParams.SetStripeAccount(accountID)
	if fullPI, ferr := paymentintent.Get(pi.ID, fetchParams); ferr != nil {
		log.Warn("[stripe terminal] refetch pi with latest_charge failed for pi=" + pi.ID + ": " + ferr.Error())
	} else {
		cardDetails = extractCardPresentDetails(fullPI.LatestCharge)
	}

	var confirmed, duplicatePI bool
	err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.LockOrderForUpdate(txCtx, merchantID, orderID); err != nil {
			return fmt.Errorf("lock order: %w", err)
		}

		capturedPI, hasCaptured, err := s.repo.GetCapturedPaymentIntentForOrder(txCtx, merchantID, orderID)
		if err != nil {
			return fmt.Errorf("resolve captured payment intent for order: %w", err)
		}
		if hasCaptured && capturedPI != pi.ID {
			duplicatePI = true
			log.Error("[stripe terminal] pi=" + pi.ID + " order=" + orderID + " merchant=" + merchantID +
				" but this order was already captured by a different payment_intent=" + capturedPI +
				" -- possible double charge, flagging pi=" + pi.ID + " for manual refund review (no auto-refund)")
			if err := s.repo.UpdatePaymentIntentStatus(txCtx, pi.ID, "TO_REFUND"); err != nil {
				log.Warn("[stripe terminal] UpdatePaymentIntentStatus(TO_REFUND) failed for pi=" + pi.ID + ": " + err.Error())
			}
			return nil
		}

		// brand_status: PENDING_CARD_PAYMENT -> PENDING. merchant_approval reste
		// "ACCEPTED" (déjà posé à la création côté Kiosk, jamais touché ici) — le
		// kiosk n'a pas d'étape d'acceptation restaurateur, contrairement au
		// paiement comptoir ScanNOrder/POS. Guard côté SQL (WHERE brand_status =
		// 'PENDING_CARD_PAYMENT') : un replay du webhook Stripe (même PI) est un
		// no-op. Voir docs/KIOSK_DECISIONS.md.
		confirmed, err = s.repo.ConfirmKioskCardPayment(txCtx, merchantID, orderID)
		if err != nil {
			log.Error("[stripe terminal] ConfirmKioskCardPayment failed for pi=" + pi.ID + " order=" + orderID + " merchant=" + merchantID + ": " + err.Error())
			return err
		}
		if !confirmed {
			// Guard WHERE brand_status = 'PENDING_CARD_PAYMENT' n'a matché aucune
			// ligne. Deux cas très différents selon l'appelant :
			//  - finalLocalStatus != "" (payment_intent.succeeded, capture
			//    réelle différée) : c'est le cas NOMINAL désormais — la commande a
			//    déjà été confirmée plus tôt par amount_capturable_updated. Info,
			//    pas une anomalie.
			//  - finalLocalStatus == "" (amount_capturable_updated) : la commande
			//    n'était pas dans l'état attendu au moment même de l'autorisation
			//    (annulée, basculée caisse, replay) — Warn, cas à surveiller. Sans
			//    ce log, indiscernable d'un succès silencieux.
			msg := "[stripe terminal] ConfirmKioskCardPayment: no row matched (order not in PENDING_CARD_PAYMENT) for pi=" + pi.ID + " order=" + orderID + " merchant=" + merchantID
			if finalLocalStatus != "" {
				log.Info(msg)
			} else {
				log.Warn(msg)
			}
		}

		if cardDetails != nil {
			if err := s.repo.SetTerminalCardDetails(txCtx, pi.ID, cardDetails.Brand, cardDetails.Last4, cardDetails.ApplicationPreferredName, cardDetails.DedicatedFileName, cardDetails.AuthorizationCode); err != nil {
				log.Warn("[stripe terminal] SetTerminalCardDetails failed for pi=" + pi.ID + ": " + err.Error())
			}
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	if duplicatePI {
		return true, nil
	}

	if s.redis != nil {
		s.redis.Delete(ctx, helpers.GetRedisOrderKey(merchantID, orderID))
	}

	// confirmed==false ET finalLocalStatus!="" : replay attendu du webhook
	// payment_intent.succeeded après une confirmation déjà faite par
	// amount_capturable_updated — la commande a déjà été enregistrée/notifiée/
	// poussée, inutile de le refaire. Ne reste plus qu'à marquer CAPTURED
	// ci-dessous.
	if confirmed || finalLocalStatus == "" {
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

		// Push server-driven (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) — après le
		// commit, jamais pendant. "succeeded" est le seul statut poussé
		// directement (pas de recalcul) : le statut local qui vient d'être posé
		// est déjà la source de vérité à cet instant précis (requires_capture ET
		// succeeded se normalisent tous deux vers "succeeded", voir
		// terminal_status.go).
		s.pushTerminalPaymentUpdateDirect(ctx, pi.ID, "succeeded", nil, cardDetails)
	}

	// finalLocalStatus=="" (amount_capturable_updated) : ne touche pas
	// payment_intent_status, qui doit rester REQUIRES_CONFIRMATION pour que le
	// cron CapturePayments/CancelPayments trouve cette ligne.
	if finalLocalStatus != "" {
		// Marque la ligne stripe_payments comme capturée : sans ça,
		// CancelActivePaymentIntentForOrder pourrait encore la considérer comme
		// "active" après coup (best-effort, un échec ici n'affecte pas la
		// confirmation déjà faite ci-dessus).
		if err := s.repo.UpdatePaymentIntentStatus(ctx, pi.ID, finalLocalStatus); err != nil {
			log.Warn("[stripe terminal] UpdatePaymentIntentStatus(" + finalLocalStatus + ") failed for pi=" + pi.ID + ": " + err.Error())
		}
	}

	return true, nil
}

// extractCardPresentDetails lit les champs carte affichables depuis un
// Charge card_present (v78 — ce fichier est sur v78, contrairement à
// internal/infrastructure/stripe qui est sur v84 ; même forme de champs dans
// les deux SDK, dupliqué délibérément plutôt que de faire traverser un type
// stripe-go d'une version à l'autre entre ces deux packages). Retourne nil si
// le charge ou les détails card_present sont absents.
func extractCardPresentDetails(charge *stripe.Charge) *stripeclient.CardPresentDetails {
	if charge == nil || charge.PaymentMethodDetails == nil || charge.PaymentMethodDetails.CardPresent == nil {
		return nil
	}
	cp := charge.PaymentMethodDetails.CardPresent
	details := &stripeclient.CardPresentDetails{
		Brand: string(cp.Brand),
		Last4: cp.Last4,
	}
	if cp.Receipt != nil {
		details.ApplicationPreferredName = cp.Receipt.ApplicationPreferredName
		details.DedicatedFileName = cp.Receipt.DedicatedFileName
		details.AuthorizationCode = cp.Receipt.AuthorizationCode
	}
	return details
}

// pushTerminalPaymentUpdateDirect construit le PaymentStatus directement
// depuis les valeurs déjà en main (status/failureCode/cardPresent) et le
// pousse. Utilisé UNIQUEMENT pour "succeeded" — voir confirmTerminalPayment.
func (s *StripeWebhookService) pushTerminalPaymentUpdateDirect(ctx context.Context, paymentIntentID, status string, failureCode *string, cardPresent *stripeclient.CardPresentDetails) {
	var failureMessage *string
	if failureCode != nil {
		msg := stripeclient.FailureMessageFR(*failureCode)
		failureMessage = &msg
	}
	s.pushTerminalPaymentUpdate(ctx, paymentIntentID, &stripeclient.PaymentStatus{
		PaymentIntentID: paymentIntentID,
		Status:          status,
		FailureCode:     failureCode,
		FailureMessage:  failureMessage,
		CardPresent:     cardPresent,
	})
}

// pushTerminalPaymentUpdateRecomputed résout kioskID + son reader appairé,
// puis appelle stripeclient.TerminalService.GetPaymentStatus (même chemin
// que le polling client) et pousse CE résultat — jamais le statut brut tiré
// de l'event courant. Raison (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) : un
// webhook d'échec (action_failed / payment_failed / canceled) peut arriver
// APRÈS qu'un retry sur le même PaymentIntent réutilisé ait déjà relancé une
// action reader — la règle 4 de normalisation (action in_progress →
// "waiting_for_card") doit alors l'emporter sur l'échec, désormais périmé,
// que cet event à lui seul semblerait signaler. Si le recalcul échoue
// (Stripe indisponible, etc.) : ne pousse RIEN (log Warn) — le fallback de
// polling du client prend le relais, pas de faux signal.
func (s *StripeWebhookService) pushTerminalPaymentUpdateRecomputed(ctx context.Context, paymentIntentID string) {
	log := logger.FromContext(ctx)

	orderID, merchantID, kioskID, found, err := s.repo.GetOrderMerchantKioskForPaymentIntent(ctx, paymentIntentID)
	if err != nil {
		log.Warn("[stripe terminal] pushTerminalPaymentUpdateRecomputed: resolve order/merchant/kiosk failed for pi=" + paymentIntentID + ": " + err.Error())
		return
	}
	if !found || kioskID == nil || *kioskID == "" {
		// Reader jamais dispatché pour ce PI (ou webhook arrivé avant
		// SetKioskIDForPaymentIntent) : rien à pousser, pas d'erreur.
		return
	}

	var readerID *string
	if rid, rfound, rerr := s.repo.GetReaderIDForKiosk(ctx, *kioskID); rerr != nil {
		log.Warn("[stripe terminal] pushTerminalPaymentUpdateRecomputed: resolve reader for kiosk=" + *kioskID + " failed: " + rerr.Error())
	} else if rfound && rid != "" {
		readerID = &rid
	}

	if s.terminal == nil {
		log.Warn("[stripe terminal] pushTerminalPaymentUpdateRecomputed: terminal service not wired, skipping recompute for pi=" + paymentIntentID)
		return
	}
	status, err := s.terminal.GetPaymentStatus(ctx, merchantID, orderID, readerID)
	if err != nil {
		log.Warn("[stripe terminal] pushTerminalPaymentUpdateRecomputed: GetPaymentStatus failed for pi=" + paymentIntentID + " order=" + orderID + ": " + err.Error())
		return
	}
	s.pushTerminalPaymentUpdate(ctx, paymentIntentID, status)
}

// pushTerminalPaymentUpdate résout kioskID via GetOrderMerchantKioskForPaymentIntent
// et pousse {"type":"terminal_payment_update", ...PaymentStatus} au kiosk
// concerné (SendToKiosk, ciblé — jamais un broadcast merchant-wide). no-op
// silencieux (log Warn) si kioskID absent.
func (s *StripeWebhookService) pushTerminalPaymentUpdate(ctx context.Context, paymentIntentID string, status *stripeclient.PaymentStatus) {
	log := logger.FromContext(ctx)

	orderID, merchantID, kioskID, found, err := s.repo.GetOrderMerchantKioskForPaymentIntent(ctx, paymentIntentID)
	if err != nil {
		log.Warn("[stripe terminal] pushTerminalPaymentUpdate: resolve order/merchant/kiosk failed for pi=" + paymentIntentID + ": " + err.Error())
		return
	}
	if !found || kioskID == nil || *kioskID == "" {
		return
	}
	if status.OrderID == "" {
		status.OrderID = orderID
	}

	if s.notification == nil {
		return
	}
	payload := map[string]interface{}{
		"type":              "terminal_payment_update",
		"order_id":          status.OrderID,
		"payment_intent_id": status.PaymentIntentID,
		"status":            status.Status,
		"failure_code":      status.FailureCode,
		"failure_message":   status.FailureMessage,
		"card_present":      status.CardPresent,
	}
	if !s.notification.SendToKiosk(merchantID, *kioskID, payload) {
		log.Warn("[stripe terminal] pushTerminalPaymentUpdate: SendToKiosk found no active connection for kiosk=" + *kioskID + " pi=" + paymentIntentID)
	}
}

// HandleTerminalReaderActionSucceeded : traitement minimal (log seulement).
// payment_intent.amount_capturable_updated reste seul responsable de la
// clôture métier et du push de succès (payment_intent.succeeded ne fait plus
// que confirmer la capture réelle, différée — voir
// docs/KIOSK_DECISIONS.md, "Capture différée Terminal") — voir
// confirmTerminalPayment.
func (s *StripeWebhookService) HandleTerminalReaderActionSucceeded(ctx context.Context, data json.RawMessage) error {
	var reader stripe.TerminalReader
	if err := json.Unmarshal(data, &reader); err != nil {
		return fmt.Errorf("unmarshal terminal reader: %w", err)
	}
	logger.FromContext(ctx).Info("[stripe terminal] terminal.reader.action_succeeded reader=" + reader.ID)
	return nil
}

// HandleTerminalReaderActionFailed pousse un signal temps réel au kiosk
// concerné — ne touche JAMAIS orders/stripe_payments (séparation des
// responsabilités, les transitions restent gérées par les handlers
// payment_intent.*). Le statut poussé est TOUJOURS recalculé
// (pushTerminalPaymentUpdateRecomputed), jamais construit directement depuis
// cet event — voir sa documentation pour la raison (staleness).
func (s *StripeWebhookService) HandleTerminalReaderActionFailed(ctx context.Context, data json.RawMessage) error {
	var reader stripe.TerminalReader
	if err := json.Unmarshal(data, &reader); err != nil {
		return fmt.Errorf("unmarshal terminal reader: %w", err)
	}
	logger.FromContext(ctx).Info("[stripe terminal] terminal.reader.action_failed reader=" + reader.ID)

	if reader.Action == nil || reader.Action.Type != stripe.TerminalReaderActionTypeProcessPaymentIntent {
		return nil
	}
	if reader.Action.ProcessPaymentIntent == nil || reader.Action.ProcessPaymentIntent.PaymentIntent == nil {
		return nil
	}
	paymentIntentID := reader.Action.ProcessPaymentIntent.PaymentIntent.ID
	if paymentIntentID == "" {
		return nil
	}

	s.pushTerminalPaymentUpdateRecomputed(ctx, paymentIntentID)
	return nil
}

// recordTerminalPayment insère la ligne payments d'un encaissement Terminal
// (card_present) via l'unique fonction d'insertion du projet
// (CreatePaymentNoNotification -> AddPaymentAndReturnID), la même que le
// Checkout en ligne. Champs :
//   - amount   : montant du PaymentIntent en centimes ;
//   - mop      : models.KioskMOP ('KIOSK', distinct de 'CB' pour que la
//     gestion distingue les encaissements borne des vrais paiements carte —
//     rattachable à la clôture de caisse comme n'importe quel paiement, voir
//     analytics.PaymentMethodKiosk pour son traitement en reporting) ;
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
		MOP:             models.KioskMOP,
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

// 7. Invoices (Subscription) — LOT B B2a-0 : REMPLACE l'ancien comportement
// (INSERT/UPDATE subscription_invoices via repo.CreateInvoice/PayInvoice) au
// lieu de le dupliquer, confirmé sans aucun lecteur applicatif de cette
// table (docs/decisions.md, chantier B2a-0). Écrit désormais
// subscriptions.status/current_period_end directement, le modèle LOT B
// B1b/B1c.
func (s *StripeWebhookService) HandleInvoiceCreated(ctx context.Context, data json.RawMessage) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(data, &invoice); err != nil {
		return fmt.Errorf("unmarshal invoice: %w", err)
	}

	merchantID, err := s.resolveInvoiceMerchantID(ctx, &invoice)
	if err != nil {
		return err
	}
	if merchantID == "" {
		return nil
	}

	return s.repo.UpdateSubscriptionBillingPeriod(ctx, merchantID, invoice.PeriodEnd)
}

func (s *StripeWebhookService) HandleInvoicePaid(ctx context.Context, data json.RawMessage) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(data, &invoice); err != nil {
		return fmt.Errorf("unmarshal invoice: %w", err)
	}

	merchantID, err := s.resolveInvoiceMerchantID(ctx, &invoice)
	if err != nil {
		return err
	}
	if merchantID == "" {
		return nil
	}

	if err := s.repo.SetSubscriptionStatus(ctx, merchantID, "active"); err != nil {
		return err
	}
	// LOT B B2b-1 : une échéance réussie arrête la cascade d'impayé en
	// cours, s'il y en avait une — no-op si aucune (le cas normal).
	if err := s.dunning.ClearDunning(ctx, merchantID); err != nil {
		return err
	}
	return s.repo.UpdateSubscriptionBillingPeriod(ctx, merchantID, invoice.PeriodEnd)
}

// HandleInvoicePaymentFailed — LOT B B2b-1 : point d'entrée de la cascade
// d'impayé (§7.5). Toute la logique d'escalade (1er échec vs 2e, envoi des
// relances) vit dans dunning.Service.HandlePaymentFailed.
func (s *StripeWebhookService) HandleInvoicePaymentFailed(ctx context.Context, data json.RawMessage) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(data, &invoice); err != nil {
		return fmt.Errorf("unmarshal invoice: %w", err)
	}

	merchantID, err := s.resolveInvoiceMerchantID(ctx, &invoice)
	if err != nil {
		return err
	}
	if merchantID == "" {
		return nil
	}

	return s.dunning.HandlePaymentFailed(ctx, merchantID)
}

// resolveInvoiceMerchantID — LOT B F3 (docs/decisions.md) : found by running
// a real invoice.created/invoice.paid webhook cycle end-to-end for the first
// time. invoice.Metadata is EMPTY on every real Invoice Stripe generates
// from a Subscription — Stripe does not copy a Subscription's metadata onto
// its invoices, contrary to what these three handlers assumed since B2a-0.
// Confirmed by replaying the actual raw webhook payload: metadata:{},
// subscription:"sub_...". Falls back to invoice.Subscription.ID (a bare id
// reference, always present even unexpanded — same shape as the
// PaymentMethod finding in B2b-0) resolved against
// subscriptions.stripe_subscription_id, which is unambiguous even for a
// merchant sharing a mutualized platform_billing_customers.stripe_customer_id
// with another merchant (invoice.Customer.ID would NOT be unambiguous there
// — each merchant still gets its own distinct Stripe Subscription
// regardless of which Customer bills it). Metadata is checked first and
// kept as a free fast path in case Stripe's behavior here ever changes.
func (s *StripeWebhookService) resolveInvoiceMerchantID(ctx context.Context, invoice *stripe.Invoice) (string, error) {
	if merchantID := invoice.Metadata["merchant_id"]; merchantID != "" {
		return merchantID, nil
	}
	if invoice.Subscription == nil || invoice.Subscription.ID == "" {
		return "", nil
	}
	return s.repo.GetMerchantIDByStripeSubscriptionID(ctx, invoice.Subscription.ID)
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
