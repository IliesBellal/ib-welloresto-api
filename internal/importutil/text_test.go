package importutil

import (
	"strings"
	"testing"
)

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
			got := SplitLabels(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("SplitLabels(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("SplitLabels(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Deux libelles qui ne different que par les accents sont deux entites
// distinctes pour l'utilisateur : NormalizeLabel doit les separer. FoldHeader,
// lui, ne sert qu'aux en-tetes de colonnes et doit au contraire les confondre.
func TestNormalizeLabelKeepsAccentsAndFoldHeaderDropsThem(t *testing.T) {
	if NormalizeLabel("VÉGÉ") == NormalizeLabel("VEGE") {
		t.Fatal("NormalizeLabel confond VÉGÉ et VEGE")
	}
	if got, want := NormalizeLabel("  NOS   PIZZA "), "nos pizza"; got != want {
		t.Fatalf("NormalizeLabel = %q, want %q", got, want)
	}
	if got, want := FoldHeader(" Catégorie "), "categorie"; got != want {
		t.Fatalf("FoldHeader = %q, want %q", got, want)
	}
	if FoldHeader("Prix emporté") != FoldHeader("PRIX  EMPORTE") {
		t.Fatal("FoldHeader distingue deux ecritures du meme en-tete")
	}
}

func TestSlugify(t *testing.T) {
	if got, want := Slugify("Pizza Margherita !"), "pizza-margherita"; got != want {
		t.Fatalf("Slugify = %q, want %q", got, want)
	}
	if got := Slugify("🍕🔥"); got != "" {
		t.Fatalf("Slugify(non latin) = %q, want vide", got)
	}
	long := Slugify(strings.Repeat("a ", 100))
	if len(long) > maxSlugLen {
		t.Fatalf("Slugify tronque a %d caracteres, got %d", maxSlugLen, len(long))
	}
}

// L'identifiant genere porte l'idempotence de l'import : il doit etre stable
// pour un meme libelle, distinct pour deux libelles differents, et tenir dans
// import_*_mapping.external_id (varchar(64)).
func TestGeneratedExternalID(t *testing.T) {
	const (
		prefix           = "test-p"
		maxExternalIDLen = 64
	)

	pizza := GeneratedExternalID(prefix, "Pizza Margherita")

	if got := GeneratedExternalID(prefix, "  pizza   MARGHERITA "); got != pizza {
		t.Fatalf("identifiant instable selon la casse et l'espacement: %q != %q", got, pizza)
	}
	if got := GeneratedExternalID(prefix, "Pizza Margarita"); got == pizza {
		t.Fatalf("deux libelles differents partagent l'identifiant %q", got)
	}
	if !strings.HasPrefix(pizza, prefix+"-pizza-margherita-") {
		t.Fatalf("identifiant = %q, want un slug lisible apres le prefixe", pizza)
	}

	long := GeneratedExternalID(prefix, strings.Repeat("Pizza tres longuement nommee ", 10))
	if len(long) > maxExternalIDLen {
		t.Fatalf("identifiant de %d caracteres, want <= %d: %q", len(long), maxExternalIDLen, long)
	}

	// Un nom sans caractere latin ne donne pas de slug : l'empreinte doit
	// suffire a produire un identifiant exploitable.
	emoji := GeneratedExternalID(prefix, "🍕🔥")
	if suffix := strings.TrimPrefix(emoji, prefix+"-"); suffix == emoji || len(suffix) != generatedIDHashLen {
		t.Fatalf("identifiant pour un libelle non latin = %q, want prefixe + empreinte seule", emoji)
	}
	if got := GeneratedExternalID(prefix, "🍕🔥"); got != emoji {
		t.Fatalf("identifiant non latin instable: %q != %q", got, emoji)
	}
}
