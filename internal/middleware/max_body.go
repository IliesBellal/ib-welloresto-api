package middleware

import (
	"net/http"
	"strings"
)

// MaxBodyForPrefix borne le corps des requêtes dont le chemin commence par
// prefix. À poser à la racine du routeur, AVANT LoggingMiddleware et
// RequestLoggerMiddleware : tous deux lisent le corps en entier (et le second
// l'écrit dans api_request_logs), si bien qu'une limite posée plus bas, sur un
// groupe de routes, arriverait trop tard. Un corps trop gros est tronqué à max
// octets : le JSON devient illisible et le handler répond 400.
//
// Limité à un préfixe plutôt que global : d'autres routes (uploads, imports de
// menu, photos HACCP) ont des corps légitimement plus gros.
func MaxBodyForPrefix(prefix string, max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && strings.HasPrefix(r.URL.Path, prefix) {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}
