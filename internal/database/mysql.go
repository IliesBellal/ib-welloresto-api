package database

import (
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func NewMySQL(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
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
