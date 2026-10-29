package helpers

import "testing"

func TestPasswordMatches(t *testing.T) {
	hash, err := HashUserPassword("bon-mot-de-passe")
	if err != nil {
		t.Fatalf("HashUserPassword: %v", err)
	}
	cases := []struct {
		name, input, stored string
		want                bool
	}{
		{"bon mot de passe", "bon-mot-de-passe", hash, true},
		{"mauvais mot de passe", "faux", hash, false},
		// Compte Google seul (password = '') : un mot de passe vide ne
		// doit jamais ouvrir la session.
		{"vide contre vide", "", "", false},
		{"saisie vide", "", hash, false},
		// Plus de comparaison en clair (ancienne version PHP).
		{"stocké en clair", "secret", "secret", false},
	}
	for _, c := range cases {
		if got := PasswordMatches(c.input, c.stored); got != c.want {
			t.Errorf("%s : PasswordMatches = %v, want %v", c.name, got, c.want)
		}
	}
}
