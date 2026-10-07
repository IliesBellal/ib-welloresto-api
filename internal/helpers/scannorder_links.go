package helpers

import (
	"net/url"
	"strings"
)

// DefaultScanNOrderBaseURL est l'adresse du front ScanNOrder utilisée quand
// SCANNORDER_BASE_URL n'est pas renseignée. Aucun lien client ne doit être
// écrit en dur ailleurs (docs/SCANNORDER_PUBLIC_ORDER_ID.md, D4).
const DefaultScanNOrderBaseURL = "https://scannorder.welloresto.fr"

// IsOrderPublicID indique si ref est un orders.public_id plutôt qu'un
// order_id interne (entier).
func IsOrderPublicID(ref string) bool {
	return strings.HasPrefix(ref, OrderPublicIDPrefix)
}

// ScanNOrderOrderURL construit le lien de suivi d'une commande sur le front
// ScanNOrder : {baseURL}/restaurant/{slug}/order/{publicOrderID}. baseURL vide
// retombe sur DefaultScanNOrderBaseURL. Renvoie "" si le slug ou l'id public
// manque : mieux vaut pas de lien qu'un lien cassé.
func ScanNOrderOrderURL(baseURL, slug, publicOrderID string) string {
	slug = strings.TrimSpace(slug)
	publicOrderID = strings.TrimSpace(publicOrderID)
	if slug == "" || publicOrderID == "" {
		return ""
	}
	return ScanNOrderBaseURL(baseURL) + "/restaurant/" + url.PathEscape(slug) + "/order/" + url.PathEscape(publicOrderID)
}

// ScanNOrderBaseURL normalise SCANNORDER_BASE_URL (sans "/" final) et
// applique le repli DefaultScanNOrderBaseURL.
func ScanNOrderBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return DefaultScanNOrderBaseURL
	}
	return baseURL
}
