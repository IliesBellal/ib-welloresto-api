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
	got := ScanNOrderSameAs("https://www.google.com/maps/place/?q=place_id:ChIJ123", `["instagram.com/pizzeria","https://instagram.com/pizzeria/","nope"]`, "www.pizzeria-x.fr")
	want := []string{"https://www.google.com/maps/place/?q=place_id:ChIJ123", "https://instagram.com/pizzeria", "https://www.pizzeria-x.fr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanNOrderSameAs = %v, want %v", got, want)
	}
	if got := ScanNOrderSameAs("", "pas du json", ""); got != nil {
		t.Fatalf("valeur illisible : %v, want nil", got)
	}
}

func TestNormalizeGoogleLink(t *testing.T) {
	ok := map[string]string{
		"https://g.page/r/CabcDEF123/review":                         "https://g.page/r/CabcDEF123/review",
		"http://search.google.com/local/writereview?placeid=ChIJ123": "https://search.google.com/local/writereview?placeid=ChIJ123",
		"maps.app.goo.gl/AbC123":                                     "https://maps.app.goo.gl/AbC123",
		"https://www.google.fr/maps/place/Da+Mario":                  "https://www.google.fr/maps/place/Da+Mario",
		"https://www.google.co.uk/maps?cid=123":                      "https://www.google.co.uk/maps?cid=123",
	}
	for in, want := range ok {
		if got, err := NormalizeGoogleLink(in); err != nil || got != want {
			t.Errorf("NormalizeGoogleLink(%q) = %q, %v ; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "https://evil.com/google.com", "https://google.com.evil.com/", "https://notgoogle.com/x", "https://instagram.com/x", "javascript:alert(1)"} {
		if got, err := NormalizeGoogleLink(in); err == nil {
			t.Errorf("NormalizeGoogleLink(%q) = %q, want error", in, got)
		}
	}
}

func TestGooglePlaceURLs(t *testing.T) {
	if !ValidGooglePlaceID("ChIJN1t_tDeuEmsRUsoyG83frY4") || ValidGooglePlaceID("x") || ValidGooglePlaceID("ChIJ<script>aaaaaaa") {
		t.Fatal("ValidGooglePlaceID")
	}
	if got := GoogleReviewURL("ChIJN1t_tDeuEmsRUsoyG83frY4"); got != "https://search.google.com/local/writereview?placeid=ChIJN1t_tDeuEmsRUsoyG83frY4" {
		t.Fatalf("GoogleReviewURL = %s", got)
	}
	if got := GoogleMapsPlaceURL("ChIJN1t_tDeuEmsRUsoyG83frY4"); got != "https://www.google.com/maps/place/?q=place_id:ChIJN1t_tDeuEmsRUsoyG83frY4" {
		t.Fatalf("GoogleMapsPlaceURL = %s", got)
	}
}
