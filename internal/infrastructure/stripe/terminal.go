package stripeclient

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"

	"github.com/stripe/stripe-go/v84"
)

// stripeCallTimeout borne tout appel Stripe fait pendant qu'un verrou
// consultatif ou une transaction Postgres est tenu (WithOrderLock) — un
// Stripe lent/qui ne répond pas ne doit jamais laisser un verrou ou une
// transaction ouverts indéfiniment (voir docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md).
// Appliqué aussi à CancelReaderAction (appelée depuis les deux contextes,
// verrouillé ou non) par simplicité — un timeout de 10s est sans risque dans
// les deux cas.
const stripeCallTimeout = 10 * time.Second

func withStripeTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, stripeCallTimeout)
}

// ErrNoStripeAccount est retourné quand aucun compte Stripe connecté n'est
// configuré pour le merchant (pas de ligne stripe_accounts). L'appelant
// (kiosk.Service) le mappe vers models.ErrKioskTerminalNotConfigured.
var ErrNoStripeAccount = errors.New("stripe terminal: no connected account for merchant")

// ErrTerminalMappingNotFound est retourné quand le PaymentIntent identifié
// appartient à un autre merchant que celui qui demande l'annulation (ligne
// stripe_payments trouvée mais merchant différent).
var ErrTerminalMappingNotFound = errors.New("stripe terminal: payment intent mapping not found")

// ErrTerminalPaymentIntentConflict est retourné par CreateTerminalPaymentIntent
// quand un PaymentIntent existant de la commande n'est ni réutilisable
// (encore ouvert côté Stripe) ni sûr d'être remplacé par un second — voir
// resolveExistingPaymentIntent. L'appelant (kiosk.Service) le mappe vers un
// 409 (models.ErrKioskTerminalPaymentConflict) : la borne doit relire le
// statut de la commande plutôt que retenter une création.
var ErrTerminalPaymentIntentConflict = errors.New("stripe terminal: a payment intent for this order is already succeeded or in flight")

// Sentinelles server-driven (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) —
// mappées par kiosk.mapTerminalError vers les models.ErrKioskTerminal*
// correspondantes (toutes préfixées kiosk_terminal_, voir le contrat).
var (
	// ErrTerminalLocationNotConfigured : stripe_accounts.terminal_location_id
	// n'est pas renseigné pour ce merchant.
	ErrTerminalLocationNotConfigured = errors.New("stripe terminal: no terminal location configured for merchant")
	// ErrTerminalReaderNotFound : le reader n'existe pas sur le compte
	// connecté du merchant (resource_missing côté Stripe).
	ErrTerminalReaderNotFound = errors.New("stripe terminal: reader not found on connected account")
	// ErrTerminalReaderLocationMismatch : le reader existe sur le compte
	// connecté mais appartient à une autre location que celle du merchant.
	ErrTerminalReaderLocationMismatch = errors.New("stripe terminal: reader belongs to a different location")
	// ErrTerminalReaderOffline : le reader appairé est hors ligne (ou une
	// erreur Stripe apparentée — timeout, panne matérielle, mauvaise
	// location) au moment du dispatch.
	ErrTerminalReaderOffline = errors.New("stripe terminal: reader is offline")
	// ErrTerminalReaderBusy : le reader a déjà une action en cours qui n'a
	// pas pu être annulée, ou Stripe a explicitement refusé le dispatch pour
	// cette raison.
	ErrTerminalReaderBusy = errors.New("stripe terminal: reader is busy")
	// ErrTerminalPaymentIntentNotFoundForOrder : GetPaymentStatus appelé
	// pour une commande sans aucun PaymentIntent Terminal encore créé.
	ErrTerminalPaymentIntentNotFoundForOrder = errors.New("stripe terminal: no payment intent found for order")
)

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
	// GetTerminalLocationID retourne stripe_accounts.terminal_location_id,
	// nil si non configuré. Dupliquée délibérément depuis
	// kiosk.Repository.GetTerminalLocationID pour garder ce package
	// auto-suffisant (même principe que GetTerminalAccount ci-dessus).
	GetTerminalLocationID(ctx context.Context, merchantID string) (*string, error)
}

// PaymentIntentRecord porte l'état local complet d'un PaymentIntent Terminal
// (stripe_payments), utilisé par GetPaymentStatus pour répondre 100%
// localement quand LocalStatus ∈ {CAPTURED, TO_REFUND} — voir
// docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md.
type PaymentIntentRecord struct {
	PaymentIntentID              string
	LocalStatus                  string
	KioskID                      *string
	CardBrand                    *string
	CardLast4                    *string
	CardApplicationPreferredName *string
	CardDedicatedFileName        *string
	CardAuthorizationCode        *string
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
	// plus récent d'une commande, encore actif (ni annulé, ni capturé, ni
	// flagué TO_REFUND). found=false si aucune ligne n'existe. Un statut
	// local FAILED est désormais considéré actif (voir docs/KIOSK_DECISIONS.md,
	// "retry après failed réutilise le même PI") : resolveExistingPaymentIntent
	// revérifie de toute façon le statut réel côté Stripe avant réutilisation.
	GetActivePaymentIntentForOrder(ctx context.Context, merchantID, orderID string) (paymentIntentID string, found bool, err error)
	// GetLatestPaymentIntentRecordForOrder retourne l'état local complet du
	// PaymentIntent le plus récent de la commande, quel que soit son statut.
	// found=false si aucune ligne n'existe.
	GetLatestPaymentIntentRecordForOrder(ctx context.Context, merchantID, orderID string) (*PaymentIntentRecord, bool, error)
	// GetMerchantIDForPaymentIntent résout le merchant propriétaire d'un
	// PaymentIntent Terminal (vérification d'appartenance). found=false si
	// aucune ligne stripe_payments ne porte ce payment_intent_id.
	GetMerchantIDForPaymentIntent(ctx context.Context, paymentIntentID string) (merchantID string, found bool, err error)
	// MarkPaymentIntentStatus met à jour payment_intent_status (no-op si
	// aucune ligne ne correspond).
	MarkPaymentIntentStatus(ctx context.Context, paymentIntentID, status string) error
	// SetKioskIDForPaymentIntent pose kiosk_id sur la ligne du PaymentIntent —
	// appelé avant le dispatch reader (POST /payment), c'est ce qui permet au
	// webhook de résoudre quelle borne notifier (voir docs/KIOSK_DECISIONS.md).
	SetKioskIDForPaymentIntent(ctx context.Context, paymentIntentID, kioskID string) error
	// CountPaymentIntentAttemptsForOrder retourne le nombre de PaymentIntents
	// déjà créés pour cette commande (toutes lignes stripe_payments, y compris
	// annulées/échouées). Sert de numéro de tentative pour la clé
	// d'idempotence Stripe (order_id + numéro de tentative) passée à
	// CreateTerminalPaymentIntent — voir docs/KIOSK_DECISIONS.md.
	CountPaymentIntentAttemptsForOrder(ctx context.Context, merchantID, orderID string) (int, error)
	// WithOrderLock exécute fn dans une transaction tenant un verrou
	// consultatif Postgres pg_advisory_xact_lock(hashtext(order_id)) —
	// auto-libéré au commit/rollback. Sérialise deux appels concurrents de
	// résolution/dispatch de PaymentIntent pour la même commande : le second
	// bloque jusqu'au commit du premier, puis relit un PaymentIntent déjà
	// créé/réutilisé au lieu d'en créer un second.
	WithOrderLock(ctx context.Context, orderID string, fn func(txCtx context.Context) error) error
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
//
// DEPRECATED — flux SDK-driven, conservé jusqu'à bascule de l'app kiosk vers
// le modèle server-driven (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md).
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

// CreateTerminalPaymentIntent crée ou réutilise un PaymentIntent card_present
// pour la commande — mince wrapper verrouillé autour de
// resolveOrCreatePaymentIntentLocked (voir ci-dessous).
//
// DEPRECATED — flux SDK-driven, conservé jusqu'à bascule de l'app kiosk vers
// le modèle server-driven (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md).
func (t *TerminalService) CreateTerminalPaymentIntent(ctx context.Context, merchantID, orderID string, amountCents int64, variableFees float64, fixedFees int64) (clientSecret, paymentIntentID string, err error) {
	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return "", "", err
	}
	err = t.payments.WithOrderLock(ctx, orderID, func(txCtx context.Context) error {
		var lockedErr error
		clientSecret, paymentIntentID, lockedErr = t.resolveOrCreatePaymentIntentLocked(txCtx, accountID, merchantID, orderID, amountCents, variableFees, fixedFees)
		return lockedErr
	})
	return clientSecret, paymentIntentID, err
}

// resolveOrCreatePaymentIntentLocked implémente la règle "un seul
// PaymentIntent actif par commande, réutilisé à chaque tentative" (audit
// wello-kiosk/docs/AUDIT_STRIPE_TERMINAL.md §8 point 4, voir
// docs/KIOSK_DECISIONS.md) : si un PaymentIntent existe déjà pour cette
// commande, on interroge Stripe pour son statut réel (le statut local
// grossier de stripe_payments ne distingue pas requires_payment_method de
// requires_confirmation ni de processing) et on RÉUTILISE ce PaymentIntent
// s'il est encore ouvert plutôt que d'en créer un second — c'est exactement
// le scénario qui a produit un risque de double-encaissement documenté
// (timeout Kiosk non annulé, retry créant un second PaymentIntent orphelin).
// S'il a déjà réussi (ou est en cours de confirmation côté Stripe), la
// création est refusée (ErrTerminalPaymentIntentConflict, 409 côté kiosk).
//
// DOIT être appelée à l'intérieur d'un TerminalPaymentStore.WithOrderLock —
// jamais directement — pour fermer le gap de concurrence entre la lecture du
// PaymentIntent actif et sa création (deux appels concurrents pour la même
// commande créeraient sinon potentiellement deux PaymentIntents Stripe
// distincts).
func (t *TerminalService) resolveOrCreatePaymentIntentLocked(txCtx context.Context, accountID, merchantID, orderID string, amountCents int64, variableFees float64, fixedFees int64) (clientSecret, paymentIntentID string, err error) {
	if existingPI, found, err := t.payments.GetActivePaymentIntentForOrder(txCtx, merchantID, orderID); err != nil {
		return "", "", fmt.Errorf("stripe terminal: resolve active payment intent for order: %w", err)
	} else if found && existingPI != "" {
		reuse, refuse, secret, resolveErr := t.resolveExistingPaymentIntent(txCtx, accountID, existingPI)
		if resolveErr != nil {
			return "", "", resolveErr
		}
		if refuse {
			return "", "", ErrTerminalPaymentIntentConflict
		}
		if reuse {
			return secret, existingPI, nil
		}
		// Ni réutilisable ni à refuser : Stripe le considère fermé (canceled)
		// alors que le statut local ne l'était pas encore — resolveExistingPaymentIntent
		// vient de le corriger en best-effort. On tombe dans la création d'un
		// nouveau PaymentIntent ci-dessous.
	}

	attempts, err := t.payments.CountPaymentIntentAttemptsForOrder(txCtx, merchantID, orderID)
	if err != nil {
		return "", "", fmt.Errorf("stripe terminal: count payment intent attempts for order: %w", err)
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
	stripeCtx, cancel := withStripeTimeout(txCtx)
	defer cancel()
	params.Context = stripeCtx
	params.SetStripeAccount(accountID)
	// Clé d'idempotence Stripe = order_id + numéro de tentative : un retry
	// réseau du même appel HTTP (borne qui rejoue la requête après un timeout
	// côté client, avant même d'avoir vu la réponse) ne doit jamais créer deux
	// PaymentIntents Stripe distincts pour la même tentative. attempts+1 :
	// CountPaymentIntentAttemptsForOrder compte les lignes stripe_payments
	// déjà créées pour cette commande, avant celle-ci.
	params.SetIdempotencyKey(fmt.Sprintf("kiosk_terminal_pi_%s_attempt_%d", orderID, attempts+1))

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
	if err := t.payments.CreateMapping(txCtx, orderID, pi.ID); err != nil {
		logger.FromContext(txCtx).Error("[stripe terminal] CreateMapping failed for pi=" + pi.ID + " order=" + orderID + " merchant=" + merchantID + ": " + err.Error())
		if cancelErr := t.cancelOnStripe(txCtx, merchantID, pi.ID); cancelErr != nil {
			logger.FromContext(txCtx).Warn("[stripe terminal] cleanup cancel failed for orphaned pi=" + pi.ID + ": " + cancelErr.Error())
		}
		return "", "", fmt.Errorf("stripe terminal: persist payment mapping: %w", err)
	}

	return pi.ClientSecret, pi.ID, nil
}

// resolveExistingPaymentIntent interroge Stripe pour le statut réel d'un
// PaymentIntent Terminal déjà associé à la commande (le statut local en base,
// stripe_payments.payment_intent_status, est une catégorie grossière —
// REQUIRES_CONFIRMATION par défaut jusqu'au premier webhook reçu — qui ne
// distingue pas requires_payment_method de requires_confirmation ni de
// processing). reuse=true signifie "renvoyer ce PaymentIntent tel quel", le
// client carte peut continuer dessus ; refuse=true signifie "refuser la
// création d'un nouveau PaymentIntent (409)". Ni l'un ni l'autre (les deux
// false) signifie que Stripe l'a déjà fermé (canceled) : le statut local est
// corrigé en best-effort et l'appelant peut créer un nouveau PaymentIntent.
func (t *TerminalService) resolveExistingPaymentIntent(ctx context.Context, accountID, paymentIntentID string) (reuse, refuse bool, clientSecret string, err error) {
	params := &stripe.PaymentIntentParams{}
	stripeCtx, cancel := withStripeTimeout(ctx)
	defer cancel()
	params.Context = stripeCtx
	params.SetStripeAccount(accountID)

	pi, err := t.sm.client.PaymentIntents.Get(paymentIntentID, params)
	if err != nil {
		return false, false, "", fmt.Errorf("stripe terminal: retrieve existing payment intent: %w", err)
	}

	switch pi.Status {
	case stripe.PaymentIntentStatusRequiresPaymentMethod,
		stripe.PaymentIntentStatusRequiresConfirmation,
		stripe.PaymentIntentStatusRequiresAction:
		// Encore ouvert côté Stripe : le client carte n'a pas fini d'interagir
		// avec ce PaymentIntent, on le lui rend tel quel. Remet aussi le
		// statut local à un état actif : un retry après un échec précédent
		// (webhook payment_intent.payment_failed déjà reçu, statut local
		// FAILED) doit redevenir REQUIRES_CONFIRMATION puisqu'on le réutilise
		// réellement — voir docs/KIOSK_DECISIONS.md, "retry après failed".
		if err := t.payments.MarkPaymentIntentStatus(ctx, paymentIntentID, "REQUIRES_CONFIRMATION"); err != nil {
			logger.FromContext(ctx).Warn("[stripe terminal] sync local status(REQUIRES_CONFIRMATION) failed for pi=" + paymentIntentID + ": " + err.Error())
		}
		return true, false, pi.ClientSecret, nil

	case stripe.PaymentIntentStatusSucceeded:
		// Déjà payé — refus explicite (409). Pas de remboursement automatique
		// ici ; le webhook payment_intent.succeeded (handleTerminalPaymentSucceeded)
		// est la seule source de vérité pour confirmer la commande et, le cas
		// échéant, flaguer un doublon pour remboursement manuel.
		if err := t.payments.MarkPaymentIntentStatus(ctx, paymentIntentID, "CAPTURED"); err != nil {
			logger.FromContext(ctx).Warn("[stripe terminal] sync local status(CAPTURED) failed for pi=" + paymentIntentID + ": " + err.Error())
		}
		return false, true, "", nil

	case stripe.PaymentIntentStatusProcessing, stripe.PaymentIntentStatusRequiresCapture:
		// Une confirmation est déjà en vol côté Stripe (ou en attente de
		// capture, non atteint normalement en capture_method automatic) :
		// créer un second PaymentIntent maintenant recrée exactement le risque
		// de double PaymentIntent actif que cette règle doit éliminer — même
		// traitement que succeeded, refus explicite plutôt qu'une réutilisation
		// hasardeuse (le client carte a déjà quitté ce PaymentIntent).
		return false, true, "", nil

	default: // stripe.PaymentIntentStatusCanceled
		if err := t.payments.MarkPaymentIntentStatus(ctx, paymentIntentID, "CANCELED"); err != nil {
			logger.FromContext(ctx).Warn("[stripe terminal] sync local status(CANCELED) failed for pi=" + paymentIntentID + ": " + err.Error())
		}
		return false, false, "", nil
	}
}

// CancelTerminalPaymentIntent annule un PaymentIntent en cours (cas abandon/
// timeout côté borne) et marque la ligne stripe_payments associée comme
// annulée. merchantID est requis (écart assumé vs la signature du brief) pour
// deux raisons : résoudre le compte connecté sur lequel le PaymentIntent vit
// (l'annulation exige SetStripeAccount), et refuser qu'une borne annule le
// PaymentIntent d'un autre merchant.
//
// DEPRECATED — flux SDK-driven, conservé jusqu'à bascule de l'app kiosk vers
// le modèle server-driven (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md).
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
	stripeCtx, cancel := withStripeTimeout(ctx)
	defer cancel()
	params.Context = stripeCtx
	params.SetStripeAccount(accountID)

	if _, err := t.sm.client.PaymentIntents.Cancel(paymentIntentID, params); err != nil {
		return fmt.Errorf("stripe terminal: cancel payment intent: %w", err)
	}
	return nil
}

// ---- Server-driven : lecteurs (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) ----

// ReaderInfo est la vue publique d'un reader Stripe Terminal, alignée sur le
// contrat (id/label/serial_number/status).
type ReaderInfo struct {
	ID           string
	Label        string
	SerialNumber string
	Status       string // "online"|"offline"
}

// ReaderStatus étend ReaderInfo avec l'action en cours/dernière du reader
// (types Stripe conservés — pas de conversion string prématurée — pour que
// GetPaymentStatus puisse les passer directement à NormalizePaymentStatus).
type ReaderStatus struct {
	ReaderInfo
	HasAction             bool
	ActionType            stripe.TerminalReaderActionType
	ActionStatus          stripe.TerminalReaderActionStatus
	ActionFailureCode     string
	ActionFailureMessage  string
	ActionPaymentIntentID string
}

func readerInfoFrom(r *stripe.TerminalReader) ReaderInfo {
	return ReaderInfo{ID: r.ID, Label: r.Label, SerialNumber: r.SerialNumber, Status: string(r.Status)}
}

// resolveAccountAndLocation résout accountID + terminal_location_id du
// merchant — factorisé car ListReaders/GetReaderForPairing en ont tous deux
// besoin. ErrTerminalLocationNotConfigured si la location n'est pas
// renseignée.
func (t *TerminalService) resolveAccountAndLocation(ctx context.Context, merchantID string) (accountID, locationID string, err error) {
	accountID, err = t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return "", "", err
	}
	loc, err := t.store.GetTerminalLocationID(ctx, merchantID)
	if err != nil {
		return "", "", fmt.Errorf("stripe terminal: resolve terminal location: %w", err)
	}
	if loc == nil || *loc == "" {
		return "", "", ErrTerminalLocationNotConfigured
	}
	return accountID, *loc, nil
}

// ListReaders liste les readers de la location Stripe du merchant.
func (t *TerminalService) ListReaders(ctx context.Context, merchantID string) ([]ReaderInfo, error) {
	accountID, locationID, err := t.resolveAccountAndLocation(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	params := &stripe.TerminalReaderListParams{Location: stripe.String(locationID)}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	var readers []ReaderInfo
	iter := t.sm.client.TerminalReaders.List(params)
	for iter.Next() {
		readers = append(readers, readerInfoFrom(iter.TerminalReader()))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("stripe terminal: list readers: %w", err)
	}
	return readers, nil
}

// GetReaderForPairing valide qu'un reader appartient bien au compte connecté
// ET à la terminal_location_id du merchant avant appairage (décision actée).
func (t *TerminalService) GetReaderForPairing(ctx context.Context, merchantID, readerID string) (*ReaderInfo, error) {
	accountID, locationID, err := t.resolveAccountAndLocation(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	params := &stripe.TerminalReaderParams{}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	reader, err := t.sm.client.TerminalReaders.Get(readerID, params)
	if err != nil {
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) && stripeErr.Code == stripe.ErrorCodeResourceMissing {
			return nil, ErrTerminalReaderNotFound
		}
		return nil, fmt.Errorf("stripe terminal: retrieve reader: %w", err)
	}
	if reader.Location == nil || reader.Location.ID != locationID {
		return nil, ErrTerminalReaderLocationMismatch
	}

	info := readerInfoFrom(reader)
	return &info, nil
}

// GetReaderStatus retourne le statut live d'un reader déjà appairé (statut
// online/offline + action en cours). Retourne (nil, nil) — pas une erreur —
// si le reader n'existe plus côté Stripe (resource_missing) : le contrat
// exige que GET /reader efface silencieusement l'appairage dans ce cas
// précis, donc l'appelant (kiosk.Service) distingue ce cas d'une vraie panne
// réseau/API et déclenche l'effacement.
func (t *TerminalService) GetReaderStatus(ctx context.Context, merchantID, readerID string) (*ReaderStatus, error) {
	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	params := &stripe.TerminalReaderParams{}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	reader, err := t.sm.client.TerminalReaders.Get(readerID, params)
	if err != nil {
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) && stripeErr.Code == stripe.ErrorCodeResourceMissing {
			return nil, nil
		}
		return nil, fmt.Errorf("stripe terminal: retrieve reader status: %w", err)
	}

	status := &ReaderStatus{ReaderInfo: readerInfoFrom(reader)}
	if reader.Action != nil {
		status.HasAction = true
		status.ActionType = reader.Action.Type
		status.ActionStatus = reader.Action.Status
		status.ActionFailureCode = reader.Action.FailureCode
		status.ActionFailureMessage = reader.Action.FailureMessage
		if reader.Action.ProcessPaymentIntent != nil && reader.Action.ProcessPaymentIntent.PaymentIntent != nil {
			status.ActionPaymentIntentID = reader.Action.ProcessPaymentIntent.PaymentIntent.ID
		}
	}
	return status, nil
}

// ProcessPaymentIntentOnReader résout/réutilise le PaymentIntent de la
// commande puis le dispatche vers le reader appairé
// (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md, section Dispatch).
//
// Règle de commit (critique) : une erreur de dispatch (vérification
// pré-dispatch ou l'appel ProcessPaymentIntent lui-même) ne doit JAMAIS faire
// rollback de la résolution du PaymentIntent ni de l'écriture kiosk_id —
// sinon un PaymentIntent tout juste créé/résolu côté Stripe perdrait son
// mapping DB alors qu'il existe bel et bien chez Stripe, devenant orphelin.
// dispatchErr est donc capturé par la closure verrouillée mais celle-ci
// retourne toujours nil (commit), et dispatchErr n'est renvoyé qu'après coup.
func (t *TerminalService) ProcessPaymentIntentOnReader(ctx context.Context, merchantID, orderID, readerID, kioskID, clientIdempotencyKey string, amountCents int64, variableFees float64, fixedFees int64) (paymentIntentID, readerActionStatus string, err error) {
	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return "", "", err
	}

	var dispatchErr error
	lockErr := t.payments.WithOrderLock(ctx, orderID, func(txCtx context.Context) error {
		_, piID, resolveErr := t.resolveOrCreatePaymentIntentLocked(txCtx, accountID, merchantID, orderID, amountCents, variableFees, fixedFees)
		if resolveErr != nil {
			return resolveErr // rollback : le PaymentIntent lui-même n'a pas pu être résolu
		}
		if err := t.payments.SetKioskIDForPaymentIntent(txCtx, piID, kioskID); err != nil {
			return err // rollback : kiosk_id doit être posé de façon fiable avant tout dispatch
		}
		if err := t.payments.MarkPaymentIntentStatus(txCtx, piID, "REQUIRES_CONFIRMATION"); err != nil {
			// Best-effort : corrige un statut local FAILED d'une tentative
			// précédente sur ce même PI réutilisé — cosmétique/diagnostic,
			// ne bloque jamais le dispatch.
			logger.FromContext(txCtx).Warn("[stripe terminal] reset status before dispatch failed for pi=" + piID + ": " + err.Error())
		}

		paymentIntentID = piID
		readerActionStatus, dispatchErr = t.dispatchToReader(txCtx, accountID, readerID, piID, clientIdempotencyKey)
		return nil // TOUJOURS nil : dispatchErr ne fait jamais rollback des écritures ci-dessus
	})
	if lockErr != nil {
		return "", "", lockErr
	}
	if dispatchErr != nil {
		return "", "", dispatchErr
	}
	return paymentIntentID, readerActionStatus, nil
}

// dispatchToReader effectue la vérification pré-dispatch puis le dispatch
// Stripe lui-même. Appelée uniquement depuis ProcessPaymentIntentOnReader, à
// l'intérieur du verrou de commande.
func (t *TerminalService) dispatchToReader(txCtx context.Context, accountID, readerID, paymentIntentID, clientIdempotencyKey string) (readerActionStatus string, err error) {
	getParams := &stripe.TerminalReaderParams{}
	getCtx, cancelGet := withStripeTimeout(txCtx)
	defer cancelGet()
	getParams.Context = getCtx
	getParams.SetStripeAccount(accountID)

	reader, err := t.sm.client.TerminalReaders.Get(readerID, getParams)
	if err != nil {
		return "", fmt.Errorf("stripe terminal: retrieve reader before dispatch: %w", err)
	}

	if reader.Action != nil && reader.Action.Status == stripe.TerminalReaderActionStatusInProgress {
		if reader.Action.ProcessPaymentIntent != nil && reader.Action.ProcessPaymentIntent.PaymentIntent != nil &&
			reader.Action.ProcessPaymentIntent.PaymentIntent.ID == paymentIntentID {
			// Action déjà en cours sur CE PI précis : tap dupliqué, ou retry
			// client sans la même Idempotency-Key (ex. redémarrage d'app) —
			// no-op idempotent, ne redispatche pas.
			return string(reader.Action.Status), nil
		}
		// Action en cours sur un AUTRE PI (commande abandonnée puis
		// relancée) : on l'annule d'abord pour ne jamais risquer deux
		// actions concurrentes sur le même reader physique.
		if err := t.CancelReaderAction(txCtx, "", readerID); err != nil {
			// merchantID vide : on a déjà accountID en main, inutile de le
			// re-résoudre — CancelReaderAction accepte donc soit merchantID
			// soit un accountID déjà connu, voir sa signature ci-dessous.
			return "", ErrTerminalReaderBusy
		}
	}

	params := &stripe.TerminalReaderProcessPaymentIntentParams{
		PaymentIntent: stripe.String(paymentIntentID),
	}
	stripeCtx, cancel := withStripeTimeout(txCtx)
	defer cancel()
	params.Context = stripeCtx
	params.SetStripeAccount(accountID)
	params.SetIdempotencyKey(fmt.Sprintf("kiosk_terminal_process_%s_%s", paymentIntentID, clientIdempotencyKey))

	updatedReader, err := t.sm.client.TerminalReaders.ProcessPaymentIntent(readerID, params)
	if err != nil {
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) {
			switch stripeErr.Code {
			case stripe.ErrorCodeTerminalReaderBusy:
				return "", ErrTerminalReaderBusy
			case stripe.ErrorCodeTerminalReaderOffline,
				stripe.ErrorCodeTerminalReaderTimeout,
				stripe.ErrorCodeTerminalReaderHardwareFault,
				stripe.ErrorCodeTerminalReaderInvalidLocationForPayment:
				return "", ErrTerminalReaderOffline
			}
		}
		return "", fmt.Errorf("stripe terminal: process payment intent on reader: %w", err)
	}

	if updatedReader.Action != nil {
		return string(updatedReader.Action.Status), nil
	}
	return "", nil
}

// CancelReaderAction annule l'action en cours d'un reader, si elle existe.
// Relit d'abord l'état live du reader : si aucune action n'est in_progress,
// ne fait AUCUN appel Stripe et retourne nil (rien à annuler). merchantID
// peut être vide si accountIDHint est déjà connu de l'appelant (évite une
// résolution redondante depuis dispatchToReader, qui a déjà accountID en
// main) — sinon merchantID est résolu normalement.
func (t *TerminalService) CancelReaderAction(ctx context.Context, merchantID, readerID string) error {
	return t.cancelReaderActionWithAccount(ctx, merchantID, "", readerID)
}

func (t *TerminalService) cancelReaderActionWithAccount(ctx context.Context, merchantID, accountIDHint, readerID string) error {
	accountID := accountIDHint
	if accountID == "" {
		resolved, err := t.store.GetTerminalAccount(ctx, merchantID)
		if err != nil {
			return err
		}
		accountID = resolved
	}

	getParams := &stripe.TerminalReaderParams{}
	getCtx, cancelGet := withStripeTimeout(ctx)
	defer cancelGet()
	getParams.Context = getCtx
	getParams.SetStripeAccount(accountID)

	reader, err := t.sm.client.TerminalReaders.Get(readerID, getParams)
	if err != nil {
		return fmt.Errorf("stripe terminal: retrieve reader for cancel: %w", err)
	}
	if reader.Action == nil || reader.Action.Status != stripe.TerminalReaderActionStatusInProgress {
		return nil
	}

	cancelParams := &stripe.TerminalReaderCancelActionParams{}
	cancelCtx, cancel := withStripeTimeout(ctx)
	defer cancel()
	cancelParams.Context = cancelCtx
	cancelParams.SetStripeAccount(accountID)

	if _, err := t.sm.client.TerminalReaders.CancelAction(readerID, cancelParams); err != nil {
		return fmt.Errorf("stripe terminal: cancel reader action: %w", err)
	}
	return nil
}

// CancelPaymentIntentIfCancelable résout le PaymentIntent le plus récent de
// la commande, relit son statut réel côté Stripe, et l'annule UNIQUEMENT si
// ce statut n'est ni succeeded, ni processing, ni requires_capture (les
// trois exclus explicitement — annuler un PI dans l'un de ces états risque
// d'annuler un paiement qui a en réalité abouti ou est en train d'aboutir).
// Retourne le statut réel après tentative (annulé ou non).
func (t *TerminalService) CancelPaymentIntentIfCancelable(ctx context.Context, merchantID, orderID string) (status string, err error) {
	record, found, err := t.payments.GetLatestPaymentIntentRecordForOrder(ctx, merchantID, orderID)
	if err != nil {
		return "", fmt.Errorf("stripe terminal: resolve latest payment intent: %w", err)
	}
	if !found {
		return "", ErrTerminalPaymentIntentNotFoundForOrder
	}

	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return "", err
	}

	getParams := &stripe.PaymentIntentParams{}
	getCtx, cancelGet := withStripeTimeout(ctx)
	defer cancelGet()
	getParams.Context = getCtx
	getParams.SetStripeAccount(accountID)

	pi, err := t.sm.client.PaymentIntents.Get(record.PaymentIntentID, getParams)
	if err != nil {
		return "", fmt.Errorf("stripe terminal: retrieve payment intent for cancel: %w", err)
	}

	switch pi.Status {
	case stripe.PaymentIntentStatusSucceeded, stripe.PaymentIntentStatusProcessing, stripe.PaymentIntentStatusRequiresCapture:
		return string(pi.Status), nil
	}

	cancelParams := &stripe.PaymentIntentCancelParams{}
	cancelCtx, cancel := withStripeTimeout(ctx)
	defer cancel()
	cancelParams.Context = cancelCtx
	cancelParams.SetStripeAccount(accountID)

	canceled, err := t.sm.client.PaymentIntents.Cancel(record.PaymentIntentID, cancelParams)
	if err != nil {
		return "", fmt.Errorf("stripe terminal: cancel payment intent: %w", err)
	}
	if err := t.payments.MarkPaymentIntentStatus(ctx, record.PaymentIntentID, "CANCELED"); err != nil {
		logger.FromContext(ctx).Warn("[stripe terminal] mark canceled failed for pi=" + record.PaymentIntentID + ": " + err.Error())
	}
	return string(canceled.Status), nil
}

// PresentTestPaymentMethod simule la présentation d'une carte sur un reader
// simulé Stripe (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md, endpoint dev) —
// outcome "success" utilise la carte de test générique 4242..., "declined"
// la carte de refus générique 4000000000000002 (confirmées dans la doc API
// Stripe, endpoint present_payment_method, champ card_present.number :
// https://docs.stripe.com/testing, https://docs.stripe.com/api/terminal/readers/present_payment_method).
// N'est PAS gatée ici par un contrôle de clé de test — c'est la
// responsabilité de l'appelant (kiosk.Service.PresentTestPaymentMethod, gate
// StripeTestMode) ; ce package infrastructure ne connaît pas la config
// applicative.
func (t *TerminalService) PresentTestPaymentMethod(ctx context.Context, merchantID, readerID, outcome string) error {
	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return err
	}

	cardNumber := "4242424242424242"
	if outcome == "declined" {
		cardNumber = "4000000000000002"
	}

	params := &stripe.TestHelpersTerminalReaderPresentPaymentMethodParams{
		Type: stripe.String("card_present"),
		CardPresent: &stripe.TestHelpersTerminalReaderPresentPaymentMethodCardPresentParams{
			Number: stripe.String(cardNumber),
		},
	}
	params.Context = ctx
	params.SetStripeAccount(accountID)

	if _, err := t.sm.client.TestHelpersTerminalReaders.PresentPaymentMethod(readerID, params); err != nil {
		return fmt.Errorf("stripe terminal: present test payment method: %w", err)
	}
	return nil
}

// GetPaymentStatus assemble le PaymentStatus consolidé
// (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) — point d'assemblage unique
// réutilisé par POST /payment, POST /payment/cancel, GET /payment/status et
// par le recalcul déclenché depuis les webhooks (voir
// internal/webhook/stripe). readerID peut être nil (commande sans reader
// appairé au moment de l'appel) : l'action reader est alors simplement
// absente de la normalisation.
func (t *TerminalService) GetPaymentStatus(ctx context.Context, merchantID, orderID string, readerID *string) (*PaymentStatus, error) {
	record, found, err := t.payments.GetLatestPaymentIntentRecordForOrder(ctx, merchantID, orderID)
	if err != nil {
		return nil, fmt.Errorf("stripe terminal: resolve payment intent record: %w", err)
	}
	if !found {
		return nil, ErrTerminalPaymentIntentNotFoundForOrder
	}

	// Court-circuit local : un statut déjà terminal (succeeded) ne peut plus
	// changer, inutile de retaper Stripe à chaque tick de polling.
	if record.LocalStatus == "CAPTURED" || record.LocalStatus == "TO_REFUND" {
		return &PaymentStatus{
			OrderID:         orderID,
			PaymentIntentID: record.PaymentIntentID,
			Status:          "succeeded",
			CardPresent:     cardPresentFromRecord(record),
		}, nil
	}

	accountID, err := t.store.GetTerminalAccount(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	piParams := &stripe.PaymentIntentParams{}
	piParams.Context = ctx
	piParams.SetStripeAccount(accountID)
	pi, err := t.sm.client.PaymentIntents.Get(record.PaymentIntentID, piParams)
	if err != nil {
		return nil, fmt.Errorf("stripe terminal: retrieve payment intent: %w", err)
	}

	input := PaymentStatusInput{
		LocalStatus: record.LocalStatus,
		PIStatus:    pi.Status,
	}
	if pi.LastPaymentError != nil {
		input.HasLastPaymentError = true
		input.LastPaymentErrorCode = string(pi.LastPaymentError.Code)
	}

	if readerID != nil && *readerID != "" {
		readerStatus, rsErr := t.GetReaderStatus(ctx, merchantID, *readerID)
		if rsErr != nil {
			logger.FromContext(ctx).Warn("[stripe terminal] GetPaymentStatus: reader status lookup failed for reader=" + *readerID + ": " + rsErr.Error())
		} else if readerStatus != nil && readerStatus.HasAction && readerStatus.ActionPaymentIntentID == record.PaymentIntentID {
			actionStatus := readerStatus.ActionStatus
			input.ReaderActionStatus = &actionStatus
			input.ReaderActionFailureCode = readerStatus.ActionFailureCode
		}
	}

	statusStr, failureCode := NormalizePaymentStatus(input)
	result := &PaymentStatus{
		OrderID:         orderID,
		PaymentIntentID: record.PaymentIntentID,
		Status:          statusStr,
		FailureCode:     failureCode,
	}
	if failureCode != nil {
		msg := FailureMessageFR(*failureCode)
		result.FailureMessage = &msg
	}
	if statusStr == "succeeded" {
		result.CardPresent = cardPresentFromRecord(record)
	}
	return result, nil
}

func cardPresentFromRecord(record *PaymentIntentRecord) *CardPresentDetails {
	if record.CardBrand == nil {
		return nil
	}
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return &CardPresentDetails{
		Brand:                    *record.CardBrand,
		Last4:                    deref(record.CardLast4),
		ApplicationPreferredName: deref(record.CardApplicationPreferredName),
		DedicatedFileName:        deref(record.CardDedicatedFileName),
		AuthorizationCode:        deref(record.CardAuthorizationCode),
	}
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
// ” explicite. payment_id et payment_intent_status sont omis (NULL / défaut
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
// timeout, voir docs/KIOSK_DECISIONS.md). CAPTURED/TO_REFUND/CANCELED
// exclus : ce sont les seuls statuts locaux réellement terminaux. FAILED
// n'exclut plus (contrairement à l'implémentation initiale) : un PI marqué
// FAILED (webhook payment_intent.payment_failed déjà reçu, ex. carte
// refusée) reste généralement requires_payment_method côté Stripe, donc
// réutilisable pour un retry — voir docs/KIOSK_DECISIONS.md, "retry après
// failed réutilise le même PI". resolveExistingPaymentIntent revérifie de
// toute façon le statut réel côté Stripe avant réutilisation.
func (s *terminalPaymentStore) GetActivePaymentIntentForOrder(ctx context.Context, merchantID, orderID string) (string, bool, error) {
	db := dbx.GetDB(ctx, s.db)
	const q = `
		SELECT sp.payment_intent_id
		FROM stripe_payments sp
		INNER JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.order_id = ? AND o.merchant_id = ?
		  AND sp.payment_intent_id IS NOT NULL AND sp.payment_intent_id != ''
		  AND sp.payment_intent_status NOT IN ('CANCELED', 'CAPTURED', 'TO_REFUND')
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

// GetLatestPaymentIntentRecordForOrder — même sélection que
// GetActivePaymentIntentForOrder mais sans filtre de statut (le PI le plus
// récent, quel qu'il soit) et avec l'état local complet (kiosk_id + détails
// carte), pour GetPaymentStatus.
func (s *terminalPaymentStore) GetLatestPaymentIntentRecordForOrder(ctx context.Context, merchantID, orderID string) (*PaymentIntentRecord, bool, error) {
	db := dbx.GetDB(ctx, s.db)
	const q = `
		SELECT sp.payment_intent_id, sp.payment_intent_status, sp.kiosk_id,
		       sp.card_brand, sp.card_last4, sp.card_application_preferred_name,
		       sp.card_dedicated_file_name, sp.card_authorization_code
		FROM stripe_payments sp
		INNER JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.order_id = ? AND o.merchant_id = ?
		  AND sp.payment_intent_id IS NOT NULL AND sp.payment_intent_id != ''
		ORDER BY sp.id DESC
		LIMIT 1`
	var rec PaymentIntentRecord
	var kioskID, cardBrand, cardLast4, cardAppName, cardFileName, cardAuthCode sql.NullString
	err := db.QueryRowContext(ctx, q, orderID, merchantID).Scan(
		&rec.PaymentIntentID, &rec.LocalStatus, &kioskID,
		&cardBrand, &cardLast4, &cardAppName, &cardFileName, &cardAuthCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if kioskID.Valid {
		rec.KioskID = &kioskID.String
	}
	if cardBrand.Valid {
		rec.CardBrand = &cardBrand.String
	}
	if cardLast4.Valid {
		rec.CardLast4 = &cardLast4.String
	}
	if cardAppName.Valid {
		rec.CardApplicationPreferredName = &cardAppName.String
	}
	if cardFileName.Valid {
		rec.CardDedicatedFileName = &cardFileName.String
	}
	if cardAuthCode.Valid {
		rec.CardAuthorizationCode = &cardAuthCode.String
	}
	return &rec, true, nil
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

func (s *terminalPaymentStore) SetKioskIDForPaymentIntent(ctx context.Context, paymentIntentID, kioskID string) error {
	db := dbx.GetDB(ctx, s.db)
	_, err := db.ExecContext(ctx, `UPDATE stripe_payments SET kiosk_id = ? WHERE payment_intent_id = ?`, kioskID, paymentIntentID)
	return err
}

// CountPaymentIntentAttemptsForOrder — toutes les lignes stripe_payments de la
// commande, quel que soit leur statut (une ligne par PaymentIntent Stripe
// créé, voir CreateMapping) : sert de numéro de tentative pour la clé
// d'idempotence Stripe de CreateTerminalPaymentIntent.
func (s *terminalPaymentStore) CountPaymentIntentAttemptsForOrder(ctx context.Context, merchantID, orderID string) (int, error) {
	db := dbx.GetDB(ctx, s.db)
	const q = `
		SELECT COUNT(*)
		FROM stripe_payments sp
		INNER JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.order_id = ? AND o.merchant_id = ?`
	var n int
	err := db.QueryRowContext(ctx, q, orderID, merchantID).Scan(&n)
	return n, err
}

// WithOrderLock — voir TerminalPaymentStore.WithOrderLock. pg_advisory_xact_lock
// est un verrou consultatif Postgres scopé à la transaction courante,
// automatiquement libéré au commit/rollback — pas de risque de verrou orphelin
// même en cas de panic/crash pendant fn (la transaction ne commit jamais).
func (s *terminalPaymentStore) WithOrderLock(ctx context.Context, orderID string, fn func(txCtx context.Context) error) error {
	return dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, s.db)
		if _, err := db.ExecContext(txCtx, `SELECT pg_advisory_xact_lock(hashtext(?))`, orderID); err != nil {
			return fmt.Errorf("stripe terminal: acquire order lock: %w", err)
		}
		return fn(txCtx)
	})
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

func (s *terminalAccountStore) GetTerminalLocationID(ctx context.Context, merchantID string) (*string, error) {
	const q = `SELECT terminal_location_id FROM stripe_accounts WHERE merchant_id = ? LIMIT 1`

	var locationID sql.NullString
	err := s.db.QueryRowContext(ctx, dbx.Rebind(q), merchantID).Scan(&locationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !locationID.Valid || locationID.String == "" {
		return nil, nil
	}
	return &locationID.String, nil
}
