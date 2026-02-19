package config

import "os"

type GoogleConfig struct {
	APIKey string
}

func loadGoogle() GoogleConfig {
	return GoogleConfig{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	}
}
