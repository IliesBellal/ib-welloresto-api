package assistant

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Normalize applique EXACTEMENT les règles de normalize() du back-office
// (src/lib/assistant/normalize.ts) : minuscules, ligatures, accents retirés,
// tout caractère hors [a-z0-9] remplacé par une espace, espaces réduites.
// Un terme appris doit être stocké sous la forme que le client compare.
func Normalize(text string) string {
	lower := strings.ToLower(text)
	lower = strings.NewReplacer("œ", "oe", "æ", "ae", "ß", "ss").Replace(lower)
	decomposed := norm.NFD.String(lower)

	var b strings.Builder
	b.Grow(len(decomposed))
	space := false
	for _, r := range decomposed {
		if r >= 0x300 && r <= 0x36f {
			continue // diacritique (même plage que le client)
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
			continue
		}
		space = true
	}
	return b.String()
}

// Tokens découpe un texte déjà normalisé.
func Tokens(normalized string) []string {
	if normalized == "" {
		return nil
	}
	return strings.Split(normalized, " ")
}
