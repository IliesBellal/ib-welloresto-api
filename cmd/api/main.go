package main

import (
	"log"
	"net/http"

	"welloresto/internal/config"
	"welloresto/internal/database"
	"welloresto/internal/logger"
)

func main() {
	// Load .env variables
	cfg := config.Load()

	// Structured logger
	zlog := logger.New()

	// --- MySQL Connection (ONLY MySQL for now) ---
	mysqlDB, err := database.NewMySQL(cfg.MySQLURL)
	if err != nil {
		zlog.Fatal("Failed to connect to MySQL", "error", err)
	}
	defer mysqlDB.Close()

	// --- Setup Routes ---
	r := SetupRoutes(zlog, mysqlDB, cfg)

	// --- Start API ---
	zlog.Info("Server running", "port", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}
