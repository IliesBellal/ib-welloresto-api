package accountingsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---- Doubles ---------------------------------------------------------------

type memBox struct{ ok bool }

func (b memBox) Available() bool { return b.ok }
func (b memBox) Encrypt(p []byte) ([]byte, error) {
	return append([]byte("enc:"), p...), nil
}
func (b memBox) Decrypt(c []byte) ([]byte, error) {
	if !strings.HasPrefix(string(c), "enc:") {
		return nil, errors.New("authentication failed")
	}
	return c[4:], nil
}

type memRepo struct {
	conn     *Connection
	closures []Closure
	items    map[string]*ItemUpdate // par date
	attempts map[string]int
	tz       string
	syncErr  string
	lockHits int
	locked   bool // un autre envoi tient le verrou
}

func newMemRepo() *memRepo {
	return &memRepo{items: map[string]*ItemUpdate{}, attempts: map[string]int{}, tz: "Europe/Paris"}
}

func (r *memRepo) GetConnection(context.Context, string) (*Connection, error) {
	if r.conn == nil {
		return nil, nil
	}
	c := *r.conn
	return &c, nil
}
func (r *memRepo) CreateConnection(_ context.Context, c Connection) error {
	if r.conn != nil {
		return ErrAlreadyConnected
	}
	c.Status = StatusSetup
	r.conn = &c
	return nil
}
func (r *memRepo) SaveMapping(_ context.Context, _ string, m Mapping, st json.RawMessage, from time.Time) error {
	r.conn.Config, r.conn.State, r.conn.Status = m, st, StatusActive
	if r.conn.SyncFrom == nil {
		r.conn.SyncFrom = &from
	}
	return nil
}
func (r *memRepo) WithCredentialsLock(_ context.Context, _ string, fn func([]byte) ([]byte, error)) error {
	r.lockHits++
	updated, err := fn(r.conn.Credentials)
	if err != nil {
		return err
	}
	if updated != nil {
		r.conn.Credentials = updated
	}
	return nil
}
func (r *memRepo) MarkError(_ context.Context, _, reason string) error {
	r.conn.Status, r.conn.LastError = StatusError, reason
	return nil
}
func (r *memRepo) RecordSync(_ context.Context, _, lastError string) error {
	r.syncErr = lastError
	return nil
}
func (r *memRepo) DeleteConnection(context.Context, string) error { r.conn = nil; return nil }
func (r *memRepo) ListActive(context.Context) ([]Connection, error) {
	if r.conn != nil && r.conn.Status == StatusActive {
		return []Connection{*r.conn}, nil
	}
	return nil, nil
}
func (r *memRepo) TryLockSync(context.Context, string) (func(), bool, error) {
	if r.locked {
		return nil, false, nil
	}
	return func() {}, true, nil
}
func (r *memRepo) MerchantTimezone(context.Context, string) (string, error) { return r.tz, nil }
func (r *memRepo) PendingClosures(_ context.Context, _, _ string, from time.Time, max, limit int) ([]Closure, error) {
	var out []Closure
	for _, c := range r.closures {
		it := r.items[c.Date]
		if it != nil && !(it.Status == ItemFailed && r.attempts[c.Date] < max) {
			continue
		}
		c.Attempts = r.attempts[c.Date]
		out = append(out, c)
	}
	return out, nil
}
func (r *memRepo) RecordItem(ctx context.Context, u ItemUpdate) error {
	if err := ctx.Err(); err != nil {
		return err // comme Postgres : une requête sur un contexte annulé échoue
	}
	r.items[u.Date] = &u
	r.attempts[u.Date]++
	return nil
}
func (r *memRepo) ListItems(context.Context, string, string, int) ([]Item, error) { return nil, nil }
func (r *memRepo) ResetItem(_ context.Context, _, _ string, date string) error {
	it, ok := r.items[date]
	if !ok {
		return ErrItemNotFound
	}
	if it.Status == ItemSent {
		return ErrItemNotRetryable
	}
	it.Status = ItemFailed
	r.attempts[date] = 0
	return nil
}

type fakeProvider struct {
	pushes   []string // références envoyées
	pushErr  func(entry Entry) error
	prepErr  error
	finder   func(ref string) (string, bool, error)
	refresh  int
	refreshE error
	revoked  bool
}

func (p *fakeProvider) Info() ProviderInfo {
	return ProviderInfo{Key: "fake", Name: "Fake", AuthMethods: []string{AuthAPIToken, AuthOAuth}}
}
func (p *fakeProvider) Validate(context.Context, Credentials) (string, error) {
	return "Société Test", nil
}
func (p *fakeProvider) Prepare(context.Context, Credentials, PrepareRequest) (json.RawMessage, error) {
	if p.prepErr != nil {
		return nil, p.prepErr
	}
	return json.RawMessage(`{"journal":1}`), nil
}
func (p *fakeProvider) PushEntry(_ context.Context, _ Credentials, _ json.RawMessage, e Entry) (string, error) {
	p.pushes = append(p.pushes, e.Reference)
	if p.pushErr != nil {
		if err := p.pushErr(e); err != nil {
			return "", err
		}
	}
	return "ext-" + e.Reference, nil
}
func (p *fakeProvider) AuthURL(state string) (string, error) {
	return "https://auth/?state=" + state, nil
}
func (p *fakeProvider) Exchange(context.Context, string) (Credentials, error) {
	exp := time.Now().Add(24 * time.Hour)
	return Credentials{Kind: AuthOAuth, AccessToken: "a", RefreshToken: "r", ExpiresAt: &exp}, nil
}
func (p *fakeProvider) Refresh(context.Context, Credentials) (Credentials, error) {
	p.refresh++
	if p.refreshE != nil {
		return Credentials{}, p.refreshE
	}
	exp := time.Now().Add(24 * time.Hour)
	return Credentials{Kind: AuthOAuth, AccessToken: fmt.Sprintf("a%d", p.refresh), RefreshToken: fmt.Sprintf("r%d", p.refresh), ExpiresAt: &exp}, nil
}
func (p *fakeProvider) Revoke(context.Context, Credentials) error { p.revoked = true; return nil }

// finderProvider ajoute EntryFinder à fakeProvider.
type finderProvider struct{ *fakeProvider }

func (p finderProvider) FindEntry(_ context.Context, _ Credentials, ref string) (string, bool, error) {
	return p.finder(ref)
}

func closure(date string, ttc int64) Closure {
	ht, tva := ttc*10/11, ttc-ttc*10/11
	return Closure{
		ID: 1, Date: date, Hash: "h-" + date,
		VATRaw:      []byte(fmt.Sprintf(`[{"rate":10,"ttc":%d,"ht":%d,"tva":%d}]`, ttc, ht, tva)),
		PaymentsRaw: []byte(fmt.Sprintf(`[{"mop":"CB","operation_type":"SALE","enabled":true,"amount":%d,"count":1}]`, ttc)),
	}
}

func setup(t *testing.T, p Provider) (*Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	sealed, _ := memBox{ok: true}.Encrypt([]byte(`{"kind":"api_token","access_token":"tok"}`))
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	repo.conn = &Connection{
		MerchantID: "212", Provider: "fake", Status: StatusActive, Credentials: sealed,
		Config: testMapping(), State: json.RawMessage(`{}`), SyncFrom: &from,
	}
	svc := NewService(repo, NewRegistry(p), memBox{ok: true}, nil)
	svc.throttle = 0
	return svc, repo
}

func sync(t *testing.T, svc *Service, repo *memRepo) Result {
	t.Helper()
	conn, _ := repo.GetConnection(context.Background(), "212")
	return svc.SyncConnection(context.Background(), *conn)
}

// ---- Synchronisation -------------------------------------------------------

func TestSync_SendsPendingDaysAndSkipsEmptyOnes(t *testing.T) {
	p := &fakeProvider{}
	svc, repo := setup(t, p)
	repo.closures = []Closure{closure("2026-10-10", 11000), closure("2026-10-11", 0), closure("2026-10-12", 5500)}
	repo.closures[1].VATRaw, repo.closures[1].PaymentsRaw = []byte(`[]`), []byte(`[]`)

	res := sync(t, svc, repo)
	if res.Sent != 2 || res.Failed != 0 {
		t.Fatalf("résultat = %+v", res)
	}
	if got := strings.Join(p.pushes, ","); got != "WR-212-20261010,WR-212-20261012" {
		t.Errorf("envois = %s", got)
	}
	if repo.items["2026-10-11"].Status != ItemSkipped {
		t.Errorf("jour vide = %s, attendu skipped", repo.items["2026-10-11"].Status)
	}
	if repo.items["2026-10-10"].ExternalID != "ext-WR-212-20261010" || repo.items["2026-10-10"].ClosureHash != "h-2026-10-10" {
		t.Errorf("envoi enregistré = %+v", repo.items["2026-10-10"])
	}

	// Rejoué : rien ne repart, pas de doublon.
	p.pushes = nil
	sync(t, svc, repo)
	if len(p.pushes) != 0 {
		t.Errorf("jours déjà envoyés renvoyés : %v", p.pushes)
	}
}

// La requête est annulée juste après la création de l'écriture chez l'outil :
// l'envoi doit quand même être enregistré, sinon le jour serait renvoyé (doublon).
func TestSync_RecordsOutcomeEvenWhenRequestIsCancelledMidPush(t *testing.T) {
	p := &fakeProvider{}
	svc, repo := setup(t, p)
	repo.closures = []Closure{closure("2026-10-10", 11000)}
	ctx, cancel := context.WithCancel(context.Background())
	p.pushErr = func(Entry) error { cancel(); return nil } // créée chez l'outil, puis le navigateur part

	conn, _ := repo.GetConnection(context.Background(), "212")
	svc.SyncConnection(ctx, *conn)
	if it := repo.items["2026-10-10"]; it == nil || it.Status != ItemSent {
		t.Fatalf("jour envoyé mais non enregistré : %+v", it)
	}
	p.pushes = nil
	sync(t, svc, repo)
	if len(p.pushes) != 0 {
		t.Errorf("jour renvoyé au passage suivant : %v", p.pushes)
	}
}

func TestSyncNow(t *testing.T) {
	p := &fakeProvider{}
	svc, repo := setup(t, p)
	repo.closures = []Closure{closure("2026-10-10", 11000), closure("2026-10-11", 5500)}

	res, err := svc.SyncNow(context.Background(), "212")
	if err != nil || res.Sent != 2 || res.Busy {
		t.Fatalf("résultat %+v err=%v", res, err)
	}
	// Rien de nouveau : rien n'est renvoyé.
	if res, _ = svc.SyncNow(context.Background(), "212"); res.Sent != 0 || len(p.pushes) != 2 {
		t.Errorf("seconde synchro : %+v, envois %d", res, len(p.pushes))
	}
	// Un autre traitement tient le verrou.
	repo.locked = true
	if res, _ = svc.SyncNow(context.Background(), "212"); !res.Busy {
		t.Errorf("verrou occupé non signalé : %+v", res)
	}
	repo.locked = false

	// Connexion non active : refusée.
	repo.conn.Status = StatusSetup
	if _, err := svc.SyncNow(context.Background(), "212"); !errors.Is(err, ErrNotActive) {
		t.Errorf("connexion en configuration : %v", err)
	}
	repo.conn = nil
	if _, err := svc.SyncNow(context.Background(), "212"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("sans connexion : %v", err)
	}
}

func TestSync_SkipsWhenAnotherSyncHoldsTheLock(t *testing.T) {
	p := &fakeProvider{}
	svc, repo := setup(t, p)
	repo.closures = []Closure{closure("2026-10-10", 11000)}
	repo.locked = true
	sync(t, svc, repo)
	if len(p.pushes) != 0 {
		t.Fatalf("envoi malgré le verrou d'un autre traitement : %v", p.pushes)
	}
}

func TestSync_RejectedIsRetriedThenAbandoned(t *testing.T) {
	p := &fakeProvider{pushErr: func(Entry) error { return &RejectedError{Status: 422, Message: "Entry lines are not balanced"} }}
	svc, repo := setup(t, p)
	repo.closures = []Closure{closure("2026-10-10", 11000)}

	sync(t, svc, repo)
	if it := repo.items["2026-10-10"]; it.Status != ItemFailed || !strings.Contains(it.LastError, "not balanced") {
		t.Fatalf("après un refus : %+v", it)
	}
	// Corrigé côté outil : le passage suivant renvoie.
	p.pushErr = nil
	if res := sync(t, svc, repo); res.Sent != 1 {
		t.Fatalf("renvoi après correction : %+v", res)
	}

	// Trop d'échecs : abandon.
	p2 := &fakeProvider{pushErr: func(Entry) error { return &RejectedError{Status: 404, Message: "journal introuvable"} }}
	svc2, repo2 := setup(t, p2)
	repo2.closures = []Closure{closure("2026-10-10", 11000)}
	repo2.attempts["2026-10-10"] = maxAttempts - 1
	repo2.items["2026-10-10"] = &ItemUpdate{Status: ItemFailed}
	sync(t, svc2, repo2)
	if repo2.items["2026-10-10"].Status != ItemAbandoned {
		t.Errorf("statut = %s, attendu abandoned", repo2.items["2026-10-10"].Status)
	}
}

// Issue inconnue sans moyen de vérifier : jamais renvoyé automatiquement.
func TestSync_AmbiguousIsNeverResentBlindly(t *testing.T) {
	p := &fakeProvider{pushErr: func(Entry) error { return &AmbiguousError{Err: errors.New("context deadline exceeded")} }}
	svc, repo := setup(t, p)
	repo.closures = []Closure{closure("2026-10-10", 11000)}

	res := sync(t, svc, repo)
	if res.Uncertain != 1 || repo.items["2026-10-10"].Status != ItemUncertain {
		t.Fatalf("résultat %+v, statut %s", res, repo.items["2026-10-10"].Status)
	}
	p.pushes = nil
	sync(t, svc, repo)
	sync(t, svc, repo)
	if len(p.pushes) != 0 {
		t.Fatalf("un jour incertain a été renvoyé %d fois : risque de doublon", len(p.pushes))
	}

	// Relance manuelle après vérification chez l'outil.
	p.pushErr = nil
	if err := svc.RetryItem(context.Background(), "212", "2026-10-10"); err != nil {
		t.Fatal(err)
	}
	if repo.items["2026-10-10"].Status != ItemSent {
		t.Errorf("après relance manuelle : %s", repo.items["2026-10-10"].Status)
	}
}

func TestSync_AmbiguousResolvedByFinder(t *testing.T) {
	base := &fakeProvider{pushErr: func(Entry) error { return &AmbiguousError{Err: errors.New("timeout")} }}

	// L'écriture existe : enregistrée comme envoyée, sans second envoi.
	found := finderProvider{base}
	base.finder = func(string) (string, bool, error) { return "99", true, nil }
	svc, repo := setup(t, found)
	repo.closures = []Closure{closure("2026-10-10", 11000)}
	sync(t, svc, repo)
	if it := repo.items["2026-10-10"]; it.Status != ItemSent || it.ExternalID != "99" {
		t.Fatalf("écriture retrouvée : %+v", it)
	}
	if len(base.pushes) != 1 {
		t.Errorf("envois = %d, attendu 1", len(base.pushes))
	}

	// Elle n'existe pas : sûr de réessayer.
	base2 := &fakeProvider{pushErr: func(Entry) error { return &AmbiguousError{Err: errors.New("timeout")} }}
	base2.finder = func(string) (string, bool, error) { return "", false, nil }
	svc2, repo2 := setup(t, finderProvider{base2})
	repo2.closures = []Closure{closure("2026-10-10", 11000)}
	sync(t, svc2, repo2)
	if repo2.items["2026-10-10"].Status != ItemFailed {
		t.Errorf("écriture absente : %s, attendu failed (retentable)", repo2.items["2026-10-10"].Status)
	}

	// La recherche elle-même échoue : incertain.
	base3 := &fakeProvider{pushErr: func(Entry) error { return &AmbiguousError{Err: errors.New("timeout")} }}
	base3.finder = func(string) (string, bool, error) { return "", false, errors.New("filtre refusé") }
	svc3, repo3 := setup(t, finderProvider{base3})
	repo3.closures = []Closure{closure("2026-10-10", 11000)}
	sync(t, svc3, repo3)
	if repo3.items["2026-10-10"].Status != ItemUncertain {
		t.Errorf("recherche impossible : %s, attendu uncertain", repo3.items["2026-10-10"].Status)
	}
}

func TestSync_AuthRejectedStopsAndFlagsConnection(t *testing.T) {
	p := &fakeProvider{pushErr: func(Entry) error { return fmt.Errorf("%w (HTTP 401)", ErrAuthRejected) }}
	svc, repo := setup(t, p)
	repo.closures = []Closure{closure("2026-10-10", 11000), closure("2026-10-11", 11000)}

	sync(t, svc, repo)
	if repo.conn.Status != StatusError {
		t.Errorf("statut connexion = %s, attendu error", repo.conn.Status)
	}
	if len(p.pushes) != 1 {
		t.Errorf("envois = %d : la synchro doit s'arrêter au premier refus d'identifiants", len(p.pushes))
	}
	if len(repo.items) != 0 {
		t.Errorf("un refus d'identifiants ne doit pas consommer de tentative : %v", repo.items)
	}
}

// ---- Identifiants OAuth ----------------------------------------------------

func TestCredentials_RefreshedOnceUnderLockAndPersisted(t *testing.T) {
	p := &fakeProvider{}
	svc, repo := setup(t, p)
	soon := time.Now().Add(2 * time.Minute)
	sealed, _ := svc.seal(Credentials{Kind: AuthOAuth, AccessToken: "old", RefreshToken: "r0", ExpiresAt: &soon})
	repo.conn.Credentials = sealed

	got, err := svc.credentials(context.Background(), repo.conn, p)
	if err != nil {
		t.Fatal(err)
	}
	if p.refresh != 1 || got.AccessToken != "a1" || got.RefreshToken != "r1" {
		t.Fatalf("refresh=%d creds=%+v", p.refresh, got)
	}
	// Le nouveau jeton est déjà en base : un second appel ne renouvelle pas (jeton à usage unique).
	again, err := svc.credentials(context.Background(), repo.conn, p)
	if err != nil || p.refresh != 1 || again.AccessToken != "a1" {
		t.Fatalf("second appel : refresh=%d creds=%+v err=%v", p.refresh, again, err)
	}
}

func TestCredentials_APITokenNeverRefreshed(t *testing.T) {
	p := &fakeProvider{}
	svc, repo := setup(t, p)
	if _, err := svc.credentials(context.Background(), repo.conn, p); err != nil || p.refresh != 0 || repo.lockHits != 0 {
		t.Errorf("refresh=%d verrou=%d err=%v", p.refresh, repo.lockHits, err)
	}
}

func TestCredentials_RefreshRejectedFlagsConnection(t *testing.T) {
	p := &fakeProvider{refreshE: fmt.Errorf("%w (invalid_grant)", ErrAuthRejected)}
	svc, repo := setup(t, p)
	soon := time.Now().Add(time.Minute)
	repo.conn.Credentials, _ = svc.seal(Credentials{Kind: AuthOAuth, AccessToken: "old", RefreshToken: "r0", ExpiresAt: &soon})
	repo.closures = []Closure{closure("2026-10-10", 11000)}

	sync(t, svc, repo)
	if repo.conn.Status != StatusError || len(p.pushes) != 0 {
		t.Errorf("statut=%s envois=%d", repo.conn.Status, len(p.pushes))
	}
}

// ---- Connexion -------------------------------------------------------------

func TestConnect_OneToolPerMerchant(t *testing.T) {
	p := &fakeProvider{}
	repo := newMemRepo()
	svc := NewService(repo, NewRegistry(p), memBox{ok: true}, nil)

	view, err := svc.ConnectWithToken(context.Background(), "212", "fake", "  secret-token ")
	if err != nil || view.Status != StatusSetup || view.AccountLabel != "Société Test" {
		t.Fatalf("connexion : %+v %v", view, err)
	}
	// Les identifiants sont chiffrés en base et absents de la vue.
	if strings.Contains(string(repo.conn.Credentials), "secret-token") && !strings.HasPrefix(string(repo.conn.Credentials), "enc:") {
		t.Error("identifiants en clair")
	}
	if b, _ := json.Marshal(view); strings.Contains(string(b), "secret-token") {
		t.Error("jeton exposé dans la vue")
	}
	if _, err := svc.ConnectWithToken(context.Background(), "212", "fake", "other"); !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("seconde connexion : %v", err)
	}
	if _, err := svc.ConnectWithToken(context.Background(), "300", "nope", "x"); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("outil inconnu : %v", err)
	}
	if _, err := NewService(newMemRepo(), NewRegistry(p), memBox{ok: false}, nil).ConnectWithToken(context.Background(), "1", "fake", "x"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("sans clé de chiffrement : %v", err)
	}
}

func TestSaveMapping_ActivatesFromToday(t *testing.T) {
	p := &fakeProvider{}
	repo := newMemRepo()
	svc := NewService(repo, NewRegistry(p), memBox{ok: true}, nil)
	// 23 h 30 UTC le 11 = 01 h 30 le 12 à Paris : le jour de départ est le 12 local.
	svc.now = func() time.Time { return time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC) }
	if _, err := svc.ConnectWithToken(context.Background(), "212", "fake", "tok"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SaveMapping(context.Background(), "212", Mapping{JournalCode: "VE"}); !errors.Is(err, ErrInvalidMapping) {
		t.Errorf("correspondance incomplète acceptée : %v", err)
	}
	view, err := svc.SaveMapping(context.Background(), "212", testMapping())
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusActive || view.SyncFrom == nil || *view.SyncFrom != "2026-10-12" {
		t.Fatalf("vue = %+v (sync_from %v)", view, view.SyncFrom)
	}

	// Enregistrer à nouveau ne recule ni n'avance le jour de départ.
	svc.now = func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) }
	view, _ = svc.SaveMapping(context.Background(), "212", testMapping())
	if *view.SyncFrom != "2026-10-12" {
		t.Errorf("sync_from modifié : %s", *view.SyncFrom)
	}

	// Comptes introuvables chez l'outil : rien n'est activé ni modifié.
	p.prepErr = &UnknownAccountsError{Accounts: []string{"707000"}}
	var unknown *UnknownAccountsError
	if _, err := svc.SaveMapping(context.Background(), "212", testMapping()); !errors.As(err, &unknown) {
		t.Errorf("comptes inconnus : %v", err)
	}
}

func TestOAuthState_RoundTripTamperAndExpiry(t *testing.T) {
	p := &fakeProvider{}
	repo := newMemRepo()
	svc := NewService(repo, NewRegistry(p), memBox{ok: true}, nil)

	authURL, err := svc.OAuthStartURL(context.Background(), "212", "fake")
	if err != nil {
		t.Fatal(err)
	}
	state := strings.TrimPrefix(authURL, "https://auth/?state=")

	merchant, err := svc.OAuthComplete(context.Background(), state, "code")
	if err != nil || merchant != "212" || repo.conn == nil || repo.conn.Status != StatusSetup {
		t.Fatalf("retour OAuth : marchand=%q err=%v conn=%+v", merchant, err, repo.conn)
	}

	for name, bad := range map[string]string{"vide": "", "forgé": "ZmF1eA", "tronqué": state[:len(state)-4]} {
		if _, err := svc.OAuthComplete(context.Background(), bad, "code"); !errors.Is(err, ErrInvalidState) {
			t.Errorf("state %s accepté : %v", name, err)
		}
	}
	svc.now = func() time.Time { return time.Now().Add(stateTTL + time.Minute) }
	if _, err := svc.OAuthComplete(context.Background(), state, "code"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("state expiré accepté : %v", err)
	}
}

func TestDisconnect_RevokesAndKeepsJournal(t *testing.T) {
	p := &fakeProvider{}
	svc, repo := setup(t, p)
	repo.conn.Credentials, _ = svc.seal(Credentials{Kind: AuthOAuth, AccessToken: "a", RefreshToken: "r"})
	repo.items["2026-10-10"] = &ItemUpdate{Status: ItemSent}

	if err := svc.Disconnect(context.Background(), "212"); err != nil {
		t.Fatal(err)
	}
	if repo.conn != nil || !p.revoked {
		t.Errorf("connexion=%v révoquée=%v", repo.conn, p.revoked)
	}
	if len(repo.items) != 1 {
		t.Error("le journal des envois doit survivre à la déconnexion")
	}
	if err := svc.Disconnect(context.Background(), "212"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("seconde déconnexion : %v", err)
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry(&fakeProvider{})
	if _, ok := r.Get("fake"); !ok || len(r.List()) != 1 {
		t.Error("catalogue incomplet")
	}
	defer func() {
		if recover() == nil {
			t.Error("un outil enregistré deux fois doit faire paniquer")
		}
	}()
	NewRegistry(&fakeProvider{}, &fakeProvider{})
}
