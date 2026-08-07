package importer

import (
	"strings"
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

func TestSplitLabels(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"liste simple", "NOS PIZZA, VEGE, BASE TOMATE", []string{"NOS PIZZA", "VEGE", "BASE TOMATE"}},
		{"sans espace", "a,b", []string{"a", "b"}},
		{"cellule vide", "", nil},
		{"virgules seules", " , , ", nil},
		{"element vide intercale", "a,,b", []string{"a", "b"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitLabels(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitLabels(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("splitLabels(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Deux libelles qui ne different que par les accents sont deux tags distincts
// pour le restaurateur : normalizeLabel doit les separer. foldHeader, lui, ne
// sert qu'aux en-tetes de colonnes et doit au contraire les confondre.
func TestNormalizeLabelKeepsAccentsAndFoldHeaderDropsThem(t *testing.T) {
	if normalizeLabel("VÉGÉ") == normalizeLabel("VEGE") {
		t.Fatal("normalizeLabel confond VÉGÉ et VEGE")
	}
	if got, want := normalizeLabel("  NOS   PIZZA "), "nos pizza"; got != want {
		t.Fatalf("normalizeLabel = %q, want %q", got, want)
	}
	if got, want := foldHeader(" Catégorie "), "categorie"; got != want {
		t.Fatalf("foldHeader = %q, want %q", got, want)
	}
	if foldHeader("Prix emporté") != foldHeader("PRIX  EMPORTE") {
		t.Fatal("foldHeader distingue deux ecritures du meme en-tete")
	}
}

// L'identifiant genere porte l'idempotence de l'import : il doit etre stable
// pour un meme libelle, distinct pour deux libelles differents, et tenir dans
// import_*_mapping.external_id (varchar(64)).
func TestGeneratedExternalID(t *testing.T) {
	const maxExternalIDLen = 64

	pizza := generatedExternalID(welloGenericProductPrefix, "Pizza Margherita")

	if got := generatedExternalID(welloGenericProductPrefix, "  pizza   MARGHERITA "); got != pizza {
		t.Fatalf("identifiant instable selon la casse et l'espacement: %q != %q", got, pizza)
	}
	if got := generatedExternalID(welloGenericProductPrefix, "Pizza Margarita"); got == pizza {
		t.Fatalf("deux libelles differents partagent l'identifiant %q", got)
	}
	if !strings.HasPrefix(pizza, welloGenericProductPrefix+"-pizza-margherita-") {
		t.Fatalf("identifiant = %q, want un slug lisible apres le prefixe", pizza)
	}

	long := generatedExternalID(welloGenericProductPrefix, strings.Repeat("Pizza tres longuement nommee ", 10))
	if len(long) > maxExternalIDLen {
		t.Fatalf("identifiant de %d caracteres, want <= %d: %q", len(long), maxExternalIDLen, long)
	}

	// Un nom sans caractere latin ne donne pas de slug : l'empreinte doit
	// suffire a produire un identifiant exploitable.
	emoji := generatedExternalID(welloGenericProductPrefix, "🍕🔥")
	if suffix := strings.TrimPrefix(emoji, welloGenericProductPrefix+"-"); suffix == emoji || len(suffix) != generatedIDHashLen {
		t.Fatalf("identifiant pour un libelle non latin = %q, want prefixe + empreinte seule", emoji)
	}
	if got := generatedExternalID(welloGenericProductPrefix, "🍕🔥"); got != emoji {
		t.Fatalf("identifiant non latin instable: %q != %q", got, emoji)
	}
}

func ptr[T any](v T) *T { return &v }
