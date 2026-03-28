package requestlogger

import (
	"bytes"
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
				if len(bodyCopy) > 0 {
					payload = bodyCopy
				}
				// On remet le body pour la suite
				r.Body = io.NopCloser(bytes.NewBuffer(bodyCopy))
			}

			// Utilisation du WrapResponseWriter de Chi
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			var userID *int64
			var merchantID *int64

			if v := r.Context().Value(models.ContextUserID); v != nil {
				id := v.(int64)
				userID = &id
			}

			if v := r.Context().Value(models.ContextMerchantID); v != nil {
				id := v.(int64)
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
