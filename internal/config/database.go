package config

import "os"

type DatabaseConfig struct {
	MySQLURL string
}

func loadDatabase() DatabaseConfig {
	return DatabaseConfig{
		MySQLURL: os.Getenv("MYSQL_URL"),
	}
}
