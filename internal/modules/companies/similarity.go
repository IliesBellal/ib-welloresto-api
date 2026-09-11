package companies

import (
	"strings"
	"unicode"
)

// nameSimilarity is the "similarité applicative" fallback the chantier asks
// for when pg_trgm is unavailable (confirmed 2026-09-11: not installed on
// the staging Postgres this dépôt uses — see docs/decisions.md). Normalized
// Levenshtein ratio: 1 - distance/max(len(a),len(b)), on lowercased,
// diacritic-stripped, punctuation-collapsed input — cheap, dependency-free,
// and good enough to separate "Le Maghreb" from "Ok Pizza" without pulling
// in a fuzzy-matching library this repo has never needed before.
func nameSimilarity(a, b string) float64 {
	na, nb := normalizeName(a), normalizeName(b)
	if na == "" && nb == "" {
		return 1
	}
	if na == "" || nb == "" {
		return 0
	}
	dist := levenshtein(na, nb)
	maxLen := len(na)
	if len(nb) > maxLen {
		maxLen = len(nb)
	}
	if maxLen == 0 {
		return 1
	}
	sim := 1 - float64(dist)/float64(maxLen)
	if sim < 0 {
		return 0
	}
	return sim
}

// normalizeName lowercases, strips diacritics, and collapses any run of
// non-alphanumeric characters to a single space — "Café LE MAGHREB!!" and
// "cafe le maghreb" compare identically.
func normalizeName(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(s) {
		r = stripDiacritic(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// stripDiacritic maps the handful of accented Latin letters that show up in
// French business names to their plain equivalent — not a general Unicode
// normalizer, just enough for this input.
func stripDiacritic(r rune) rune {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä':
		return 'a'
	case 'ç':
		return 'c'
	case 'è', 'é', 'ê', 'ë':
		return 'e'
	case 'ì', 'í', 'î', 'ï':
		return 'i'
	case 'ñ':
		return 'n'
	case 'ò', 'ó', 'ô', 'õ', 'ö':
		return 'o'
	case 'ù', 'ú', 'û', 'ü':
		return 'u'
	default:
		return r
	}
}

// levenshtein is the classic edit-distance DP, O(len(a)*len(b)) — fine at
// business-name lengths.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n, m := len(ra), len(rb)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}
	prev := make([]int, m+1)
	curr := make([]int, m+1)
	for j := 0; j <= m; j++ {
		prev[j] = j
	}
	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			min := del
			if ins < min {
				min = ins
			}
			if sub < min {
				min = sub
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}
	return prev[m]
}
