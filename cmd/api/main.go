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
	cfg := config.Load()

	// Logger
	zlog := logger.New()
	zap.ReplaceGlobals(zlog)

	// MySQL
	mysqlDB, err := database.NewMySQL(cfg.Database)
	if err != nil {
		zlog.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer mysqlDB.Close()

	// Router
	r := SetupRoutes(zlog, mysqlDB, cfg)

	zlog.Info("Server running", zap.String("port", cfg.App.Port))
	log.Fatal(http.ListenAndServe(":"+cfg.App.Port, r))
}
