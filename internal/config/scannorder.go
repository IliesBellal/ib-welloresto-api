package config

import (
	"os"
)

type ScanNOrderConfig struct {
	SNORedirectBaseURL string
}

func loadScanNOrderConfig() ScanNOrderConfig {
	return ScanNOrderConfig{
		SNORedirectBaseURL: os.Getenv("SCANNORDER_BASE_URL"),
	}
}
