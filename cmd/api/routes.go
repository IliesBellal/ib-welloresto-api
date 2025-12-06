package main

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/config"
	"welloresto-api/internal/handlers"

	"welloresto-api/internal/repositories"
	"welloresto-api/internal/services"
)

func SetupRoutes(log *zap.Logger, mysqlDB *sql.DB, cfg config.Config) *chi.Mux {
	r := chi.NewRouter()

	// --- Global Middlewares ---
	// r.Use(middleware.RecoveryMiddleware(log))
	// r.Use(middleware.LoggingMiddleware(log))
	// r.Use(middleware.ExtractTokenMiddleware)

	// --- Repositories ---
	userRepo := repositories.NewUserRepository(mysqlDB)
	posRepo := repositories.NewPOSRepository(mysqlDB)
	deviceRepo := repositories.NewDeviceRepository(mysqlDB)
	appVersionRepo := repositories.NewAppVersionRepository(mysqlDB)

	menuRepoOpt := repositories.NewOptimizedMenuRepository(mysqlDB)
	menuRepoLegacy := repositories.NewMenuRepository(mysqlDB, log)

	ordersRepo := repositories.NewOrdersRepository(mysqlDB, log)
	ordersLifeCycleRepo := repositories.NewOrdersLifeCycleRepository(mysqlDB, log)
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
	menuService := services.NewMenuService(userRepo, menuRepoLegacy, menuRepoOpt, false)
	ordersService := services.NewOrdersService(ordersRepo, deliverySessionsRepo, userRepo)
	ordersLifeCycleService := services.NewOrdersLifeCycleService(ordersLifeCycleRepo, deliverySessionsRepo, userRepo)
	deliverySessionsService := services.NewDeliverySessionsService(deliverySessionsRepo, userRepo)
	cashDrawerService := services.NewCashDrawerService(cashDrawerRepo, userRepo)
	locationsService := services.NewLocationsService(locationsRepo, userRepo)
	cashRegisterService := services.NewCashRegisterService(cashRegisterRepo, userRepo)
	bookingsService := services.NewBookingsService(bookingsRepo, userRepo)
	customersService := services.NewCustomersService(customersRepo, userRepo)
	usersService := services.NewUsersService(userRepo)
	stocksService := services.NewStockService(stocksRepo, userRepo)

	// --- Handlers ---
	authH := handlers.NewAuthHandler(authService)
	posH := handlers.NewPOSHandler(posService)
	deviceH := handlers.NewDeviceHandler(deviceService)
	appVersionH := handlers.NewAppVersionHandler(appVersionService)
	menuH := handlers.NewMenuHandler(menuService)
	ordersH := handlers.NewOrdersHandler(ordersService, deliverySessionsService)
	ordersLifeCycleH := handlers.NewOrdersLifeCycleHandler(ordersLifeCycleService, deliverySessionsService)
	deliverySessionsH := handlers.NewDeliverySessionsHandler(deliverySessionsService)
	cashDrawerH := handlers.NewCashDrawerHandler(cashDrawerService)
	locationsH := handlers.NewLocationsHandler(locationsService)
	cashRegisterH := handlers.NewCashRegisterHandler(cashRegisterService)
	bookingsH := handlers.NewBookingsHandler(bookingsService)
	customersH := handlers.NewCustomersHandler(customersService)
	usersH := handlers.NewUsersHandler(usersService)
	stocksH := handlers.NewStocksHandler(stocksService, usersService)

	// ============================================================
	//                      ROUTING
	// ============================================================

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("OK"))
	})

	// --- AUTH ---
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authH.Login)
	})

	// --- USERS ---
	r.Route("/users", func(r chi.Router) {
		r.Get("/{user_id}/location", usersH.GetUserLocation)
	})

	// --- POS ---
	r.Route("/pos", func(r chi.Router) {
		r.Get("/status", posH.GetPOSStatus)
		r.Patch("/status", posH.UpdatePOSStatus)

		r.Get("/deletion_reasons/{object}", posH.GetDeletionReasons)
		r.Get("/delivery_men", posH.GetDeliveryMen)

		r.Route("/settings", func(r chi.Router) {
			r.Patch("/scannorder", posH.ToggleScanNOrder)
			r.Patch("/production_paid_only", posH.ToggleProductionPaidOnly)
			r.Patch("/safety_stock", posH.ToggleSafetyStockActive)
		})

		r.Get("/payments/tr/check/{tr_code}", posH.CheckTR)
	})

	// --- STOCKS ---
	r.Route("/stocks", func(r chi.Router) {
		r.Get("/barcode/{barcode_id}", stocksH.GetBarcodeInfo)
		r.Post("/barcode", stocksH.CreateBarcode)
		r.Delete("/barcode/{barcode_id}", stocksH.DeleteBarcode)

		r.Post("/barcodes/scan", stocksH.AddStockBarcode)
		r.Patch("/loss", stocksH.SetStockLoss)
		r.Get("/products", stocksH.GetStockProducts)
	})

	// --- DEVICES ---
	r.Route("/device", func(r chi.Router) {
		r.Post("/token", deviceH.SaveDeviceToken)
	})

	// --- APP VERSION ---
	r.Route("/app", func(r chi.Router) {
		r.Post("/version/check", appVersionH.CheckAppVersion)
	})

	// --- MENU ---
	r.Route("/menu", func(r chi.Router) {
		r.Get("/", menuH.GetMenu)
		r.Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
		r.Patch("/product/{product_id}/availability", menuH.SetProductAvailability)
	})

	// --- LOCATIONS ---
	r.Route("/locations", func(r chi.Router) {
		r.Get("/", locationsH.GetLocations)
		r.Patch("/{location_id}/coordinates", locationsH.UpdateLocationCoordinates)
	})

	// --- ORDERS ---
	r.Route("/orders", func(r chi.Router) {

		r.Post("/create", ordersH.CreateOrder)
		r.Post("/pricing", ordersH.GetPricing)

		r.Get("/pending", ordersH.GetPendingOrders)
		r.Post("/history", ordersH.GetHistory)
		r.Get("/{order_id}", ordersH.GetOrder)

		r.Patch("/{order_id}/reopen", ordersLifeCycleH.ReopenClosedOrder)
		r.Post("/{order_id}/distributed_products", ordersLifeCycleH.SetDistributedProducts)

		r.Route("/{order_id}/payments", func(r chi.Router) {
			r.Post("/", ordersLifeCycleH.AddPayment)
			r.Get("/", ordersLifeCycleH.GetPayments)
			r.Delete("/{payment_id}", ordersLifeCycleH.DeletePayment)
		})
	})

	// --- DELIVERY SESSIONS ---
	r.Route("/delivery_sessions", func(r chi.Router) {
		r.Get("/pending", deliverySessionsH.GetPendingDeliverySessions)
		r.Get("/{delivery_session_id}", deliverySessionsH.GetDeliverySession)
		r.Delete("/{delivery_session_id}", deliverySessionsH.CancelDeliverySession)
		r.Post("/{delivery_session_id}/close", deliverySessionsH.CloseDeliverySession)

		r.Post("/start", deliverySessionsH.StartDeliverySession)
	})

	// --- CASH DRAWER ---
	r.Route("/cash_drawer", func(r chi.Router) {
		r.Get("/open", cashDrawerH.OpenCashDrawer)
	})

	// --- CUSTOMERS ---
	r.Route("/customer", func(r chi.Router) {
		r.Get("/{customer_id}/loyalty", customersH.GetCustomerLoyalty)
		r.Patch("/{customer_id}/loyalty/progress", customersH.UpdateLoyaltyProgress)
		r.Patch("/{customer_id}/loyalty/reward", customersH.UpdateLoyaltyReward)
	})

	// --- CASH REGISTER ---
	r.Route("/cash_register", func(r chi.Router) {
		r.Post("/open", cashRegisterH.OpenCashRegister)
		r.Get("/history", cashRegisterH.GetHistory)

		r.Route("/{cash_register_id}", func(r chi.Router) {
			r.Get("/summary", cashRegisterH.GetCashRegisterSummary)
			r.Get("/tva_details", cashRegisterH.GetCashRegisterTVADetails)
			r.Patch("/close", cashRegisterH.CloseCashRegister)
			r.Patch("/enclose", cashRegisterH.EncloseCashRegister)

			r.Post("/custom_items", cashRegisterH.AddCustomItem)
			r.Delete("/custom_items/{item_id}", cashRegisterH.DeleteCustomItem)
		})
	})

	// --- BOOKINGS ---
	r.Route("/bookings", func(r chi.Router) {
		r.Post("/", bookingsH.SearchBookings)
		r.Get("/availability/{date}", bookingsH.GetBookingAvailability)

		r.Post("/create", bookingsH.CreateBooking)
		r.Get("/{booking_id}", bookingsH.GetBooking)

		r.Post("/{booking_id}/accept", bookingsH.AcceptBooking)
		r.Post("/{booking_id}/deny", bookingsH.DenyBooking)
	})

	return r
}
