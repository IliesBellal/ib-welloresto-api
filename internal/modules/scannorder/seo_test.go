package scannorder

import "testing"

func strPtr(s string) *string { return &s }

func TestApplyMerchantSEO(t *testing.T) {
	tests := []struct {
		name          string
		row           MerchantSEORow
		wantIndexable bool
	}{
		{"carte affichée, non bloquée, QR principal", MerchantSEORow{StorefrontEnabled: true, MainSlug: strPtr("pizzeria-x")}, true},
		{"carte masquée", MerchantSEORow{StorefrontEnabled: false, MainSlug: strPtr("pizzeria-x")}, false},
		{"abonnement bloquant", MerchantSEORow{StorefrontEnabled: true, Blocked: true, MainSlug: strPtr("pizzeria-x")}, false},
		{"sans QR principal", MerchantSEORow{StorefrontEnabled: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MerchantData{}
			applyMerchantSEO(m, &tt.row, "")
			if m.SEO.Indexable != tt.wantIndexable {
				t.Fatalf("Indexable = %v, want %v", m.SEO.Indexable, tt.wantIndexable)
			}
		})
	}
}

func TestApplyMerchantSEO_Fields(t *testing.T) {
	m := &MerchantData{Address: Address{Address: "1 rue X", Lat: 1, Lng: 2}}
	applyMerchantSEO(m, &MerchantSEORow{
		City: "Lyon", ZipCode: "69001", Country: "France",
		SEOTitle: "  ", SEODescription: "Pizzas au feu de bois", SEOCuisineType: "Italienne",
		StorefrontEnabled: true, MainSlug: strPtr("pizzeria-x"),
	}, "")

	if m.Address.City != "Lyon" || m.Address.ZipCode != "69001" || m.Address.Country != "France" || m.Address.Address != "1 rue X" {
		t.Fatalf("adresse inattendue: %+v", m.Address)
	}
	if m.SEO.Title != nil {
		t.Fatalf("un seo_title blanc doit rester nil, got %q", *m.SEO.Title)
	}
	if m.Description == nil || *m.Description != "Pizzas au feu de bois" {
		t.Fatalf("Description = %v", m.Description)
	}
	if m.CuisineType == nil || *m.CuisineType != "Italienne" {
		t.Fatalf("CuisineType = %v", m.CuisineType)
	}
	if m.SEO.CanonicalSlug == nil || *m.SEO.CanonicalSlug != "pizzeria-x" {
		t.Fatalf("CanonicalSlug = %v", m.SEO.CanonicalSlug)
	}
}

func TestApplyMerchantSEO_CuisinesLinksReservation(t *testing.T) {
	m := &MerchantData{}
	applyMerchantSEO(m, &MerchantSEORow{
		SEOCuisineType:  "pizza,italien",
		SEOLinks:        `["instagram.com/da-mario","https://g.page/r/da-mario"]`,
		WebSite:         "www.da-mario.fr",
		ReservationCode: strPtr("da-mario"),
		MainSlug:        strPtr("da-mario-metz"),
	}, "https://rsv.example/")

	if len(m.Cuisines) != 2 || m.Cuisines[0].Code != "pizza" || m.Cuisines[0].Place != "Pizzeria" {
		t.Fatalf("Cuisines = %+v", m.Cuisines)
	}
	if m.CuisineType == nil || *m.CuisineType != "Pizza, Italienne" {
		t.Fatalf("CuisineType = %v", m.CuisineType)
	}
	wantSameAs := []string{"https://instagram.com/da-mario", "https://g.page/r/da-mario", "https://www.da-mario.fr"}
	if len(m.SameAs) != len(wantSameAs) {
		t.Fatalf("SameAs = %v, want %v", m.SameAs, wantSameAs)
	}
	for i := range wantSameAs {
		if m.SameAs[i] != wantSameAs[i] {
			t.Fatalf("SameAs = %v, want %v", m.SameAs, wantSameAs)
		}
	}
	if m.ReservationURL == nil || *m.ReservationURL != "https://rsv.example/restaurant/da-mario" {
		t.Fatalf("ReservationURL = %v", m.ReservationURL)
	}

	// sans base de réservation configurée, ni cuisine : rien d'inventé
	m = &MerchantData{}
	applyMerchantSEO(m, &MerchantSEORow{ReservationCode: strPtr("da-mario")}, "")
	if m.ReservationURL != nil || m.CuisineType != nil || m.Cuisines != nil || m.SameAs != nil {
		t.Fatalf("champs inventés : %+v", m)
	}
}
