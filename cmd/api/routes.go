package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/config"
    
	"welloresto-api/internal/handlers"
	// "welloresto-api/internal/middleware"
	"welloresto-api/internal/repositories"
	"welloresto-api/internal/services"
)

func SetupRoutes(log *zap.Logger, mysqlDB *sql.DB, cfg config.Config) *chi.Mux {
	r := chi.NewRouter()

	// r.Use(middleware.RequestLogger(log))
	// r.Use(middleware.Recoverer)

	// --- Repositories ---
	userRepo := repositories.NewUserRepository(mysqlDB)

	// --- Services ---
	authService := services.NewAuthService(userRepo)

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(authService)

	// --- Routes ---
	// r.Get("/health", handlers.HealthCheck)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authHandler.Login)
		r.Get("/logintoken", authHandler.Login) // compatibilité API existante
	})

	return r
}