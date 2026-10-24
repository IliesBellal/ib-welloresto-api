package config

import (
	"os"
)

type ScanNOrderConfig struct {
	SNORedirectBaseURL string
	// ReservationBaseURL : site de réservation public (Reservation.PublicBaseURL,
	// recopié par Load), pour les liens « Réserver une table » du hub.
	ReservationBaseURL string
}

func loadScanNOrderConfig() ScanNOrderConfig {
	return ScanNOrderConfig{
		SNORedirectBaseURL: os.Getenv("SCANNORDER_BASE_URL"),
	}
}
