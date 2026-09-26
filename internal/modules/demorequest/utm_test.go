package demorequest

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanUTM(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"absent", "", ""},
		{"espaces seuls", "   ", ""},
		{"valeur du dépliant", "dpl_sno_brn_2610", "dpl_sno_brn_2610"},
		{"espaces de bord retirés", "  depliant \t", "depliant"},
		{"caractères de contrôle supprimés", "print\r\nBcc: x@y.z", "printBcc: x@y.z"},
		{"accents conservés", "dépliant", "dépliant"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanUTM(c.in); got != c.want {
				t.Errorf("cleanUTM(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Une valeur forgée trop longue est bornée à maxUTMLength runes, sans jamais
// couper un caractère UTF-8 en deux.
func TestCleanUTMTruncatesOnRuneBoundary(t *testing.T) {
	got := cleanUTM(strings.Repeat("é", maxUTMLength+50))
	if n := utf8.RuneCountInString(got); n != maxUTMLength {
		t.Errorf("rune count = %d, want %d", n, maxUTMLength)
	}
	if !utf8.ValidString(got) {
		t.Error("truncated value is not valid UTF-8")
	}
}

func TestFormatOrigin(t *testing.T) {
	cases := []struct {
		source, medium, campaign, want string
	}{
		{"depliant", "print", "dpl_sno_brn_2610", "depliant · print · dpl_sno_brn_2610"},
		{"", "", "dpl_sno_brn_2610", "dpl_sno_brn_2610"},
		{"newsletter", "email", "", "newsletter · email"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := formatOrigin(c.source, c.medium, c.campaign); got != c.want {
			t.Errorf("formatOrigin(%q, %q, %q) = %q, want %q", c.source, c.medium, c.campaign, got, c.want)
		}
	}
}
