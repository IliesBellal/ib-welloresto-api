package service

import (
	"context"
	"strings"
	"testing"
)

func TestUberCustomer_commandeSansConvive_neFaitPasPlanter(t *testing.T) {
	// payload anormal : aucun convive (eaters vide). Le code lisait Eaters[0] sans garde.
	order := parseOrder(t, `{"id": "o1", "display_id": "ABCDE", "eaters": [], "eater": {"first_name": "Samantha", "last_name": "K."},
	  "cart": {"items": [{"id": "a", "title": "Pizza", "quantity": 1, "price": {"unit_price": {"amount": 1000}}}]},
	  "payment": {"charges": {}}}`)

	req := MapUberOrderToRequest(context.Background(), order, "212")

	c := req.Order.Customer
	if c == nil || c.Name == nil || *c.Name != "Samantha K." {
		t.Fatalf("client = %+v", c)
	}
	if c.BrandCustomerID != nil {
		t.Errorf("sans convive il n'y a pas d'identifiant, got %q", *c.BrandCustomerID)
	}
}

func TestUberCustomer_premierConviveIdentifieLeClient(t *testing.T) {
	order := parseOrder(t, `{"eaters": [{"id": "eater-1", "first_name": "A"}, {"id": "eater-2", "first_name": "B"}], "eater": {"first_name": "Samantha"}}`)

	c := uberCustomer(order, "212")

	if c.BrandCustomerID == nil || *c.BrandCustomerID != "eater-1" {
		t.Fatalf("BrandCustomerID = %v, want eater-1", c.BrandCustomerID)
	}
}

func TestUberCustomer_numeroTemporaireEtCode(t *testing.T) {
	order := parseOrder(t, `{"eater": {"first_name": "Samantha", "phone": "+33 6 44 60 35 83", "phone_code": "320 82 206"}}`)

	c := uberCustomer(order, "212")

	if c.TemporaryPhone == nil || *c.TemporaryPhone != "+33 6 44 60 35 83" || c.TemporaryPhoneCode == nil || *c.TemporaryPhoneCode != "320 82 206" {
		t.Fatalf("numéro temporaire = %v / %v", c.TemporaryPhone, c.TemporaryPhoneCode)
	}
	if c.Tel != nil {
		t.Error("le numéro anonymisé ne doit pas devenir le téléphone du client")
	}
}

func TestUberCustomer_sansNumero_pasDeNumeroNiDeCode(t *testing.T) {
	c := uberCustomer(parseOrder(t, `{"eater": {"first_name": "Samantha", "phone_code": "123"}}`), "212")
	if c.TemporaryPhone != nil || c.TemporaryPhoneCode != nil {
		t.Fatalf("un code sans numéro n'a pas de sens : %v / %v", c.TemporaryPhone, c.TemporaryPhoneCode)
	}
}

func TestUberCustomer_valeursTropLonguesSontTronquees(t *testing.T) {
	long := strings.Repeat("é", 120)
	order := parseOrder(t, `{"eater": {"first_name": "`+long+`", "last_name": "`+long+`", "phone": "`+strings.Repeat("1", 60)+`",
	  "delivery": {"location": {"street_address": "`+strings.Repeat("rue ", 100)+`"}}}}`)

	c := uberCustomer(order, "212")

	for field, got := range map[string]int{
		"nom":            len([]rune(*c.Name)),
		"prénom":         len([]rune(*c.FirstName)),
		"nom de famille": len([]rune(*c.LastName)),
		"téléphone":      len([]rune(*c.TemporaryPhone)),
		"adresse":        len([]rune(*c.Address)),
	} {
		limit := map[string]int{"nom": 50, "prénom": 50, "nom de famille": 50, "téléphone": 20, "adresse": 255}[field]
		if got > limit || got == 0 {
			t.Errorf("%s : %d caractères, want entre 1 et %d", field, got, limit)
		}
	}
}

func TestUberCustomer_adresse(t *testing.T) {
	cases := []struct {
		name string
		loc  string
		want string // "" = pas d'adresse
	}{
		{"adresse complète", `{"street_address": "12 rue de la Gare, 57000 Metz", "unit_number": "Apt 4B", "business_name": "Résidence Les Lilas"}`,
			"12 rue de la Gare, 57000 Metz, Apt 4B, Résidence Les Lilas"},
		{"google place : première ligne saisie", `{"type": "GOOGLE_PLACE", "google_place_id": "abc", "title": "5 place Saint-Louis", "unit_number": "3"}`,
			"5 place Saint-Louis, 3"},
		{"google place sans texte", `{"type": "GOOGLE_PLACE", "google_place_id": "abc"}`, ""},
		{"commande livrée par Uber : pas d'adresse", `{}`, ""},
	}
	for _, c := range cases {
		order := parseOrder(t, `{"eater": {"first_name": "S", "delivery": {"location": `+c.loc+`}}}`)
		got := uberCustomer(order, "212")
		switch {
		case c.want == "" && got.Address != nil:
			t.Errorf("%s : adresse %q, want aucune", c.name, *got.Address)
		case c.want != "" && (got.Address == nil || *got.Address != c.want):
			t.Errorf("%s : adresse %v, want %q", c.name, got.Address, c.want)
		}
	}
}

func TestUberCustomer_googlePlaceEtCoordonneesConserves(t *testing.T) {
	order := parseOrder(t, `{"eater": {"first_name": "S", "delivery": {"location": {"type": "STREET_ADDRESS", "street_address": "1 rue X", "latitude": 49.1, "longitude": 6.17}}}}`)
	c := uberCustomer(order, "212")
	if c.Lat == nil || *c.Lat != 49.1 || c.Lng == nil || *c.Lng != 6.17 {
		t.Fatalf("coordonnées = %v / %v", c.Lat, c.Lng)
	}
}
