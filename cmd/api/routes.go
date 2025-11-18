package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto/internal/handlers"
	"welloresto/internal/middleware"
	"welloresto/internal/repositories"
	"welloresto/internal/services"
)

func SetupRoutes(log *zap.Logger, mysqlDB *sql.DB, cfg Config) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.Recoverer)

	// --- Repositories ---
	userRepo := repositories.NewUserRepository(mysqlDB)

	// --- Services ---
	authService := services.NewAuthService(userRepo)
	userService := services.NewUserService(userRepo)

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService)

	// --- Routes ---
	r.Get("/health", handlers.HealthCheck)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authHandler.Login)
		r.Get("/logintoken", authHandler.Login) // compatibilité API existante
	})

	r.Route("/users", func(r chi.Router) {
		r.Use(middleware.RequireAuth(authService))
		r.Get("/me", userHandler.GetMe)
	})

	return r
}
