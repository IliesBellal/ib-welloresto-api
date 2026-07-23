package stripeclient

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"

	"github.com/stripe/stripe-go/v84"
)

// ErrNoStripeAccount est retourné quand aucun compte Stripe connecté n'est
// configuré pour le merchant (pas de ligne stripe_accounts). L'appelant
// (kiosk.Service) le mappe vers models.ErrKioskTerminalNotConfigured.
var ErrNoStripeAccount = errors.New("stripe terminal: no connected account for merchant")

// ErrTerminalMappingNotFound est retourné quand le PaymentIntent identifié
// appartient à un autre merchant que celui qui demande l'annulation (ligne
// stripe_payments trouvée mais merchant différent).
var ErrTerminalMappingNotFound = errors.New("stripe terminal: payment intent mapping not found")

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

// TerminalPaymentStore porte le mapping order_id <-> payment_intent_id dans
// stripe_payments — remplace l'ancien mapping Redis (terminal_pi:{id} /
// terminal_order_pi:{merchant}:{order}), diagnostiqué comme cause racine de
// paiements Terminal bloqués (mapping silencieusement perdu/expiré, voir
// docs/KIOSK_DECISIONS.md, "Retrait de Redis du mapping order_id/payment_intent_id").
type TerminalPaymentStore interface {
	// CreateMapping pré-crée la ligne stripe_payments (order_id,
	// payment_intent_id, payment_id=NULL) à la création du PaymentIntent,
	// avant toute confirmation Stripe. Complétée plus tard (payment_id rempli)
	// par order_life_cycle.AddPaymentAndReturnID quand le paiement est
	// réellement encaissé (voir docs/KIOSK_DECISIONS.md).
	CreateMapping(ctx context.Context, orderID, paymentIntentID string) error
	// GetActivePaymentIntentForOrder retourne le PaymentIntent Terminal le
	// plus récent d'une commande, encore actif (ni annulé, ni échoué, ni
	// capturé). found=false si aucune ligne n'existe.
	GetActivePaymentIntentForOrder(ctx context.Context, merchantID, orderID string) (paymentIntentID string, found bool, err error)
	// GetMerchantIDForPaymentIntent résout le merchant propriétaire d'un
	// PaymentIntent Terminal (vérification d'appartenance). found=false si
	// aucune ligne stripe_payments ne porte ce payment_intent_id.
	GetMerchantIDForPaymentIntent(ctx context.Context, paymentIntentID string) (merchantID string, found bool, err error)
	// MarkPaymentIntentStatus met à jour payment_intent_status (no-op si
	// aucune ligne ne correspond).
	MarkPaymentIntentStatus(ctx context.Context, paymentIntentID, status string) error
}

// TerminalService porte toute la logique Stripe Terminal, paramétrée par
// merchantID — jamais couplée à un canal (Kiosk aujourd'hui, POS demain). Les
// handlers /kiosk/terminal/* restent de simples adaptateurs qui extraient le
// merchantID du contexte KioskAuth puis appellent ce service (voir
// docs/KIOSK_DECISIONS.md, règle de découplage).
type TerminalService struct {
	sm       *StripeManager
	store    TerminalAccountStore
	payments TerminalPaymentStore
}

// NewTerminalService construit le service Terminal. sm réutilise le client
// Stripe déjà initialisé (même clé API, même compte plateforme que le reste du
// projet).
func NewTerminalService(sm *StripeManager, store TerminalAccountStore, payments TerminalPaymentStore) *TerminalService {
	return &TerminalService{sm: sm, store: store, payments: payments}
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
// et appliqués ici avec la même formule que CreateCheckoutSession. Pré-crée
// ensuite le mapping order_id <-> payment_intent_id dans stripe_payments (voir
// TerminalPaymentStore) — remplace l'ancien mapping Redis.
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

	// Contrairement à l'ancien mapping Redis (best-effort, erreur seulement
	// loguée — cause racine diagnostiquée dans docs/KIOSK_DECISIONS.md), un
	// échec ici fait échouer l'appel : la base est déjà une dépendance dure du
	// reste de l'application, donc un échec d'écriture signale un vrai
	// problème, pas une dégradation acceptable. Le PaymentIntent orphelin est
	// annulé en best-effort pour ne pas laisser un PI vivant que plus rien ne
	// pourra jamais rattacher à une commande.
	if err := t.payments.CreateMapping(ctx, orderID, pi.ID); err != nil {
		logger.FromContext(ctx).Error("[stripe terminal] CreateMapping failed for pi=" + pi.ID + " order=" + orderID + " merchant=" + merchantID + ": " + err.Error())
		if cancelErr := t.cancelOnStripe(ctx, merchantID, pi.ID); cancelErr != nil {
			logger.FromContext(ctx).Warn("[stripe terminal] cleanup cancel failed for orphaned pi=" + pi.ID + ": " + cancelErr.Error())
		}
		return "", "", fmt.Errorf("stripe terminal: persist payment mapping: %w", err)
	}

	return pi.ClientSecret, pi.ID, nil
}

// CancelTerminalPaymentIntent annule un PaymentIntent en cours (cas abandon/
// timeout côté borne) et marque la ligne stripe_payments associée comme
// annulée. merchantID est requis (écart assumé vs la signature du brief) pour
// deux raisons : résoudre le compte connecté sur lequel le PaymentIntent vit
// (l'annulation exige SetStripeAccount), et refuser qu'une borne annule le
// PaymentIntent d'un autre merchant.
func (t *TerminalService) CancelTerminalPaymentIntent(ctx context.Context, merchantID, paymentIntentID string) error {
	ownerMerchantID, found, err := t.payments.GetMerchantIDForPaymentIntent(ctx, paymentIntentID)
	if err != nil {
		return fmt.Errorf("stripe terminal: resolve payment mapping: %w", err)
	}
	if found && ownerMerchantID != merchantID {
		return ErrTerminalMappingNotFound
	}

	if err := t.cancelOnStripe(ctx, merchantID, paymentIntentID); err != nil {
		return err
	}

	if found {
		if err := t.payments.MarkPaymentIntentStatus(ctx, paymentIntentID, "CANCELED"); err != nil {
			logger.FromContext(ctx).Warn("[stripe terminal] mark canceled failed for pi=" + paymentIntentID + ": " + err.Error())
		}
	}
	return nil
}

// CancelActivePaymentIntentForOrder retrouve le PaymentIntent actif d'une
// commande via stripe_payments et l'annule. No-op (nil) si aucune ligne active
// n'existe (le client n'avait pas encore lancé de paiement carte, ou il a déjà
// été résolu) — utilisé par le basculement carte -> caisse.
func (t *TerminalService) CancelActivePaymentIntentForOrder(ctx context.Context, merchantID, orderID string) error {
	piID, found, err := t.payments.GetActivePaymentIntentForOrder(ctx, merchantID, orderID)
	if err != nil {
		return fmt.Errorf("stripe terminal: resolve active payment intent: %w", err)
	}
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

// ---- Implémentation SQL de TerminalPaymentStore ----

type terminalPaymentStore struct {
	db *sql.DB
}

// NewTerminalPaymentStore construit le store SQL du mapping order_id <->
// payment_intent_id, adossé à stripe_payments (remplace l'ancien mapping
// Redis).
func NewTerminalPaymentStore(db *sql.DB) TerminalPaymentStore {
	return &terminalPaymentStore{db: db}
}

// CreateMapping — success_key est NOT NULL sans défaut en base (même
// contrainte que le flux Checkout web, voir order_life_cycle/repository.go) :
// '' explicite. payment_id et payment_intent_status sont omis (NULL / défaut
// DB 'REQUIRES_CONFIRMATION'), complétés plus tard par
// order_life_cycle.AddPaymentAndReturnID quand le paiement est réellement
// encaissé (upsert par payment_intent_id, voir docs/KIOSK_DECISIONS.md).
func (s *terminalPaymentStore) CreateMapping(ctx context.Context, orderID, paymentIntentID string) error {
	db := dbx.GetDB(ctx, s.db)
	_, err := db.ExecContext(ctx,
		`INSERT INTO stripe_payments(order_id, payment_intent_id, success_key) VALUES (?, ?, '')`,
		orderID, paymentIntentID)
	return err
}

// GetActivePaymentIntentForOrder — la jointure orders vérifie l'appartenance
// merchant (stripe_payments n'a pas de colonne merchant_id propre tant que
// payment_id est NULL). ORDER BY id DESC : le PaymentIntent le plus récent,
// au cas où plusieurs lignes existeraient pour la même commande (retry après
// timeout, voir docs/KIOSK_DECISIONS.md).
func (s *terminalPaymentStore) GetActivePaymentIntentForOrder(ctx context.Context, merchantID, orderID string) (string, bool, error) {
	db := dbx.GetDB(ctx, s.db)
	const q = `
		SELECT sp.payment_intent_id
		FROM stripe_payments sp
		INNER JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.order_id = ? AND o.merchant_id = ?
		  AND sp.payment_intent_id IS NOT NULL AND sp.payment_intent_id != ''
		  AND sp.payment_intent_status NOT IN ('CANCELED', 'FAILED', 'CAPTURED')
		ORDER BY sp.id DESC
		LIMIT 1`
	var piID string
	err := db.QueryRowContext(ctx, q, orderID, merchantID).Scan(&piID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return piID, true, nil
}

// GetMerchantIDForPaymentIntent résout le merchant propriétaire d'un
// PaymentIntent Terminal — remplace le test d'appartenance que portait le
// mapping direct Redis (terminal_pi:{id}).
func (s *terminalPaymentStore) GetMerchantIDForPaymentIntent(ctx context.Context, paymentIntentID string) (string, bool, error) {
	db := dbx.GetDB(ctx, s.db)
	const q = `
		SELECT o.merchant_id
		FROM stripe_payments sp
		INNER JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.payment_intent_id = ?
		ORDER BY sp.id DESC
		LIMIT 1`
	var merchantID string
	err := db.QueryRowContext(ctx, q, paymentIntentID).Scan(&merchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return merchantID, true, nil
}

func (s *terminalPaymentStore) MarkPaymentIntentStatus(ctx context.Context, paymentIntentID, status string) error {
	db := dbx.GetDB(ctx, s.db)
	_, err := db.ExecContext(ctx, `UPDATE stripe_payments SET payment_intent_status = ? WHERE payment_intent_id = ?`, status, paymentIntentID)
	return err
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

	// Rebind requis : ce store n'utilisait jusqu'ici que le placeholder `?`
	// directement contre s.db, sans passer par dbx.GetDB — sous
	// DB_DIALECT=postgres, la requête aurait échoué systématiquement (le
	// driver Postgres n'accepte pas `?`), empêchant toute création de
	// PaymentIntent Terminal. Sans transaction à propager ici (pas d'appelant
	// qui englobe cette lecture dans dbutils.InjectTx), dbx.Rebind suffit.
	var accountID sql.NullString
	err := s.db.QueryRowContext(ctx, dbx.Rebind(q), merchantID).Scan(&accountID)
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
