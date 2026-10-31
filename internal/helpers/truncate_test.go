package helpers

import "testing"

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"court", 10, "court"},
		{"exactement", 10, "exactement"},
		{"un texte trop long", 7, "un text"},
		{"coupe sur un espace ", 13, "coupe sur un"},
		{"🍕🍕🍕🍕", 2, "🍕🍕"},
		{"éééé", 3, "ééé"},
		{"", 5, ""},
		{"abc", 0, ""},
		{"abc", -1, ""},
	}
	for _, c := range cases {
		if got := TruncateRunes(c.in, c.n); got != c.want {
			t.Errorf("TruncateRunes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
