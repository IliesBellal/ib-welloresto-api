package config

import (
	"log"
	"os"
)

type AppConfig struct {
	App       App
	Database  Database
	Google    Google
	UberEats  UberEats
	Deliveroo Deliveroo
}

type App struct {
	Port string
}

func Load() *AppConfig {
	cfg := &AppConfig{
		App: App{
			Port: getEnv("PORT", "8080"),
		},
		Database:  loadDatabase(),
		Google:    loadGoogle(),
		UberEats:  loadUberEats(),
		Deliveroo: loadDeliveroo(),
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
