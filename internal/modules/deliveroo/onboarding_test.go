package deliveroo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"welloresto-api/internal/config"
)

type fakeDeliverooAPI struct {
	mu          sync.Mutex
	sitesConfig string // corps renvoyé par GET sites-config
	puts        []map[string][]SiteWebhookConfig
}

func (f *fakeDeliverooAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	})
	mux.HandleFunc("/site/v1/restaurant_locations/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/site/v1/restaurant_locations/") {
		case "212":
			_, _ = w.Write([]byte(`{"id":"212","brand_id":["croqo-fr"]}`))
		case "212-2":
			_, _ = w.Write([]byte(`{"id":"212-2","brand_id":[]}`))
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	mux.HandleFunc("/order/v1/integrator/brands/croqo-fr/sites-config", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(f.sitesConfig))
		case http.MethodPut:
			var body map[string][]SiteWebhookConfig
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("PUT sites-config body: %v", err)
			}
			f.puts = append(f.puts, body)
			_, _ = w.Write([]byte(`{}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newOnboardingDeliveroo(t *testing.T, sitesConfig string) (*DeliverooService, *fakeDeliverooAPI) {
	t.Helper()
	api := &fakeDeliverooAPI{sitesConfig: sitesConfig}
	srv := api.server(t)
	cfg := config.DeliverooConfig{BaseURL: srv.URL, AuthBaseURL: srv.URL, BasicAuth: "x"}
	return &DeliverooService{client: NewDeliverooClient(nil, cfg), cfg: cfg}, api
}

func TestLookupBrandID(t *testing.T) {
	svc, _ := newOnboardingDeliveroo(t, `{"sites":[]}`)
	ctx := context.Background()

	if b, err := svc.client.LookupBrandID(ctx, "212"); err != nil || b != "croqo-fr" {
		t.Fatalf("linked site = (%q, %v)", b, err)
	}
	for _, loc := range []string{"999", "212-2"} {
		if _, err := svc.client.LookupBrandID(ctx, loc); !errors.Is(err, ErrLocationNotLinked) {
			t.Errorf("%s: want ErrLocationNotLinked, got %v", loc, err)
		}
	}
	if _, err := svc.client.LookupBrandID(ctx, "500"); err == nil || errors.Is(err, ErrLocationNotLinked) {
		t.Errorf("server error must not read as « not linked »: %v", err)
	}
}

func TestEnsureOrderEventsWebhook(t *testing.T) {
	ctx := context.Background()

	t.Run("already order events: nothing sent", func(t *testing.T) {
		svc, api := newOnboardingDeliveroo(t, `{"sites":[{"location_id":"212","name":"Croqo","orders_api_webhook_type":"POS_AND_ORDER_EVENTS"}]}`)
		if err := svc.ensureOrderEventsWebhook(ctx, "croqo-fr", "212"); err != nil {
			t.Fatal(err)
		}
		if len(api.puts) != 0 {
			t.Fatalf("unexpected PUT: %+v", api.puts)
		}
	})

	t.Run("legacy POS type switched, other sites kept", func(t *testing.T) {
		svc, api := newOnboardingDeliveroo(t, `{"sites":[{"location_id":456,"name":"Autre","orders_api_webhook_type":"POS"},{"location_id":"212","orders_api_webhook_type":"POS"}]}`)
		if err := svc.ensureOrderEventsWebhook(ctx, "croqo-fr", "212"); err != nil {
			t.Fatal(err)
		}
		if len(api.puts) != 1 {
			t.Fatalf("expected one PUT, got %d", len(api.puts))
		}
		got := api.puts[0]["sites"]
		want := []SiteWebhookConfig{{LocationID: "456", OrdersAPIWebhookType: "POS"}, {LocationID: "212", OrdersAPIWebhookType: "ORDER_EVENTS"}}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("PUT sites = %+v, want %+v", got, want)
		}
	})

	t.Run("site absent: appended", func(t *testing.T) {
		svc, api := newOnboardingDeliveroo(t, `{"sites":[]}`)
		if err := svc.ensureOrderEventsWebhook(ctx, "croqo-fr", "212"); err != nil {
			t.Fatal(err)
		}
		if len(api.puts) != 1 || len(api.puts[0]["sites"]) != 1 || api.puts[0]["sites"][0] != (SiteWebhookConfig{LocationID: "212", OrdersAPIWebhookType: "ORDER_EVENTS"}) {
			t.Fatalf("PUT = %+v", api.puts)
		}
	})
}

func TestLocationCandidates(t *testing.T) {
	c := locationCandidates("212")
	if len(c) != maxExtraSites || c[0] != "212" || c[1] != "212-2" || c[len(c)-1] != "212-9" {
		t.Fatalf("candidates = %v", c)
	}
	for _, id := range c {
		if len(id) > 20 { // integration_deliveroo.location_id varchar(20)
			t.Fatalf("%q exceeds the column size", id)
		}
	}
}
