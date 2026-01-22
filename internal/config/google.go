package config

import "os"

type Google struct {
	APIKey string
}

func loadGoogle() Google {
	return Google{
		APIKey: os.Getenv("GOOGLE_API_KEY"),
	}
}
