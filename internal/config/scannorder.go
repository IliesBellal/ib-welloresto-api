package config

import (
	"os"
)

type ScanNOrderConfig struct {
	SNORedirectBaseURL string
	// ReservationBaseURL : site de réservation public (Reservation.PublicBaseURL,
	// recopié par Load), pour les liens « Réserver une table » du hub.
	ReservationBaseURL string
	// IndexNowKey : clé IndexNow (8 à 128 caractères a-z, A-Z, 0-9, -), la
	// même que la variable INDEXNOW_KEY du front ScanNOrder, qui sert le
	// fichier de clé. Vide = signalement désactivé (docs/scannorder-seo.md) :
	// ne la définir qu'en production.
	IndexNowKey string
}

func loadScanNOrderConfig() ScanNOrderConfig {
	return ScanNOrderConfig{
		SNORedirectBaseURL: os.Getenv("SCANNORDER_BASE_URL"),
		IndexNowKey:        os.Getenv("INDEXNOW_KEY"),
	}
}
