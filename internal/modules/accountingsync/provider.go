package accountingsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Modes d'authentification qu'un outil peut proposer.
const (
	AuthAPIToken = "api_token" // jeton d'API généré par le restaurateur dans son compte
	AuthOAuth    = "oauth"     // autorisation OAuth 2.0 (application enregistrée chez l'outil)
)

// Credentials sont les identifiants d'un établissement chez un outil. Stockés
// chiffrés (JSON puis AES-256-GCM), jamais renvoyés par l'API.
type Credentials struct {
	Kind         string     `json:"kind"`
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// ExpiresWithin indique si le jeton d'accès expire dans d (jamais pour un
// jeton d'API, qui n'expire pas).
func (c Credentials) ExpiresWithin(now time.Time, d time.Duration) bool {
	return c.Kind == AuthOAuth && c.ExpiresAt != nil && !c.ExpiresAt.After(now.Add(d))
}

// ProviderInfo décrit un outil au catalogue proposé aux établissements.
type ProviderInfo struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// AuthMethods : modes de connexion disponibles MAINTENANT (OAuth n'y figure
	// que si l'application de l'éditeur est configurée).
	AuthMethods []string `json:"auth_methods"`
	// SuggestedMapping : correspondance de comptes proposée par défaut à
	// l'écran de configuration (plan comptable général) ; à confirmer.
	SuggestedMapping Mapping `json:"suggested_mapping"`
}

// PrepareRequest demande à l'outil de résoudre la correspondance de comptes.
type PrepareRequest struct {
	JournalCode    string
	AccountNumbers []string
}

// Provider est un logiciel de comptabilité. Ajouter un outil = écrire un
// Provider et l'enregistrer (cmd/api/routes.go) ; rien d'autre ne change : ni
// la planification, ni le journal des envois, ni l'API, ni l'écran.
type Provider interface {
	Info() ProviderInfo

	// Validate contrôle les identifiants et renvoie le nom du compte chez
	// l'outil (ex. la société), affiché à l'établissement.
	Validate(ctx context.Context, creds Credentials) (accountLabel string, err error)

	// Prepare vérifie que le journal et les comptes existent chez l'outil et
	// renvoie l'état propre à l'outil (identifiants internes) que PushEntry
	// recevra. Erreur *UnknownAccountsError si des comptes manquent.
	Prepare(ctx context.Context, creds Credentials, req PrepareRequest) (state json.RawMessage, err error)

	// PushEntry crée l'écriture chez l'outil et renvoie son identifiant. Les
	// erreurs doivent être classées : ErrAuthRejected, *RejectedError (refus
	// avéré, rien n'a été créé) ou *AmbiguousError (on ne sait pas si
	// l'écriture existe).
	PushEntry(ctx context.Context, creds Credentials, state json.RawMessage, entry Entry) (externalID string, err error)
}

// OAuthProvider est un outil connectable par OAuth 2.0.
type OAuthProvider interface {
	Provider
	AuthURL(state string) (string, error)
	Exchange(ctx context.Context, code string) (Credentials, error)
	// Refresh renouvelle les jetons. Les jetons de rafraîchissement peuvent
	// être à usage unique : l'appelant enregistre le résultat avant tout autre
	// appel (voir Service.credentials).
	Refresh(ctx context.Context, creds Credentials) (Credentials, error)
	Revoke(ctx context.Context, creds Credentials) error
}

// EntryFinder retrouve une écriture déjà créée d'après sa référence : permet
// de lever le doute après un envoi ambigu au lieu de risquer un doublon.
type EntryFinder interface {
	FindEntry(ctx context.Context, creds Credentials, reference string) (externalID string, found bool, err error)
}

// Erreurs de classement des envois.
var (
	// ErrAuthRejected : l'outil refuse les identifiants (expirés, révoqués,
	// droits retirés). La connexion passe en erreur : reconnexion nécessaire.
	ErrAuthRejected = errors.New("accountingsync: identifiants refusés par l'outil")
)

// RejectedError : l'outil a répondu par un refus ; rien n'a été créé, l'envoi
// peut être retenté (après correction éventuelle de la configuration).
type RejectedError struct {
	Status  int
	Message string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("refusé par l'outil (HTTP %d) : %s", e.Status, e.Message)
}

// AmbiguousError : l'issue est inconnue (délai dépassé, erreur serveur, réponse
// illisible). L'écriture peut exister chez l'outil : ne jamais renvoyer sans
// vérification, au risque d'un doublon.
type AmbiguousError struct{ Err error }

func (e *AmbiguousError) Error() string { return "issue inconnue : " + e.Err.Error() }
func (e *AmbiguousError) Unwrap() error { return e.Err }

// UnknownAccountsError : comptes (ou journal) de la correspondance introuvables
// chez l'outil.
type UnknownAccountsError struct {
	Journal  string
	Accounts []string
}

func (e *UnknownAccountsError) Error() string {
	var parts []string
	if e.Journal != "" {
		parts = append(parts, "journal "+e.Journal)
	}
	if len(e.Accounts) > 0 {
		parts = append(parts, "comptes "+strings.Join(e.Accounts, ", "))
	}
	return "introuvable chez l'outil : " + strings.Join(parts, " ; ")
}

// Registry est le catalogue des outils disponibles.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry construit le catalogue. Une clé en double est une erreur de
// programmation : panique au démarrage plutôt qu'un outil masqué en silence.
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: map[string]Provider{}}
	for _, p := range providers {
		key := p.Info().Key
		if _, dup := r.providers[key]; dup {
			panic("accountingsync: outil enregistré deux fois : " + key)
		}
		r.providers[key] = p
	}
	return r
}

// Get renvoie l'outil de clé key.
func (r *Registry) Get(key string) (Provider, bool) {
	p, ok := r.providers[key]
	return p, ok
}

// List renvoie le catalogue, trié par clé.
func (r *Registry) List() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
