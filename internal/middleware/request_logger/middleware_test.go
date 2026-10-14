package requestlogger

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

func captureLogEntry(t *testing.T, handler http.HandlerFunc, req *http.Request) LogEntry {
	t.Helper()

	// Construit le Logger à la main plutôt que via NewLogger : celle-ci démarre
	// un worker qui vide le channel en tâche de fond et entrerait en course
	// avec la lecture ci-dessous.
	logger := &Logger{log: zap.NewNop(), queue: make(chan LogEntry, 1)}
	rec := httptest.NewRecorder()

	RequestLoggerMiddleware(logger)(handler).ServeHTTP(rec, req)

	select {
	case entry := <-logger.queue:
		return entry
	case <-time.After(time.Second):
		t.Fatal("no LogEntry was queued")
		return LogEntry{}
	}
}

// Régression : la réponse doit être capturée sans troncature quand elle est du
// JSON valide (voir migration 108).
func TestRequestLoggerMiddleware_CapturesJSONResponseBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"hello":"world"}`))
	})

	entry := captureLogEntry(t, handler, httptest.NewRequest(http.MethodGet, "/json", nil))

	if string(entry.ResponsePayload) != `{"hello":"world"}` {
		t.Fatalf("ResponsePayload = %q, want %q", entry.ResponsePayload, `{"hello":"world"}`)
	}
}

// Un corps de réponse non-JSON (export de fichier, binaire...) ne doit jamais
// être inséré tel quel : seule sa taille est retenue.
func TestRequestLoggerMiddleware_NonJSONResponseBodyIsSizeMarkerOnly(t *testing.T) {
	body := []byte{0xff, 0xd8, 0xff, 0xe0} // en-tête JPEG, pas du JSON valide
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	})

	entry := captureLogEntry(t, handler, httptest.NewRequest(http.MethodGet, "/file", nil))

	want := `{"non_json_body_bytes":4}`
	if string(entry.ResponsePayload) != want {
		t.Fatalf("ResponsePayload = %q, want %q", entry.ResponsePayload, want)
	}
}

// Une réponse vide (ex: 204 No Content) retombe sur le même défaut que le
// payload de requête.
func TestRequestLoggerMiddleware_EmptyResponseBodyDefaultsToEmptyObject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	entry := captureLogEntry(t, handler, httptest.NewRequest(http.MethodGet, "/empty", nil))

	if string(entry.ResponsePayload) != "{}" {
		t.Fatalf("ResponsePayload = %q, want %q", entry.ResponsePayload, "{}")
	}
}

// Régression : user_id et merchant_id restaient toujours NULL, le journal lisant
// des clés de contexte que personne ne posait (et le contexte dérivé par
// l'authentification est de toute façon invisible après coup). L'identité
// remplie par l'authentification, via le pointeur déposé dans la requête, doit
// remonter dans l'entrée.
func TestRequestLoggerMiddleware_RecordsAuthenticatedIdentity(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := r.Context().Value(models.ContextRequestIdentity).(*models.RequestIdentity)
		if !ok {
			t.Fatal("aucun RequestIdentity dans le contexte de la requête")
		}
		identity.UserID, identity.MerchantID = "226", "212"
	})

	entry := captureLogEntry(t, handler, httptest.NewRequest(http.MethodPost, "/orders", nil))
	if entry.UserID == nil || *entry.UserID != 226 || entry.MerchantID == nil || *entry.MerchantID != "212" {
		t.Fatalf("entrée = (user %v, merchant %v), want (226, 212)", entry.UserID, entry.MerchantID)
	}

	// Requête non authentifiée, ou identifiant non numérique : NULL, jamais d'échec.
	entry = captureLogEntry(t, func(w http.ResponseWriter, r *http.Request) {
		r.Context().Value(models.ContextRequestIdentity).(*models.RequestIdentity).UserID = "abc"
	}, httptest.NewRequest(http.MethodGet, "/x", nil))
	if entry.UserID != nil || entry.MerchantID != nil {
		t.Fatalf("entrée = (user %v, merchant %v), want (nil, nil)", entry.UserID, entry.MerchantID)
	}
}
