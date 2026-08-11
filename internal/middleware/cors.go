package middleware

import (
	"net/http"

	"github.com/rs/cors"
)

// SetCORSHeaders ajoute manuellement les headers CORS à une réponse
// Utilisé quand on doit envoyer une réponse d'erreur avant que le middleware CORS puisse s'exécuter
func SetCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")

	// Liste des origines autorisées (doit correspondre à CORSMiddleware)
	allowedOrigins := map[string]bool{
		"https://mywelloresto.welloresto.fr": true,

		"https://my-wello-resto-prod.onrender.com":    true,
		"https://my-wello-resto-staging.onrender.com": true,
		"https://my.welloresto.fr":                    true,

		"https://wello-resto-scannorder-staging.onrender.com": true,
		"https://wello-resto-scannorder-prod.onrender.com":    true,
		"https://scannorder.welloresto.fr":                    true,

		"https://rsv-staging.onrender.com": true,
		"https://rsv.onrender.com":         true,
		"https://rsv.welloresto.fr":        true,

		"https://wello-resto.postman.co": true,

		"http://localhost:8080": true,
		"http://localhost:8081": true,
	}

	// Vérifier si l'origine est autorisée
	if allowedOrigins[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-BasicAuth, X-App-Source, Idempotency-Key")
		w.Header().Set("Access-Control-Expose-Headers", "Link")
	}
}

func CORSMiddleware() *cors.Cors {
	return cors.New(cors.Options{
		AllowedOrigins: []string{
			// Prod
			"https://mywelloresto.welloresto.fr",
			"https://wello-back-office.onrender.com",
			"https://my-wello-resto-prod.onrender.com",
			"https://my-wello-resto-staging.onrender.com",

			// RSV
			"https://rsv-staging.onrender.com",
			"https://rsv-prod.onrender.com",
			"https://rsv.onrender.com",
			"https://rsv.welloresto.fr",

			// Postman
			"https://wello-resto.postman.co",

			// Dev
			"http://localhost:8080",
			"http://localhost:8081",

			// ScanNOrder
			"https://scannorder.welloresto.fr",
			"https://wello-resto-scannorder-staging.onrender.com",
			"https://wello-resto-scannorder-prod.onrender.com",
			"https://my.welloresto.fr",
		},

		AllowedMethods: []string{
			"GET",
			"POST",
			"PUT",
			"PATCH",
			"DELETE",
			"OPTIONS",
		},

		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
			"X-CSRF-BasicAuth",
			"X-App-Source",
			"Idempotency-Key",
		},

		ExposedHeaders: []string{
			"Link",
		},

		AllowCredentials: true,
		MaxAge:           300, // cache preflight 5 min
	})
}
