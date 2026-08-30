package requestlogger

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
