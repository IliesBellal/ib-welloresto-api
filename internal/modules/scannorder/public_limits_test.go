package scannorder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"

	"welloresto-api/internal/models"
)

// TestMerchantClientKey_PerMerchantBuckets : posé en middleware inline, le
// seuil voit le slug de la route — un établissement saturé ne touche pas les
// autres, même derrière une adresse partagée.
func TestMerchantClientKey_PerMerchantBuckets(t *testing.T) {
	r := chi.NewRouter()
	limit := httprate.LimitBy(2, time.Minute, MerchantClientKey)
	r.With(limit).Get("/scannorder/{merchant_slug}/menu", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	call := func(slug string) int {
		req := httptest.NewRequest(http.MethodGet, "/scannorder/"+slug+"/menu", nil)
		req.RemoteAddr = "10.0.0.1:443"
		req.Header.Set("X-Forwarded-For", "198.51.100.4")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if call("resto-a") != http.StatusOK || call("resto-a") != http.StatusOK {
		t.Fatal("les deux premiers appels doivent passer")
	}
	if got := call("resto-a"); got != http.StatusTooManyRequests {
		t.Fatalf("3e appel resto-a = %d, want 429", got)
	}
	if got := call("resto-b"); got != http.StatusOK {
		t.Fatalf("resto-b = %d : son seuil ne doit pas dépendre de resto-a", got)
	}
}

// TestPublicSNOOrder_HidesInternalOrderID : la réponse publique ne porte que
// l'id public ; le code serveur garde l'accès à l'id interne.
func TestPublicSNOOrder_HidesInternalOrderID(t *testing.T) {
	publicID := "order--abc"
	order := &PublicSNOOrder{Order: &models.Order{OrderID: "12345", PublicID: &publicID}}

	raw, err := json.Marshal(order)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["order_id"]; ok {
		t.Fatalf("order_id exposé : %s", raw)
	}
	if !strings.Contains(string(fields["public_id"]), publicID) {
		t.Fatalf("public_id absent : %s", raw)
	}
	if order.OrderID != "12345" {
		t.Fatal("l'id interne doit rester lisible côté serveur")
	}
}
