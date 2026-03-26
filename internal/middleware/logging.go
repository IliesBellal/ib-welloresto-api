package middleware

import (
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"welloresto-api/internal/logger"
)

func LoggingMiddleware(log *zap.Logger) func(http.Handler) http.Handler {
	env := os.Getenv("ENV")

	logPayload := os.Getenv("LOG_PAYLOAD") != "false"

	slowThreshold := 500 * time.Millisecond
	verySlowThreshold := 2 * time.Second

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := uuid.NewString()

			// Récupère le pattern chi
			routePattern := chi.RouteContext(r.Context()).RoutePattern()
			if routePattern == "" {
				routePattern = r.URL.Path // fallback
			}

			endpoint := r.Method + " " + routePattern

			// Logger enrichi UNE FOIS ICI
			log := log.With(
				zap.String("request_id", requestID),
				zap.String("endpoint", endpoint),
				zap.String("method", r.Method),
				//zap.String("path", r.URL.Path),
				//zap.String("ip", r.RemoteAddr),
			)

			ctx := logger.WithRequestID(r.Context(), requestID)
			r = r.WithContext(ctx)

			rw := &responseWriter{ResponseWriter: w}

			var body string
			if logPayload && env != "production" {
				if b, err := readRequestBody(r); err == nil {
					body = sanitizePayload(string(b))
				}
			}

			log.Debug("request started",
				//zap.String("request_id", requestID),
				//zap.String("method", r.Method),
				//zap.String("path", r.URL.Path),
				zap.String("query", r.URL.RawQuery),
				//zap.String("ip", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()),
				zap.String("body", body),
			)

			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						zap.String("request_id", requestID),
						zap.Any("panic", rec),
						zap.Stack("stack"),
					)
					http.Error(rw, "internal server error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			level := zap.InfoLevel

			if rw.status >= 500 {
				level = zap.ErrorLevel
			} else if duration > verySlowThreshold || (rw.status >= 400 && rw.status < 500) {
				level = zap.WarnLevel
			} else if duration > slowThreshold {
				level = zap.InfoLevel
			}

			log.Log(level, "request completed",
				zap.String("request_id", requestID),
				zap.Int("status", rw.status),
				zap.Int("response_size", rw.size),
				zap.Duration("duration", duration),
			)
		})
	}
}
