package handlers

import (
	"net/http"
	"strings"
)

// helper to extract token either from Authorization header (Bearer ...) or token query param
func extractToken(r *http.Request) string {
	// Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		// allow "Bearer <token>" or raw token
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			return strings.TrimSpace(auth[7:])
		}
		return strings.TrimSpace(auth)
	}
	// fallback to query param token (legacy)
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}
