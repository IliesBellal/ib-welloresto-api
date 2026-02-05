package config

import "os"

type DeliverooConfig struct {
	BaseURL     string
	AuthBaseURL string
	BasicAuth   string
}

func loadDeliveroo() DeliverooConfig {
	return DeliverooConfig{
		BaseURL:     os.Getenv("DELIVEROO_BASE_URL"),
		AuthBaseURL: os.Getenv("DELIVEROO_AUTH_BASE_URL"),
		BasicAuth:   os.Getenv("DELIVEROO_TOKEN"),
	}
}
