// Package pennylane est le connecteur Pennylane (API Company v2) de la
// synchronisation comptable : il crée, dans le journal de ventes de
// l'entreprise, l'écriture de ventes de chaque journée.
//
// Points propres à Pennylane :
//   - authentification par jeton d'API (généré par l'entreprise) ou OAuth 2.0
//     (application enregistrée par l'éditeur) ; en OAuth le jeton d'accès vit 24 h
//     et le jeton de rafraîchissement, à usage unique, 90 jours ;
//   - les identifiants de journaux et de comptes sont propres à chaque
//     entreprise : Prepare les résout à partir des numéros de la correspondance ;
//   - POST /ledger_entries n'a pas de clé d'idempotence : rejouer un envoi dont
//     l'issue est inconnue créerait un doublon (voir accountingsync.AmbiguousError
//     et FindEntry).
//
// Documentation : https://pennylane.readme.io (Create Ledger Entries, v2 scopes,
// Implement OAuth 2.0).
package pennylane

import (
	requestlogger "welloresto-api/internal/middleware/request_logger"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/modules/accountingsync"
)

// Key est la clé du connecteur dans le registry et en base.
const Key = "pennylane"

// Scopes demandés à l'autorisation OAuth : créer des écritures, lire les
// journaux et les comptes pour y résoudre la correspondance.
const oauthScopes = "ledger_entries:all journals:readonly ledger_accounts:readonly"

const (
	maxPages = 50
	pageSize = 100
)

// Config règle le connecteur.
type Config struct {
	// BaseURL : https://app.pennylane.com (ou l'adresse d'un environnement de test).
	BaseURL string
	// ClientID, ClientSecret, RedirectURI : application OAuth de l'éditeur ;
	// vides, seule la connexion par jeton d'API est proposée.
	ClientID     string
	ClientSecret string
	RedirectURI  string
	HTTPClient   *http.Client
	Now          func() time.Time
}

// Provider implémente accountingsync.Provider, OAuthProvider et EntryFinder.
type Provider struct {
	cfg  Config
	http *http.Client
	now  func() time.Time
}

// New construit le connecteur.
func New(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://app.pennylane.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	c := cfg.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second, Transport: requestlogger.Transport(nil)}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Provider{cfg: cfg, http: c, now: now}
}

func (p *Provider) oauthConfigured() bool {
	return p.cfg.ClientID != "" && p.cfg.ClientSecret != "" && p.cfg.RedirectURI != ""
}

// Info décrit l'outil au catalogue.
func (p *Provider) Info() accountingsync.ProviderInfo {
	methods := []string{accountingsync.AuthAPIToken}
	if p.oauthConfigured() {
		methods = append(methods, accountingsync.AuthOAuth)
	}
	return accountingsync.ProviderInfo{
		Key:         Key,
		Name:        "Pennylane",
		Description: "Envoie chaque jour l'écriture de ventes de la clôture dans le journal de ventes de Pennylane.",
		AuthMethods: methods,
		// Propositions du plan comptable général, à confirmer avec le comptable.
		SuggestedMapping: accountingsync.Mapping{
			JournalCode:       "VE",
			SalesAccounts:     map[string]string{accountingsync.DefaultKey: "707000"},
			VATAccounts:       map[string]string{accountingsync.DefaultKey: "445710"},
			PaymentAccounts:   map[string]string{accountingsync.DefaultKey: "580000", "ES": "531000"},
			DifferenceAccount: "471000",
		},
	}
}

// ---- Appels HTTP ----------------------------------------------------------

// apiError est la réponse d'erreur de l'API, classée.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return fmt.Sprintf("HTTP %d : %s", e.status, e.message) }

// call exécute un appel JSON authentifié. Une erreur de transport est
// renvoyée telle quelle (l'issue est inconnue), une réponse d'erreur en *apiError.
func (p *Provider) call(ctx context.Context, creds accountingsync.Credentials, method, path string, query url.Values, body, out any) error {
	u := p.cfg.BaseURL + "/api/external/v2" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{status: resp.StatusCode, message: errorMessage(raw)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func errorMessage(raw []byte) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Message != "" {
			return e.Message
		}
		if e.Error != "" {
			return e.Error
		}
	}
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

// classify traduit une erreur d'appel en erreur du noyau. mutating : l'appel
// crée quelque chose ; une issue inconnue y est ambiguë (doublon possible),
// alors qu'une lecture peut simplement être refaite.
func classify(err error, mutating bool) error {
	if err == nil {
		return nil
	}
	var ae *apiError
	if errors.As(err, &ae) {
		switch {
		case ae.status == http.StatusUnauthorized || ae.status == http.StatusForbidden:
			return fmt.Errorf("%w (%s)", accountingsync.ErrAuthRejected, ae.Error())
		case ae.status >= 500:
			if mutating {
				return &accountingsync.AmbiguousError{Err: err}
			}
			return err
		default:
			return &accountingsync.RejectedError{Status: ae.status, Message: ae.message}
		}
	}
	if mutating {
		return &accountingsync.AmbiguousError{Err: err}
	}
	return err
}

type page[T any] struct {
	Items      []T     `json:"items"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

// listAll parcourt toutes les pages d'une liste.
func listAll[T any](ctx context.Context, p *Provider, creds accountingsync.Credentials, path string, query url.Values) ([]T, error) {
	var all []T
	cursor := ""
	for i := 0; i < maxPages; i++ {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		q.Set("limit", strconv.Itoa(pageSize))
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var pg page[T]
		if err := p.call(ctx, creds, http.MethodGet, path, q, nil, &pg); err != nil {
			return nil, err
		}
		all = append(all, pg.Items...)
		if !pg.HasMore || pg.NextCursor == nil || *pg.NextCursor == "" {
			return all, nil
		}
		cursor = *pg.NextCursor
	}
	return all, fmt.Errorf("pennylane : plus de %d pages sur %s", maxPages, path)
}

// ---- Provider -------------------------------------------------------------

// Validate contrôle le jeton (GET /me, accessible à tout jeton valide) et
// renvoie le nom de l'entreprise.
func (p *Provider) Validate(ctx context.Context, creds accountingsync.Credentials) (string, error) {
	var me struct {
		Company struct {
			Name string `json:"name"`
		} `json:"company"`
	}
	if err := p.call(ctx, creds, http.MethodGet, "/me", nil, nil, &me); err != nil {
		return "", classify(err, false)
	}
	if me.Company.Name != "" {
		return me.Company.Name, nil
	}
	return "Pennylane", nil
}

type preparedState struct {
	JournalID int64            `json:"journal_id"`
	Accounts  map[string]int64 `json:"accounts"`
}

// Prepare résout le journal et les comptes de la correspondance.
func (p *Provider) Prepare(ctx context.Context, creds accountingsync.Credentials, req accountingsync.PrepareRequest) (json.RawMessage, error) {
	type journal struct {
		ID   int64  `json:"id"`
		Code string `json:"code"`
	}
	journals, err := listAll[journal](ctx, p, creds, "/journals", nil)
	if err != nil {
		return nil, classify(err, false)
	}
	st := preparedState{Accounts: map[string]int64{}}
	unknown := &accountingsync.UnknownAccountsError{}
	for _, j := range journals {
		if strings.EqualFold(strings.TrimSpace(j.Code), req.JournalCode) {
			st.JournalID = j.ID
			break
		}
	}
	if st.JournalID == 0 {
		unknown.Journal = req.JournalCode
	}

	for _, number := range req.AccountNumbers {
		id, err := p.resolveAccount(ctx, creds, number)
		if err != nil {
			return nil, classify(err, false)
		}
		if id == 0 {
			unknown.Accounts = append(unknown.Accounts, number)
			continue
		}
		st.Accounts[number] = id
	}
	if unknown.Journal != "" || len(unknown.Accounts) > 0 {
		return nil, unknown
	}
	return json.Marshal(st)
}

type ledgerAccount struct {
	ID      int64  `json:"id"`
	Number  string `json:"number"`
	Enabled *bool  `json:"enabled"`
}

// resolveAccount renvoie l'identifiant du compte de ce numéro (0 si absent).
// Filtre côté serveur d'abord ; si l'API refuse le filtre, parcours complet.
func (p *Provider) resolveAccount(ctx context.Context, creds accountingsync.Credentials, number string) (int64, error) {
	filter, _ := json.Marshal([]map[string]string{{"field": "number", "operator": "eq", "value": number}})
	accounts, err := listAll[ledgerAccount](ctx, p, creds, "/ledger_accounts", url.Values{"filter": {string(filter)}})
	var ae *apiError
	if errors.As(err, &ae) && ae.status == http.StatusBadRequest {
		accounts, err = listAll[ledgerAccount](ctx, p, creds, "/ledger_accounts", nil)
	}
	if err != nil {
		return 0, err
	}
	// Un même numéro peut avoir plusieurs identifiants (paramétrage de TVA
	// différent) : on prend le premier actif.
	for _, a := range accounts {
		if strings.TrimSpace(a.Number) == number && (a.Enabled == nil || *a.Enabled) {
			return a.ID, nil
		}
	}
	return 0, nil
}

type ledgerEntryLine struct {
	Debit           string `json:"debit"`
	Credit          string `json:"credit"`
	LedgerAccountID int64  `json:"ledger_account_id"`
	Label           string `json:"label"`
}

type ledgerEntryRequest struct {
	Date             string            `json:"date"`
	Label            string            `json:"label"`
	JournalID        int64             `json:"journal_id"`
	LedgerEntryLines []ledgerEntryLine `json:"ledger_entry_lines"`
}

// money écrit des centimes en euros décimaux (« 12.34 »), en chaîne comme
// l'exige l'API.
func money(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// PushEntry crée l'écriture dans le journal de ventes.
func (p *Provider) PushEntry(ctx context.Context, creds accountingsync.Credentials, state json.RawMessage, entry accountingsync.Entry) (string, error) {
	var st preparedState
	if err := json.Unmarshal(state, &st); err != nil || st.JournalID == 0 {
		return "", &accountingsync.RejectedError{Message: "configuration Pennylane absente : enregistrer à nouveau la correspondance de comptes"}
	}
	req := ledgerEntryRequest{Date: entry.Date, Label: entry.Label, JournalID: st.JournalID}
	for _, l := range entry.Lines {
		id, ok := st.Accounts[l.Account]
		if !ok {
			return "", &accountingsync.RejectedError{Message: "compte " + l.Account + " non résolu : enregistrer à nouveau la correspondance de comptes"}
		}
		req.LedgerEntryLines = append(req.LedgerEntryLines, ledgerEntryLine{
			Debit: money(l.Debit), Credit: money(l.Credit), LedgerAccountID: id, Label: l.Label,
		})
	}
	var out struct {
		ID json.Number `json:"id"`
	}
	if err := p.call(ctx, creds, http.MethodPost, "/ledger_entries", nil, req, &out); err != nil {
		return "", classify(err, true)
	}
	if out.ID.String() == "" {
		return "", &accountingsync.AmbiguousError{Err: errors.New("réponse sans identifiant d'écriture")}
	}
	return out.ID.String(), nil
}

// FindEntry cherche l'écriture d'un jour d'après la référence portée par son
// libellé. Sert à lever le doute après un envoi dont l'issue est inconnue.
func (p *Provider) FindEntry(ctx context.Context, creds accountingsync.Credentials, reference string) (string, bool, error) {
	// WR-<établissement>-<AAAAMMJJ> : la date de l'écriture est dans la référence.
	parts := strings.Split(reference, "-")
	day := parts[len(parts)-1]
	if len(day) != 8 {
		return "", false, fmt.Errorf("référence illisible : %q", reference)
	}
	date := day[0:4] + "-" + day[4:6] + "-" + day[6:8]
	filter, _ := json.Marshal([]map[string]string{{"field": "date", "operator": "eq", "value": date}})
	type entry struct {
		ID    json.Number `json:"id"`
		Label string      `json:"label"`
	}
	entries, err := listAll[entry](ctx, p, creds, "/ledger_entries", url.Values{"filter": {string(filter)}})
	if err != nil {
		return "", false, classify(err, false)
	}
	marker := "[" + reference + "]"
	for _, e := range entries {
		if strings.Contains(e.Label, marker) {
			return e.ID.String(), true, nil
		}
	}
	return "", false, nil
}

// ---- OAuth ----------------------------------------------------------------

// AuthURL renvoie l'adresse d'autorisation chez Pennylane.
func (p *Provider) AuthURL(state string) (string, error) {
	if !p.oauthConfigured() {
		return "", accountingsync.ErrAuthMethod
	}
	q := url.Values{
		"client_id":     {p.cfg.ClientID},
		"redirect_uri":  {p.cfg.RedirectURI},
		"response_type": {"code"},
		"scope":         {oauthScopes},
		"state":         {state},
	}
	return p.cfg.BaseURL + "/oauth/authorize?" + q.Encode(), nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (p *Provider) token(ctx context.Context, form url.Values) (accountingsync.Credentials, error) {
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return accountingsync.Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return accountingsync.Credentials{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return accountingsync.Credentials{}, err
	}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		// invalid_grant : code ou jeton de rafraîchissement périmé ou déjà utilisé.
		return accountingsync.Credentials{}, fmt.Errorf("%w (%s)", accountingsync.ErrAuthRejected, errorMessage(raw))
	}
	if resp.StatusCode != http.StatusOK {
		return accountingsync.Credentials{}, fmt.Errorf("pennylane oauth : HTTP %d : %s", resp.StatusCode, errorMessage(raw))
	}
	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil || tr.AccessToken == "" {
		return accountingsync.Credentials{}, errors.New("pennylane oauth : réponse sans jeton d'accès")
	}
	c := accountingsync.Credentials{Kind: accountingsync.AuthOAuth, AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken}
	if tr.ExpiresIn > 0 {
		exp := p.now().Add(time.Duration(tr.ExpiresIn) * time.Second)
		c.ExpiresAt = &exp
	}
	return c, nil
}

// Exchange échange le code d'autorisation contre les jetons.
func (p *Provider) Exchange(ctx context.Context, code string) (accountingsync.Credentials, error) {
	return p.token(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {p.cfg.RedirectURI},
	})
}

// Refresh renouvelle les jetons. L'ancien jeton de rafraîchissement n'est plus
// valable une fois utilisé : le nouveau doit être enregistré immédiatement.
func (p *Provider) Refresh(ctx context.Context, creds accountingsync.Credentials) (accountingsync.Credentials, error) {
	return p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {creds.RefreshToken}})
}

// Revoke révoque l'accès chez Pennylane.
func (p *Provider) Revoke(ctx context.Context, creds accountingsync.Credentials) error {
	form := url.Values{
		"client_id": {p.cfg.ClientID}, "client_secret": {p.cfg.ClientSecret}, "token": {creds.AccessToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/oauth/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pennylane oauth revoke : HTTP %d", resp.StatusCode)
	}
	return nil
}

var (
	_ accountingsync.Provider      = (*Provider)(nil)
	_ accountingsync.OAuthProvider = (*Provider)(nil)
	_ accountingsync.EntryFinder   = (*Provider)(nil)
)
