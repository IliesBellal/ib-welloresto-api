package helpers

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// helper to extract token either from Authorization header (Bearer ...) or token query param
func ExtractToken(r *http.Request) string {
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

func handleJSON(w http.ResponseWriter, payload interface{}, err error) {
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

// ClientIP returns the caller's IP address, preferring the leftmost entry of
// X-Forwarded-For (the API runs behind a reverse proxy, where RemoteAddr is
// the proxy's address) and falling back to RemoteAddr.
//
// X-Forwarded-For is client-spoofable, so the result must only be used for
// best-effort throttling and traceability — never as a security boundary.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, found := strings.Cut(xff, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}

	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
