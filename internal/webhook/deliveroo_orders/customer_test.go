package deliveroo_orders

import (
	"strings"
	"testing"

	"welloresto-api/internal/models"
)

func restaurantOrder(d *DeliverooDelivery) DeliverooOrder {
	return DeliverooOrder{FulfillmentType: models.FulfillmentTypeRestaurant, Delivery: d}
}

func TestDeliverooCustomer_livraisonParLEtablissement_adresseComplete(t *testing.T) {
	ord := restaurantOrder(&DeliverooDelivery{
		CustomerName: "Samantha K", ContactNumber: "+33644603583", ContactAccessCode: "320822",
		Line1: "12 rue de la Gare", Line2: "Bât. B, 3e étage", Postcode: "57000", City: "Metz",
		Location: DeliverooLocation{Latitude: 49.1, Longitude: 6.17},
	})

	c := deliverooCustomer("212", ord)

	if *c.Address != "12 rue de la Gare, Bât. B, 3e étage, 57000, Metz" {
		t.Errorf("adresse = %q (Line2 doit figurer)", *c.Address)
	}
	if *c.Name != "Samantha K" || *c.Tel != "+33644603583" || *c.Lat != 49.1 || *c.Lng != 6.17 {
		t.Errorf("client = %+v", c)
	}
	if c.TemporaryPhone == nil || *c.TemporaryPhone != "+33644603583" || c.TemporaryPhoneCode == nil || *c.TemporaryPhoneCode != "320822" {
		t.Errorf("le numéro intermédiaire doit être enregistré avec son code d'accès : %v / %v", c.TemporaryPhone, c.TemporaryPhoneCode)
	}
}

// Sans Line2 l'adresse garde exactement le format d'avant : « Line1, Postcode, City ».
func TestDeliverooCustomer_adresseSansLine2_formatInchange(t *testing.T) {
	c := deliverooCustomer("212", restaurantOrder(&DeliverooDelivery{Line1: "12 rue de la Gare", Postcode: "57000", City: "Metz"}))
	if *c.Address != "12 rue de la Gare, 57000, Metz" {
		t.Errorf("adresse = %q", *c.Address)
	}
}

func TestDeliverooCustomer_aEmporter_pasDAdresse(t *testing.T) {
	ord := DeliverooOrder{FulfillmentType: "CUSTOMER", Customer: &DeliverooCustomer{FirstName: "Samantha", ContactNumber: "+33644603583", ContactAccessCode: "1234"}}

	c := deliverooCustomer("212", ord)

	if *c.Address != "" || *c.Name != "Samantha" || *c.Tel != "+33644603583" {
		t.Errorf("client = %+v", c)
	}
	if c.TemporaryPhoneCode == nil || *c.TemporaryPhoneCode != "1234" {
		t.Errorf("code d'accès = %v", c.TemporaryPhoneCode)
	}
}

func TestDeliverooCustomer_sansCodeOuSansNumero_pasDeNumeroTemporaire(t *testing.T) {
	c := deliverooCustomer("212", restaurantOrder(&DeliverooDelivery{CustomerName: "S", ContactNumber: "+33644603583"}))
	if c.TemporaryPhone != nil || c.TemporaryPhoneCode != nil {
		t.Errorf("sans code d'accès rien à enregistrer : %v / %v", c.TemporaryPhone, c.TemporaryPhoneCode)
	}
	c = deliverooCustomer("212", restaurantOrder(&DeliverooDelivery{CustomerName: "S", ContactAccessCode: "1234"}))
	if c.TemporaryPhone != nil {
		t.Errorf("un code sans numéro n'a pas de sens : %v", c.TemporaryPhone)
	}
}

func TestDeliverooCustomer_valeursTropLonguesSontTronquees(t *testing.T) {
	ord := restaurantOrder(&DeliverooDelivery{
		CustomerName: strings.Repeat("é", 120), ContactNumber: strings.Repeat("1", 60),
		Line1: strings.Repeat("rue ", 100),
	})

	c := deliverooCustomer("212", ord)

	if n := len([]rune(*c.Name)); n != 50 {
		t.Errorf("nom = %d caractères, want 50", n)
	}
	if n := len([]rune(*c.Tel)); n != 20 {
		t.Errorf("téléphone = %d caractères, want 20", n)
	}
	if n := len([]rune(*c.Address)); n == 0 || n > 255 {
		t.Errorf("adresse = %d caractères, want entre 1 et 255", n)
	}
}

func TestDeliverooCustomer_commandeSansClient_neFaitPasPlanter(t *testing.T) {
	c := deliverooCustomer("212", DeliverooOrder{})
	if c == nil || *c.Name != "" {
		t.Fatalf("client = %+v", c)
	}
}
