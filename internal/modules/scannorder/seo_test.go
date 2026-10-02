package scannorder

import "testing"

func strPtr(s string) *string { return &s }

func TestApplyMerchantSEO(t *testing.T) {
	tests := []struct {
		name          string
		row           MerchantSEORow
		wantIndexable bool
	}{
		{"activé, non bloqué, QR principal", MerchantSEORow{Activated: true, MainSlug: strPtr("pizzeria-x")}, true},
		{"ScanNOrder désactivé", MerchantSEORow{Activated: false, MainSlug: strPtr("pizzeria-x")}, false},
		{"abonnement bloquant", MerchantSEORow{Activated: true, Blocked: true, MainSlug: strPtr("pizzeria-x")}, false},
		{"sans QR principal", MerchantSEORow{Activated: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MerchantData{}
			applyMerchantSEO(m, &tt.row)
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
		Activated: true, MainSlug: strPtr("pizzeria-x"),
	})

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
