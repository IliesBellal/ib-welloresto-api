package requestlogger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/middleware"
)

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func RequestLoggerMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			var payload []byte = []byte("{}") // Par défaut

			if r.Body != nil {
				bodyCopy, _ := io.ReadAll(r.Body)
				// On remet le body pour la suite, avant tout traitement du payload de log
				r.Body = io.NopCloser(bytes.NewBuffer(bodyCopy))

				if len(bodyCopy) > 0 {
					// La colonne api_request_logs.payload est un jsonb : un body non-JSON
					// (upload multipart, binaire...) ferait échouer tout le batch d'insertion,
					// pas seulement cette ligne. On ne stocke que sa taille dans ce cas.
					if json.Valid(bodyCopy) {
						payload = bodyCopy
					} else {
						payload = []byte(fmt.Sprintf(`{"non_json_body_bytes":%d}`, len(bodyCopy)))
					}
				}
			}

			// Utilisation du WrapResponseWriter de Chi
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			var userID *int64
			var merchantID *string

			if id, ok := r.Context().Value(models.ContextUserID).(int64); ok {
				userID = &id
			}

			if id, ok := r.Context().Value(models.ContextMerchantID).(string); ok {
				merchantID = &id
			}

			logger.Log(LogEntry{
				UserID:     userID,
				MerchantID: merchantID,
				Method:     r.Method,
				URL:        r.URL.String(),
				Payload:    payload,
				StatusCode: ww.Status(), // ww récupère le vrai statut final
				IP:         r.RemoteAddr,
			})
		})
	}
}
