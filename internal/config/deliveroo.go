package config

import "os"

type Deliveroo struct {
	BaseURL     string
	AuthBaseURL string
	BasicAuth   string
}

func loadDeliveroo() Deliveroo {
	return Deliveroo{
		BaseURL:     os.Getenv("DELIVEROO_BASE_URL"),
		AuthBaseURL: os.Getenv("DELIVEROO_AUTH_BASE_URL"),
		BasicAuth:   os.Getenv("DELIVEROO_TOKEN"),
	}
}
