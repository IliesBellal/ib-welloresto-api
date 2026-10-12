package pennylane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/modules/accountingsync"
)

var apiToken = accountingsync.Credentials{Kind: accountingsync.AuthAPIToken, AccessToken: "tok"}

func newProvider(t *testing.T, h http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec", RedirectURI: "https://api.test/cb"})
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestInfo_OAuthOnlyWhenConfigured(t *testing.T) {
	if got := New(Config{}).Info().AuthMethods; len(got) != 1 || got[0] != accountingsync.AuthAPIToken {
		t.Errorf("sans application OAuth : %v", got)
	}
	got := New(Config{ClientID: "a", ClientSecret: "b", RedirectURI: "c"}).Info().AuthMethods
	if len(got) != 2 {
		t.Errorf("avec application OAuth : %v", got)
	}
	if err := New(Config{}).Info().SuggestedMapping.Validate(); err != nil {
		t.Errorf("la correspondance suggérée doit être complète : %v", err)
	}
}

func TestValidate(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/external/v2/me" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("appel inattendu : %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		writeJSON(w, 200, `{"user":{"id":1},"company":{"id":7,"name":"Chez Test SAS"}}`)
	})
	label, err := p.Validate(context.Background(), apiToken)
	if err != nil || label != "Chez Test SAS" {
		t.Fatalf("label=%q err=%v", label, err)
	}

	denied := newProvider(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 401, `{"error":"Unauthorized"}`) })
	if _, err := denied.Validate(context.Background(), apiToken); !errors.Is(err, accountingsync.ErrAuthRejected) {
		t.Errorf("401 : %v", err)
	}
}

func TestPrepare_ResolvesJournalAndAccounts(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/external/v2/journals":
			// Deux pages : le journal cherché est sur la seconde.
			if r.URL.Query().Get("cursor") == "" {
				writeJSON(w, 200, `{"items":[{"id":1,"code":"AC"}],"has_more":true,"next_cursor":"c2"}`)
				return
			}
			writeJSON(w, 200, `{"items":[{"id":9,"code":"ve"}],"has_more":false,"next_cursor":null}`)
		case "/api/external/v2/ledger_accounts":
			var f []map[string]string
			_ = json.Unmarshal([]byte(r.URL.Query().Get("filter")), &f)
			number := f[0]["value"]
			ids := map[string]string{
				"707000": `{"id":101,"number":"707000","enabled":true}`,
				// numéro en double : le compte désactivé est ignoré
				"531000": `{"id":201,"number":"531000","enabled":false},{"id":202,"number":"531000","enabled":true}`,
			}
			writeJSON(w, 200, `{"items":[`+ids[number]+`],"has_more":false,"next_cursor":null}`)
		default:
			t.Errorf("appel inattendu %s", r.URL.Path)
		}
	})
	raw, err := p.Prepare(context.Background(), apiToken, accountingsync.PrepareRequest{JournalCode: "VE", AccountNumbers: []string{"531000", "707000"}})
	if err != nil {
		t.Fatal(err)
	}
	var st preparedState
	_ = json.Unmarshal(raw, &st)
	if st.JournalID != 9 || st.Accounts["707000"] != 101 || st.Accounts["531000"] != 202 {
		t.Errorf("état = %+v", st)
	}
}

func TestPrepare_ReportsUnknownJournalAndAccounts(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"items":[],"has_more":false,"next_cursor":null}`)
	})
	_, err := p.Prepare(context.Background(), apiToken, accountingsync.PrepareRequest{JournalCode: "VE", AccountNumbers: []string{"707000", "531000"}})
	var unknown *accountingsync.UnknownAccountsError
	if !errors.As(err, &unknown) || unknown.Journal != "VE" || len(unknown.Accounts) != 2 {
		t.Fatalf("err = %v", err)
	}
}

// L'API refuse le filtre : on retombe sur un parcours complet de la liste.
func TestPrepare_FallsBackWhenFilterRejected(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/external/v2/journals":
			writeJSON(w, 200, `{"items":[{"id":9,"code":"VE"}],"has_more":false,"next_cursor":null}`)
		case r.URL.Query().Get("filter") != "":
			writeJSON(w, 400, `{"error":"invalid filter"}`)
		default:
			writeJSON(w, 200, `{"items":[{"id":1,"number":"606000"},{"id":101,"number":"707000"}],"has_more":false,"next_cursor":null}`)
		}
	})
	raw, err := p.Prepare(context.Background(), apiToken, accountingsync.PrepareRequest{JournalCode: "VE", AccountNumbers: []string{"707000"}})
	if err != nil {
		t.Fatal(err)
	}
	var st preparedState
	_ = json.Unmarshal(raw, &st)
	if st.Accounts["707000"] != 101 {
		t.Errorf("état = %+v", st)
	}
}

func testEntry() accountingsync.Entry {
	return accountingsync.Entry{
		Date: "2026-10-11", Label: "Ventes Wello Resto du 11/10/2026 [WR-212-20261011]", Reference: "WR-212-20261011",
		Lines: []accountingsync.EntryLine{
			{Account: "531000", Label: "Encaissements ES", Debit: 12345},
			{Account: "707000", Label: "Ventes TVA 10 %", Credit: 11222},
			{Account: "445710", Label: "TVA collectée 10 %", Credit: 1123},
		},
	}
}

var testState = json.RawMessage(`{"journal_id":9,"accounts":{"531000":202,"707000":101,"445710":303}}`)

func TestPushEntry_PayloadAndID(t *testing.T) {
	var got map[string]any
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/external/v2/ledger_entries" {
			t.Errorf("appel inattendu %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeJSON(w, 201, `{"id":4242,"label":"x","journal_id":9,"status":"recorded"}`)
	})
	id, err := p.PushEntry(context.Background(), apiToken, testState, testEntry())
	if err != nil || id != "4242" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if got["date"] != "2026-10-11" || got["journal_id"] != float64(9) {
		t.Errorf("corps = %v", got)
	}
	lines := got["ledger_entry_lines"].([]any)
	first := lines[0].(map[string]any)
	// Montants en chaînes décimales, comptes en identifiants Pennylane.
	if first["debit"] != "123.45" || first["credit"] != "0.00" || first["ledger_account_id"] != float64(202) {
		t.Errorf("première ligne = %v", first)
	}
	if lines[2].(map[string]any)["credit"] != "11.23" {
		t.Errorf("troisième ligne = %v", lines[2])
	}
}

func TestPushEntry_ErrorClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		check  func(error) bool
	}{
		{"401 identifiants", 401, func(e error) bool { return errors.Is(e, accountingsync.ErrAuthRejected) }},
		{"403 droits", 403, func(e error) bool { return errors.Is(e, accountingsync.ErrAuthRejected) }},
		{"422 déséquilibrée", 422, func(e error) bool {
			var r *accountingsync.RejectedError
			return errors.As(e, &r) && r.Status == 422 && strings.Contains(r.Message, "not balanced")
		}},
		{"404 journal", 404, func(e error) bool { var r *accountingsync.RejectedError; return errors.As(e, &r) }},
		{"429 limite", 429, func(e error) bool { var r *accountingsync.RejectedError; return errors.As(e, &r) }},
		{"500 serveur : issue inconnue", 500, func(e error) bool { var a *accountingsync.AmbiguousError; return errors.As(e, &a) }},
		{"502 passerelle : issue inconnue", 502, func(e error) bool { var a *accountingsync.AmbiguousError; return errors.As(e, &a) }},
	}
	for _, c := range cases {
		p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, c.status, `{"message":"Entry lines are not balanced."}`)
		})
		_, err := p.PushEntry(context.Background(), apiToken, testState, testEntry())
		if !c.check(err) {
			t.Errorf("%s : erreur = %v", c.name, err)
		}
	}
}

// Délai dépassé : l'écriture a pu être créée, ne jamais conclure à un simple échec.
func TestPushEntry_TimeoutIsAmbiguous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, 201, `{"id":1}`)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	_, err := p.PushEntry(context.Background(), apiToken, testState, testEntry())
	var amb *accountingsync.AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, attendu AmbiguousError", err)
	}
}

func TestPushEntry_UnresolvedAccountIsRejectedWithoutCall(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Error("aucun appel attendu") })
	_, err := p.PushEntry(context.Background(), apiToken, json.RawMessage(`{"journal_id":9,"accounts":{}}`), testEntry())
	var rej *accountingsync.RejectedError
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v", err)
	}
	_, err = p.PushEntry(context.Background(), apiToken, json.RawMessage(`{}`), testEntry())
	if !errors.As(err, &rej) {
		t.Fatalf("état vide : %v", err)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[int64]string{0: "0.00", 5: "0.05", 100: "1.00", 12345: "123.45", 99: "0.99"} {
		if got := money(in); got != want {
			t.Errorf("money(%d) = %q, attendu %q", in, got, want)
		}
	}
}

func TestFindEntry(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var f []map[string]string
		_ = json.Unmarshal([]byte(r.URL.Query().Get("filter")), &f)
		if len(f) != 1 || f[0]["field"] != "date" || f[0]["value"] != "2026-10-11" {
			t.Errorf("filtre = %v", f)
		}
		writeJSON(w, 200, `{"items":[
			{"id":1,"label":"Autre écriture du jour"},
			{"id":77,"label":"Ventes Wello Resto du 11/10/2026 [WR-212-20261011]"}],"has_more":false,"next_cursor":null}`)
	})
	id, found, err := p.FindEntry(context.Background(), apiToken, "WR-212-20261011")
	if err != nil || !found || id != "77" {
		t.Fatalf("id=%q found=%v err=%v", id, found, err)
	}
	// Un autre établissement le même jour ne correspond pas.
	if _, found, _ := p.FindEntry(context.Background(), apiToken, "WR-999-20261011"); found {
		t.Error("l'écriture d'un autre établissement ne doit pas être retrouvée")
	}
	if _, _, err := p.FindEntry(context.Background(), apiToken, "bad"); err == nil {
		t.Error("référence illisible acceptée")
	}
}

// ---- OAuth -----------------------------------------------------------------

func TestAuthURL(t *testing.T) {
	p := New(Config{BaseURL: "https://app.pennylane.com", ClientID: "cid", ClientSecret: "sec", RedirectURI: "https://api.test/cb"})
	raw, err := p.AuthURL("STATE")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if u.Host != "app.pennylane.com" || u.Path != "/oauth/authorize" || q.Get("client_id") != "cid" ||
		q.Get("response_type") != "code" || q.Get("state") != "STATE" || q.Get("redirect_uri") != "https://api.test/cb" ||
		q.Get("scope") != "ledger_entries:all journals:readonly ledger_accounts:readonly" {
		t.Errorf("url = %s", raw)
	}
	if _, err := New(Config{}).AuthURL("x"); err == nil {
		t.Error("AuthURL sans application OAuth")
	}
}

func TestExchangeAndRefresh(t *testing.T) {
	var forms []url.Values
	fixed := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		forms = append(forms, r.PostForm)
		if r.URL.Path != "/oauth/token" {
			t.Errorf("chemin %s", r.URL.Path)
		}
		if r.PostForm.Get("refresh_token") == "stale" {
			writeJSON(w, 400, `{"error":"invalid_grant"}`)
			return
		}
		writeJSON(w, 200, `{"access_token":"acc","refresh_token":"ref","expires_in":86400,"token_type":"Bearer"}`)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, ClientID: "cid", ClientSecret: "sec", RedirectURI: "https://api.test/cb", Now: func() time.Time { return fixed }})

	c, err := p.Exchange(context.Background(), "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != accountingsync.AuthOAuth || c.AccessToken != "acc" || c.RefreshToken != "ref" || !c.ExpiresAt.Equal(fixed.Add(24*time.Hour)) {
		t.Errorf("jetons = %+v", c)
	}
	f := forms[0]
	if f.Get("grant_type") != "authorization_code" || f.Get("code") != "the-code" || f.Get("client_secret") != "sec" || f.Get("redirect_uri") != "https://api.test/cb" {
		t.Errorf("échange = %v", f)
	}

	if _, err := p.Refresh(context.Background(), accountingsync.Credentials{RefreshToken: "ref"}); err != nil {
		t.Fatal(err)
	}
	if forms[1].Get("grant_type") != "refresh_token" || forms[1].Get("refresh_token") != "ref" {
		t.Errorf("rafraîchissement = %v", forms[1])
	}

	// Jeton de rafraîchissement déjà utilisé ou expiré : reconnexion nécessaire.
	if _, err := p.Refresh(context.Background(), accountingsync.Credentials{RefreshToken: "stale"}); !errors.Is(err, accountingsync.ErrAuthRejected) {
		t.Errorf("invalid_grant : %v", err)
	}
}
