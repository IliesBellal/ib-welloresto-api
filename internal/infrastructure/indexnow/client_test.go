package indexnow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_Validation(t *testing.T) {
	if _, err := New("short", "https://scannorder.welloresto.fr"); err == nil {
		t.Fatal("clé trop courte acceptée")
	}
	if _, err := New("abcd1234efgh", "http://scannorder.welloresto.fr"); err == nil {
		t.Fatal("site en http accepté")
	}
	if _, err := New("abcd1234efgh", ""); err == nil {
		t.Fatal("site vide accepté")
	}
}

func TestSubmit(t *testing.T) {
	var got submission
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Errorf("requête inattendue : %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c, err := New("abcd1234efgh", "https://scannorder.welloresto.fr/")
	if err != nil {
		t.Fatal(err)
	}
	urls := []string{"https://scannorder.welloresto.fr/restaurant/da-mario-metz"}
	if err := c.WithEndpoint(srv.URL).Submit(context.Background(), urls); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got.Host != "scannorder.welloresto.fr" || got.Key != "abcd1234efgh" ||
		got.KeyLocation != "https://scannorder.welloresto.fr/abcd1234efgh.txt" || len(got.URLList) != 1 || got.URLList[0] != urls[0] {
		t.Fatalf("soumission inattendue : %+v", got)
	}
}

func TestSubmit_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "key not valid", http.StatusForbidden)
	}))
	defer srv.Close()
	c, _ := New("abcd1234efgh", "https://scannorder.welloresto.fr")
	err := c.WithEndpoint(srv.URL).Submit(context.Background(), []string{"https://scannorder.welloresto.fr/"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v, want HTTP 403", err)
	}
}
