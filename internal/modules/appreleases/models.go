// Package appreleases expose publiquement le téléchargement de la dernière
// version des applications Android (caisse, borne, affichage client), pour la
// page Téléchargements du site vitrine. Source : la table app_version, celle
// que les appareils consultent déjà pour se mettre à jour (POST
// /app/version/check, /kiosk/app/version-check, /cds/app/version-check). Voir
// docs/telechargements-apps.md.
package appreleases

import "time"

// publicApps associe le nom public d'une application, utilisé dans l'URL
// (/v1/public/apps/{app}/...), à son app_id dans app_version. Seules ces
// applications sont exposées : WR_DELIVERY / WR_WAITER et les app_id de test
// ne le sont jamais.
var publicApps = map[string]string{
	"caisse": "WR_RECEPTION",
	"borne":  "kiosk",
	"cds":    "cds",
}

// LatestRelease est la dernière version publique d'une application.
type LatestRelease struct {
	App         string
	VersionCode int
	ReleaseDate time.Time
	DownloadURL string
}
