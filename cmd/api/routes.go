package main

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/config"

	"welloresto-api/internal/handlers"
	"welloresto-api/internal/repositories"
	"welloresto-api/internal/services"
)

func SetupRoutes(log *zap.Logger, mysqlDB *sql.DB, cfg config.Config) *chi.Mux {
	r := chi.NewRouter()

	// r.Use(middleware.RequestLogger(log))
	// r.Use(middleware.Recoverer)
	// r.Use(middleware.ExtractToken)

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
	ordersLifecycleRepo := repositories.NewOrdersLifeCycleRepository(mysqlDB, log)
	deliverySessionsRepo := repositories.NewDeliverySessionsRepository(mysqlDB, log)
	cashDrawerRepo := repositories.NewCashDrawerRepository(mysqlDB, log)
	locationsRepo := repositories.NewLocationsRepository(mysqlDB, log)
	cashRegisterRepo := repositories.NewCashRegisterRepository(mysqlDB, log)
	bookingsRepo := repositories.NewBookingsRepository(mysqlDB, log)
	customersRepo := repositories.NewCustomerRepository(mysqlDB, log)
	stocksRepo := repositories.NewStockRepository(mysqlDB, log)

	// --- Services ---
	authService := services.NewAuthService(userRepo)
	posService := services.NewPOSService(userRepo, posRepo)
	deviceService := services.NewDeviceService(userRepo, deviceRepo)
	appVersionService := services.NewAppVersionService(appVersionRepo, userRepo)
	menuService := services.NewMenuService(userRepo, menuRepoLegacy, menuRepoOpti, false)
	ordersService := services.NewOrdersService(ordersRepo, deliverySessionsRepo, userRepo)
	ordersLifecycleService := services.NewOrdersLifeCycleService(ordersLifecycleRepo, deliverySessionsRepo, userRepo)
	deliverySessionsService := services.NewDeliverySessionsService(deliverySessionsRepo, userRepo)
	cashDrawerService := services.NewCashDrawerService(cashDrawerRepo, userRepo)
	locationsService := services.NewLocationsService(locationsRepo, userRepo)
	cashRegisterService := services.NewCashRegisterService(cashRegisterRepo, userRepo)
	bookingsService := services.NewBookingsService(bookingsRepo, userRepo)
	customersService := services.NewCustomersService(customersRepo, userRepo)
	usersService := services.NewUsersService(userRepo)
	stocksService := services.NewStockService(stocksRepo, userRepo)

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(authService)
	posHandler := handlers.NewPOSHandler(posService)
	deviceHandler := handlers.NewDeviceHandler(deviceService)
	appVersionHandler := handlers.NewAppVersionHandler(appVersionService)
	menuHandler := handlers.NewMenuHandler(menuService)
	ordersHandler := handlers.NewOrdersHandler(ordersService, deliverySessionsService)
	ordersLifeCycleHandler := handlers.NewOrdersLifeCycleHandler(ordersLifecycleService, deliverySessionsService)
	deliverySessionsHandler := handlers.NewDeliverySessionsHandler(deliverySessionsService)
	cashDrawerHandler := handlers.NewCashDrawerHandler(cashDrawerService)
	locationsHandler := handlers.NewLocationsHandler(locationsService)
	cashRegisterHandler := handlers.NewCashRegisterHandler(cashRegisterService)
	bookingsHandler := handlers.NewBookingsHandler(bookingsService)
	customersHandler := handlers.NewCustomersHandler(customersService)
	usersHandler := handlers.NewUsersHandler(usersService)
	stocksHandler := handlers.NewStocksHandler(stocksService, usersService)

	// --- Routes ---
	// r.Get("/health", handlers.HealthCheck)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authHandler.Login)
	})

	r.Route("/users", func(r chi.Router) {
		r.Get("/{user_id}/location", usersHandler.GetUserLocation)
	})

	r.Route("/pos", func(r chi.Router) {
		r.Get("/status", posHandler.GetPOSStatus)
		r.Patch("/status", posHandler.UpdatePOSStatus)
		r.Get("/deletion_reasons/{object}", posHandler.GetDeletionReasons)

		r.Get("/delivery_men", posHandler.GetDeliveryMen)

		r.Patch("/settings/scannorder", posHandler.ToggleScanNOrder)
		r.Patch("/settings/production_paid_only", posHandler.ToggleProductionPaidOnly)
		r.Patch("/settings/safety_stock", posHandler.ToggleSafetyStockActive)

		r.Get("/payments/tr/check/{tr_code}", posHandler.CheckTR)
	})

	r.Route("/stocks", func(r chi.Router) {

		r.Get("/barcode/{barcode_id}", stocksHandler.GetBarcodeInfo)
		r.Post("/barcode/create", stocksHandler.CreateBarcode)
		r.Delete("/barcode/{barcode_id}", stocksHandler.DeleteBarcode)
		r.Post("/barcodes/scan", stocksHandler.AddStockBarcode)

		r.Post("/barcodes/scan", stocksHandler.AddStockBarcode)
		r.Patch("/loss", stocksHandler.SetStockLoss)
		r.Get("/products", stocksHandler.GetStockProducts)
	})

	r.Route("/device", func(r chi.Router) {
		r.Post("/token", deviceHandler.SaveDeviceToken)
	})

	r.Route("/app", func(r chi.Router) {
		r.Post("/version/check", appVersionHandler.CheckAppVersion)
	})

	r.Route("/menu", func(r chi.Router) {
		r.Get("/", menuHandler.GetMenu)

		r.Patch("/component/{component_id}/availability", menuHandler.SetComponentAvailability)
		r.Patch("/product/{product_id}/availability", menuHandler.SetProductAvailability)
	})

	r.Route("/locations", func(r chi.Router) {
		r.Get("/", locationsHandler.GetLocations)

		r.Patch("/{location_id}/coordinates", locationsHandler.UpdateLocationCoordinates)

	})

	r.Route("/orders", func(r chi.Router) {
		r.Get("/pending", ordersHandler.GetPendingOrders)
		r.Post("/history", ordersHandler.GetHistory)
		r.Get("/{order_id}", ordersHandler.GetOrder)

		r.Post("/{order_id}/reopen", ordersLifeCycleHandler.ReopenClosedOrder)
		r.Post("/orders/{order_id}/distributed_products", ordersLifeCycleHandler.SetDistributedProducts)

		r.Post("/{order_id}/payments", ordersLifeCycleHandler.AddPayment)
		r.Get("/{order_id}/payments", ordersLifeCycleHandler.GetPayments)
		r.Delete("/{order_id}/payments/{payment_id}", ordersLifeCycleHandler.DeletePayment)
	})

	r.Route("/delivery_sessions", func(r chi.Router) {
		r.Get("/pending", deliverySessionsHandler.GetPendingDeliverySessions)
		r.Get("/{delivery_session_id}", deliverySessionsHandler.GetDeliverySession)
		r.Delete("/{delivery_session_id}", deliverySessionsHandler.CancelDeliverySession)
		r.Post("/{delivery_session_id}/close", deliverySessionsHandler.CloseDeliverySession)

		r.Post("/start", deliverySessionsHandler.StartDeliverySession)
	})

	r.Route("/cash_drawer", func(r chi.Router) {
		r.Get("/open", cashDrawerHandler.OpenCashDrawer)
	})

	r.Route("/customer", func(r chi.Router) {
		r.Get("/{customer_id}/loyalty", customersHandler.GetCustomerLoyalty)

		r.Patch("/{customer_id}/loyalty/progress", customersHandler.UpdateLoyaltyProgress)
		r.Patch("/{customer_id}/loyalty/reward", customersHandler.UpdateLoyaltyReward)
	})

	r.Route("/cash_register", func(r chi.Router) {
		r.Post("/open", cashRegisterHandler.OpenCashRegister)
		r.Get("/history", cashRegisterHandler.GetHistory)

		r.Get("/{cash_register_id}/summary", cashRegisterHandler.GetCashRegisterSummary)
		r.Get("/{cash_register_id}/tva_details", cashRegisterHandler.GetCashRegisterTVADetails)
		r.Patch("/{cash_register_id}/close", cashRegisterHandler.CloseCashRegister)
		r.Patch("/{cash_register_id}/enclose", cashRegisterHandler.EncloseCashRegister)

		r.Post("/{cash_register_id}/custom_items", cashRegisterHandler.AddCustomItem)
		r.Delete("/{cash_register_id}/custom_items/{item_id}", cashRegisterHandler.DeleteCustomItem)
	})

	r.Route("/bookings", func(r chi.Router) {
		r.Post("/", bookingsHandler.SearchBookings)

		r.Get("/availability/{date}", bookingsHandler.GetBookingAvailability)

		r.Post("/create", bookingsHandler.CreateBooking)
		r.Get("/{booking_id}", bookingsHandler.GetBooking)

		r.Post("/{booking_id}/accept", bookingsHandler.AcceptBooking)
		r.Post("/{booking_id}/deny", bookingsHandler.DenyBooking)
	})

	return r
}
