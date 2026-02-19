package main

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"
	"welloresto-api/internal/infrastructure/mailer"
	stripeInternalClient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/modules/googlemaps"
	"welloresto-api/internal/modules/scannorder"
	"welloresto-api/internal/webhook/deliveroo"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/config"
	"welloresto-api/internal/middleware"

	// ---- MODULES ----
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
	uberModule "welloresto-api/internal/modules/ubereats"
	servicesModule "welloresto-api/internal/modules/user_services"
	usersModule "welloresto-api/internal/modules/users"

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

	// =============================
	//  MODULE INITIALIZATION
	// =============================

	// ---- MAILER ----
	smtpPort, _ := strconv.Atoi(os.Getenv("SMTP_PORT")) // ex: 587
	mailConfig := mailer.Config{
		Host:     os.Getenv("SMTP_HOST"), // smtp.hostinger.com
		Port:     smtpPort,
		Username: os.Getenv("SMTP_USER"),     // invoice@welloresto.fr
		Password: os.Getenv("SMTP_PASSWORD"), // Ton mot de passe
		From:     os.Getenv("SMTP_FROM"),     // invoice@welloresto.fr
	}

	// Initialisation du service
	mailService := mailer.NewMailer(mailConfig)
	// Ensuite, tu injectes 'mailService' dans tes handlers/services
	// ex: webhookService := webhook.NewStripeWebhookService(repo, mailService)

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
	authRepo := authModule.NewAuthRepository(mysqlDB)
	authService := authModule.NewAuthService(authRepo)

	// ---- POS ----
	posRepo := posModule.NewPOSRepository(mysqlDB)
	posService := posModule.NewPOSService(authService, posRepo)

	// ---- Menu ----
	menuRepoLegacy := menuModule.NewMenuRepository(mysqlDB)
	menuService := menuModule.NewMenuService(menuRepoLegacy, authService)

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

	// ---- Customers ----
	customersRepo := customersModule.NewCustomerRepository(mysqlDB)
	customersService := customersModule.NewCustomersService(customersRepo, authService)

	// ---- Stripe ----
	stripeManager := stripeInternalClient.NewStripeManager(cfg.Stripe.APIKey)

	// ---- ScanNOrder ----
	scannRepo := scannorder.NewRepository(mysqlDB)
	scannService := scannorder.NewService(cfg.ScanNOrder, scannRepo, menuService, ordersService, *stripeManager)
	scannHandler := scannorder.NewHandler(scannService)

	// ---- Uber ----
	uberService := uberModule.NewUberEatsService(mysqlDB, cfg.UberEats)

	// ---- Orders Lifecycle ----
	ordersLifeCycleRepo := ordersLCModule.NewOrdersLifeCycleRepository(mysqlDB, ordersFetcher)
	ordersLifeCycleService := ordersLCModule.NewOrdersLifeCycleService(
		ordersLifeCycleRepo,
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
		ordersLifeCycleService,
		notificationService,
	)
	stripeWebhookHandler := webhookstripe.NewHandler(stripeWebhookService)

	// WH
	deliverooWebhookRepo := deliveroo.NewRepository(mysqlDB)
	deliverooWebhookService := deliveroo.NewDeliverooService(deliverooWebhookRepo, ordersService, ordersLifeCycleService, deliverooService)
	deliverooWebhookHandler := deliveroo.NewDeliverooHandler(deliverooWebhookService)

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
	locationsRepo := locModule.NewLocationsRepository(mysqlDB, log)
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

	// =============================
	//  HANDLERS
	// =============================

	authH := authModule.NewAuthHandler(authService)
	posH := posModule.NewPOSHandler(posService)
	menuH := menuModule.NewMenuHandler(menuService)
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
	})

	// Webhooks
	r.Route("/webhooks", func(r chi.Router) {
		r.Post("/uber-eats", uberWebhookHandler.HandleWebhook)
		r.Post("/deliveroo", deliverooWebhookHandler.HandleWebhook)
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
		r.Get("/{user_id}/location", usersH.GetUserLocation)
		r.Patch("/{user_id}/settings", usersH.UpdateUserSettings)
		r.Patch("/{user_id}/reset-password", usersH.UpdatePassword)
	})

	// --- POS ---
	r.Route("/pos", func(r chi.Router) {
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

		r.Get("/payments/tr/check/{tr_code}", posH.CheckTR)
	})

	// --- SCANNORDER ---
	r.Route("/scannorder", func(r chi.Router) {
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
		r.Post("/update", ordersH.UpdateOrder)
		r.Post("/pricing", ordersH.GetPricing)
		r.Post("/list", ordersH.GetOrders)

		r.Get("/pending", ordersH.GetPendingOrders)
		r.Post("/history", ordersH.GetHistory)
		r.Get("/{order_id}", ordersH.GetOrder)

		r.Patch("/{order_id}/reopen", ordersLifeCycleH.ReopenClosedOrder)

		r.Patch("/{order_id}/accept", ordersLifeCycleH.AcceptOrder)
		r.Patch("/{order_id}/deny", ordersLifeCycleH.DenyOrder)

		r.Patch("/{order_id}/cancel", ordersLifeCycleH.DeleteOrder)

		r.Patch("/{order_id}/delivered", ordersLifeCycleH.SetDelivered)
		r.Patch("/{order_id}/delivery-start", ordersLifeCycleH.StartDelivery)

		r.Patch("/{order_id}/distributed", ordersLifeCycleH.SetReadyForDistribution)
		r.Patch("/{order_id}/distributed-products", ordersLifeCycleH.SetDistributedProducts)

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
