package config

import "os"

type Config struct {
	Port     string
	MySQLURL string
}

func Load() Config {
	return Config{
		Port:     getEnv("PORT", "8080"),
		MySQLURL: os.Getenv("MYSQL_URL"),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
