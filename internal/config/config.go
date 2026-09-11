package config

import (
	"log"
	"os"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/database/dbx"
)

type AppConfig struct {
	App         App
	Database    DatabaseConfig
	Google      GoogleConfig
	UberEats    UberEatsConfig
	Deliveroo   DeliverooConfig
	ScanNOrder  ScanNOrderConfig
	Stripe      StripeConfig
	Brevo       BrevoConfig
	R2          R2Config
	AI          ai.AIConfig
	Kiosk       KioskConfig
	Planning    PlanningConfig
	Reservation ReservationConfig
	Auth        AuthConfig
}

type App struct {
	Port             string
	PINPepper        string
	FiscalSigningKey string
	// SignupContextSigningKey signs POST /v1/public/signup-context's
	// context_token (LOT A Semaine 3, Chantier 11 — a stateless, signed JWT
	// per docs/WelloResto-Parcours-Client-v2.docx §4.4, not a DB-backed
	// opaque id). Deliberately NOT validated/fatal like FiscalSigningKey —
	// this is a new key with nothing deployed depending on it yet; signup.NewService
	// falls back to a random in-process key (logged) when unset, so a
	// missing env var never crashes startup. Set a real value before relying
	// on tokens surviving a restart or being verified by another instance.
	SignupContextSigningKey string
}

func Load() *AppConfig {
	cfg := &AppConfig{
		App: App{
			Port:                    getEnv("PORT", "8081"),
			PINPepper:               os.Getenv("PIN_PEPPER"),
			FiscalSigningKey:        os.Getenv("FISCAL_SIGNING_KEY"),
			SignupContextSigningKey: os.Getenv("SIGNUP_CONTEXT_SIGNING_KEY"),
		},
		Database:    loadDatabase(),
		Google:      loadGoogle(),
		UberEats:    loadUberEats(),
		Deliveroo:   loadDeliveroo(),
		ScanNOrder:  loadScanNOrderConfig(),
		Stripe:      loadStripeConfig(),
		Brevo:       loadBrevoConfig(),
		R2:          loadR2Config(),
		AI:          loadAIConfig(),
		Kiosk:       loadKioskConfig(),
		Planning:    loadPlanningConfig(),
		Reservation: loadReservationConfig(),
		Auth:        loadAuthConfig(),
	}

	cfg.validate()
	return cfg
}

func (c *AppConfig) validate() {
	switch dbx.ActiveDialect() {
	case dbx.Postgres:
		if c.Database.PostgresURL == "" {
			log.Fatal("POSTGRES_URL is not set (DB_DIALECT=postgres)")
		}
	default:
		if c.Database.MySQLURL == "" {
			log.Fatal("MYSQL_URL is not set")
		}
	}
	if c.Google.APIKey == "" {
		log.Fatal("GOOGLE_API_KEY is not set")
	}
	if c.R2.PrivateBucket == "" {
		log.Fatal("R2_PRIVATE_BUCKET is not set")
	}
	if c.App.PINPepper == "" {
		log.Fatal("PIN_PEPPER is not set")
	}
	if c.App.FiscalSigningKey == "" {
		log.Fatal("FISCAL_SIGNING_KEY is not set")
	}
	if err := c.AI.Validate(); err != nil {
		log.Fatal(err.Error())
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

/*
type Config struct {
	Port     string
	MySQLURL string
}

func Load() Config {
	return Config{
		Port:     getEnv("PORT", "8080"),
		MySQLURL: os.Getenv("MYSQL_URL"),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
*/
