package config

import "os"

type DatabaseConfig struct {
	MySQLURL    string
	PostgresURL string
}

func loadDatabase() DatabaseConfig {
	return DatabaseConfig{
		MySQLURL:    os.Getenv("MYSQL_URL"),
		PostgresURL: os.Getenv("POSTGRES_URL"),
	}
}
