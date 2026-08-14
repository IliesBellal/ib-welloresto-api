package importer

import (
	"testing"
)

func TestParsePriceCents(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"virgule francaise, une decimale", "9,9", 990},
		{"virgule francaise, deux decimales", "13,90", 1390},
		{"entier", "12", 1200},
		{"zero", "0", 0},
		{"cellule vide", "", 0},
		{"espaces de bordure", "  4,5  ", 450},
		{"point decimal", "2.5", 250},
		{"separateur de milliers espace", "1 234,50", 123450},
		{"separateur de milliers insecable", "1 234,50", 123450},
		{"symbole euro", "6,90 €", 690},
		{"separateur decimal orphelin", "12,", 1200},
		{"troisieme decimale arrondie au superieur", "12,345", 1235},
		{"troisieme decimale arrondie a l'inferieur", "12,344", 1234},
		{"retenue sur l'arrondi", "12,999", 1300},
		{"demi-centime exact", "1,005", 101},
		{"decimale seule", ",5", 50},
		{"supplement negatif", "-1,5", -150},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePriceCents(tc.in)
			if err != nil {
				t.Fatalf("parsePriceCents(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parsePriceCents(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}

	// Un montant illisible doit remonter, jamais devenir 0 en silence : une
	// erreur de saisie qui passe inapercue finit en prix errone sur la carte.
	for _, invalid := range []string{"gratuit", "9,90€/kg", "1.234,50", "--3", "9,9,9"} {
		if got, err := parsePriceCents(invalid); err == nil {
			t.Fatalf("parsePriceCents(%q) = %d, want une erreur", invalid, got)
		}
	}
}

func TestParseTvaRate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want *float64
	}{
		{"taux entier", "10", ptr(10.0)},
		{"taux decimal, point", "5.5", ptr(5.5)},
		{"taux decimal, virgule", "5,5", ptr(5.5)},
		{"zero explicite (canal desactive)", "0", ptr(0.0)},
		{"cellule vide (absence)", "", nil},
		{"signe pourcent", "20 %", ptr(20.0)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTvaRate(tc.in)
			if err != nil {
				t.Fatalf("parseTvaRate(%q) error = %v", tc.in, err)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("parseTvaRate(%q) = %v, want nil", tc.in, *got)
			case tc.want != nil && got == nil:
				t.Fatalf("parseTvaRate(%q) = nil, want %v", tc.in, *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("parseTvaRate(%q) = %v, want %v", tc.in, *got, *tc.want)
			}
		})
	}

	for _, invalid := range []string{"dix", "-5"} {
		if _, err := parseTvaRate(invalid); err == nil {
			t.Fatalf("parseTvaRate(%q) = nil, want une erreur", invalid)
		}
	}
}

func ptr[T any](v T) *T { return &v }
