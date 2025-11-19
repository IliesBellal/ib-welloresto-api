package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/config"

	"welloresto-api/internal/handlers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/repositories"
	"welloresto-api/internal/services"
)

func SetupRoutes(log *zap.Logger, mysqlDB *sql.DB, cfg config.Config) *chi.Mux {
	r := chi.NewRouter()

	// r.Use(middleware.RequestLogger(log))
	// r.Use(middleware.Recoverer)
	r.Use(middleware.ExtractToken)

	// --- Repositories ---
	userRepo := repositories.NewUserRepository(mysqlDB)
	posRepo := repositories.NewPOSRepository(mysqlDB)
	deviceRepo := repositories.NewDeviceRepository(mysqlDB)
	appVersionRepo := repositories.NewAppVersionRepository(mysqlDB)

	// --- Services ---
	authService := services.NewAuthService(userRepo)
	posService := services.NewPOSService(userRepo, posRepo)
	deviceService := services.NewDeviceService(userRepo, deviceRepo)
	appVersionService := services.NewAppVersionService(appVersionRepo, userRepo)

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(authService)
	posHandler := handlers.NewPOSHandler(posService)
	deviceHandler := handlers.NewDeviceHandler(deviceService)
	appVersionHandler := handlers.NewAppVersionHandler(appVersionService)

	// --- Routes ---
	// r.Get("/health", handlers.HealthCheck)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authHandler.Login)
	})

	r.Route("/pos", func(r chi.Router) {
		r.Get("/status", posHandler.GetPOSStatus)
		r.Patch("/status", posHandler.UpdatePOSStatus)
	})

	r.Route("/device", func(r chi.Router) {
		r.Post("/token", deviceHandler.SaveDeviceToken)
	})

	r.Route("/app", func(r chi.Router) {
		r.Post("/version/check", appVersionHandler.CheckAppVersion)
	})

	return r
}
