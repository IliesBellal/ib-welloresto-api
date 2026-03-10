package main

import (
	"database/sql"
	"net/http"
	"os"
	"welloresto-api/internal/infrastructure/brevo_mailer"
	"welloresto-api/internal/infrastructure/brevo_sms"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/infrastructure/sms"
	stripeInternalClient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/modules/googlemaps"
	"welloresto-api/internal/modules/scannorder"
	"welloresto-api/internal/webhook/deliveroo_menu"
	"welloresto-api/internal/webhook/deliveroo_orders"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/config"
	"welloresto-api/internal/middleware"

	// ---- MODULES ----
	allergensModule "welloresto-api/internal/modules/allergens"
	authModule "welloresto-api/internal/modules/auth"
	bookingsModule "welloresto-api/internal/modules/bookings"
	cashregisterModule "welloresto-api/internal/modules/cash_registers"
	customersModule "welloresto-api/internal/modules/customers"
	deliverooModule "welloresto-api/internal/modules/deliveroo"
	deliverysessionsModule "welloresto-api/internal/modules/delivery_sessions"
	locModule "welloresto-api/internal/modules/locations"
	menuModule "welloresto-api/internal/modules/menu"
	notificationModule "welloresto-api/internal/modules/notification"
	ordersLCModule "welloresto-api/internal/modules/order_life_cycle"
	ordersModule "welloresto-api/internal/modules/orders"
	posModule "welloresto-api/internal/modules/pos"
	stocksModule "welloresto-api/internal/modules/stocks"
	tagsModule "welloresto-api/internal/modules/tags"
	uberModule "welloresto-api/internal/modules/ubereats"
	servicesModule "welloresto-api/internal/modules/user_services"
	usersModule "welloresto-api/internal/modules/users"

	redisclient "welloresto-api/internal/infrastructure/redis"

	// ---- WEBHOOKS ----
	webhookstripe "welloresto-api/internal/webhook/stripe"
	webhookuberheandler "welloresto-api/internal/webhook/ubereats/handler"
	webhookuberservice "welloresto-api/internal/webhook/ubereats/service"
)

func SetupRoutes(log *zap.Logger, mysqlDB *sql.DB, cfg *config.AppConfig) *chi.Mux {

	r := chi.NewRouter()

	// =============================
	//  GLOBAL MIDDLEWARES
	// =============================
	r.Use(middleware.CORSMiddleware().Handler)
	r.Use(middleware.LoggingMiddleware(log))
	// Il semblerait que ce middleware cause des timeout lors d'appels d'API uber eats, désactivé temporairement
	// r.Use(requestlogger.RequestLoggerMiddleware(requestlogger.NewLogger(mysqlDB, 1000)))

	// ============================
	// REDIS
	// ============================
	redisClient, err := redisclient.New()
	if err != nil {
		log.Error("Erreur lors de l'initialisation du client Redis", zap.Error(err))
	}
	log.Info("Redis connecté avec succès")

	// =============================
	//  MODULE INITIALIZATION
	// =============================

	// ---- MAILER & SMS (BREVO) ----
	// Initialize Brevo Email Service
	brevoMailService := brevo_mailer.NewBrevoMailer(brevo_mailer.Config{
		APIKey: cfg.Brevo.APIKey,
	})

	// Initialize Brevo SMS Service
	brevoSMSService := brevo_sms.NewBrevoSMS(brevo_sms.Config{
		APIKey: cfg.Brevo.APIKey,
	})

	// Use as mailer.Service for dependency injection
	var mailService mailer.Service = brevoMailService
	var smsService sms.Service = brevoSMSService

	// 2. Initialisation des couches (Injection de dépendances)
	repo := googlemaps.NewGoogleMapsRepository()
	googleClient := googlemaps.NewGoogleMapsClient(cfg.Google)
	svc := googlemaps.NewRouteService(repo, googleClient)
	routeHandler := googlemaps.NewRouteHandler(svc)

	// ---- Notification ---
	fcmClient := notificationModule.NewFCMClient()
	saPath := "/etc/secrets/wello-resto-150721-6d1253e00d6d.json"
	fcmTokenManager := notificationModule.NewGoogleFCMTokenManager(saPath)
	notificationRepo := notificationModule.NewNotificationRepository(mysqlDB, log)
	notificationService := notificationModule.NewNotificationService(notificationRepo, fcmClient, fcmTokenManager)

	// ---- Auth ----
	authRepo := authModule.NewAuthRepository(mysqlDB, redisClient)
	authService := authModule.NewAuthService(authRepo)

	// ---- POS ----
	posRepo := posModule.NewPOSRepository(mysqlDB)
	posService := posModule.NewPOSService(authService, posRepo)

	// ---- Menu ----
	menuRepoLegacy := menuModule.NewMenuRepository(mysqlDB)
	// NOTE: deliverooService and uberService are initialized below; we forward-declare menuService
	// and re-assign after their initialization using a late-binding approach via a pointer.
	// To keep initialization order clean, menuService is assigned after deliveroo/uber init.

	// ---- Orders ----
	ordersFetcher := ordersModule.NewOrdersFetcher(mysqlDB)
	ordersRepo := ordersModule.NewOrdersRepository(mysqlDB, ordersFetcher)
	deliverySessionsRepo := deliverysessionsModule.NewDeliverySessionsRepository(mysqlDB, ordersFetcher)
	ordersService := ordersModule.NewOrdersService(ordersRepo, authService, notificationService)

	// ---- WEBHOOK STRIPE
	// Dans main.go

	// 1. Initialiser le Repo
	stripeRepo := webhookstripe.NewRepository(mysqlDB) // db est ta connexion *sql.DB

	// 2. Initialiser les dépendances (Mocks ou implémentations réelles)
	// mailerService vient de l'étape précédente
	// Tu devras créer des struct simples qui implémentent MobileClient et OrderLifeCycleClient pour faire le lien avec ton code existant

	// 4. Utiliser stripeService dans ton Handler HTTP
	// ex: webhookHandler.HandleWebhook(w, r) -> switch event.Type -> stripeService.HandleCheckoutSessionCompleted(...)

	// ---- Deliveroo ----
	deliverooService := deliverooModule.NewDeliverooService(mysqlDB, cfg.Deliveroo)
	deliverooHandler := deliverooModule.NewDeliverooHandler(deliverooService)

	// ---- Customers ----
	customersRepo := customersModule.NewCustomerRepository(mysqlDB)
	customersService := customersModule.NewCustomersService(customersRepo, authService)

	// ---- Stripe ----
	stripeManager := stripeInternalClient.NewStripeManager(cfg.Stripe.APIKey)

	// ---- Uber ----
	uberService := uberModule.NewUberEatsService(mysqlDB, cfg.UberEats)

	// ---- Menu (initialized after deliveroo + uber) ----
	menuService := menuModule.NewMenuService(menuRepoLegacy, authService, deliverooService, uberService)

	// ---- ScanNOrder ----
	scannRepo := scannorder.NewRepository(mysqlDB)
	scannService := scannorder.NewService(cfg.ScanNOrder, scannRepo, menuService, ordersService, *stripeManager)
	scannHandler := scannorder.NewHandler(scannService)

	// ---- Orders Lifecycle ----
	ordersLifeCycleRepo := ordersLCModule.NewOrdersLifeCycleRepository(mysqlDB, ordersFetcher)
	ordersLifeCycleService := ordersLCModule.NewOrdersLifeCycleService(
		ordersLifeCycleRepo,
		stripeManager,
		uberService,
		deliverooService,
		deliverySessionsRepo,
		authService,
		log,
		notificationService,
		customersRepo,
	)

	// 3. Initialiser le StripeWebhookService Stripe
	stripeWebhookService := webhookstripe.NewStripeWebhookService(
		stripeRepo,
		os.Getenv("STRIPE_SECRET_KEY"),
		mailService,
		smsService,
		ordersLifeCycleService,
		notificationService,
	)
	stripeWebhookHandler := webhookstripe.NewHandler(stripeWebhookService)

	// WH
	deliverooWebhookRepo := deliveroo_orders.NewRepository(mysqlDB)
	deliverooWebhookService := deliveroo_orders.NewDeliverooService(deliverooWebhookRepo, ordersService, ordersLifeCycleService, deliverooService)
	deliverooWebhookHandler := deliveroo_orders.NewDeliverooHandler(deliverooWebhookService)

	deliverooMenuWebhookRepo := deliveroo_menu.NewRepository(mysqlDB)
	deliverooMenuWebhookService := deliveroo_menu.NewMenuWebhookService(deliverooMenuWebhookRepo, deliverooService)
	deliverooMenuWebhookHandler := deliveroo_menu.NewMenuWebhookHandler(deliverooMenuWebhookService)

	uberWebhookService := webhookuberservice.NewService(
		mysqlDB,
		"",
		"",
		uberService,
		&googleClient,
		ordersService,
		menuService,
		ordersLifeCycleService,
		notificationService,
	)

	uberWebhookHandler := webhookuberheandler.NewHandler(uberWebhookService)

	// ---- Delivery Sessions ----
	deliverySessionsService := deliverysessionsModule.NewDeliverySessionsService(deliverySessionsRepo, authService, notificationService)

	// ---- Locations ----
	locationsRepo := locModule.NewLocationsRepository(mysqlDB)
	locationsService := locModule.NewLocationsService(locationsRepo, authService)

	// ---- Cash Register ----
	cashRegisterRepo := cashregisterModule.NewCashRegisterRepository(mysqlDB, log)
	cashRegisterService := cashregisterModule.NewCashRegisterService(cashRegisterRepo, authService)

	// ---- Bookings ----
	bookingsRepo := bookingsModule.NewBookingsRepository(mysqlDB, log)
	bookingsService := bookingsModule.NewBookingsService(bookingsRepo, authService)

	// ---- Users ----
	usersRepo := usersModule.NewUserRepository(mysqlDB)
	usersService := usersModule.NewUsersService(usersRepo)

	// ---- Stocks ----
	stocksRepo := stocksModule.NewStockRepository(mysqlDB, log)
	stocksService := stocksModule.NewStockService(stocksRepo, authService)

	// ---- Services ----
	servicesRepo := servicesModule.NewServicesRepository(mysqlDB, log)
	servicesService := servicesModule.NewServicesService(servicesRepo, authService)

	// ---- Allergens ----
	allergensRepo := allergensModule.NewRepository(mysqlDB)
	allergensService := allergensModule.NewService(allergensRepo, authService)

	// ---- Tags ----
	tagsRepo := tagsModule.NewRepository(mysqlDB)
	tagsService := tagsModule.NewService(tagsRepo, authService)

	// =============================
	//  HANDLERS
	// =============================

	authH := authModule.NewAuthHandler(authService)
	posH := posModule.NewPOSHandler(posService)
	menuH := menuModule.NewMenuHandler(menuService)
	allergensH := allergensModule.NewHandler(allergensService)
	tagsH := tagsModule.NewHandler(tagsService)
	ordersH := ordersModule.NewOrdersHandler(ordersService)
	ordersLifeCycleH := ordersLCModule.NewOrdersLifeCycleHandler(ordersLifeCycleService, deliverySessionsService, notificationService)
	deliverySessionsH := deliverysessionsModule.NewDeliverySessionsHandler(deliverySessionsService)
	locationsH := locModule.NewLocationsHandler(locationsService)
	cashRegisterH := cashregisterModule.NewCashRegisterHandler(cashRegisterService)
	bookingsH := bookingsModule.NewBookingsHandler(bookingsService)
	customersH := customersModule.NewCustomersHandler(customersService)
	usersH := usersModule.NewUsersHandler(usersService)
	stocksH := stocksModule.NewStocksHandler(stocksService)
	servicesH := servicesModule.NewServicesHandler(servicesService)

	// ============================================================
	//                      CRON JOBS
	// ============================================================

	SetupTasks(log, &mailService, ordersLifeCycleService, stripeManager, bookingsService, mysqlDB)

	// ============================================================
	//                      ROUTING
	// ============================================================

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("OK"))
	})
	r.Route("/test", func(r chi.Router) {
		r.Get("/test-mailer", mailService.TriggerTestEmail)
		r.Get("/test-sms", smsService.TriggerTestSMS)

		r.Post("/deliveroo/brandID", deliverooHandler.SyncSiteBrandID)
		r.Post("/deliveroo/upload-menu", deliverooHandler.UploadTestMenu)
		r.Post("/deliveroo/unavailabilities", deliverooHandler.RunUnavailabilitiesScenario)
		r.Post("/deliveroo/9", deliverooHandler.HandleScenario9)
		r.Post("/deliveroo/10", deliverooHandler.HandleScenario10)
		r.Post("/deliveroo/11", deliverooHandler.HandleScenario11)
		r.Post("/deliveroo/12", deliverooHandler.HandleScenario12)
		r.Post("/deliveroo/13", deliverooHandler.HandleTriggerScenario13)
		r.Post("/deliveroo/14", deliverooHandler.HandleScenario14)
		r.Post("/deliveroo/15", deliverooHandler.HandleScenario15)
		r.Post("/deliveroo/16/{job_id}", deliverooHandler.HandleScenario16)
		r.Post("/deliveroo/17", deliverooHandler.HandleScenario17)
	})

	// Webhooks
	r.Route("/webhooks", func(r chi.Router) {
		r.Post("/uber-eats", uberWebhookHandler.HandleWebhook)
		r.Post("/deliveroo/orders", deliverooWebhookHandler.HandleOrdersWebhook)
		r.Post("/deliveroo/menu", deliverooMenuWebhookHandler.HandleMenuWebhook)
		r.Get("/deliveroo/menu", deliverooMenuWebhookHandler.HandleMenuWebhook)
		r.Post("/stripe", stripeWebhookHandler.HandleWebhook)
	})

	// API externes
	r.Route("/external", func(r chi.Router) {
		r.Get("/routes", routeHandler.HandleGetRoute)
	})

	// --- AUTH ---
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authH.Login)
		r.Post("/login", authH.Login)
	})

	// --- USERS ---
	r.Route("/users", func(r chi.Router) {
		r.Post("/create", usersH.CreateUser)
		r.Get("/{user_id}/location", usersH.GetUserLocation)
		r.Patch("/{user_id}/settings", usersH.UpdateUserSettings)
		r.Patch("/{user_id}/reset-password", usersH.UpdatePassword)
	})

	// --- POS ---
	r.Route("/pos", func(r chi.Router) {
		r.Post("/create", posH.CreateMerchant)
		r.Post("/link-user", posH.LinkUser)
		r.Get("/status", posH.GetPOSStatus)
		r.Patch("/status", posH.UpdatePOSStatus)

		r.Get("/deletion_reasons/{object}", posH.GetDeletionReasons)
		r.Get("/delivery_men", posH.GetDeliveryMen)
		r.Get("/users", posH.GetDeliveryMen)
		r.Get("/tva_rates", posH.GetTVARates)

		r.Route("/settings", func(r chi.Router) {
			r.Get("/", posH.GetSettings)
			r.Patch("/", posH.UpdateMerchantSettings)

			r.Patch("/scannorder", posH.ToggleScanNOrder)
			r.Patch("/production_paid_only", posH.ToggleProductionPaidOnly)
			r.Patch("/safety_stock", posH.ToggleSafetyStockActive)
		})

		r.Route("/tags", func(r chi.Router) {
			r.Get("/", tagsH.ListTags)
			r.Post("/create", tagsH.CreateTag)
			r.Delete("/{tag_id}", tagsH.DeleteTag)
		})

		r.Get("/payments/tr/check/{tr_code}", posH.CheckTR)
	})

	// --- SCANNORDER ---
	r.Route("/scannorder", func(r chi.Router) {
		r.Get("/brands/{slug}", scannHandler.GetBrand)
		r.Get("/{qr_code}", scannHandler.GetMerchant)
		r.Get("/{qr_code}/menu", scannHandler.GetMenu)
		r.Post("/{qr_code}/pricing", scannHandler.GetPricingSNO)
		r.Post("/{qr_code}/create", scannHandler.CreateOrderSNO)
		r.Get("/{qr_code}/order/{order_id}", scannHandler.GetOrderSNO)
		r.Post("/{qr_code}/order/{order_id}/cancel", scannHandler.CancelOrderSNO)
	})

	// --- STOCKS ---
	r.Route("/stocks", func(r chi.Router) {
		r.Get("/barcode/{barcode}", stocksH.GetBarcodeInfo)
		r.Post("/barcode/create", stocksH.CreateBarcode)
		r.Delete("/barcode/{barcode}", stocksH.DeleteBarcode)

		r.Post("/barcodes/scan", stocksH.AddStockBarcode)
		r.Patch("/loss", stocksH.SetStockLoss)
		r.Get("/products", stocksH.GetStockProducts)
	})

	// --- DEVICES ---
	r.Route("/device", func(r chi.Router) {
		r.Post("/token", authH.SaveDeviceToken)
	})

	// --- APP VERSION ---
	r.Route("/app", func(r chi.Router) {
		r.Post("/version/check", authH.CheckAppVersion)
	})

	// --- MENU ---
	r.Route("/menu", func(r chi.Router) {
		r.Get("/", menuH.GetMenu)
		r.Patch("/component/{component_id}/availability", menuH.SetComponentAvailability)
		r.Patch("/product/{product_id}/availability", menuH.SetProductAvailability)
		r.Post("/product", menuH.CreateProduct)
		r.Patch("/product/{product_id}", menuH.UpdateProduct)
		r.Patch("/product/{product_id}/attributes", menuH.UpdateProductAttributes)
		r.Get("/attributes", menuH.GetAttributes)
		r.Get("/units_of_measures", menuH.GetUnitsOfMeasures)

		r.Post("/product/create", menuH.CreateProduct)
		r.Get("/product/{product_id}", menuH.GetProduct)

		// --- Bulk assign (additive) ---
		r.Route("/bulk", func(r chi.Router) {
			r.Post("/tags/assign", menuH.BulkAssignTag)
			r.Post("/allergens/assign", menuH.BulkAssignAllergen)
		})

		// --- Allergens & Tags (full sync) ---
		r.Put("/product/{product_id}/allergens", menuH.SyncProductAllergens)
		r.Put("/product/{product_id}/tags", menuH.SyncProductTags)

		// --- Plateformes externes ---
		r.Get("/deliveroo", menuH.GetDeliverooMenu)
		r.Patch("/deliveroo/sync", menuH.SyncDeliverooMenu)
		r.Get("/uber-eats", menuH.GetUberEatsMenu)
		r.Patch("/uber-eats/sync", menuH.SyncUberEatsMenu)
	})

	// --- ALLERGENS (system-wide, read-only) ---
	r.Route("/allergens", func(r chi.Router) {
		r.Get("/", allergensH.ListAllergens)
	})

	// --- LOCATIONS ---
	r.Route("/locations", func(r chi.Router) {
		r.Get("/", locationsH.GetLocations)
		r.Patch("/{location_id}/coordinates", locationsH.UpdateLocationCoordinates)
	})

	// --- SERVICES ---
	r.Route("/services", func(r chi.Router) {
		r.Get("/{device_id}", servicesH.GetCurrentService)
	})

	// --- ORDERS ---
	r.Route("/orders", func(r chi.Router) {

		r.Post("/create", ordersH.CreateOrder)
		r.Post("/pricing", ordersH.GetPricing)
		r.Post("/list", ordersH.GetOrders)

		r.Get("/pending", ordersH.GetPendingOrders)
		r.Post("/history", ordersH.GetHistory)
		r.Get("/{order_id}", ordersH.GetOrder)

		r.Post("/{order_id}/update", ordersH.UpdateOrder)

		r.Patch("/{order_id}/reopen", ordersLifeCycleH.ReopenClosedOrder)

		r.Patch("/{order_id}/accept", ordersLifeCycleH.AcceptOrder)
		r.Patch("/{order_id}/deny", ordersLifeCycleH.DenyOrder)

		r.Patch("/{order_id}/cancel", ordersLifeCycleH.DeleteOrder)

		r.Patch("/{order_id}/delivered", ordersLifeCycleH.SetDelivered)
		r.Patch("/{order_id}/delivery-start", ordersLifeCycleH.StartDelivery)

		r.Patch("/{order_id}/distributed", ordersLifeCycleH.SetReadyForDistribution)
		r.Patch("/{order_id}/distributed-products", ordersLifeCycleH.SetDistributedProducts)
		r.Patch("/multiple-production-status", ordersLifeCycleH.UpdateProductionStatus)

		r.Route("/{order_id}/payments", func(r chi.Router) {
			r.Post("/create", ordersLifeCycleH.AddPayment)
			r.Get("/", ordersLifeCycleH.GetPayments)
			r.Delete("/{payment_id}", ordersLifeCycleH.DeletePayment)
		})
	})

	// --- DELIVERY SESSIONS ---
	r.Route("/delivery_sessions", func(r chi.Router) {
		r.Get("/pending", deliverySessionsH.GetPendingDeliverySessions)
		r.Get("/{delivery_session_id}", deliverySessionsH.GetDeliverySession)
		r.Delete("/{delivery_session_id}", deliverySessionsH.CancelDeliverySession)
		r.Patch("/{delivery_session_id}/close", deliverySessionsH.CloseDeliverySession)

		r.Post("/start", deliverySessionsH.StartDeliverySession)
	})

	// --- CASH DRAWER ---
	r.Route("/cash_drawer", func(r chi.Router) {
		r.Get("/open", cashRegisterH.OpenCashDrawer)
	})

	// --- CUSTOMERS ---
	r.Route("/customer", func(r chi.Router) {
		r.Get("/search", customersH.SearchCustomers)
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
			r.Get("/tva-details", cashRegisterH.GetCashRegisterTVADetails)
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

		r.Patch("/{booking_id}/accept", bookingsH.AcceptBooking)
		r.Patch("/{booking_id}/deny", bookingsH.DenyBooking)
	})

	return r
}
