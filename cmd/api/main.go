package main

import (
	"database/sql"
	"log"
	"net/http"

	"go.uber.org/zap"

	"welloresto-api/internal/config"
	"welloresto-api/internal/database"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
)

func main() {
	// Load .env variables
	cfg := config.Load()

	// Logger
	zlog := logger.New()
	zap.ReplaceGlobals(zlog)

	// DB (MySQL par défaut, Postgres si DB_DIALECT=postgres — migration en cours)
	var db *sql.DB
	var err error
	if dbx.ActiveDialect() == dbx.Postgres {
		db, err = database.NewPostgres(cfg.Database)
		if err != nil {
			zlog.Fatal("Failed to connect to Postgres", zap.Error(err))
		}
	} else {
		db, err = database.NewMySQL(cfg.Database)
		if err != nil {
			zlog.Fatal("Failed to connect to MySQL", zap.Error(err))
		}
	}
	defer db.Close()

	// Router
	r := SetupRoutes(zlog, db, cfg)

	zlog.Info("Server running", zap.String("port", cfg.App.Port))
	log.Fatal(http.ListenAndServe(":"+cfg.App.Port, r))
}
