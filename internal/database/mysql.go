package database

import (
	"database/sql"
	"time"
	"welloresto-api/internal/config"

	_ "github.com/go-sql-driver/mysql"
)

func NewMySQL(dsn config.Database) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn.MySQLURL)
	if err != nil {
		return nil, err
	}

	// ❗ Hostinger: 1 connexion MAX
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)

	// Limite basse pour éviter les connexions zombie
	db.SetConnMaxLifetime(2 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}
