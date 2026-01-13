package middleware

import "github.com/rs/cors"

func CORSMiddleware() *cors.Cors {
	return cors.New(cors.Options{
		AllowedOrigins: []string{
			"https://mywelloresto.welloresto.fr",
			"https://wello-back-office.onrender.com",
			"*.lovable.app",
			"*.lovableproject.com",
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
			"X-CSRF-Token",
		},

		ExposedHeaders: []string{
			"Link",
		},

		AllowCredentials: true,
		MaxAge:           300, // cache preflight 5 min
	})
}
