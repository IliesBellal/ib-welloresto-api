package helpers

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// Liens publics d'un établissement (fiche Google, Instagram, Facebook,
// TripAdvisor, site web…), publiés en sameAs sur sa page ScanNOrder
// (docs/scannorder-seo.md). Stockés en tableau JSON dans
// scannorder_settings.seo_links.

const (
	// ScanNOrderMaxSEOLinks : nombre de liens retenus par établissement.
	ScanNOrderMaxSEOLinks = 8
	// scannorderSEOLinkMaxLen : longueur maximale d'un lien normalisé.
	scannorderSEOLinkMaxLen = 300
)

// ErrInvalidSEOLink : lien vide, trop long, sans domaine ou qui n'est pas une
// page web publique (http/https).
var ErrInvalidSEOLink = errors.New("invalid_seo_link")

// NormalizeScanNOrderSEOLink valide un lien saisi par le restaurateur et le
// ramène à une forme stable : schéma https ajouté s'il manque
// (« instagram.com/pizzeria » → « https://instagram.com/pizzeria »), schéma et
// domaine en minuscules, fragment retiré.
func NormalizeScanNOrderSEOLink(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return "", ErrInvalidSEOLink
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", ErrInvalidSEOLink
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || u.Port() != "" {
		return "", ErrInvalidSEOLink
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = host
	u.Fragment = ""
	u.RawFragment = ""
	out := u.String()
	if len(out) > scannorderSEOLinkMaxLen {
		return "", ErrInvalidSEOLink
	}
	return out, nil
}

// ParseScanNOrderSEOLinks lit la valeur stockée. Une valeur illisible ou un
// élément invalide est ignoré : la page publique ne doit jamais échouer pour
// un lien.
func ParseScanNOrderSEOLinks(stored string) []string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return nil
	}
	var raw []string
	if err := json.Unmarshal([]byte(stored), &raw); err != nil {
		return nil
	}
	return dedupeSEOLinks(raw)
}

// ScanNOrderSameAs assemble les liens publiés en sameAs : ceux du
// référencement, puis le site web de la fiche merchant s'il est valide, sans
// doublon.
func ScanNOrderSameAs(storedLinks, webSite string) []string {
	links := ParseScanNOrderSEOLinks(storedLinks)
	if strings.TrimSpace(webSite) != "" {
		links = append(links, webSite)
	}
	return dedupeSEOLinks(links)
}

func dedupeSEOLinks(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range raw {
		n, err := NormalizeScanNOrderSEOLink(l)
		if err != nil || seen[strings.TrimSuffix(n, "/")] {
			continue
		}
		seen[strings.TrimSuffix(n, "/")] = true
		out = append(out, n)
	}
	return out
}
