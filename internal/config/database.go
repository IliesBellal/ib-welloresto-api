package config

import "os"

type DatabaseConfig struct {
	MySQLURL    string
	PostgresURL string
	// AnalyticsPostgresURL is the DSN for the low-priority analytics pool
	// (internal/database/postgres.go's NewAnalyticsPostgres). Falls back to
	// PostgresURL when unset, so analytics runs against the same instance
	// until a dedicated read replica exists — at which point only this env
	// var changes, no code does. See docs/analytics (wello-back-office repo)
	// PROMPT 03 §1.5.
	AnalyticsPostgresURL string
}

func loadDatabase() DatabaseConfig {
	postgresURL := os.Getenv("POSTGRES_URL")
	analyticsURL := os.Getenv("ANALYTICS_DATABASE_URL")
	if analyticsURL == "" {
		analyticsURL = postgresURL
	}
	return DatabaseConfig{
		MySQLURL:             os.Getenv("MYSQL_URL"),
		PostgresURL:          postgresURL,
		AnalyticsPostgresURL: analyticsURL,
	}
}
