package requestlogger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

// newTestLogger rend un Logger sans worker : les entrées restent dans la file.
func newTestLogger(t *testing.T) *Logger {
	t.Helper()
	l := &Logger{log: zap.NewNop(), queue: make(chan LogEntry, 16)}
	SetDefault(l)
	t.Cleanup(func() { SetDefault(nil) })
	return l
}

func nextEntry(t *testing.T, l *Logger) LogEntry {
	t.Helper()
	select {
	case e := <-l.queue:
		return e
	case <-time.After(time.Second):
		t.Fatal("aucune entrée journalisée")
		return LogEntry{}
	}
}

func client() *http.Client {
	return &http.Client{Transport: Transport(nil), Timeout: 5 * time.Second}
}

func TestOutbound_NilLoggerIsPassThrough(t *testing.T) {
	SetDefault(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resp, err := client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != `{"ok":true}` {
		t.Fatalf("corps altéré: %s", b)
	}
}

func TestOutbound_LogsRequestAndResponse_CallerSeesEverything(t *testing.T) {
	l := newTestLogger(t)

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/orders", strings.NewReader(`{"stage":"ready"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer SECRET")
	resp, err := client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if gotBody != `{"stage":"ready"}` || string(b) != `{"id":"abc"}` || resp.StatusCode != 201 {
		t.Fatalf("échange altéré: req=%q resp=%q status=%d", gotBody, b, resp.StatusCode)
	}
	e := nextEntry(t, l)
	if e.Method != "POST" || e.StatusCode != 201 || !strings.HasPrefix(e.URL, "http://") || !strings.HasSuffix(e.URL, "/v1/orders") {
		t.Fatalf("entrée inattendue: %+v", e)
	}
	if string(e.Payload) != `{"stage":"ready"}` || string(e.ResponsePayload) != `{"id":"abc"}` {
		t.Fatalf("corps inattendus: %s / %s", e.Payload, e.ResponsePayload)
	}
}

func TestOutbound_LogsEvenWhenCallerNeverReadsNorClosesBody(t *testing.T) {
	l := newTestLogger(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":1}`))
	}))
	defer srv.Close()

	if _, err := client().Get(srv.URL); err != nil {
		t.Fatal(err)
	}
	if e := nextEntry(t, l); string(e.ResponsePayload) != `{"ok":1}` {
		t.Fatalf("réponse non journalisée: %s", e.ResponsePayload)
	}
}

func TestOutbound_LargeResponse_TruncatedInLogNotForCaller(t *testing.T) {
	l := newTestLogger(t)
	big := strings.Repeat("a", outboundBodyCap*3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(big))
	}))
	defer srv.Close()

	resp, err := client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != big {
		t.Fatalf("l'appelant doit recevoir la réponse complète, reçu %d octets", len(b))
	}

	e := nextEntry(t, l)
	var s string
	if err := json.Unmarshal(e.ResponsePayload, &s); err != nil {
		t.Fatalf("payload tronqué doit être une chaîne JSON valide: %v", err)
	}
	if !strings.HasSuffix(s, truncatedSuffix) || len(s) > outboundBodyCap+len(truncatedSuffix) {
		t.Fatalf("troncature incorrecte (len=%d)", len(s))
	}
}

func TestOutbound_NonJSONTextAndBinary_AlwaysValidJSON(t *testing.T) {
	l := newTestLogger(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/html" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(502)
			w.Write([]byte("<html>Bad gateway</html>"))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-\x00\x01\x02"))
	}))
	defer srv.Close()

	for _, p := range []string{"/html", "/pdf"} {
		resp, err := client().Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		e := nextEntry(t, l)
		if !json.Valid(e.ResponsePayload) || !json.Valid(e.Payload) {
			t.Fatalf("%s: payload JSON invalide: %s", p, e.ResponsePayload)
		}
		if p == "/html" && !strings.Contains(string(e.ResponsePayload), "Bad gateway") {
			t.Fatalf("le HTML d'erreur doit être conservé: %s", e.ResponsePayload)
		}
	}
}

func TestOutbound_TransportError_StatusZeroAndSecretQueryMasked(t *testing.T) {
	l := newTestLogger(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL + "/x?key=TOPSECRET&origin=Metz"
	srv.Close() // connexion refusée

	_, err := client().Get(url)
	if err == nil {
		t.Fatal("erreur attendue")
	}
	e := nextEntry(t, l)
	if e.StatusCode != 0 || !strings.Contains(string(e.ResponsePayload), "transport_error") {
		t.Fatalf("échec de transport mal journalisé: %+v / %s", e, e.ResponsePayload)
	}
	if strings.Contains(e.URL, "TOPSECRET") || strings.Contains(string(e.ResponsePayload), "TOPSECRET") {
		t.Fatalf("clé non masquée: %s / %s", e.URL, e.ResponsePayload)
	}
	if !strings.Contains(e.URL, "origin=Metz") {
		t.Fatalf("les paramètres non sensibles doivent rester: %s", e.URL)
	}
}

func TestOutbound_AuthEndpointBodiesNeverStored(t *testing.T) {
	l := newTestLogger(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"ABC123"}`))
	}))
	defer srv.Close()

	resp, err := client().Post(srv.URL+"/oauth2/token", "application/x-www-form-urlencoded", strings.NewReader("client_secret=SHH&grant_type=client_credentials"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != `{"access_token":"ABC123"}` {
		t.Fatalf("l'appelant doit toujours recevoir le jeton: %s", b)
	}
	e := nextEntry(t, l)
	for _, p := range [][]byte{e.Payload, e.ResponsePayload} {
		if strings.Contains(string(p), "SHH") || strings.Contains(string(p), "ABC123") {
			t.Fatalf("secret stocké: %s", p)
		}
	}
}

func TestOutbound_SecretsInBodiesAreRedacted(t *testing.T) {
	l := newTestLogger(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"pi_1","client_secret":"pi_1_secret_XYZ","amount":10}`))
	}))
	defer srv.Close()

	resp, err := client().Post(srv.URL+"/v1/payment_intents", "application/json", strings.NewReader(`{"password":"hunter2","a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	e := nextEntry(t, l)
	if strings.Contains(string(e.ResponsePayload), "pi_1_secret_XYZ") || strings.Contains(string(e.Payload), "hunter2") {
		t.Fatalf("secret stocké: %s / %s", e.Payload, e.ResponsePayload)
	}
	if !json.Valid(e.ResponsePayload) || !strings.Contains(string(e.ResponsePayload), `"amount":10`) {
		t.Fatalf("le reste du JSON doit rester exploitable: %s", e.ResponsePayload)
	}
}

func TestOutbound_CarriesIdentityFromRequestContext(t *testing.T) {
	l := newTestLogger(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	ctx := context.WithValue(context.Background(), models.ContextRequestIdentity, &models.RequestIdentity{UserID: "7", MerchantID: "m42"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	e := nextEntry(t, l)
	if e.UserID == nil || *e.UserID != 7 || e.MerchantID == nil || *e.MerchantID != "m42" {
		t.Fatalf("identité non reprise: %+v", e)
	}
}

func TestOutbound_FullQueueNeverBlocksNorBreaksCall(t *testing.T) {
	l := &Logger{log: zap.NewNop(), queue: make(chan LogEntry)} // file sans capacité, personne ne lit
	SetDefault(l)
	t.Cleanup(func() { SetDefault(nil) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	defer srv.Close()

	done := make(chan error, 1)
	go func() {
		resp, err := client().Get(srv.URL)
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("l'appel est bloqué par le journal")
	}
}

type failingBody struct{ sent bool }

func (f *failingBody) Read(p []byte) (int, error) {
	if !f.sent {
		f.sent = true
		return copy(p, `{"partial":`), nil
	}
	return 0, errors.New("boom")
}
func (f *failingBody) Close() error { return nil }

type stubTransport struct{ resp *http.Response }

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) { return s.resp, nil }

func TestOutbound_BodyReadErrorIsPropagatedToCaller(t *testing.T) {
	l := newTestLogger(t)
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: &failingBody{}}
	c := &http.Client{Transport: Transport(stubTransport{resp})}

	r, err := c.Get("http://example.invalid/x")
	if err != nil {
		t.Fatal(err)
	}
	b, rerr := io.ReadAll(r.Body)
	if rerr == nil || rerr.Error() != "boom" || string(b) != `{"partial":` {
		t.Fatalf("l'erreur de lecture doit remonter intacte: %q / %v", b, rerr)
	}
	if e := nextEntry(t, l); !json.Valid(e.ResponsePayload) {
		t.Fatalf("payload invalide: %s", e.ResponsePayload)
	}
}

func TestEnsureJSON_RejectsWhatPostgresJsonbRefuses(t *testing.T) {
	for _, in := range []string{`not json`, `"a\u0000b"`} {
		out := ensureJSON([]byte(in))
		if !json.Valid(out) || strings.Contains(string(out), `\u0000`) {
			t.Fatalf("%q -> %s", in, out)
		}
	}
}
