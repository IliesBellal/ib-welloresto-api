package middleware

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey string

const TokenKey ctxKey = "authToken"

// ExtractToken middleware: reads Authorization OR ?token=
func ExtractToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// 1. Authorization header
		auth := r.Header.Get("Authorization")
		if auth != "" {
			// remove "Bearer "
			if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				auth = auth[7:]
			}
			auth = strings.TrimSpace(auth)

			ctx := context.WithValue(r.Context(), TokenKey, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 2. token passed as URL param
		tokenParam := r.URL.Query().Get("token")
		if tokenParam != "" {
			ctx := context.WithValue(r.Context(), TokenKey, tokenParam)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 3. No token -> empty
		ctx := context.WithValue(r.Context(), TokenKey, "")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Helper: retrieves token anywhere
func GetToken(r *http.Request) string {
	token := r.Context().Value(TokenKey)
	if token == nil {
		return ""
	}
	return token.(string)
}
