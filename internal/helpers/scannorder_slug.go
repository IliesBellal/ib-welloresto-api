package helpers

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Longueurs maximales des deux parties du code : le nom, puis la ville.
const (
	scannorderSlugNameMax = 40
	scannorderSlugCityMax = 30
)

// scannorderReservedSlugs : segments déjà pris par une route
// /scannorder/{segment} (cmd/api/routes.go). Un établissement qui en porterait
// le nom serait inatteignable : la route fixe passe avant /{merchant_slug}.
var scannorderReservedSlugs = map[string]bool{
	"franchises": true,
	"events":     true,
	"sitemap":    true,
}

// slugFolds : lettres que la décomposition Unicode ne ramène pas à une base.
var slugFolds = strings.NewReplacer(
	"œ", "oe", "æ", "ae", "ß", "ss", "ø", "o", "đ", "d", "ł", "l", "ð", "d", "þ", "th",
)

// slugPart met un libellé en minuscules, sans accents, avec un tiret unique à
// la place de tout ce qui n'est ni lettre ni chiffre (espaces, apostrophes,
// ponctuation), et le borne à max caractères.
func slugPart(s string, max int) string {
	s = slugFolds.Replace(strings.ToLower(s))

	var b strings.Builder
	lastDash := true // pas de tiret de tête
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// accent détaché par la décomposition : ignoré
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > max {
		out = strings.Trim(out[:max], "-")
	}
	return out
}

// ScanNOrderSlugBase construit le code lisible du QR principal d'un
// établissement : son nom puis sa ville, ex. « L'Atelier du Bœuf » à Lyon →
// l-atelier-du-boeuf-lyon. La ville n'est pas répétée quand le nom la
// contient déjà en fin (« Pizza Metz » à Metz → pizza-metz). Un nom sans
// lettre latine (idéogrammes…) donne « restaurant ». Les segments réservés
// aux routes (scannorderReservedSlugs) reçoivent le suffixe « -restaurant ».
//
// Le résultat n'est pas forcément libre : l'appelant ajoute -2, -3… si
// besoin (ScanNOrderSlugCandidate).
func ScanNOrderSlugBase(name, city string) string {
	base := slugPart(name, scannorderSlugNameMax)
	if base == "" {
		base = "restaurant"
	}
	if c := slugPart(city, scannorderSlugCityMax); c != "" && base != c && !strings.HasSuffix(base, "-"+c) {
		base += "-" + c
	}
	if scannorderReservedSlugs[base] {
		base += "-restaurant"
	}
	return base
}

// ScanNOrderSlugCandidate : la n-ième tentative (n ≥ 1) pour une base donnée —
// la base elle-même, puis base-2, base-3…
func ScanNOrderSlugCandidate(base string, n int) string {
	if n <= 1 {
		return base
	}
	return base + "-" + strconv.Itoa(n)
}
