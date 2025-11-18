package main

import (
	"log"
	"net/http"

    "go.uber.org/zap"

	"welloresto-api/internal/config"
	"welloresto-api/internal/database"
	"welloresto-api/internal/logger"
)

func main() {
	// Load .env variables
	cfg := config.Load() // cfg est un *config.Config

	// Structured logger
	zlog := logger.New()

	// --- MySQL Connection (ONLY MySQL for now) ---
	mysqlDB, err := database.NewMySQL(cfg.MySQLURL)
	if err != nil {
		zlog.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer mysqlDB.Close()

	// --- Setup Routes ---
	// La fonction SetupRoutes est appelée avec le pointeur cfg
	r := SetupRoutes(zlog, mysqlDB, cfg)

	// --- Start API ---
	zlog.Info("Server running", zap.String("port", cfg.Port))
	log.Fatal(http.ListenAndServe(":"+cfg.Port, r))
}