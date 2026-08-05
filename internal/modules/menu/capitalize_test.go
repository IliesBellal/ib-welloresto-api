package menu

import "testing"

func TestCapitalizeFirst(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"vide", "", ""},
		{"ascii simple", "viandes", "Viandes"},
		{"ascii deja capitalise", "Viandes", "Viandes"},
		{"une lettre", "a", "A"},
		// Régression : la forme par octet produisait "Ã©picerie" — le 0xC3 de
		// tête était réinterprété comme une rune isolée puis réencodé en UTF-8.
		{"accent initial", "épicerie", "Épicerie"},
		{"accent initial majuscule", "Épicerie", "Épicerie"},
		{"accent non initial", "crémerie", "Crémerie"},
		{"ligature", "œufs", "Œufs"},
		{"non latin", "日本", "日本"},
		{"emoji", "🥕 carottes", "🥕 carottes"},
		{"chiffre initial", "1er choix", "1er choix"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capitalizeFirst(tc.in); got != tc.want {
				t.Errorf("capitalizeFirst(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// La forme historique corrompait les noms accentués : ce test documente
// pourquoi capitalizeFirst ne doit pas revenir à un découpage par octet.
func TestCapitalizeFirstPreservesByteLength(t *testing.T) {
	const in = "épicerie"
	got := capitalizeFirst(in)
	if len([]rune(got)) != len([]rune(in)) {
		t.Fatalf("capitalizeFirst(%q) = %q : %d runes au lieu de %d", in, got, len([]rune(got)), len([]rune(in)))
	}
}
