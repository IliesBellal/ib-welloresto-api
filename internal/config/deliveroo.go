package config

import "os"

type Deliveroo struct {
	BaseURL string
	Token   string
}

func loadDeliveroo() Deliveroo {
	return Deliveroo{
		BaseURL: os.Getenv("DELIVEROO_BASE_URL"),
		Token:   os.Getenv("DELIVEROO_TOKEN"),
	}
}
