package stripeclient

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/stripe/stripe-go/v84"
)

// ErrNoStripeAccount est retourné quand aucun compte Stripe connecté n'est
// configuré pour le merchant (pas de ligne stripe_accounts). L'appelant
// (kiosk.Service) le mappe vers models.ErrKioskTerminalNotConfigured.
var ErrNoStripeAccount = errors.New("stripe terminal: no connected account for merchant")

// ErrTerminalMappingNotFound est retourné quand aucun mapping Redis
// paymentIntentID -> commande n'existe (PaymentIntent inconnu, ou déjà annulé/
// confirmé, ou TTL expiré).
var ErrTerminalMappingNotFound = errors.New("stripe terminal: payment intent mapping not found")

// ---- Clés Redis + mapping partagé avec le webhook Stripe ----
//
// Le webhook (internal/webhook/stripe) consomme ces mêmes clés et cette même
// struct : elles sont exportées ici pour ne jamais dupliquer le format entre le
// producteur (ce service, à la création du PaymentIntent) et le consommateur (le
// webhook, à la réception de payment_intent.succeeded/payment_failed).

const (
	terminalPIKeyPrefix    = "terminal_pi:"       // terminal_pi:{paymentIntentID}       -> TerminalPaymentMapping (JSON)
	terminalOrderKeyPrefix = "terminal_order_pi:" // terminal_order_pi:{merchant}:{order} -> paymentIntentID (reverse lookup)
	terminalMappingTTL     = time.Hour
)

// TerminalPaymentMapping est la valeur stockée sous terminal_pi:{paymentIntentID}.
// Le brief ne demandait que order_id, mais le webhook a besoin de merchant_id
// pour appeler SetOrderAccepted et diffuser la notification — les deux sont donc
// stockés ensemble.
type TerminalPaymentMapping struct {
	OrderID    string `json:"order_id"`
	MerchantID string `json:"merchant_id"`
}

// TerminalPaymentIntentKey construit la clé du mapping direct
// paymentIntentID -> commande (lu par le webhook).
func TerminalPaymentIntentKey(paymentIntentID string) string {
	return terminalPIKeyPrefix + paymentIntentID
}

// TerminalOrderKey construit la clé du mapping inverse commande -> paymentIntentID
// (utilisé par le basculement carte -> caisse pour retrouver le PaymentIntent
// actif d'une commande sans que le client ait à le fournir).
func TerminalOrderKey(merchantID, orderID string) string {
	return terminalOrderKeyPrefix + merchantID + ":" + orderID
}

// TerminalAccountStore résout le compte Stripe connecté d'un merchant.
// Interface (plutôt qu'un import direct d'un module) pour que ce package
// infrastructure reste découplé des modules métier. La commission
// (application_fee_amount) n'est plus résolue ici : elle est calculée par
// l'appelant (kiosk.Service, depuis kiosk_settings.variable_fees/fixed_fees)
// et transmise directement à CreateTerminalPaymentIntent — voir
// docs/KIOSK_DECISIONS.md, "Incrément Terminal 3".
type TerminalAccountStore interface {
	// GetTerminalAccount retourne l'account_id Stripe connecté du merchant.
	// Doit retourner ErrNoStripeAccount quand aucun compte n'existe.
	GetTerminalAccount(ctx context.Context, merchantID string) (accountID string, err error)
}

// TerminalMappingStore est le sous-ensemble de l'API Redis dont ce service a
// besoin — satisfait tel quel par *redisclient.Client.
type TerminalMappingStore interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key, value string, ttl time.Duration) bool
	Delete(ctx context.Context, key string) bool
}

// TerminalService porte toute la logique Stripe Terminal, paramétrée par
// merchantID — jamais couplée à un canal (Kiosk aujourd'hui, POS demain). Les
// handlers /kiosk/terminal/* restent de simples adaptateurs qui extraient le
// merchantID du contexte KioskAuth puis appellent ce service (voir
// docs/KIOSK_DECISIONS.md, règle de découplage).
type TerminalService struct {
	sm      *StripeManager
	store   TerminalAccountStore
	mapping TerminalMappingStore
}

// NewTerminalService construit le service Terminal. sm réutilise le client
// Stripe déjà initialisé (même clé API, même compte plateforme que le reste du
// projet).
func NewTerminalService(sm *StripeManager, store TerminalAccountStore, mapping TerminalMappingStore) *TerminalService {
	return &TerminalService{sm: sm, store: store, mapping: mapping}
}

// CreateConnectionToken retourne un secret de connexion à usage court, scopé au
// compte connecté du merchant — le SDK Stripe Terminal (côté Flutter) s'en sert
// pour appairer le lecteur de carte physique.
func (t *TerminalService) CreateConnectionToken(ctx context.Context, merchantID string) (string, error) {
	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return "", err
	}

	params := &stripe.TerminalConnectionTokenParams{}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	tok, err := t.sm.client.TerminalConnectionTokens.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe terminal: create connection token: %w", err)
	}
	return tok.Secret, nil
}

// CreateTerminalPaymentIntent crée un PaymentIntent card_present sur le compte
// connecté du merchant, avec le même modèle de charge directe (SetStripeAccount)
// que le Checkout web existant (voir stripe.checkout.go) — plutôt que le modèle
// destination charge (OnBehalfOf/TransferData), pour rester cohérent avec
// l'existant. variableFees/fixedFees sont résolus par l'appelant (kiosk_settings,
// pas scannorder_settings — voir docs/KIOSK_DECISIONS.md, "Incrément Terminal 3")
// et appliqués ici avec la même formule que CreateCheckoutSession. Stocke ensuite
// les deux mappings Redis (direct + inverse), TTL 1h.
func (t *TerminalService) CreateTerminalPaymentIntent(ctx context.Context, merchantID, orderID string, amountCents int64, variableFees float64, fixedFees int64) (clientSecret, paymentIntentID string, err error) {
	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return "", "", err
	}

	// Commission identique à CreateCheckoutSession : floor(ttc*variable + fixed + 0.5).
	// + 0.5 pour arrondir correctement à l'entier le plus proche (math.Floor tronque vers le bas).
	fees := int64(math.Floor(float64(amountCents)*variableFees + float64(fixedFees) + 0.5))

	params := &stripe.PaymentIntentParams{
		Amount:               stripe.Int64(amountCents),
		Currency:             stripe.String(string(stripe.CurrencyEUR)),
		CaptureMethod:        stripe.String(string(stripe.PaymentIntentCaptureMethodAutomatic)),
		PaymentMethodTypes:   []*string{stripe.String("card_present")},
		ApplicationFeeAmount: stripe.Int64(fees),
		Metadata: map[string]string{
			"order_id":    orderID,
			"merchant_id": merchantID,
			"channel":     "kiosk",
		},
	}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	pi, err := t.sm.client.PaymentIntents.New(params)
	if err != nil {
		return "", "", fmt.Errorf("stripe terminal: create payment intent: %w", err)
	}

	t.storeMapping(ctx, merchantID, orderID, pi.ID)

	return pi.ClientSecret, pi.ID, nil
}

// CancelTerminalPaymentIntent annule un PaymentIntent en cours (cas abandon/
// timeout côté borne) et supprime les mappings Redis associés. merchantID est
// requis (écart assumé vs la signature du brief) pour deux raisons : résoudre le
// compte connecté sur lequel le PaymentIntent vit (l'annulation exige
// SetStripeAccount), et refuser qu'une borne annule le PaymentIntent d'un autre
// merchant.
func (t *TerminalService) CancelTerminalPaymentIntent(ctx context.Context, merchantID, paymentIntentID string) error {
	mapping, found := t.getMapping(ctx, paymentIntentID)
	if !found {
		// Pas de mapping : soit déjà annulé/confirmé, soit inconnu. On tente
		// tout de même l'annulation Stripe sur le compte du merchant, idempotente.
		return t.cancelOnStripe(ctx, merchantID, paymentIntentID)
	}
	if mapping.MerchantID != merchantID {
		return ErrTerminalMappingNotFound
	}

	if err := t.cancelOnStripe(ctx, merchantID, paymentIntentID); err != nil {
		return err
	}

	t.deleteMapping(ctx, paymentIntentID, mapping.MerchantID, mapping.OrderID)
	return nil
}

// CancelActivePaymentIntentForOrder retrouve le PaymentIntent actif d'une
// commande via le mapping inverse et l'annule. No-op (nil) si aucun mapping
// n'existe (le client n'avait pas encore lancé de paiement carte, ou il a déjà
// expiré) — utilisé par le basculement carte -> caisse.
func (t *TerminalService) CancelActivePaymentIntentForOrder(ctx context.Context, merchantID, orderID string) error {
	piID, found := t.mapping.Get(ctx, TerminalOrderKey(merchantID, orderID))
	if !found || piID == "" {
		return nil
	}
	return t.CancelTerminalPaymentIntent(ctx, merchantID, piID)
}

func (t *TerminalService) cancelOnStripe(ctx context.Context, merchantID, paymentIntentID string) error {
	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return err
	}
	params := &stripe.PaymentIntentCancelParams{}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	if _, err := t.sm.client.PaymentIntents.Cancel(paymentIntentID, params); err != nil {
		return fmt.Errorf("stripe terminal: cancel payment intent: %w", err)
	}
	return nil
}

func (t *TerminalService) storeMapping(ctx context.Context, merchantID, orderID, paymentIntentID string) {
	if t.mapping == nil {
		return
	}
	payload, err := json.Marshal(TerminalPaymentMapping{OrderID: orderID, MerchantID: merchantID})
	if err != nil {
		return
	}
	t.mapping.Set(ctx, TerminalPaymentIntentKey(paymentIntentID), string(payload), terminalMappingTTL)
	t.mapping.Set(ctx, TerminalOrderKey(merchantID, orderID), paymentIntentID, terminalMappingTTL)
}

func (t *TerminalService) getMapping(ctx context.Context, paymentIntentID string) (TerminalPaymentMapping, bool) {
	if t.mapping == nil {
		return TerminalPaymentMapping{}, false
	}
	val, found := t.mapping.Get(ctx, TerminalPaymentIntentKey(paymentIntentID))
	if !found {
		return TerminalPaymentMapping{}, false
	}
	var m TerminalPaymentMapping
	if err := json.Unmarshal([]byte(val), &m); err != nil {
		return TerminalPaymentMapping{}, false
	}
	return m, true
}

func (t *TerminalService) deleteMapping(ctx context.Context, paymentIntentID, merchantID, orderID string) {
	if t.mapping == nil {
		return
	}
	t.mapping.Delete(ctx, TerminalPaymentIntentKey(paymentIntentID))
	t.mapping.Delete(ctx, TerminalOrderKey(merchantID, orderID))
}

// ---- Implémentation SQL de TerminalAccountStore ----

type terminalAccountStore struct {
	db *sql.DB
}

// NewTerminalAccountStore construit le résolveur de compte connecté adossé à
// la base (table stripe_accounts).
func NewTerminalAccountStore(db *sql.DB) TerminalAccountStore {
	return &terminalAccountStore{db: db}
}

func (s *terminalAccountStore) GetTerminalAccount(ctx context.Context, merchantID string) (string, error) {
	const q = `SELECT account_id FROM stripe_accounts WHERE merchant_id = ? LIMIT 1`

	var accountID sql.NullString
	err := s.db.QueryRowContext(ctx, q, merchantID).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoStripeAccount
	}
	if err != nil {
		return "", err
	}
	if !accountID.Valid || accountID.String == "" {
		return "", ErrNoStripeAccount
	}
	return accountID.String, nil
}
