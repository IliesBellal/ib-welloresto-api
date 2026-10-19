package stripeclient

import "testing"

func TestStatementDescriptor(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"accents et majuscules", "Le Café de la Gare", "LE CAFE DE LA GARE"},
		{"ponctuation interdite", `Chez "Mamie" l'Italienne*`, "CHEZ MAMIE L ITALIENNE"},
		{"ligature", "Bœuf & Co", "BOEUF CO"},
		{"tronqué à 22 sans espace final", "La Brasserie du Vieux Port de Metz", "LA BRASSERIE DU VIEUX"},
		{"point et tiret conservés", "Pizz-a.Roma", "PIZZ-A.ROMA"},
		{"trop court", "Bao", ""},
		{"aucune lettre", "12345 678", ""},
		{"non latin", "寿司屋", ""},
		{"vide", "  ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StatementDescriptor(tc.in)
			if got != tc.want {
				t.Fatalf("StatementDescriptor(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got != "" && (len(got) < 5 || len(got) > 22) {
				t.Fatalf("StatementDescriptor(%q) = %q: longueur %d hors 5..22", tc.in, got, len(got))
			}
		})
	}
}

func TestConnectAccountProfileParams(t *testing.T) {
	p := ConnectAccountProfile{BusinessName: "Le Bistrot", URL: "https://scannorder.welloresto.fr/restaurant/le-bistrot"}

	bp := p.businessProfileParams()
	if bp.Name == nil || *bp.Name != "Le Bistrot" {
		t.Fatalf("business_profile.name = %v, want Le Bistrot", bp.Name)
	}
	if bp.MCC == nil || *bp.MCC != mccRestaurants {
		t.Fatalf("business_profile.mcc = %v, want %s", bp.MCC, mccRestaurants)
	}
	if bp.URL == nil || *bp.URL != p.URL {
		t.Fatalf("business_profile.url = %v, want %s", bp.URL, p.URL)
	}

	s := p.settingsParams()
	if s == nil || s.Payments == nil || *s.Payments.StatementDescriptor != "LE BISTROT" {
		t.Fatalf("settings.payments.statement_descriptor inattendu: %+v", s)
	}

	empty := ConnectAccountProfile{BusinessName: "Bao"}
	if empty.businessProfileParams().URL != nil {
		t.Fatal("business_profile.url doit rester absent sans URL")
	}
	if empty.settingsParams() != nil {
		t.Fatal("settings doit rester absent sans libellé valide")
	}
}
