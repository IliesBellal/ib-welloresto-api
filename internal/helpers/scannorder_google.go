package helpers

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Fiche Google d'un établissement (docs/scannorder-seo.md, « Fiche Google et
// demandes d'avis ») : lien public de la fiche, publié en sameAs sur la page
// ScanNOrder, et lien « laisser un avis » envoyé aux clients par e-mail.

// ErrInvalidGoogleLink : lien vide, invalide ou hors domaine Google. Le lien
// d'avis part dans des e-mails envoyés au nom de l'établissement : seul un
// domaine Google est accepté, pour qu'il ne puisse pas mener ailleurs.
var ErrInvalidGoogleLink = errors.New("invalid_google_link")

var (
	googleHost   = regexp.MustCompile(`(^|\.)google\.[a-z]{2,3}(\.[a-z]{2})?$`)
	placeIDValue = regexp.MustCompile(`^[A-Za-z0-9_-]{10,255}$`)
)

// NormalizeGoogleLink valide un lien de fiche ou d'avis Google et le passe en
// https : google.<tld> et ses sous-domaines (www, maps, search, business),
// g.page, goo.gl et maps.app.goo.gl.
func NormalizeGoogleLink(raw string) (string, error) {
	n, err := NormalizeScanNOrderSEOLink(raw)
	if err != nil {
		return "", ErrInvalidGoogleLink
	}
	u, err := url.Parse(n)
	if err != nil {
		return "", ErrInvalidGoogleLink
	}
	host := u.Hostname()
	switch {
	case googleHost.MatchString(host), host == "g.page", host == "goo.gl", host == "maps.app.goo.gl":
	default:
		return "", ErrInvalidGoogleLink
	}
	u.Scheme = "https"
	return u.String(), nil
}

// ValidGooglePlaceID : format d'un Place ID Google.
func ValidGooglePlaceID(id string) bool {
	return placeIDValue.MatchString(id)
}

// GoogleMapsPlaceURL : fiche Google d'un lieu, d'après son Place ID.
func GoogleMapsPlaceURL(placeID string) string {
	return "https://www.google.com/maps/place/?q=place_id:" + url.QueryEscape(strings.TrimSpace(placeID))
}

// GoogleReviewURL : formulaire « laisser un avis » d'un lieu, d'après son
// Place ID (format documenté par Google).
func GoogleReviewURL(placeID string) string {
	return "https://search.google.com/local/writereview?placeid=" + url.QueryEscape(strings.TrimSpace(placeID))
}
