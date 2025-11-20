package main

import (
	"database/sql"
	"net/http"
	"time"

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

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			log.Info("request completed",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Duration("duration", time.Since(start)),
			)
		})
	})

	// --- Repositories ---
	userRepo := repositories.NewUserRepository(mysqlDB)
	posRepo := repositories.NewPOSRepository(mysqlDB)
	deviceRepo := repositories.NewDeviceRepository(mysqlDB)
	appVersionRepo := repositories.NewAppVersionRepository(mysqlDB)

	menuRepoOpti := repositories.NewOptimizedMenuRepository(mysqlDB)
	menuRepoLegacy := repositories.NewMenuRepository(mysqlDB, log)

	ordersRepo := repositories.NewOrdersRepository(mysqlDB, log)
	deliverySessionsRepo := repositories.NewDeliverySessionsRepository(mysqlDB, log)

	// --- Services ---
	authService := services.NewAuthService(userRepo)
	posService := services.NewPOSService(userRepo, posRepo)
	deviceService := services.NewDeviceService(userRepo, deviceRepo)
	appVersionService := services.NewAppVersionService(appVersionRepo, userRepo)
	menuService := services.NewMenuService(userRepo, menuRepoLegacy, menuRepoOpti, false)
	ordersService := services.NewOrdersService(ordersRepo, deliverySessionsRepo, userRepo)
	deliverySessionsService := services.NewDeliverySessionsService(deliverySessionsRepo, userRepo)

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(authService)
	posHandler := handlers.NewPOSHandler(posService)
	deviceHandler := handlers.NewDeviceHandler(deviceService)
	appVersionHandler := handlers.NewAppVersionHandler(appVersionService)
	menuHandler := handlers.NewMenuHandler(menuService)
	ordersHandler := handlers.NewOrdersHandler(ordersService, deliverySessionsService)
	deliverySessionsHandler := handlers.NewDeliverySessionsHandler(deliverySessionsService)

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

	r.Route("/menu", func(r chi.Router) {
		r.Get("/", menuHandler.GetMenu)
	})

	r.Route("/orders", func(r chi.Router) {
		r.Get("/pending", ordersHandler.GetPendingOrders) // GET /orders/pending
	})

	r.Route("/delivery_sessions", func(r chi.Router) {
		r.Get("/pending", deliverySessionsHandler.GetPendingDeliverySessions) // GET /delivery/sessions
	})

	return r
}
