package main

import (
	"database/sql"
	"net/http"
	"welloresto-api/internal/infrastructure/brevo_mailer"
	"welloresto-api/internal/infrastructure/brevo_sms"
	"welloresto-api/internal/infrastructure/r2"
	stripeInternalClient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/infrastructure/websocket"
	requestlogger "welloresto-api/internal/middleware/request_logger"
	adminModule "welloresto-api/internal/modules/admin"
	"welloresto-api/internal/modules/googlemaps"
	"welloresto-api/internal/modules/receipt"
	"welloresto-api/internal/modules/reservation"
	"welloresto-api/internal/modules/scannorder"
	tasksPkg "welloresto-api/internal/tasks"
	"welloresto-api/internal/webhook/deliveroo_menu"
	"welloresto-api/internal/webhook/deliveroo_orders"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/config"
	"welloresto-api/internal/middleware"

	// ---- MODULES ----
	allergensModule "welloresto-api/internal/modules/allergens"
	authModule "welloresto-api/internal/modules/auth"
	availabilitiesModule "welloresto-api/internal/modules/availabilities"
	bookingsModule "welloresto-api/internal/modules/bookings"
	cashregisterModule "welloresto-api/internal/modules/cash_registers"
	customersModule "welloresto-api/internal/modules/customers"
	deliverooModule "welloresto-api/internal/modules/deliveroo"
	deliverysessionsModule "welloresto-api/internal/modules/delivery_sessions"
	discountsModule "welloresto-api/internal/modules/discounts"
	integrationsModule "welloresto-api/internal/modules/integrations"
	locModule "welloresto-api/internal/modules/locations"
	menuModule "welloresto-api/internal/modules/menu"
	notificationModule "welloresto-api/internal/modules/notification"
	ordersLCModule "welloresto-api/internal/modules/order_life_cycle"
	ordersModule "welloresto-api/internal/modules/orders"
	posModule "welloresto-api/internal/modules/pos"
	posAccountingModule "welloresto-api/internal/modules/pos/accounting"
	posReportsModule "welloresto-api/internal/modules/pos/reports"
	statsModule "welloresto-api/internal/modules/stats"
	stocksModule "welloresto-api/internal/modules/stocks"
	tagsModule "welloresto-api/internal/modules/tags"
	uberModule "welloresto-api/internal/modules/ubereats"
	servicesModule "welloresto-api/internal/modules/user_services"
	usersModule "welloresto-api/internal/modules/users"

	redisclient "welloresto-api/internal/infrastructure/redis"
	auditModule "welloresto-api/internal/modules/audit"
	translationModule "welloresto-api/internal/modules/translation"
	upsellModule "welloresto-api/internal/modules/upsell"

	// ---- AI LAYER ----
	"welloresto-api/internal/ai"
	aicache "welloresto-api/internal/ai/cache"
	"welloresto-api/internal/ai/providers"

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
	r.Use(requestlogger.RequestLoggerMiddleware(requestlogger.NewLogger(mysqlDB, 1000)))

	// ============================
	// REDIS
	// ============================
	redisClient, err := redisclient.New()
	if err != nil {
		log.Error("Erreur lors de l'initialisation du client Redis", zap.Error(err))
	} else {
		log.Info("Redis connecté avec succès")
	}

	// ============================
	// AI LAYER
	// ============================
	aiCache := aicache.New(redisClient)

	aiRegistry, err := buildAIRegistry(cfg.AI)
	if err != nil {
		log.Error("Erreur lors de l'initialisation du registre AI", zap.Error(err))
	} else {
		log.Info("AI registry initialisé avec succès")
	}

	// ---- R2 (Cloudflare Storage) ----
	r2Client, err := r2.NewClient(r2.UploadConfig{
		AccessKeyID:     cfg.R2.AccessKeyID,
		SecretAccessKey: cfg.R2.SecretAccessKey,
		Endpoint:        cfg.R2.Endpoint,
		Bucket:          cfg.R2.Bucket,
		PublicBaseURL:   cfg.R2.PublicBaseURL,
	})
	if err != nil {
		log.Error("Erreur lors de l'initialisation du client R2", zap.Error(err))
	} else {
		log.Info("R2 client connecté avec succès")
	}

	// =============================
	//  MODULE INITIALIZATION
	// =============================

	// ---- Audit Logger ----
	auditRepo := auditModule.NewAuditRepository(mysqlDB)
	auditService := auditModule.NewAuditService(auditRepo)

	// ---- MAILER & SMS (BREVO) ----
	// Initialize Brevo Email Service
	mailService := brevo_mailer.NewBrevoMailer(brevo_mailer.Config{
		APIKey: cfg.Brevo.APIKey,
	})

	// Initialize Brevo SMS Service
	smsService := brevo_sms.NewBrevoSMS(brevo_sms.Config{
		APIKey: cfg.Brevo.APIKey,
	})

	// 2. Initialisation des couches (Injection de dépendances)
	repo := googlemaps.NewGoogleMapsRepository()
	googleClient := googlemaps.NewGoogleMapsClient(cfg.Google)
	svc := googlemaps.NewRouteService(repo, googleClient)
	routeHandler := googlemaps.NewRouteHandler(svc)

	// ---- WebSocket Hub ----
	wsHub := websocket.NewHub()

	// ---- Notification ---
	fcmClient := notificationModule.NewFCMClient()
	saPath := "/etc/secrets/wello-resto-150721-6d1253e00d6d.json"
	fcmTokenManager := notificationModule.NewGoogleFCMTokenManager(saPath)
	notificationRepo := notificationModule.NewNotificationRepository(mysqlDB)
	notificationService := notificationModule.NewNotificationService(notificationRepo, fcmClient, fcmTokenManager, wsHub)

	// ---- Auth ----
	authRepo := authModule.NewAuthRepository(mysqlDB)
	authService := authModule.NewAuthService(authRepo, redisClient, mailService, smsService)
	authMiddleware := middleware.Auth(&authService)

	// ---- POS ----
	posRepo := posModule.NewPOSRepository(mysqlDB)
	posService := posModule.NewPOSService(posRepo)

	// ---- POS Reports ----
	posReportsRepo := posReportsModule.NewReportsRepository(mysqlDB)
	posReportsService := posReportsModule.NewReportsService(posReportsRepo)
	posReportsHandler := posReportsModule.NewReportsHandler(posReportsService)

	// ---- POS Accounting ----
	posAccountingRepo := posAccountingModule.NewAccountingRepository(mysqlDB)
	posAccountingService := posAccountingModule.NewAccountingService(posAccountingRepo)
	posAccountingHandler := posAccountingModule.NewAccountingHandler(posAccountingService, r2Client)

	// ---- STATS ----
	statsRepo := statsModule.NewStatsRepository(mysqlDB)
	statsService := statsModule.NewStatsService(statsRepo)

	// ---- Menu ----
	menuRepoLegacy := menuModule.NewMenuRepository(mysqlDB)
	// NOTE: deliverooService and uberService are initialized below; we forward-declare menuService
	// and re-assign after their initialization using a late-binding approach via a pointer.
	// To keep initialization order clean, menuService is assigned after deliveroo/uber init.

	// ---- Orders ----
	ordersFetcher := ordersModule.NewOrdersFetcher(mysqlDB)
	ordersRepo := ordersModule.NewOrdersRepository(mysqlDB, ordersFetcher)
	deliverySessionsRepo := deliverysessionsModule.NewDeliverySessionsRepository(mysqlDB, ordersFetcher)
	ordersService := ordersModule.NewOrdersService(ordersRepo, notificationService, redisClient, auditService)

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
	customersService := customersModule.NewCustomersService(customersRepo)

	// ---- Stripe ----
	stripeManager := stripeInternalClient.NewStripeManager(cfg.Stripe.APIKey)

	// ---- Uber ----
	uberService := uberModule.NewUberEatsService(mysqlDB, cfg.UberEats, redisClient)
	uberHandler := uberModule.NewUberHandler(uberService)

	// ---- Menu (initialized after deliveroo + uber) ----
	menuService := menuModule.NewMenuService(menuRepoLegacy, deliverooService, uberService)

	// ---- Translation ----
	translationRepo := translationModule.NewRepository(mysqlDB)
	translationService := translationModule.NewService(translationRepo, aiRegistry, aiCache)

	// ---- Upsell ----
	upsellRepo := upsellModule.NewRepository(mysqlDB)
	upsellTracker := upsellModule.NewTracker(upsellRepo, log)
	upsellService := upsellModule.NewService(upsellRepo, menuRepoLegacy, aiRegistry, aiCache, log)

	// ---- Receipt ----
	receiptRepo := receipt.NewReceiptRepository(mysqlDB)
	receiptService := receipt.NewReceiptService(receiptRepo)

	// ---- Stocks (initialized here because ordersLifeCycleService depends on it) ----
	stocksRepo := stocksModule.NewStockRepository(mysqlDB)
	stocksService := stocksModule.NewStockService(stocksRepo)

	// ---- Orders Lifecycle ----
	ordersLifeCycleRepo := ordersLCModule.NewOrdersLifeCycleRepository(mysqlDB, customersRepo)
	ordersLifeCycleService := ordersLCModule.NewOrdersLifeCycleService(
		ordersLifeCycleRepo,
		stripeManager,
		uberService,
		deliverooService,
		deliverySessionsRepo,
		log,
		notificationService,
		customersService,
		redisClient,
		auditService,
		ordersService,
		receiptService,
		mysqlDB,
		stocksRepo,
		upsellTracker,
	)

	// ---- ScanNOrder ----
	scannRepo := scannorder.NewRepository(mysqlDB)
	scannService := scannorder.NewService(cfg.ScanNOrder, scannRepo, menuService, ordersService, stripeManager, redisClient, ordersLifeCycleService)
	scannHandler := scannorder.NewHandler(scannService)

	// ---- Integrations dashboard ----
	integrationsService := integrationsModule.NewService(
		mysqlDB,
		stripeManager,
		uberService,
		deliverooService,
		cfg.Stripe.OnboardingReturnURL,
		cfg.Stripe.OnboardingRefreshURL,
	)
	integrationsHandler := integrationsModule.NewHandler(integrationsService, r2Client)

	// 3. Initialiser le StripeWebhookService Stripe
	stripeWebhookService := webhookstripe.NewStripeWebhookService(
		stripeRepo,
		cfg.Stripe.APIKey,
		mailService,
		smsService,
		ordersLifeCycleService,
		notificationService,
		redisClient,
	)
	stripeWebhookHandler := webhookstripe.NewHandler(stripeWebhookService)

	// WH
	deliverooWebhookRepo := deliveroo_orders.NewRepository(mysqlDB)
	deliverooWebhookService := deliveroo_orders.NewDeliverooService(deliverooWebhookRepo, ordersService, ordersLifeCycleService, deliverooService, redisClient)
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
		redisClient,
	)

	uberWebhookHandler := webhookuberheandler.NewHandler(uberWebhookService)

	// ---- Delivery Sessions ----
	deliverySessionsService := deliverysessionsModule.NewDeliverySessionsService(deliverySessionsRepo, notificationService)

	// ---- Locations ----
	locationsRepo := locModule.NewLocationsRepository(mysqlDB)
	locationsService := locModule.NewLocationsService(locationsRepo)

	// ---- Cash Register ----
	cashRegisterRepo := cashregisterModule.NewCashRegisterRepository(mysqlDB)
	cashRegisterService := cashregisterModule.NewCashRegisterService(cashRegisterRepo)

	// ---- Bookings ----
	bookingsRepo := bookingsModule.NewBookingsRepository(mysqlDB, log)
	bookingsService := bookingsModule.NewBookingsService(bookingsRepo, mysqlDB)

	// ---- Reservation (externe) ----
	reservationRepo := reservation.NewReservationRepository(mysqlDB)
	reservationService := reservation.NewReservationService(reservationRepo, bookingsService)
	reservationHandler := reservation.NewReservationHandler(reservationService)

	// ---- Users ----
	usersRepo := usersModule.NewUserRepository(mysqlDB)
	usersService := usersModule.NewUsersService(usersRepo)

	// ---- Services ----
	servicesRepo := servicesModule.NewServicesRepository(mysqlDB)
	servicesService := servicesModule.NewServicesService(servicesRepo)

	// ---- Allergens ----
	allergensRepo := allergensModule.NewRepository(mysqlDB)
	allergensService := allergensModule.NewService(allergensRepo)

	// ---- Tags ----
	tagsRepo := tagsModule.NewRepository(mysqlDB)
	tagsService := tagsModule.NewService(tagsRepo)

	// ---- Discounts ----
	discountsRepo := discountsModule.NewRepository(mysqlDB)
	discountsService := discountsModule.NewService(discountsRepo)

	// ---- Availabilities ----
	availabilitiesRepo := availabilitiesModule.NewAvailabilitiesRepository(mysqlDB)
	availabilitiesService := availabilitiesModule.NewAvailabilitiesService(availabilitiesRepo)

	// =============================
	//  HANDLERS
	// =============================

	authH := authModule.NewAuthHandler(authService)
	posH := posModule.NewPOSHandler(posService)
	statsH := statsModule.NewStatsHandler(statsService)
	menuH := menuModule.NewMenuHandler(menuService, r2Client, translationRepo, translationService)
	allergensH := allergensModule.NewHandler(allergensService)
	tagsH := tagsModule.NewHandler(tagsService)
	discountsH := discountsModule.NewHandler(discountsService)
	availabilitiesH := availabilitiesModule.NewAvailabilitiesHandler(availabilitiesService)
	ordersH := ordersModule.NewOrdersHandler(ordersService, upsellService)
	ordersLifeCycleH := ordersLCModule.NewOrdersLifeCycleHandler(ordersLifeCycleService, deliverySessionsService, notificationService)
	deliverySessionsH := deliverysessionsModule.NewDeliverySessionsHandler(deliverySessionsService)
	locationsH := locModule.NewLocationsHandler(locationsService)
	cashRegisterH := cashregisterModule.NewCashRegisterHandler(cashRegisterService)
	bookingsH := bookingsModule.NewBookingsHandler(bookingsService)
	customersH := customersModule.NewCustomersHandler(customersService)
	usersH := usersModule.NewUsersHandler(usersService, r2Client)
	stocksH := stocksModule.NewStocksHandler(stocksService)
	servicesH := servicesModule.NewServicesHandler(servicesService)
	notificationH := notificationModule.NewNotificationHandler(notificationService)

	// Option A: instantiate a single TasksManager in SetupRoutes and share it with cron wiring and admin manual trigger.
	taskManager := tasksPkg.NewTasksManager(mysqlDB, &mailService, ordersLifeCycleService, stripeManager, bookingsService, aiCache, upsellRepo, log)
	adminUpsellH := adminModule.NewAdminUpsellHandler(taskManager, log)

	// ============================================================
	//                      CRON JOBS
	// ============================================================

	SetupTasks(log, taskManager)

	// ============================================================
	//                      ROUTING
	// ============================================================

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("OK"))
	})
	r.Route("/test", func(r chi.Router) {
		r.Get("/test-mailer", mailService.TriggerTestEmail)
		r.Get("/test-sms", smsService.TriggerTestSMS)
		r.Post("/notification", notificationH.SendTestNotification)

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
		r.Use(authMiddleware)

		r.Get("/routes", routeHandler.HandleGetRoute)
	})

	// --- AUTH ---
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authH.Login)
		r.Post("/login", authH.Login)
		r.Get("/mfa/fallback-sms", authH.FallbackSMS)
		r.Post("/send-verification", authH.SendVerification)
		r.Post("/verify", authH.VerifyCode)
	})

	// --- USERS ---
	r.Route("/users", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/profile", usersH.GetProfile)           // used by: back-office
		r.Patch("/profile", usersH.UpdateProfile)      // used by: back-office
		r.Post("/profile/avatar", usersH.UploadAvatar) // used by: back-office
		r.Get("/notifications", usersH.GetNotifications)

		r.Post("/create", usersH.CreateUser)
		r.Get("/{user_id}/location", usersH.GetUserLocation)
		r.Patch("/location", usersH.SetUserLocation)
		r.Patch("/reset-password", usersH.UpdatePassword)
	})

	// --- STATS ---
	r.Route("/stats", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Route("/dashboard", func(r chi.Router) {
			r.Get("/summary", statsH.GetDashboardSummary)
		})
	})

	// --- POS ---
	r.Route("/pos", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Post("/create", posH.CreateMerchant)
		r.Post("/link-user", posH.LinkUser)
		r.Get("/status", posH.GetPOSStatus)
		r.Patch("/status", posH.UpdatePOSStatus)

		r.Get("/deletion_reasons/{object}", posH.GetDeletionReasons)
		r.Get("/delivery_men", posH.GetDeliveryMen)
		r.Get("/users", posH.GetDeliveryMen)
		r.Get("/tva_rates", posH.GetTVARates)

		r.Route("/settings", func(r chi.Router) {
			r.Get("/", posH.GetSettings)              // used by: back-office
			r.Patch("/", posH.UpdateMerchantSettings) // used by: back-office
			r.Post("/hours_of_operations", posH.CreateHourOfOperation)
			r.Patch("/hours_of_operations/{hour_id}", posH.UpdateHourOfOperation)
			r.Delete("/hours_of_operations/{hour_id}", posH.DeleteHourOfOperation)

			r.Patch("/scannorder", posH.ToggleScanNOrder)
			r.Patch("/production_paid_only", posH.ToggleProductionPaidOnly)
			r.Patch("/safety_stock", posH.ToggleSafetyStockActive)
		})

		r.Get("/payments/tr/check/{tr_code}", posH.CheckTR)

		r.Route("/reports", func(r chi.Router) {
			r.Post("/tva", posReportsHandler.GetTVAReport)
			r.Post("/payments", posReportsHandler.GetPaymentsReport)
		})

		r.Route("/accounting", func(r chi.Router) {
			r.Post("/export", posAccountingHandler.ExportAccounting)
		})
	})

	// --- SCANNORDER ---
	r.Route("/scannorder", func(r chi.Router) {
		r.Get("/brands/{brand_slug}", scannHandler.GetBrand)
		r.Get("/{merchant_slug}", scannHandler.GetMerchant)
		r.Get("/{merchant_slug}/slots", scannHandler.GetSlots)

		r.Get("/{merchant_slug}/menu", scannHandler.GetMenu)
		r.Get("/{merchant_slug}/loyalty_programs", scannHandler.GetLoyaltyPrograms)
		r.Get("/{merchant_slug}/discounts", scannHandler.GetDiscounts)

		r.Get("/{merchant_slug}/upsell", scannHandler.GetUpsell)
		r.Post("/{merchant_slug}/pricing", scannHandler.GetPricingSNO)
		r.Post("/{merchant_slug}/delivery/check", scannHandler.CheckDeliveryZone)

		r.Post("/{merchant_slug}/orders", scannHandler.CreateOrderSNO)
		r.Post("/{merchant_slug}/create", scannHandler.CreateOrderSNO) // TO BE DELETED
		r.Get("/{merchant_slug}/orders/{order_id}", scannHandler.GetOrderSNO)
		r.Get("/{merchant_slug}/products/{product_id}", scannHandler.GetProduct)

		r.Delete("/{merchant_slug}/orders/{order_id}", scannHandler.CancelOrderSNO)
	})

	// --- ACCOUNTING ---
	r.Route("/accounting", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Post("/vat/calculate", posAccountingHandler.CalculateVAT)
		r.Post("/vat/export-csv", posAccountingHandler.ExportVATCSV)
	})

	// --- STOCKS ---
	r.Route("/stocks", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/barcode/{barcode}", stocksH.GetBarcodeInfo)
		r.Post("/barcode/create", stocksH.CreateBarcode)
		r.Delete("/barcode/{barcode}", stocksH.DeleteBarcode)

		r.Post("/barcodes/scan", stocksH.AddStockBarcode)
		r.Patch("/loss", stocksH.SetStockLoss)
		r.Get("/products", stocksH.GetStockProducts)

		//// New endpoints, previous were probably never used and can be deleted after some time (today is 2026/04/26)
		r.Get("/components/list", stocksH.GetComponentsList)
		r.Put("/components/{component_id}", stocksH.RecordComponentMovement)
		r.Get("/movements", stocksH.GetMovements)
	})

	// --- DEVICES ---
	r.Route("/device", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Post("/token", authH.SaveDeviceToken)
	})

	// --- APP VERSION ---
	r.Route("/app", func(r chi.Router) {
		r.Post("/version/check", authH.CheckAppVersion)
	})

	// --- MENU ---
	r.Route("/menu", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/", menuH.GetMenu)
		r.Get("/translation-langs", menuH.GetTranslationLanguages)
		r.Patch("/translation-langs", menuH.PatchTranslationLanguages)

		r.Get("/products", menuH.GetAllProducts)     // used by: back-office
		r.Get("/components", menuH.GetAllComponents) // used by: back-office

		r.Get("/components/{component_id}", menuH.GetComponent) // used by: back-office
		r.Patch("/component/{component_id}/status", menuH.SetComponentStatus)
		r.Patch("/components/{component_id}", menuH.UpdateComponent)  // used by: back-office
		r.Delete("/components/{component_id}", menuH.DeleteComponent) // used by: back-office

		r.Patch("/display-orders", menuH.UpdateDisplayOrder) // used by: back-office

		r.Patch("/products/categories/{category_id}", menuH.UpdateProductCategory)
		r.Patch("/products/categories/{category_id}/availability", menuH.SetProductCategoryAvailability)
		r.Patch("/products/categories/{category_id}/bulk-assign", menuH.BulkAssignProductsToCategory) // used by: back-office
		r.Delete("/products/categories/{category_id}", menuH.DeleteProductCategory)
		r.Patch("/products/{product_id}/marketing-category", menuH.AssignProductMarketingCategory)
		r.Delete("/products/{product_id}/marketing-category", menuH.UnassignProductMarketingCategory)

		r.Patch("/products/bulk", menuH.BulkUpdateProductPrices) // used by: back-office

		r.Post("/products", menuH.CreateProduct) // used by: back-office
		r.Get("/products/{product_id}", menuH.GetProduct)
		r.Patch("/products/{product_id}", menuH.UpdateProduct)
		r.Patch("/products/{product_id}/attributes", menuH.UpdateProductAttributes)
		r.Put("/products/{product_id}/image", menuH.UploadProductImage)
		r.Patch("/products/{product_id}/status", menuH.SetProductStatus)
		r.Patch("/products/{product_id}/availability", menuH.SetProductAvailability)
		r.Delete("/products/{product_id}", menuH.DeleteProduct)
		r.Put("/products/{product_id}/allergens", menuH.SyncProductAllergens)
		r.Put("/products/{product_id}/tags", menuH.SyncProductTags)

		r.Get("/attributes", menuH.GetAttributes)
		r.Get("/attributes/{attribute_id}", menuH.GetAttribute) // used by: back-office
		r.Post("/attributes", menuH.CreateAttribute)            // used by: back-office
		r.Patch("/attributes/{attribute_id}", menuH.UpdateAttribute)
		r.Delete("/attributes/{attribute_id}", menuH.DeleteAttribute)
		r.Get("/units_of_measures", menuH.GetUnitsOfMeasures)

		r.Route("/tags", func(r chi.Router) {
			r.Get("/", tagsH.ListTags)
			r.Post("/create", tagsH.CreateTag)
			r.Patch("/display-order", tagsH.UpdateTagsDisplayOrder)
			r.Patch("/{tag_id}/bulk_assign", menuH.BulkAssignProductsToTag)
			r.Patch("/{tag_id}", tagsH.UpdateTag)
			r.Delete("/{tag_id}", tagsH.DeleteTag)
		})

		// --- Bulk assign (additive) ---
		r.Route("/bulk", func(r chi.Router) {
			r.Post("/allergens/assign", menuH.BulkAssignAllergen)
		})

		// --- Plateformes externes ---
		r.Get("/deliveroo", menuH.GetDeliverooMenu)
		r.Patch("/deliveroo/sync", menuH.SyncDeliverooMenu) // used by: back-office
		r.Get("/uber-eats", menuH.GetUberEatsMenu)
		r.Patch("/uber-eats/sync", menuH.SyncUberEatsMenu) // used by: back-office

		r.Post("/products/categories", menuH.CreateProductCategory)                                             // used by: back-office
		r.Get("/marketing-categories", menuH.GetMarketingCategories)                                            // used by: back-office
		r.Post("/marketing-categories", menuH.CreateMarketingCategory)                                          // used by: back-office
		r.Patch("/marketing-categories/display-order", menuH.UpdateMarketingCategoriesDisplayOrder)             // used by: back-office
		r.Patch("/marketing-categories/{category_id}", menuH.UpdateMarketingCategory)                           // used by: back-office
		r.Delete("/marketing-categories/{category_id}", menuH.DeleteMarketingCategory)                          // used by: back-office
		r.Patch("/marketing-categories/{category_id}/bulk-assign", menuH.BulkAssignProductsToMarketingCategory) // used by: back-office
		r.Post("/components", menuH.CreateComponent)                                                            // used by: back-office
		r.Post("/components/categories", menuH.CreateComponentCategory)                                         // used by: back-office
		r.Delete("/components/categories/{category_id}", menuH.DeleteComponentCategory)                         // used by: back-office

		// --- Discounts/Promotions ---
		r.Get("/discounts", discountsH.ListActiveDiscounts)
		r.Get("/discounts/all", discountsH.ListAllDiscounts)            // for back-office
		r.Post("/discounts", discountsH.CreateDiscount)                 // for back-office
		r.Get("/discounts/{discount_id}", discountsH.GetDiscount)       // for back-office
		r.Patch("/discounts/{discount_id}", discountsH.UpdateDiscount)  // for back-office
		r.Delete("/discounts/{discount_id}", discountsH.DeleteDiscount) // for back-office

		// --- Availabilities/Schedules ---
		r.Get("/availabilities", availabilitiesH.GetAvailabilities)
		r.Post("/availabilities", availabilitiesH.CreateAvailability)
		r.Patch("/availabilities/{id}", availabilitiesH.UpdateAvailability)
		r.Delete("/availabilities/{id}", availabilitiesH.DeleteAvailability)
		r.Get("/availabilities/check", availabilitiesH.CheckProductAvailability)
	})

	// --- ALLERGENS (system-wide, read-only) ---
	r.Route("/allergens", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/", allergensH.ListAllergens)
	})

	// --- FLOORS ---
	r.Route("/floors", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Post("/", locationsH.CreateFloor)
	})

	// --- LOCATIONS ---
	r.Route("/locations", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/", locationsH.GetLocations)
		r.Patch("/{location_id}/coordinates", locationsH.UpdateLocationCoordinates)

		// Floor tables management
		r.Post("/floors/{floor_id}/tables", locationsH.CreateTable)
		r.Patch("/tables/{location_id}", locationsH.UpdateTable)
		r.Delete("/tables/{location_id}", locationsH.DeleteTable)
	})

	// --- SERVICES ---
	r.Route("/services", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/{device_id}", servicesH.GetCurrentService)
	})

	// --- ORDERS ---
	/*
		r.Route("/orders", func(r chi.Router) {
			r.Use(authMiddleware)

			r.Post("/create", ordersLifeCycleH.CreateOrder)
			r.Post("/pricing", ordersH.GetPricing)
			r.Post("/list", ordersH.GetOrders)

			r.Get("/pending", ordersH.GetPendingOrders)
			r.Post("/history", ordersH.GetHistory)
			r.Get("/{order_id}", ordersH.GetOrder)

			r.Post("/{order_id}/update", ordersLifeCycleH.UpdateOrder)

			r.Patch("/{order_id}/reopen", ordersLifeCycleH.ReopenClosedOrder)

			r.Post("/{order_id}/refund", ordersLifeCycleH.HandleRefund)

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
		})*/

	r.Route("/orders", func(r chi.Router) {
		r.Use(authMiddleware)

		// --- 1. ENDPOINTS DE CONSULTATION (Libres) ---
		// On laisse passer les GET et les POST qui servent uniquement à filtrer/lister
		r.Post("/pricing", ordersH.GetPricing)
		r.Post("/upsell", ordersH.GetUpsell)
		r.Post("/list", ordersH.GetOrders)
		r.Get("/pending", ordersH.GetPendingOrders)
		r.Post("/history", ordersH.GetHistory)
		r.Get("/{order_id}", ordersH.GetOrder)

		r.Get("/{order_id}/payments", ordersLifeCycleH.GetPayments)

		// --- 2. ENDPOINTS DE CRÉATION / MODIFICATION (Protégés) ---
		r.Group(func(r chi.Router) {
			// Ici, on applique le verrou. L'utilisateur doit être vérifié pour continuer.
			r.Use(middleware.RequirePermission(middleware.IsEmailVerified))

			r.Post("/create", ordersLifeCycleH.CreateOrder)
			r.Post("/{order_id}/update", ordersLifeCycleH.UpdateOrder)
			r.Patch("/{order_id}/reopen", ordersLifeCycleH.ReopenClosedOrder)
			r.Post("/{order_id}/refund", ordersLifeCycleH.HandleRefund)

			// Cycle de vie
			r.Patch("/{order_id}/accept", ordersLifeCycleH.AcceptOrder)
			r.Patch("/{order_id}/deny", ordersLifeCycleH.DenyOrder)
			r.Patch("/{order_id}/cancel", ordersLifeCycleH.DeleteOrder)
			r.Patch("/{order_id}/delivered", ordersLifeCycleH.SetDelivered)
			r.Patch("/{order_id}/delivery-start", ordersLifeCycleH.StartDelivery)
			r.Patch("/{order_id}/distributed", ordersLifeCycleH.SetReadyForDistribution)
			r.Patch("/{order_id}/distributed-products", ordersLifeCycleH.SetDistributedProducts)
			r.Patch("/multiple-production-status", ordersLifeCycleH.UpdateProductionStatus)

			// Sous-route paiements (en écriture)
			r.Route("/{order_id}/payments", func(r chi.Router) {
				r.Post("/create", ordersLifeCycleH.AddPayment)
				r.Delete("/{payment_id}", ordersLifeCycleH.DeletePayment)
			})
		})
	})

	// TODO(@user): protéger cette route avec un middleware admin dédié quand il sera disponible. Pour l'instant elle utilise authMiddleware seul.
	r.Route("/admin/upsell", func(r chi.Router) {
		r.Use(authMiddleware)
		r.Post("/recompute-patterns", adminUpsellH.RecomputePatterns)
	})

	// --- DELIVERY SESSIONS ---
	r.Route("/delivery_sessions", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/pending", deliverySessionsH.GetPendingDeliverySessions)
		r.Get("/{delivery_session_id}", deliverySessionsH.GetDeliverySession)
		r.Delete("/{delivery_session_id}", deliverySessionsH.CancelDeliverySession)
		r.Patch("/{delivery_session_id}/close", deliverySessionsH.CloseDeliverySession)

		r.Post("/start", deliverySessionsH.StartDeliverySession)
	})

	// --- CASH DRAWER ---
	r.Route("/cash_drawer", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/open", cashRegisterH.OpenCashDrawer)
	})

	// --- CUSTOMERS ---
	// TO BE DELETED
	// Requirements : update application to use /customers instead of /customer, then delete this route and all its references in the codebase
	// Deprecated on 2024-06-25
	r.Route("/customer", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/search", customersH.SearchCustomers)
		r.Get("/list", customersH.ListCustomers)
		r.Get("/loyalty-programs", customersH.GetLoyaltyPrograms)
		r.Get("/loyalty-programs/{loyalty_program_id}", customersH.GetLoyaltyProgram)
		r.Post("/loyalty-programs", customersH.CreateLoyaltyProgram)
		r.Patch("/loyalty-programs/{loyalty_program_id}", customersH.UpdateLoyaltyProgram)
		r.Delete("/loyalty-programs/{loyalty_program_id}", customersH.DeleteLoyaltyProgram)
		r.Get("/{customer_id}/loyalty", customersH.GetCustomerLoyalty)

		r.Patch("/{customer_id}/loyalty/{loyalty_program_id}", customersH.UpdateLoyaltyProgress)
		r.Patch("/{customer_id}/rewards/{reward_id}", customersH.UpdateLoyaltyReward)
	})

	// --- CUSTOMERS ---
	r.Route("/customers", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/search", customersH.SearchCustomers)                                             // used by: back-office | mobile-app
		r.Get("/list", customersH.ListCustomers)                                                 // used by: back-office | mobile-app
		r.Get("/loyalty-programs", customersH.GetLoyaltyPrograms)                                // used by: back-office
		r.Get("/loyalty-programs/{loyalty_program_id}", customersH.GetLoyaltyProgram)            // used by: back-office
		r.Post("/loyalty-programs", customersH.CreateLoyaltyProgram)                             // used by: back-office
		r.Patch("/loyalty-programs/{loyalty_program_id}", customersH.UpdateLoyaltyProgram)       // used by: back-office
		r.Delete("/loyalty-programs/{loyalty_program_id}", customersH.DeleteLoyaltyProgram)      // used by: back-office
		r.Get("/{customer_id}/loyalty", customersH.GetCustomerLoyalty)                           // used by: back-office
		r.Patch("/{customer_id}/loyalty/{loyalty_program_id}", customersH.UpdateLoyaltyProgress) // used by: back-office
		r.Patch("/{customer_id}/rewards/{reward_id}", customersH.UpdateLoyaltyReward)            // used by: back-office
	})

	// --- CASH REGISTER ---
	r.Route("/cash_register", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Post("/open", cashRegisterH.OpenCashRegister)
		r.Get("/history", cashRegisterH.GetHistory)
		r.Post("/history", cashRegisterH.GetHistory)
		r.Post("/link", cashRegisterH.HandleLinkDevice)

		r.Route("/{cash_register_id}", func(r chi.Router) {
			r.Get("/", cashRegisterH.GetCashRegisterHistoryByID)
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
		r.Use(authMiddleware)

		r.Post("/", bookingsH.SearchBookings)
		r.Get("/availability/{date}", bookingsH.GetBookingAvailability)

		r.Post("/create", bookingsH.CreateBooking)
		r.Get("/{booking_id}", bookingsH.GetBooking)

		r.Patch("/{booking_id}/accept", bookingsH.AcceptBooking)
		r.Patch("/{booking_id}/deny", bookingsH.DenyBooking)
	})

	// --- BOOKINGS ---
	r.Route("/rsv/{slug}", func(r chi.Router) {

		r.Get("/open-hours", reservationHandler.HandleGetOpenHours)
		r.Get("/booking-availability", reservationHandler.HandleGetAvailability)
		r.Post("/booking/create", reservationHandler.HandleCreateReservation)
		r.Get("/booking/{booking_id}", reservationHandler.HandleGetReservation)
		r.Delete("/booking/{booking_id}/cancel", reservationHandler.HandleCancelReservation)
		r.Post("/booking/{booking_id}/update", reservationHandler.HandleUpdateReservation)
	})

	r.Route("/integrations", func(r chi.Router) {

		// OAuth / connection flows (no auth required)
		r.Get("/uber-eats/connect", uberHandler.GetConnectURL)
		r.Get("/uber-eats/callback", uberHandler.Callback)
		r.Get("/uber-eats/disconnect", uberHandler.Disconnect)

		// Dashboard endpoints (auth required)
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware)

			r.Get("/uber-eats", integrationsHandler.GetUberEats)
			r.Get("/deliveroo", integrationsHandler.GetDeliveroo)
			r.Get("/scannorder", integrationsHandler.GetScanNOrder)
			r.Put("/scannorder/logo", integrationsHandler.UploadScanNOrderLogo)
			r.Put("/scannorder/banner", integrationsHandler.UploadScanNOrderBanner)
			r.Patch("/uber-eats", integrationsHandler.UpdateUberEats)
			r.Patch("/uber-eats/disable", integrationsHandler.DisableUberEats)
			r.Patch("/deliveroo", integrationsHandler.UpdateDeliveroo)
			r.Patch("/deliveroo/disable", integrationsHandler.DisableDeliveroo)
			r.Patch("/scannorder", integrationsHandler.UpdateScanNOrder)
			r.Post("/scannorder/onboarding", integrationsHandler.CreateScanNOrderOnboarding)
			r.Patch("/global/close-temporary", integrationsHandler.CloseTemporaryGlobal)

			// ---- Stripe Connect ----
			r.Get("/stripe/status", integrationsHandler.GetStripeStatus)
			r.Post("/stripe/onboarding-link", integrationsHandler.CreateStripeOnboardingLink)
			r.Get("/stripe/bank-accounts", integrationsHandler.GetStripeBankAccounts)
			r.Post("/stripe/bank-account-link", integrationsHandler.CreateStripeBankAccountLink)
			r.Get("/stripe/balance", integrationsHandler.GetStripeBalance)
			r.Post("/stripe/branding", integrationsHandler.SyncStripeBranding)
		})
	})

	// --- WEBSOCKET ---
	r.Route("/ws", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			websocket.ServeWS(wsHub, w, r)
		})
	})

	return r
}

// buildAIRegistry instantiates all declared LLM providers and wires them
// into a Registry. Add new providers here as they become available.
func buildAIRegistry(cfg ai.AIConfig) (*ai.Registry, error) {
	providerMap := make(map[string]ai.LLMProvider)

	for name, provCfg := range cfg.Providers {
		switch name {
		case "anthropic":
			// Model is resolved per-task; use an empty string so the provider falls
			// back to its built-in default (claude-haiku-4-5).
			providerMap[name] = providers.NewAnthropicProvider(provCfg, "")
		case "openai":
			// Model is resolved per-task; use an empty string so the provider falls
			// back to its built-in default (gpt-4o-mini).
			providerMap[name] = providers.NewOpenAIProvider(provCfg, "")
		default:
			// Unknown provider names are silently skipped — Validate() would have
			// already caught any task referencing an unknown provider.
		}
	}

	return ai.NewRegistry(cfg, providerMap)
}
