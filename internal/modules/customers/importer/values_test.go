package importer

import (
	"strings"
	"testing"
)

func TestNormalizePhoneFR(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"international plausible", "+33612345678", true},
		{"national plausible", "0612345678", true},
		{"vide", "", false},
		{"implausible", "0000000000", false},
		{"texte", "pas un numero", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizePhoneFR(tc.in)
			if ok != tc.ok {
				t.Fatalf("normalizePhoneFR(%q) ok = %v, want %v (got %q)", tc.in, ok, tc.ok, got)
			}
			if tc.in == "" && got != "" {
				t.Fatalf("normalizePhoneFR(vide) = %q, want vide", got)
			}
		})
	}

	// La valeur normalisee best-effort est rendue meme si implausible : une
	// saisie imparfaite doit rester visible et corrigible.
	got, ok := normalizePhoneFR("0000000000")
	if ok {
		t.Fatal("normalizePhoneFR(0000000000) ok = true, want false")
	}
	if got == "" {
		t.Fatal("normalizePhoneFR(0000000000) = vide, want une valeur best-effort")
	}
}

func TestValidateEmail(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
		state emailState
	}{
		{"valide", "jean.dupont@example.com", "jean.dupont@example.com", emailValid},
		{"casse preservee", "Jean.Dupont@Example.COM", "Jean.Dupont@Example.COM", emailValid},
		{"absent", "", "", emailAbsent},
		{"absent, espaces", "   ", "", emailAbsent},
		{"malforme, pas d'arobase", "jean.dupont-example.com", "", emailInvalid},
		{"malforme, pas de point", "jean.dupont@example", "", emailInvalid},
		{"malforme, espace interne", "jean dupont@example.com", "", emailInvalid},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, state := validateEmail(tc.in)
			if state != tc.state {
				t.Fatalf("validateEmail(%q) state = %v, want %v", tc.in, state, tc.state)
			}
			if got != tc.want {
				t.Fatalf("validateEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseFrenchDate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
		want string // format de controle, "" si nil attendu
	}{
		{"date valide", "14/08/2026", true, "2026-08-14"},
		{"vide", "", true, ""},
		{"espaces seuls", "   ", true, ""},
		{"illisible", "pas une date", false, ""},
		{"format americain refuse", "2026-08-14", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFrenchDate(tc.in)
			if ok != tc.ok {
				t.Fatalf("parseFrenchDate(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatalf("parseFrenchDate(%q) = %v, want nil", tc.in, got)
				}
				return
			}
			if got == nil || got.Format("2006-01-02") != tc.want {
				t.Fatalf("parseFrenchDate(%q) = %v, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseConsent(t *testing.T) {
	cases := []struct {
		name        string
		mail, sms   string
		wantConsent bool
	}{
		{"les deux vides", "", "", false},
		{"mail oui", "Oui", "", true},
		{"sms oui", "", "Oui", true},
		{"les deux oui", "Oui", "Oui", true},
		{"mail non", "Non", "", false},
		{"casse et espaces", "  oui  ", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseConsent(tc.mail, tc.sms)
			if got == nil {
				t.Fatal("parseConsent = nil, want toujours une valeur explicite")
			}
			if *got != tc.wantConsent {
				t.Fatalf("parseConsent(%q, %q) = %v, want %v", tc.mail, tc.sms, *got, tc.wantConsent)
			}
		})
	}
}

func TestBuildDisplayName(t *testing.T) {
	cases := []struct {
		name        string
		first, last string
		want        string
	}{
		{"nom complet", "Jean", "Dupont", "Jean Dupont"},
		{"prenom seul", "Jean", "", "Jean"},
		{"nom seul", "", "Dupont", "Dupont"},
		{"vide", "", "", ""},
		{"espaces multiples", "  Jean   Pierre ", "  Dupont  ", "Jean Pierre Dupont"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildDisplayName(tc.first, tc.last); got != tc.want {
				t.Fatalf("buildDisplayName(%q, %q) = %q, want %q", tc.first, tc.last, got, tc.want)
			}
		})
	}

	long := strings.Repeat("a", 80)
	got := buildDisplayName(long, "")
	if len([]rune(got)) != displayNameMaxLen {
		t.Fatalf("buildDisplayName tronque a %d caracteres, got %d", displayNameMaxLen, len([]rune(got)))
	}
}
