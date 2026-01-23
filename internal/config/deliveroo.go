package config

import "os"

type Deliveroo struct {
	BaseURL   string
	BasicAuth string
}

func loadDeliveroo() Deliveroo {
	return Deliveroo{
		BaseURL:   os.Getenv("DELIVEROO_BASE_URL"),
		BasicAuth: os.Getenv("DELIVEROO_TOKEN"),
	}
}
