package helpers

import (
	"reflect"
	"testing"
)

func TestScanNOrderCuisines_CodesUniqueAndComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range ScanNOrderCuisines() {
		if c.Code == "" || c.Label == "" || c.Place == "" {
			t.Fatalf("cuisine incomplète : %+v", c)
		}
		if seen[c.Code] {
			t.Fatalf("code en double : %s", c.Code)
		}
		seen[c.Code] = true
	}
}

func TestParseScanNOrderCuisines(t *testing.T) {
	got := ParseScanNOrderCuisines(" pizza, italien ,,pizza, Cuisine du marché")
	want := []ScanNOrderCuisine{
		{Code: "pizza", Label: "Pizza", Place: "Pizzeria"},
		{Code: "italien", Label: "Italienne", Place: "Restaurant italien"},
		// texte libre antérieur à la liste fermée : gardé tel quel
		{Label: "Cuisine du marché", Place: "Cuisine du marché"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseScanNOrderCuisines = %+v, want %+v", got, want)
	}
	if got := ParseScanNOrderCuisines(""); got != nil {
		t.Fatalf("valeur vide : %+v, want nil", got)
	}
}

func TestNormalizeScanNOrderSEOLink(t *testing.T) {
	ok := map[string]string{
		"instagram.com/pizzeria":                 "https://instagram.com/pizzeria",
		" https://WWW.Facebook.com/Pizzeria#top": "https://www.facebook.com/Pizzeria",
		"http://pizzeria-x.fr":                   "http://pizzeria-x.fr",
		"https://maps.app.goo.gl/AbCd123":        "https://maps.app.goo.gl/AbCd123",
	}
	for in, want := range ok {
		got, err := NormalizeScanNOrderSEOLink(in)
		if err != nil || got != want {
			t.Errorf("NormalizeScanNOrderSEOLink(%q) = %q, %v ; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "pizzeria", "ftp://pizzeria.fr", "javascript:alert(1)", "https://user:pw@pizzeria.fr", "https://pizzeria.fr:8080/", "https://pizza ria.fr", "mailto:a@b.fr"} {
		if got, err := NormalizeScanNOrderSEOLink(in); err == nil {
			t.Errorf("NormalizeScanNOrderSEOLink(%q) = %q, want error", in, got)
		}
	}
}

func TestScanNOrderSameAs(t *testing.T) {
	got := ScanNOrderSameAs(`["instagram.com/pizzeria","https://instagram.com/pizzeria/","nope"]`, "www.pizzeria-x.fr")
	want := []string{"https://instagram.com/pizzeria", "https://www.pizzeria-x.fr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanNOrderSameAs = %v, want %v", got, want)
	}
	if got := ScanNOrderSameAs("pas du json", ""); got != nil {
		t.Fatalf("valeur illisible : %v, want nil", got)
	}
}
