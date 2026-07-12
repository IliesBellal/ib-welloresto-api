package config

import (
	"log"
	"os"

	"welloresto-api/internal/ai"
)

type AppConfig struct {
	App        App
	Database   DatabaseConfig
	Google     GoogleConfig
	UberEats   UberEatsConfig
	Deliveroo  DeliverooConfig
	ScanNOrder ScanNOrderConfig
	Stripe     StripeConfig
	Brevo      BrevoConfig
	R2          R2Config
	AI          ai.AIConfig
	Kiosk       KioskConfig
	Reservation ReservationConfig
}

type App struct {
	Port      string
	PINPepper string
}

func Load() *AppConfig {
	cfg := &AppConfig{
		App: App{
			Port:      getEnv("PORT", "8080"),
			PINPepper: os.Getenv("PIN_PEPPER"),
		},
		Database:   loadDatabase(),
		Google:     loadGoogle(),
		UberEats:   loadUberEats(),
		Deliveroo:  loadDeliveroo(),
		ScanNOrder: loadScanNOrderConfig(),
		Stripe:     loadStripeConfig(),
		Brevo:      loadBrevoConfig(),
		R2:          loadR2Config(),
		AI:          loadAIConfig(),
		Kiosk:       loadKioskConfig(),
		Reservation: loadReservationConfig(),
	}

	cfg.validate()
	return cfg
}

func (c *AppConfig) validate() {
	if c.Database.MySQLURL == "" {
		log.Fatal("MYSQL_URL is not set")
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
