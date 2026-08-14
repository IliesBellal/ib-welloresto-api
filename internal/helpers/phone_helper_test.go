package helpers

import "testing"

// Reproduit l'incident production du 2026-08-14 : le nom Zelty "ÉQUITATION "
// (E-accent, 0xC3 0x89 en UTF-8) traversait Ucfirst en decoupage par octet,
// produisant un octet 0x89 orphelin en tete de chaine — invalide en UTF-8,
// rejete par Postgres a l'insertion (SQLSTATE 22021).
func TestUcfirstMultiByteFirstRune(t *testing.T) {
	got := Ucfirst("ÉQUITATION ")
	want := "ÉQUITATION "
	if got != want {
		t.Fatalf("Ucfirst(%q) = %q, want %q", "ÉQUITATION ", got, want)
	}
}

func TestUcfirstLowercaseAccent(t *testing.T) {
	got := Ucfirst("équitation")
	want := "Équitation"
	if got != want {
		t.Fatalf("Ucfirst(%q) = %q, want %q", "équitation", got, want)
	}
}

func TestUcfirstASCII(t *testing.T) {
	if got, want := Ucfirst("jean"), "Jean"; got != want {
		t.Fatalf("Ucfirst(%q) = %q, want %q", "jean", got, want)
	}
}

func TestUcfirstEmpty(t *testing.T) {
	if got := Ucfirst(""); got != "" {
		t.Fatalf("Ucfirst(\"\") = %q, want \"\"", got)
	}
}
