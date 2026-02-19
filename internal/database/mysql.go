package database

import (
	"database/sql"
	"time"
	"welloresto-api/internal/config"

	_ "github.com/go-sql-driver/mysql"
)

func NewMySQL(dsn config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn.MySQLURL)
	if err != nil {
		return nil, err
	}

	// ❗ Hostinger: 1 connexion MAX
	//db.SetMaxOpenConns(1)
	//db.SetMaxIdleConns(0)

	db.SetMaxOpenConns(1)                  // Maximum 1 connexion ouverte en même temps
	db.SetMaxIdleConns(1)                  // Maximum 1 connexion en attente
	db.SetConnMaxLifetime(time.Minute * 5) // Renouveler la connexion régulièrement

	// Limite basse pour éviter les connexions zombie
	db.SetConnMaxLifetime(2 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}
