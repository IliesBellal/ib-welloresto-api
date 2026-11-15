package helpers

import (
	"strings"
	"testing"
)

func TestScanNOrderSlugBase(t *testing.T) {
	cases := []struct {
		name, city, want string
	}{
		{"Pizzeria Luigi", "Metz", "pizzeria-luigi-metz"},
		{"L'Atelier du Bœuf", "Lyon", "l-atelier-du-boeuf-lyon"},
		{"Café de l’Été — Chez Zoé !", "Saint-Étienne", "cafe-de-l-ete-chez-zoe-saint-etienne"},
		{"  Le   Petit  Prince ", "  Aix en Provence ", "le-petit-prince-aix-en-provence"},
		{"Bar & Grill 24/7", "Nancy", "bar-grill-24-7-nancy"},
		{"Peña Ñandú", "Paris", "pena-nandu-paris"},
		{"Pizza Metz", "Metz", "pizza-metz"},
		{"Metz", "Metz", "metz"},
		{"Chez Luigi", "", "chez-luigi"},
		{"Chez Luigi", "***", "chez-luigi"},
		{"寿司屋", "Paris", "restaurant-paris"},
		{"", "", "restaurant"},
		// segments de route réservés
		{"Events", "", "events-restaurant"},
		{"Franchises", "", "franchises-restaurant"},
		{"Sitemap", "", "sitemap-restaurant"},
		{"Events", "Lyon", "events-lyon"},
	}
	for _, c := range cases {
		if got := ScanNOrderSlugBase(c.name, c.city); got != c.want {
			t.Errorf("ScanNOrderSlugBase(%q, %q) = %q, want %q", c.name, c.city, got, c.want)
		}
	}
}

func TestScanNOrderSlugBase_Bounded(t *testing.T) {
	long := strings.Repeat("restaurant du grand boulevard ", 10)
	got := ScanNOrderSlugBase(long, strings.Repeat("saint remy les chevreuse ", 5))
	if len(got) > scannorderSlugNameMax+1+scannorderSlugCityMax {
		t.Fatalf("slug trop long (%d) : %q", len(got), got)
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") || strings.Contains(got, "--") {
		t.Fatalf("tirets mal placés : %q", got)
	}
}

func TestScanNOrderSlugBase_OnlyURLSafe(t *testing.T) {
	got := ScanNOrderSlugBase("Ça va ? <script>/../\\ 100% \"vrai\"", "Zürich")
	for _, r := range got {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			t.Fatalf("caractère hors [a-z0-9-] dans %q", got)
		}
	}
}

func TestScanNOrderSlugCandidate(t *testing.T) {
	if got := ScanNOrderSlugCandidate("pizza-metz", 1); got != "pizza-metz" {
		t.Errorf("n=1 : %q", got)
	}
	if got := ScanNOrderSlugCandidate("pizza-metz", 3); got != "pizza-metz-3" {
		t.Errorf("n=3 : %q", got)
	}
}
