package middleware

import "github.com/rs/cors"

func CORSMiddleware() *cors.Cors {
	return cors.New(cors.Options{
		AllowedOrigins: []string{
			// Prod
			"https://mywelloresto.welloresto.fr",
			"https://wello-back-office.onrender.com",

			// Lovable
			"*.lovableproject.com",
			"https://scannorder-test.lovable.app",

			// Postman
			"https://wello-resto.postman.co",

			// Dev
			"http://localhost:8080",
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
		},

		ExposedHeaders: []string{
			"Link",
		},

		AllowCredentials: true,
		MaxAge:           300, // cache preflight 5 min
	})
}
