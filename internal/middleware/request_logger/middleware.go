package requestlogger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/middleware"
)

// jsonOrSizeMarker retourne body tel quel s'il s'agit de JSON valide. Sinon
// (upload multipart, binaire, export de fichier...), seule sa taille est
// renvoyée : la colonne cible est jsonb, un corps non-JSON ferait échouer tout
// le batch d'insertion, pas seulement cette ligne.
func jsonOrSizeMarker(body []byte) []byte {
	if len(body) == 0 {
		return []byte("{}")
	}
	if json.Valid(body) {
		return body
	}
	return []byte(fmt.Sprintf(`{"non_json_body_bytes":%d}`, len(body)))
}

func RequestLoggerMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			var payload []byte = []byte("{}") // Par défaut

			if r.Body != nil {
				bodyCopy, _ := io.ReadAll(r.Body)
				// On remet le body pour la suite, avant tout traitement du payload de log
				r.Body = io.NopCloser(bytes.NewBuffer(bodyCopy))
				payload = jsonOrSizeMarker(bodyCopy)
			}

			// Utilisation du WrapResponseWriter de Chi, avec Tee pour dupliquer le
			// corps de la réponse dans un buffer au fil de l'écriture (pas de copie
			// après coup : le corps n'est disponible nulle part ailleurs une fois
			// ServeHTTP revenu).
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			var respBuf bytes.Buffer
			ww.Tee(&respBuf)

			// La mesure encadre au plus près l'exécution du handler : la lecture
			// du body ci-dessus est du coût de log, pas du coût de traitement, et
			// la fausser gonflerait artificiellement les endpoints à gros payload.
			startedAt := time.Now()
			next.ServeHTTP(ww, r)
			durationMs := time.Since(startedAt).Milliseconds()

			responsePayload := jsonOrSizeMarker(respBuf.Bytes())

			var userID *int64
			var merchantID *string

			if id, ok := r.Context().Value(models.ContextUserID).(int64); ok {
				userID = &id
			}

			if id, ok := r.Context().Value(models.ContextMerchantID).(string); ok {
				merchantID = &id
			}

			logger.Log(LogEntry{
				UserID:          userID,
				MerchantID:      merchantID,
				Method:          r.Method,
				URL:             r.URL.String(),
				Payload:         payload,
				ResponsePayload: responsePayload,
				StatusCode:      ww.Status(), // ww récupère le vrai statut final
				IP:              r.RemoteAddr,
				DurationMs:      durationMs,
			})
		})
	}
}
