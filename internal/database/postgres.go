package database

import (
	"database/sql"
	"time"
	"welloresto-api/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func NewPostgres(dsn config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn.PostgresURL)
	if err != nil {
		return nil, err
	}

	// Mêmes options de pool que la config MySQL le temps de la migration
	db.SetMaxOpenConns(1)                   // Maximum 1 connexion ouverte en même temps
	db.SetMaxIdleConns(1)                   // Maximum 1 connexion en attente
	db.SetConnMaxLifetime(time.Minute * 3)  // Renouveler la connexion régulièrement
	db.SetConnMaxIdleTime(30 * time.Second) // Aligné sur la config MySQL

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}
