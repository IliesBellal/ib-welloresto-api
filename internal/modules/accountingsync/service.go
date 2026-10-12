package accountingsync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	// closuresPerRun : jours envoyés au plus par établissement et par passage.
	closuresPerRun = 40
	// maxAttempts : essais horaires avant d'abandonner un jour (3 jours).
	maxAttempts = 72
	// itemsLimit : envois renvoyés au back-office.
	itemsLimit = 100
	// refreshMargin : renouvellement du jeton d'accès avant son expiration.
	refreshMargin = 10 * time.Minute
	// pushThrottle : pause entre deux envois (Pennylane limite à 25 requêtes / 5 s).
	pushThrottle = 300 * time.Millisecond
	// recordTimeout borne l'enregistrement de l'issue d'un envoi (voir syncDay).
	recordTimeout = 15 * time.Second
	// stateTTL : durée de validité du paramètre « state » d'une autorisation OAuth.
	stateTTL = 15 * time.Minute
)

// SecretBox chiffre les identifiants (implémenté par *helpers.SecretBox).
type SecretBox interface {
	Available() bool
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// Service gère les connexions comptables des établissements et leur envoi.
type Service struct {
	repo     Repository
	registry *Registry
	box      SecretBox
	log      *zap.Logger
	now      func() time.Time
	throttle time.Duration
}

// NewService construit le service. log peut être nil.
func NewService(repo Repository, registry *Registry, box SecretBox, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{repo: repo, registry: registry, box: box, log: log, now: time.Now, throttle: pushThrottle}
}

// Catalog renvoie les outils proposés aux établissements.
func (s *Service) Catalog() []ProviderInfo { return s.registry.List() }

func (s *Service) provider(key string) (Provider, error) {
	p, ok := s.registry.Get(key)
	if !ok {
		return nil, ErrUnknownProvider
	}
	return p, nil
}

// Connection renvoie la connexion de l'établissement (nil si aucune).
func (s *Service) Connection(ctx context.Context, merchantID string) (*ConnectionView, error) {
	c, err := s.repo.GetConnection(ctx, merchantID)
	if err != nil || c == nil {
		return nil, err
	}
	view := &ConnectionView{
		Provider: c.Provider, ProviderName: c.Provider, Status: c.Status, AccountLabel: c.AccountLabel,
		Mapping: c.Config, ConnectedAt: c.ConnectedAt, LastSyncAt: c.LastSyncAt, LastError: c.LastError,
	}
	if p, ok := s.registry.Get(c.Provider); ok {
		view.ProviderName = p.Info().Name
	}
	if c.SyncFrom != nil {
		d := c.SyncFrom.Format("2006-01-02")
		view.SyncFrom = &d
	}
	return view, nil
}

// ConnectWithToken relie l'établissement à un outil avec un jeton d'API.
func (s *Service) ConnectWithToken(ctx context.Context, merchantID, providerKey, token string) (*ConnectionView, error) {
	p, err := s.provider(providerKey)
	if err != nil {
		return nil, err
	}
	if !hasMethod(p.Info(), AuthAPIToken) {
		return nil, ErrAuthMethod
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w : jeton vide", ErrAuthMethod)
	}
	return s.connect(ctx, merchantID, p, Credentials{Kind: AuthAPIToken, AccessToken: token})
}

func (s *Service) connect(ctx context.Context, merchantID string, p Provider, creds Credentials) (*ConnectionView, error) {
	if !s.box.Available() {
		return nil, ErrNotConfigured
	}
	if existing, err := s.repo.GetConnection(ctx, merchantID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrAlreadyConnected
	}
	label, err := p.Validate(ctx, creds)
	if err != nil {
		return nil, err
	}
	sealed, err := s.seal(creds)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateConnection(ctx, Connection{
		MerchantID: merchantID, Provider: p.Info().Key, Credentials: sealed, AccountLabel: label,
	}); err != nil {
		return nil, err
	}
	return s.Connection(ctx, merchantID)
}

// OAuthStartURL renvoie l'adresse d'autorisation chez l'outil. Le paramètre
// « state » est chiffré et authentifié : il porte l'établissement et une
// échéance, donc le retour (non authentifié) de l'outil ne peut ni être forgé
// ni être rejoué pour un autre établissement.
func (s *Service) OAuthStartURL(ctx context.Context, merchantID, providerKey string) (string, error) {
	p, err := s.provider(providerKey)
	if err != nil {
		return "", err
	}
	op, ok := p.(OAuthProvider)
	if !ok || !hasMethod(p.Info(), AuthOAuth) {
		return "", ErrAuthMethod
	}
	if !s.box.Available() {
		return "", ErrNotConfigured
	}
	if existing, err := s.repo.GetConnection(ctx, merchantID); err != nil {
		return "", err
	} else if existing != nil {
		return "", ErrAlreadyConnected
	}
	state, err := s.sealState(merchantID, providerKey)
	if err != nil {
		return "", err
	}
	return op.AuthURL(state)
}

// OAuthComplete termine l'autorisation : échange le code et enregistre la
// connexion. Renvoie l'établissement concerné.
func (s *Service) OAuthComplete(ctx context.Context, state, code string) (string, error) {
	merchantID, providerKey, err := s.openState(state)
	if err != nil {
		return "", err
	}
	p, err := s.provider(providerKey)
	if err != nil {
		return merchantID, err
	}
	op, ok := p.(OAuthProvider)
	if !ok {
		return merchantID, ErrAuthMethod
	}
	creds, err := op.Exchange(ctx, code)
	if err != nil {
		return merchantID, err
	}
	_, err = s.connect(ctx, merchantID, p, creds)
	return merchantID, err
}

// SaveMapping enregistre la correspondance de comptes après l'avoir fait
// vérifier par l'outil, et active la synchronisation (à partir du jour même).
func (s *Service) SaveMapping(ctx context.Context, merchantID string, m Mapping) (*ConnectionView, error) {
	conn, err := s.repo.GetConnection(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, ErrNotConnected
	}
	m = m.Normalize()
	if err := m.Validate(); err != nil {
		return nil, err
	}
	p, err := s.provider(conn.Provider)
	if err != nil {
		return nil, err
	}
	creds, err := s.credentials(ctx, conn, p)
	if err != nil {
		return nil, err
	}
	state, err := p.Prepare(ctx, creds, PrepareRequest{JournalCode: m.JournalCode, AccountNumbers: m.AccountNumbers()})
	if err != nil {
		if errors.Is(err, ErrAuthRejected) {
			_ = s.repo.MarkError(ctx, merchantID, err.Error())
		}
		return nil, err
	}

	tz, err := s.repo.MerchantTimezone(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	loc, lerr := time.LoadLocation(tz)
	if tz == "" || lerr != nil {
		return nil, fmt.Errorf("fuseau horaire de l'établissement illisible (%q)", tz)
	}
	n := s.now().In(loc)
	syncFrom := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	if err := s.repo.SaveMapping(ctx, merchantID, m, state, syncFrom); err != nil {
		return nil, err
	}
	return s.Connection(ctx, merchantID)
}

// Disconnect supprime la connexion (et révoque l'accès chez l'outil si possible).
// Le journal des envois est conservé : reconnecter ne renvoie pas les jours déjà envoyés.
func (s *Service) Disconnect(ctx context.Context, merchantID string) error {
	conn, err := s.repo.GetConnection(ctx, merchantID)
	if err != nil {
		return err
	}
	if conn == nil {
		return ErrNotConnected
	}
	if p, ok := s.registry.Get(conn.Provider); ok {
		if op, ok := p.(OAuthProvider); ok {
			if creds, err := s.open(conn.Credentials); err == nil && creds.Kind == AuthOAuth {
				if err := op.Revoke(ctx, creds); err != nil {
					s.log.Warn("[accounting-sync] révocation de l'accès impossible", zap.String("merchant_id", merchantID), zap.Error(err))
				}
			}
		}
	}
	return s.repo.DeleteConnection(ctx, merchantID)
}

// Items renvoie les derniers envois de l'établissement.
func (s *Service) Items(ctx context.Context, merchantID string) ([]Item, error) {
	conn, err := s.repo.GetConnection(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return []Item{}, nil
	}
	return s.repo.ListItems(ctx, merchantID, conn.Provider, itemsLimit)
}

// RetryItem remet un jour en échec, incertain ou abandonné en file et relance
// la synchronisation de l'établissement. Pour un jour « incertain », l'appelant
// confirme avoir vérifié chez l'outil que l'écriture n'existe pas.
func (s *Service) RetryItem(ctx context.Context, merchantID, date string) error {
	conn, err := s.repo.GetConnection(ctx, merchantID)
	if err != nil {
		return err
	}
	if conn == nil {
		return ErrNotConnected
	}
	if err := s.repo.ResetItem(ctx, merchantID, conn.Provider, date); err != nil {
		return err
	}
	if conn.Status == StatusActive {
		s.SyncConnection(ctx, *conn)
	}
	return nil
}

// SyncNow lance tout de suite la synchronisation de l'établissement (bouton
// « Synchroniser maintenant ») au lieu d'attendre l'heure pleine suivante.
// Même chemin que la tâche horaire : même verrou, mêmes garde-fous.
func (s *Service) SyncNow(ctx context.Context, merchantID string) (Result, error) {
	conn, err := s.repo.GetConnection(ctx, merchantID)
	if err != nil {
		return Result{}, err
	}
	if conn == nil {
		return Result{}, ErrNotConnected
	}
	if conn.Status != StatusActive {
		return Result{}, ErrNotActive
	}
	return s.SyncConnection(ctx, *conn), nil
}

// Result résume un passage de RunAll.
type Result struct {
	Connections int
	Sent        int
	Failed      int
	Uncertain   int
	// Busy : un autre envoi tenait déjà le verrou de l'établissement, rien n'a été fait.
	Busy bool
}

// RunAll synchronise toutes les connexions actives (tâche horaire).
func (s *Service) RunAll(ctx context.Context) (Result, error) {
	var res Result
	conns, err := s.repo.ListActive(ctx)
	if err != nil {
		return res, fmt.Errorf("lecture des connexions: %w", err)
	}
	for _, c := range conns {
		if ctx.Err() != nil {
			break
		}
		res.Connections++
		r := s.SyncConnection(ctx, c)
		res.Sent += r.Sent
		res.Failed += r.Failed
		res.Uncertain += r.Uncertain
	}
	return res, nil
}

// SyncConnection envoie les journées clôturées pas encore envoyées d'une
// connexion active. Une erreur sur un jour ne bloque pas les suivants.
func (s *Service) SyncConnection(ctx context.Context, conn Connection) Result {
	var res Result
	log := s.log.With(zap.String("merchant_id", conn.MerchantID), zap.String("provider", conn.Provider))

	release, locked, err := s.repo.TryLockSync(ctx, conn.MerchantID)
	if err != nil {
		log.Error("[accounting-sync] verrou d'envoi impossible", zap.Error(err))
		return res
	}
	if !locked {
		log.Debug("[accounting-sync] un autre envoi est en cours pour cet établissement")
		res.Busy = true
		return res
	}
	defer release()

	p, err := s.provider(conn.Provider)
	if err != nil {
		log.Error("[accounting-sync] outil inconnu, connexion ignorée")
		return res
	}
	if conn.SyncFrom == nil {
		log.Error("[accounting-sync] connexion active sans date de départ, ignorée")
		return res
	}
	creds, err := s.credentials(ctx, &conn, p)
	if err != nil {
		if errors.Is(err, ErrAuthRejected) {
			_ = s.repo.MarkError(ctx, conn.MerchantID, err.Error())
			log.Warn("[accounting-sync] identifiants refusés, reconnexion nécessaire", zap.Error(err))
			return res
		}
		log.Error("[accounting-sync] identifiants illisibles", zap.Error(err))
		_ = s.repo.RecordSync(ctx, conn.MerchantID, err.Error())
		return res
	}

	closures, err := s.repo.PendingClosures(ctx, conn.MerchantID, conn.Provider, *conn.SyncFrom, maxAttempts, closuresPerRun)
	if err != nil {
		log.Error("[accounting-sync] lecture des clôtures échouée", zap.Error(err))
		return res
	}

	var lastErr string
	for i, c := range closures {
		if ctx.Err() != nil {
			break
		}
		if i > 0 && s.throttle > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(s.throttle):
			}
		}
		outcome, stop := s.syncDay(ctx, log, p, creds, conn, c)
		switch outcome {
		case ItemSent:
			res.Sent++
		case ItemFailed, ItemAbandoned:
			res.Failed++
		case ItemUncertain:
			res.Uncertain++
		}
		if stop != "" {
			lastErr = stop
			break
		}
	}
	if lastErr == "" && (res.Failed > 0 || res.Uncertain > 0) {
		lastErr = fmt.Sprintf("%d jour(s) en échec, %d à vérifier", res.Failed, res.Uncertain)
	}
	if err := s.repo.RecordSync(ctx, conn.MerchantID, lastErr); err != nil {
		log.Error("[accounting-sync] enregistrement du passage impossible", zap.Error(err))
	}
	return res
}

// syncDay envoie une journée. Renvoie le statut enregistré et, si la connexion
// ne doit plus être traitée ce passage, le motif.
func (s *Service) syncDay(ctx context.Context, log *zap.Logger, p Provider, creds Credentials, conn Connection, c Closure) (status, stop string) {
	update := ItemUpdate{MerchantID: conn.MerchantID, Provider: conn.Provider, Date: c.Date, ClosureID: c.ID, ClosureHash: c.Hash}
	record := func(status string) string {
		update.Status = status
		// L'issue d'un envoi doit être enregistrée même si la requête ou la tâche
		// est annulée entre-temps : sinon un jour déjà créé chez l'outil serait
		// renvoyé au passage suivant (doublon).
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
		defer cancel()
		if err := s.repo.RecordItem(rctx, update); err != nil {
			log.Error("[accounting-sync] enregistrement de l'envoi impossible", zap.String("date", c.Date), zap.Error(err))
		}
		return status
	}
	fail := func(err error) string {
		update.LastError = err.Error()
		if c.Attempts+1 >= maxAttempts {
			log.Error("[accounting-sync] jour abandonné après trop d'échecs", zap.String("date", c.Date), zap.Error(err))
			return record(ItemAbandoned)
		}
		log.Warn("[accounting-sync] envoi en échec, nouvel essai à la prochaine heure", zap.String("date", c.Date), zap.Error(err))
		return record(ItemFailed)
	}

	sales := DailySales{MerchantID: conn.MerchantID, Date: c.Date, ClosureID: c.ID, ClosureHash: c.Hash}
	if err := json.Unmarshal(c.VATRaw, &sales.VATByRate); err != nil {
		return fail(fmt.Errorf("TVA de la clôture illisible: %w", err)), ""
	}
	if err := json.Unmarshal(c.PaymentsRaw, &sales.PaymentsByOp); err != nil {
		return fail(fmt.Errorf("paiements de la clôture illisibles: %w", err)), ""
	}

	entry, err := BuildEntry(sales, conn.Config)
	if errors.Is(err, ErrNothingToPost) {
		return record(ItemSkipped), ""
	}
	if err != nil {
		return fail(err), ""
	}
	update.Difference = entry.Difference
	update.Entry, _ = json.Marshal(entry)

	id, err := p.PushEntry(ctx, creds, conn.State, entry)
	if err == nil {
		update.ExternalID = id
		return record(ItemSent), ""
	}

	var rejected *RejectedError
	switch {
	case errors.Is(err, ErrAuthRejected):
		_ = s.repo.MarkError(ctx, conn.MerchantID, err.Error())
		log.Warn("[accounting-sync] identifiants refusés, reconnexion nécessaire", zap.Error(err))
		return "", err.Error()
	case errors.As(err, &rejected):
		return fail(err), ""
	}

	// Issue inconnue : l'écriture a peut-être été créée. On la cherche avant
	// toute chose ; sans moyen de la retrouver, on n'envoie PAS une seconde fois.
	if finder, ok := p.(EntryFinder); ok {
		externalID, found, ferr := finder.FindEntry(ctx, creds, entry.Reference)
		switch {
		case ferr == nil && found:
			update.ExternalID = externalID
			return record(ItemSent), ""
		case ferr == nil:
			return fail(err), "" // elle n'existe pas : sûr de réessayer
		}
		log.Warn("[accounting-sync] recherche de l'écriture impossible", zap.String("date", c.Date), zap.Error(ferr))
	}
	update.LastError = err.Error()
	log.Error("[accounting-sync] issue de l'envoi inconnue, à vérifier chez l'outil", zap.String("date", c.Date), zap.Error(err))
	return record(ItemUncertain), ""
}

// credentials déchiffre les identifiants de la connexion et, s'ils sont OAuth
// proches de l'expiration, les renouvelle. Les jetons de rafraîchissement sont
// à usage unique : le renouvellement se fait sous verrou de la ligne, avec
// relecture après verrou (une autre instance a pu renouveler entre-temps), et
// le nouveau jeton est écrit AVANT toute autre utilisation.
func (s *Service) credentials(ctx context.Context, conn *Connection, p Provider) (Credentials, error) {
	creds, err := s.open(conn.Credentials)
	if err != nil {
		return Credentials{}, err
	}
	if !creds.ExpiresWithin(s.now(), refreshMargin) {
		return creds, nil
	}
	op, ok := p.(OAuthProvider)
	if !ok {
		return creds, nil
	}
	err = s.repo.WithCredentialsLock(ctx, conn.MerchantID, func(current []byte) ([]byte, error) {
		cur, err := s.open(current)
		if err != nil {
			return nil, err
		}
		if !cur.ExpiresWithin(s.now(), refreshMargin) {
			creds = cur
			return nil, nil
		}
		renewed, err := op.Refresh(ctx, cur)
		if err != nil {
			return nil, err
		}
		sealed, err := s.seal(renewed)
		if err != nil {
			return nil, err
		}
		creds = renewed
		return sealed, nil
	})
	if err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

func (s *Service) seal(c Credentials) ([]byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return s.box.Encrypt(raw)
}

func (s *Service) open(sealed []byte) (Credentials, error) {
	raw, err := s.box.Decrypt(sealed)
	if err != nil {
		return Credentials{}, fmt.Errorf("déchiffrement des identifiants: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return Credentials{}, fmt.Errorf("identifiants illisibles: %w", err)
	}
	return c, nil
}

type statePayload struct {
	Merchant string `json:"m"`
	Provider string `json:"p"`
	Expires  int64  `json:"e"`
}

func (s *Service) sealState(merchantID, providerKey string) (string, error) {
	raw, _ := json.Marshal(statePayload{Merchant: merchantID, Provider: providerKey, Expires: s.now().Add(stateTTL).Unix()})
	sealed, err := s.box.Encrypt(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) openState(state string) (merchantID, providerKey string, err error) {
	sealed, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", "", ErrInvalidState
	}
	raw, err := s.box.Decrypt(sealed)
	if err != nil {
		return "", "", ErrInvalidState
	}
	var p statePayload
	if err := json.Unmarshal(raw, &p); err != nil || p.Merchant == "" || s.now().Unix() > p.Expires {
		return "", "", ErrInvalidState
	}
	return p.Merchant, p.Provider, nil
}

func hasMethod(info ProviderInfo, method string) bool {
	for _, m := range info.AuthMethods {
		if m == method {
			return true
		}
	}
	return false
}
