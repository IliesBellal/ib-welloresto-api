package ubereats

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── fakes ───────────────────────────────────────────────────────────────────

type fakeKV struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeKV() *fakeKV { return &fakeKV{data: map[string]string{}} }

func (f *fakeKV) Get(_ context.Context, key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	return v, ok
}
func (f *fakeKV) Set(_ context.Context, key, value string, _ time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return true
}
func (f *fakeKV) SetNX(_ context.Context, key, value string, _ time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; ok {
		return false
	}
	f.data[key] = value
	return true
}
func (f *fakeKV) Delete(_ context.Context, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return true
}

type fakeOnbRepo struct {
	owners      map[string]string // store_id -> merchant actif
	activated   []string
	deactivated []string
}

func (r *fakeOnbRepo) ActiveStoreOwner(_ context.Context, storeID string) (string, error) {
	return r.owners[storeID], nil
}
func (r *fakeOnbRepo) ActivateStore(_ context.Context, merchantID, storeID string) error {
	r.activated = append(r.activated, merchantID+"/"+storeID)
	r.owners[storeID] = merchantID
	return nil
}
func (r *fakeOnbRepo) DeactivateStore(_ context.Context, merchantID, storeID string) error {
	r.deactivated = append(r.deactivated, merchantID+"/"+storeID)
	delete(r.owners, storeID)
	return nil
}
func (r *fakeOnbRepo) DeactivateStoreEverywhere(_ context.Context, storeID string) ([]string, error) {
	m, ok := r.owners[storeID]
	if !ok {
		return nil, nil
	}
	delete(r.owners, storeID)
	r.deactivated = append(r.deactivated, m+"/"+storeID)
	return []string{m}, nil
}

// fakeUber simule l'API Uber : token, liste paginée, pos_data.
type fakeUber struct {
	mu        sync.Mutex
	provision map[string]PosDataRequest
	deleted   []string
	failStore string
}

func (f *fakeUber) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/v2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "good-code" || r.Form.Get("redirect_uri") != "https://api.test/integrations/uber-eats/callback" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"user-tok","expires_in":2592000,"scope":"eats.pos_provisioning"}`))
	})
	mux.HandleFunc("/v1/eats/stores", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("start_key") == "" {
			_, _ = w.Write([]byte(`{"stores":[
				{"store_id":"s-new","name":"Resto Neuf","location":{"address":"1 rue A","postal_code":"57000","city":"Metz"}},
				{"store_id":"s-here","name":"Resto Déjà Là","pos_data":{"integration_enabled":true,"order_manager_client_id":"cid"}}
			],"next_key":"k2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"stores":[
			{"store_id":"s-other","name":"Resto Ailleurs","pos_data":{"integration_enabled":true,"order_manager_client_id":"autre-logiciel"}},
			{"store_id":"s-fail","name":"Resto Panne"}
		]}`))
	})
	mux.HandleFunc("/v1/eats/stores/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/eats/stores/"), "/pos_data")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			if id == f.failStore {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var body PosDataRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("pos_data body: %v", err)
			}
			f.provision[id] = body
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			f.deleted = append(f.deleted, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func newOnboardingService(t *testing.T) (*UberEatsService, *fakeUber, *fakeOnbRepo, *fakeKV) {
	t.Helper()
	fu := &fakeUber{provision: map[string]PosDataRequest{}, failStore: "s-fail"}
	srv := httptest.NewServer(fu.handler(t))
	t.Cleanup(srv.Close)

	cfg := ConfigUberEats{
		BaseURL:          srv.URL,
		ClientID:         "cid",
		ClientSecret:     "secret",
		AuthURL:          "https://auth.uber.com/oauth/v2/authorize",
		TokenURL:         srv.URL + "/oauth/v2/token",
		RedirectURI:      "https://api.test/integrations/uber-eats/callback",
		ConnectReturnURL: "https://bo.test/integrations/uber-eats",
	}
	repo := &fakeOnbRepo{owners: map[string]string{"s-here": "212", "s-other": "999"}}
	kv := newFakeKV()
	svc := &UberEatsService{client: NewUberClient(cfg), config: cfg, onbKV: kv, onbRepo: repo}
	return svc, fu, repo, kv
}

func stateFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

// ─── tests ───────────────────────────────────────────────────────────────────

func TestConnectURL_UsesProvisioningScopeAndSignedState(t *testing.T) {
	svc, _, _, _ := newOnboardingService(t)

	raw, err := svc.ConnectURL("212", "user-7")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if u.Host != "auth.uber.com" || q.Get("scope") != "eats.pos_provisioning" || q.Get("response_type") != "code" ||
		q.Get("client_id") != "cid" || q.Get("redirect_uri") != "https://api.test/integrations/uber-eats/callback" {
		t.Fatalf("unexpected authorize URL: %s", raw)
	}
	st, err := svc.openState(q.Get("state"))
	if err != nil {
		t.Fatalf("openState: %v", err)
	}
	if st.Merchant != "212" || st.User != "user-7" || st.Purpose != purposeConnect {
		t.Fatalf("state = %+v", st)
	}
	// Le merchant_id ne doit plus être le state en clair.
	if q.Get("state") == "212" {
		t.Fatal("state must not be the raw merchant id")
	}
}

func TestOpenState_RejectsTamperedForeignAndExpired(t *testing.T) {
	svc, _, _, _ := newOnboardingService(t)

	good, _ := svc.sealState(oauthState{Merchant: "212", Purpose: purposeConnect, Nonce: "n", Exp: time.Now().Add(time.Minute).Unix()})
	payload, sig, _ := strings.Cut(good, ".")

	// Payload modifié (autre établissement) avec l'ancienne signature.
	forged, _ := json.Marshal(oauthState{Merchant: "999", Purpose: purposeConnect, Nonce: "n", Exp: time.Now().Add(time.Minute).Unix()})
	tampered := base64.RawURLEncoding.EncodeToString(forged) + "." + sig

	// Signé avec un autre secret.
	other := &UberEatsService{config: ConfigUberEats{ClientSecret: "autre"}}
	foreign, _ := other.sealState(oauthState{Merchant: "212", Purpose: purposeConnect, Nonce: "n", Exp: time.Now().Add(time.Minute).Unix()})

	expired, _ := svc.sealState(oauthState{Merchant: "212", Purpose: purposeConnect, Nonce: "n", Exp: time.Now().Add(-time.Second).Unix()})
	noStore, _ := svc.sealState(oauthState{Merchant: "212", Purpose: purposeDisconnect, Nonce: "n", Exp: time.Now().Add(time.Minute).Unix()})

	for name, state := range map[string]string{
		"tampered": tampered, "foreign": foreign, "expired": expired, "disconnect_without_store": noStore,
		"raw_merchant": "212", "empty": "", "no_sig": payload,
	} {
		if _, err := svc.openState(state); err == nil {
			t.Errorf("%s: openState accepted an invalid state", name)
		}
	}
	if _, err := svc.openState(good); err != nil {
		t.Fatalf("good state rejected: %v", err)
	}
}

func TestConnectFlow_ListsStoresThenProvisionsSelection(t *testing.T) {
	svc, fu, repo, kv := newOnboardingService(t)
	ctx := context.Background()

	authURL, _ := svc.ConnectURL("212", "user-7")
	out := svc.HandleOAuthCallback(ctx, url.Values{"code": {"good-code"}, "state": {stateFromURL(t, authURL)}})
	if out.Result != CallbackSelect || out.Session == "" {
		t.Fatalf("callback outcome = %+v", out)
	}

	view, err := svc.GetConnectSession(ctx, "212", out.Session)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ConnectStoreOption{}
	for _, s := range view.Stores {
		got[s.StoreID] = s
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 stores across both pages, got %+v", view.Stores)
	}
	if got["s-new"].Status != StoreAvailable || got["s-new"].Address != "1 rue A, 57000 Metz" {
		t.Errorf("s-new = %+v", got["s-new"])
	}
	if got["s-here"].Status != StoreConnectedHere || got["s-here"].OtherSoftware {
		t.Errorf("s-here = %+v (our own client id is not « another software »)", got["s-here"])
	}
	if got["s-other"].Status != StoreLinkedElsewhere || !got["s-other"].OtherSoftware {
		t.Errorf("s-other = %+v", got["s-other"])
	}
	// La session vue par le back-office ne porte pas le token.
	if raw, _ := json.Marshal(view); strings.Contains(string(raw), "user-tok") {
		t.Fatal("session view leaks the user token")
	}
	// Un autre établissement ne voit pas la session.
	if _, err := svc.GetConnectSession(ctx, "999", out.Session); !errors.Is(err, ErrConnectSessionNotFound) {
		t.Fatalf("foreign merchant session access: %v", err)
	}

	results, err := svc.CompleteConnect(ctx, "212", "user-7", out.Session, []string{"s-new", "s-other", "s-fail", "inconnu", "s-new"})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, r := range results {
		status[r.StoreID] = r.Status
	}
	if len(results) != 3 || status["s-new"] != "connected" || status["s-other"] != "linked_elsewhere" || status["s-fail"] != "failed" {
		t.Fatalf("results = %+v", results)
	}
	if body, ok := fu.provision["s-new"]; !ok || !body.IsOrderManager || body.IntegratorStoreID != "212" {
		t.Fatalf("pos_data for s-new = %+v (sent=%v)", body, ok)
	}
	if _, ok := fu.provision["s-other"]; ok {
		t.Fatal("a store linked to another establishment must not be provisioned")
	}
	if len(repo.activated) != 1 || repo.activated[0] != "212/s-new" {
		t.Fatalf("activated = %v", repo.activated)
	}
	if _, ok := kv.Get(ctx, connectSessionKeyPrefix+out.Session); ok {
		t.Fatal("session should be deleted once a store is connected")
	}
}

func TestCallback_StateIsSingleUse(t *testing.T) {
	svc, _, _, _ := newOnboardingService(t)
	ctx := context.Background()
	authURL, _ := svc.ConnectURL("212", "u")
	q := url.Values{"code": {"good-code"}, "state": {stateFromURL(t, authURL)}}

	if out := svc.HandleOAuthCallback(ctx, q); out.Result != CallbackSelect {
		t.Fatalf("first callback = %+v", out)
	}
	if out := svc.HandleOAuthCallback(ctx, q); out.Result != CallbackError || out.Reason != "invalid_state" {
		t.Fatalf("replayed callback = %+v", out)
	}
}

func TestCallback_ErrorReasons(t *testing.T) {
	svc, _, _, _ := newOnboardingService(t)
	ctx := context.Background()
	authURL, _ := svc.ConnectURL("212", "u")

	if out := svc.HandleOAuthCallback(ctx, url.Values{"error": {"access_denied"}}); out.Reason != "denied" {
		t.Errorf("denied: %+v", out)
	}
	if out := svc.HandleOAuthCallback(ctx, url.Values{"code": {"good-code"}, "state": {"212"}}); out.Reason != "invalid_state" {
		t.Errorf("raw merchant state: %+v", out)
	}
	if out := svc.HandleOAuthCallback(ctx, url.Values{"code": {"bad-code"}, "state": {stateFromURL(t, authURL)}}); out.Reason != "failed" {
		t.Errorf("bad code: %+v", out)
	}

	svc.config.ConnectReturnURL = ""
	if out := svc.HandleOAuthCallback(ctx, url.Values{}); out.Reason != "not_configured" {
		t.Errorf("not configured: %+v", out)
	}
	if _, err := svc.ConnectURL("212", "u"); !errors.Is(err, ErrOnboardingNotConfigured) {
		t.Errorf("ConnectURL without return URL: %v", err)
	}
}

func TestDisconnectFlow_DeprovisionsThenDeactivates(t *testing.T) {
	svc, fu, repo, _ := newOnboardingService(t)
	ctx := context.Background()

	state, _ := svc.sealState(oauthState{Merchant: "212", User: "u", Purpose: purposeDisconnect, Store: "s-here", Nonce: "n1", Exp: time.Now().Add(time.Minute).Unix()})
	out := svc.HandleOAuthCallback(ctx, url.Values{"code": {"good-code"}, "state": {state}})
	if out.Result != CallbackDisconnected {
		t.Fatalf("outcome = %+v", out)
	}
	if len(fu.deleted) != 1 || fu.deleted[0] != "s-here" {
		t.Fatalf("DELETE pos_data = %v", fu.deleted)
	}
	if len(repo.deactivated) != 1 || repo.deactivated[0] != "212/s-here" {
		t.Fatalf("deactivated = %v", repo.deactivated)
	}

	// Compte Uber qui ne donne pas accès au restaurant : rien n'est retiré.
	state2, _ := svc.sealState(oauthState{Merchant: "212", Purpose: purposeDisconnect, Store: "absent", Nonce: "n2", Exp: time.Now().Add(time.Minute).Unix()})
	out = svc.HandleOAuthCallback(ctx, url.Values{"code": {"good-code"}, "state": {state2}})
	if out.Reason != "store_not_in_account" || len(fu.deleted) != 1 {
		t.Fatalf("outcome = %+v, deleted = %v", out, fu.deleted)
	}
}

func TestStoreDeprovisionedWebhook_DeactivatesAndPurgesCache(t *testing.T) {
	svc, _, repo, kv := newOnboardingService(t)
	ctx := context.Background()
	kv.Set(ctx, storeCacheKey("s-here"), `{"MerchantID":"212"}`, time.Hour)

	if err := svc.HandleStoreDeprovisioned(ctx, "s-here"); err != nil {
		t.Fatal(err)
	}
	if len(repo.deactivated) != 1 || repo.deactivated[0] != "212/s-here" {
		t.Fatalf("deactivated = %v", repo.deactivated)
	}
	if _, ok := kv.Get(ctx, storeCacheKey("s-here")); ok {
		t.Fatal("webhook store cache should be purged")
	}
}

func TestListStores_FollowsPagination(t *testing.T) {
	svc, _, _, _ := newOnboardingService(t)
	stores, err := svc.client.ListStores(context.Background(), "user-tok")
	if err != nil || len(stores) != 4 {
		t.Fatalf("ListStores = (%d stores, %v)", len(stores), err)
	}
}
