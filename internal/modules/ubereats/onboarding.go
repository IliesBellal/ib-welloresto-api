package ubereats

// Connexion d'un restaurant Uber Eats depuis le back-office (« Integration
// Activation » d'Uber, flux authorization_code + scope eats.pos_provisioning).
// Conception, décisions et protocole de test : docs/marketplace-onboarding.md.
//
//	back-office ── GET /integrations/uber-eats/connect ──▶ URL d'autorisation Uber (state signé)
//	Uber ── GET /integrations/uber-eats/callback?code&state ──▶ échange du code,
//	        liste des restaurants, session Redis 15 min ──▶ back-office ?uber=select&session=…
//	back-office ── POST /integrations/uber-eats/connect/sessions/{id} ──▶ POST pos_data
//	        par restaurant choisi + ligne integration_uber_eats active
//
// La déconnexion refait le passage chez Uber (purpose "disconnect") : le token
// du restaurateur n'est jamais conservé au-delà de la session, et DELETE
// pos_data l'exige.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"welloresto-api/internal/logger"

	"go.uber.org/zap"
)

const (
	// oauthScope : seul scope du flux authorization_code. Uber interdit de le
	// mélanger aux scopes client_credentials (eats.order, eats.store...).
	oauthScope = "eats.pos_provisioning"

	// stateTTL : durée de validité d'une demande de connexion (aller-retour
	// chez Uber compris).
	stateTTL = 15 * time.Minute
	// connectSessionTTL : temps laissé au restaurateur pour choisir ses
	// restaurants après son retour d'Uber.
	connectSessionTTL = 15 * time.Minute

	purposeConnect    = "connect"
	purposeDisconnect = "disconnect"

	connectSessionKeyPrefix = "uber:connect:session:"
	stateNonceKeyPrefix     = "uber:connect:state:"
)

var (
	// ErrOnboardingNotConfigured : identifiants de l'application, adresse de
	// retour ou Redis manquants.
	ErrOnboardingNotConfigured = errors.New("uber eats onboarding not configured")
	// ErrConnectSessionNotFound : session de choix expirée ou d'un autre établissement.
	ErrConnectSessionNotFound = errors.New("uber eats connect session not found")
	// ErrNoStoreSelected : aucun restaurant de la session n'a été demandé.
	ErrNoStoreSelected = errors.New("no uber eats store selected")
	// ErrAccountNotFound : compte Uber Eats inconnu pour cet établissement.
	ErrAccountNotFound = errors.New("uber eats account not found")

	errInvalidState = errors.New("invalid oauth state")
)

// Statuts d'un restaurant dans l'écran de choix.
const (
	StoreAvailable       = "available"        // peut être connecté
	StoreConnectedHere   = "connected_here"   // déjà relié à cet établissement (reconnexion possible)
	StoreLinkedElsewhere = "linked_elsewhere" // relié à un autre établissement Wello Resto : refusé
)

// Résultats d'un retour d'Uber, transmis au back-office (paramètre `uber`).
const (
	CallbackSelect       = "select"
	CallbackDisconnected = "disconnected"
	CallbackError        = "error"
)

// ConnectStoreOption : un restaurant du compte Uber Eats, tel que présenté au
// restaurateur.
type ConnectStoreOption struct {
	StoreID string `json:"store_id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Status  string `json:"status"`
	// OtherSoftware : Uber indique qu'un autre logiciel gère déjà les
	// commandes de ce restaurant.
	OtherSoftware bool `json:"other_software"`
}

// ConnectSessionView : la session de choix, sans le token.
type ConnectSessionView struct {
	Stores    []ConnectStoreOption `json:"stores"`
	ExpiresAt time.Time            `json:"expires_at"`
}

// ConnectResult : issue de la connexion d'un restaurant choisi.
type ConnectResult struct {
	StoreID string `json:"store_id"`
	Name    string `json:"name"`
	// Status : connected | linked_elsewhere | failed.
	Status string `json:"status"`
}

// CallbackOutcome : où renvoyer le restaurateur après son passage chez Uber.
type CallbackOutcome struct {
	Result  string // CallbackSelect | CallbackDisconnected | CallbackError
	Reason  string // pour CallbackError : denied, invalid_state, no_store, store_not_in_account, not_configured, failed
	Session string // pour CallbackSelect
}

type oauthState struct {
	Merchant string `json:"m"`
	User     string `json:"u"`
	Purpose  string `json:"p"`
	Store    string `json:"s,omitempty"`
	Nonce    string `json:"n"`
	Exp      int64  `json:"e"`
}

type connectSession struct {
	MerchantID string               `json:"merchant_id"`
	UserID     string               `json:"user_id"`
	Token      string               `json:"token"`
	Stores     []ConnectStoreOption `json:"stores"`
	ExpiresAt  time.Time            `json:"expires_at"`
}

// onboardingKV : stockage des sessions de choix et des state consommés
// (Redis ; remplaçable en test).
type onboardingKV interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key, value string, ttl time.Duration) bool
	SetNX(ctx context.Context, key, value string, ttl time.Duration) bool
	Delete(ctx context.Context, key string) bool
}

// onboardingRepo : écritures de la connexion (UberRepository ; remplaçable en test).
type onboardingRepo interface {
	ActiveStoreOwner(ctx context.Context, storeID string) (string, error)
	ActivateStore(ctx context.Context, merchantID, storeID string) error
	DeactivateStore(ctx context.Context, merchantID, storeID string) error
	DeactivateStoreEverywhere(ctx context.Context, storeID string) ([]string, error)
}

func (s *UberEatsService) onboardingStore() onboardingKV {
	if s.onbKV != nil {
		return s.onbKV
	}
	if s.redis == nil {
		return nil // pas d'interface non nil autour d'un pointeur nil
	}
	return s.redis
}

func (s *UberEatsService) onboardingRepository() onboardingRepo {
	if s.onbRepo != nil {
		return s.onbRepo
	}
	return s.repo
}

// OnboardingConfigured : la connexion depuis le back-office est possible.
func (s *UberEatsService) OnboardingConfigured() bool {
	c := s.config
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI != "" && c.ConnectReturnURL != "" && s.onboardingStore() != nil
}

// ConnectReturnURL : page du back-office où renvoyer le restaurateur.
func (s *UberEatsService) ConnectReturnURL() string { return s.config.ConnectReturnURL }

// ConnectURL renvoie l'adresse d'autorisation Uber vers laquelle le back-office
// redirige le restaurateur pour connecter ses restaurants.
func (s *UberEatsService) ConnectURL(merchantID, userID string) (string, error) {
	return s.authorizeURL(oauthState{Merchant: merchantID, User: userID, Purpose: purposeConnect})
}

// DisconnectURL renvoie l'adresse d'autorisation Uber qui, au retour, retire
// l'intégration du restaurant storeID (vide : compte principal).
func (s *UberEatsService) DisconnectURL(ctx context.Context, merchantID, userID, storeID string) (string, error) {
	if !s.OnboardingConfigured() {
		return "", ErrOnboardingNotConfigured
	}
	store, err := s.resolveStore(ctx, merchantID, storeID)
	if err != nil {
		return "", err
	}
	if store == nil {
		return "", ErrAccountNotFound
	}
	return s.authorizeURL(oauthState{Merchant: merchantID, User: userID, Purpose: purposeDisconnect, Store: store.StoreID})
}

func (s *UberEatsService) authorizeURL(st oauthState) (string, error) {
	if !s.OnboardingConfigured() {
		return "", ErrOnboardingNotConfigured
	}
	nonce, err := randomHex(16)
	if err != nil {
		return "", err
	}
	st.Nonce = nonce
	st.Exp = time.Now().Add(stateTTL).Unix()
	state, err := s.sealState(st)
	if err != nil {
		return "", err
	}

	u, err := url.Parse(s.config.AuthURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", s.config.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", s.config.RedirectURI)
	q.Set("scope", oauthScope)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// HandleOAuthCallback traite le retour d'Uber (requête publique : le navigateur
// du restaurateur, l'établissement est retrouvé dans le state signé).
func (s *UberEatsService) HandleOAuthCallback(ctx context.Context, query url.Values) CallbackOutcome {
	log := logger.FromContext(ctx)
	fail := func(reason string) CallbackOutcome { return CallbackOutcome{Result: CallbackError, Reason: reason} }

	if !s.OnboardingConfigured() {
		return fail("not_configured")
	}
	if e := query.Get("error"); e != "" {
		log.Info("uber eats onboarding: authorization refused", zap.String("error", e))
		return fail("denied")
	}
	st, err := s.openState(query.Get("state"))
	if err != nil {
		log.Warn("uber eats onboarding: invalid state", zap.Error(err))
		return fail("invalid_state")
	}
	// Un state ne sert qu'une fois (le code Uber aussi, mais autant ne pas
	// rejouer l'échange).
	if !s.onboardingStore().SetNX(ctx, stateNonceKeyPrefix+st.Nonce, "1", stateTTL) {
		log.Warn("uber eats onboarding: state already used", zap.String("merchant_id", st.Merchant))
		return fail("invalid_state")
	}
	code := query.Get("code")
	if code == "" {
		return fail("failed")
	}

	tokens, err := s.client.ExchangeAuthCode(ctx, code, s.config.RedirectURI)
	if err != nil {
		log.Error("uber eats onboarding: code exchange failed", zap.String("merchant_id", st.Merchant), zap.Error(err))
		return fail("failed")
	}
	stores, err := s.client.ListStores(ctx, tokens.AccessToken)
	if err != nil {
		log.Error("uber eats onboarding: list stores failed", zap.String("merchant_id", st.Merchant), zap.Error(err))
		return fail("failed")
	}

	switch st.Purpose {
	case purposeConnect:
		return s.startConnectSession(ctx, st, tokens.AccessToken, stores)
	case purposeDisconnect:
		return s.finishDisconnect(ctx, st, tokens.AccessToken, stores)
	default:
		return fail("invalid_state")
	}
}

func (s *UberEatsService) startConnectSession(ctx context.Context, st oauthState, token string, stores []UberStoreInfo) CallbackOutcome {
	log := logger.FromContext(ctx)
	if len(stores) == 0 {
		return CallbackOutcome{Result: CallbackError, Reason: "no_store"}
	}

	options := make([]ConnectStoreOption, 0, len(stores))
	for _, info := range stores {
		if info.StoreID == "" {
			continue
		}
		opt := ConnectStoreOption{
			StoreID: info.StoreID,
			Name:    strings.TrimSpace(info.Name),
			Address: formatStoreAddress(info),
			Status:  StoreAvailable,
		}
		owner, err := s.onboardingRepository().ActiveStoreOwner(ctx, info.StoreID)
		if err != nil {
			log.Error("uber eats onboarding: owner lookup failed", zap.String("store_id", info.StoreID), zap.Error(err))
			return CallbackOutcome{Result: CallbackError, Reason: "failed"}
		}
		switch {
		case owner == st.Merchant:
			opt.Status = StoreConnectedHere
		case owner != "":
			opt.Status = StoreLinkedElsewhere
		}
		if pd := info.PosData; pd != nil && pd.IntegrationEnabled && pd.OrderManagerClientID != "" && pd.OrderManagerClientID != s.config.ClientID {
			opt.OtherSoftware = true
		}
		options = append(options, opt)
	}
	if len(options) == 0 {
		return CallbackOutcome{Result: CallbackError, Reason: "no_store"}
	}

	id, err := randomHex(24)
	if err != nil {
		return CallbackOutcome{Result: CallbackError, Reason: "failed"}
	}
	sess := connectSession{
		MerchantID: st.Merchant,
		UserID:     st.User,
		Token:      token,
		Stores:     options,
		ExpiresAt:  time.Now().Add(connectSessionTTL).UTC(),
	}
	raw, _ := json.Marshal(sess)
	if !s.onboardingStore().Set(ctx, connectSessionKeyPrefix+id, string(raw), connectSessionTTL) {
		log.Error("uber eats onboarding: session not stored", zap.String("merchant_id", st.Merchant))
		return CallbackOutcome{Result: CallbackError, Reason: "failed"}
	}
	log.Info("uber eats onboarding: stores listed",
		zap.String("merchant_id", st.Merchant), zap.String("user_id", st.User), zap.Int("stores", len(options)))
	return CallbackOutcome{Result: CallbackSelect, Session: id}
}

func (s *UberEatsService) finishDisconnect(ctx context.Context, st oauthState, token string, stores []UberStoreInfo) CallbackOutcome {
	log := logger.FromContext(ctx)

	// Le compte Uber utilisé doit bien donner accès à ce restaurant : sinon
	// DELETE répondrait 404 et l'on désactiverait chez nous un restaurant qui
	// resterait relié chez Uber.
	found := false
	for _, info := range stores {
		if info.StoreID == st.Store {
			found = true
			break
		}
	}
	if !found {
		return CallbackOutcome{Result: CallbackError, Reason: "store_not_in_account"}
	}

	if err := s.client.DeprovisionStore(ctx, token, st.Store); err != nil && !errors.Is(err, ErrUberStoreNotFound) {
		log.Error("uber eats onboarding: deprovision failed",
			zap.String("merchant_id", st.Merchant), zap.String("store_id", st.Store), zap.Error(err))
		return CallbackOutcome{Result: CallbackError, Reason: "failed"}
	}
	if err := s.onboardingRepository().DeactivateStore(ctx, st.Merchant, st.Store); err != nil {
		log.Error("uber eats onboarding: local deactivation failed",
			zap.String("merchant_id", st.Merchant), zap.String("store_id", st.Store), zap.Error(err))
		return CallbackOutcome{Result: CallbackError, Reason: "failed"}
	}
	s.invalidateStoreCache(ctx, st.Store)
	log.Info("uber eats onboarding: store disconnected",
		zap.String("merchant_id", st.Merchant), zap.String("user_id", st.User), zap.String("store_id", st.Store))
	return CallbackOutcome{Result: CallbackDisconnected}
}

// GetConnectSession renvoie les restaurants à choisir (sans le token).
func (s *UberEatsService) GetConnectSession(ctx context.Context, merchantID, sessionID string) (*ConnectSessionView, error) {
	sess, err := s.loadSession(ctx, merchantID, sessionID)
	if err != nil {
		return nil, err
	}
	return &ConnectSessionView{Stores: sess.Stores, ExpiresAt: sess.ExpiresAt}, nil
}

// CompleteConnect active l'intégration sur les restaurants choisis et crée
// leurs comptes. Chaque restaurant est traité indépendamment ; la session est
// supprimée dès qu'au moins un restaurant est connecté (sinon le restaurateur
// peut réessayer tant qu'elle n'a pas expiré).
func (s *UberEatsService) CompleteConnect(ctx context.Context, merchantID, userID, sessionID string, storeIDs []string) ([]ConnectResult, error) {
	log := logger.FromContext(ctx)
	sess, err := s.loadSession(ctx, merchantID, sessionID)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]ConnectStoreOption, len(sess.Stores))
	for _, opt := range sess.Stores {
		byID[opt.StoreID] = opt
	}
	seen := make(map[string]bool, len(storeIDs))
	var selected []ConnectStoreOption
	for _, id := range storeIDs {
		opt, ok := byID[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		selected = append(selected, opt)
	}
	if len(selected) == 0 {
		return nil, ErrNoStoreSelected
	}

	results := make([]ConnectResult, 0, len(selected))
	connected := 0
	for _, opt := range selected {
		res := ConnectResult{StoreID: opt.StoreID, Name: opt.Name, Status: "failed"}

		// Revérifié au moment de connecter : un autre établissement a pu
		// relier ce restaurant depuis l'affichage de la liste.
		owner, err := s.onboardingRepository().ActiveStoreOwner(ctx, opt.StoreID)
		if err != nil {
			log.Error("uber eats onboarding: owner lookup failed", zap.String("store_id", opt.StoreID), zap.Error(err))
			results = append(results, res)
			continue
		}
		if owner != "" && owner != merchantID {
			res.Status = "linked_elsewhere"
			results = append(results, res)
			continue
		}

		if err := s.client.ProvisionStore(ctx, sess.Token, opt.StoreID, PosDataRequest{
			IsOrderManager:    true,
			IntegratorStoreID: merchantID,
		}); err != nil {
			log.Error("uber eats onboarding: provision failed",
				zap.String("merchant_id", merchantID), zap.String("store_id", opt.StoreID), zap.Error(err))
			results = append(results, res)
			continue
		}
		if err := s.onboardingRepository().ActivateStore(ctx, merchantID, opt.StoreID); err != nil {
			// Activé chez Uber mais pas chez nous : relancer la connexion
			// réessaie les deux (POST pos_data est rejouable).
			log.Error("uber eats onboarding: provisioned at Uber but local activation failed",
				zap.String("merchant_id", merchantID), zap.String("store_id", opt.StoreID), zap.Error(err))
			results = append(results, res)
			continue
		}
		s.invalidateStoreCache(ctx, opt.StoreID)
		res.Status = "connected"
		connected++
		results = append(results, res)
		log.Info("uber eats onboarding: store connected",
			zap.String("merchant_id", merchantID), zap.String("user_id", userID), zap.String("store_id", opt.StoreID))
	}

	if connected > 0 {
		s.onboardingStore().Delete(ctx, connectSessionKeyPrefix+sessionID)
	}
	return results, nil
}

// HandleStoreProvisioned : webhook store.provisioned. L'activation faite
// depuis le back-office a déjà créé le compte ; un restaurant activé côté Uber
// (pré-intégration par leurs équipes) sans compte chez nous est seulement
// signalé : rien ne dit à quel établissement il revient.
func (s *UberEatsService) HandleStoreProvisioned(ctx context.Context, storeID string) error {
	log := logger.FromContext(ctx)
	if storeID == "" {
		return nil
	}
	owner, err := s.onboardingRepository().ActiveStoreOwner(ctx, storeID)
	if err != nil {
		return err
	}
	if owner == "" {
		log.Warn("uber eats: store provisioned for Wello Resto but not linked to any establishment", zap.String("store_id", storeID))
		return nil
	}
	log.Info("uber eats: store provisioned", zap.String("store_id", storeID), zap.String("merchant_id", owner))
	return nil
}

// HandleStoreDeprovisioned : webhook store.deprovisioned (retrait fait depuis
// Wello Resto, par le restaurateur chez Uber ou par Uber). Les comptes actifs
// de ce restaurant sont désactivés.
func (s *UberEatsService) HandleStoreDeprovisioned(ctx context.Context, storeID string) error {
	if storeID == "" {
		return nil
	}
	merchants, err := s.onboardingRepository().DeactivateStoreEverywhere(ctx, storeID)
	if err != nil {
		return err
	}
	s.invalidateStoreCache(ctx, storeID)
	logger.FromContext(ctx).Info("uber eats: store deprovisioned",
		zap.String("store_id", storeID), zap.Strings("deactivated_for", merchants))
	return nil
}

func (s *UberEatsService) loadSession(ctx context.Context, merchantID, sessionID string) (*connectSession, error) {
	kv := s.onboardingStore()
	if kv == nil || sessionID == "" {
		return nil, ErrConnectSessionNotFound
	}
	raw, ok := kv.Get(ctx, connectSessionKeyPrefix+sessionID)
	if !ok {
		return nil, ErrConnectSessionNotFound
	}
	var sess connectSession
	if err := json.Unmarshal([]byte(raw), &sess); err != nil || sess.MerchantID != merchantID {
		return nil, ErrConnectSessionNotFound
	}
	return &sess, nil
}

// invalidateStoreCache purge le cache webhook du restaurant (GetByStoreID,
// 24 h) : sans cela, les commandes continueraient d'être routées vers
// l'ancien établissement après une reconnexion ailleurs.
func (s *UberEatsService) invalidateStoreCache(ctx context.Context, storeID string) {
	if kv := s.onboardingStore(); kv != nil {
		kv.Delete(ctx, storeCacheKey(storeID))
	}
}

func storeCacheKey(storeID string) string { return fmt.Sprintf("webhook:uber:store:%s", storeID) }

// ─── state signé ─────────────────────────────────────────────────────────────

// stateKey : clé HMAC du state, dérivée du client secret de l'application
// (déjà secret partagé avec Uber seul ; aucune clé supplémentaire à gérer).
func (s *UberEatsService) stateKey() []byte {
	sum := sha256.Sum256([]byte("welloresto/uber-eats/oauth-state/v1\x00" + s.config.ClientSecret))
	return sum[:]
}

func (s *UberEatsService) sealState(st oauthState) (string, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.stateKey())
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *UberEatsService) openState(state string) (oauthState, error) {
	var st oauthState
	payload, sig, ok := strings.Cut(state, ".")
	if !ok || payload == "" || sig == "" {
		return st, errInvalidState
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return st, errInvalidState
	}
	mac := hmac.New(sha256.New, s.stateKey())
	mac.Write([]byte(payload))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return st, errInvalidState
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return st, errInvalidState
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, errInvalidState
	}
	if st.Merchant == "" || st.Nonce == "" || time.Now().Unix() > st.Exp {
		return st, errInvalidState
	}
	if st.Purpose == purposeDisconnect && st.Store == "" {
		return st, errInvalidState
	}
	return st, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func formatStoreAddress(info UberStoreInfo) string {
	parts := []string{}
	if a := strings.TrimSpace(info.Location.Address); a != "" {
		parts = append(parts, a)
	}
	city := strings.TrimSpace(strings.TrimSpace(info.Location.PostalCode) + " " + strings.TrimSpace(info.Location.City))
	if city != "" {
		parts = append(parts, city)
	}
	return strings.Join(parts, ", ")
}
