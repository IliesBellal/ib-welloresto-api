package service

import (
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// Longueurs des colonnes de customer qui reçoivent ces textes. Uber n'en limite pas
// la taille : une valeur trop longue faisait échouer l'insertion du client, donc la
// commande entière.
const (
	customerNameMax  = 50
	customerPhoneMax = 20
	customerAddrMax  = 255
)

// uberCustomer construit le client d'une commande Uber Eats. Les données d'Uber sont
// anonymisées : le numéro est temporaire (avec le code pour l'appeler), le nom se
// limite au prénom et à l'initiale. L'adresse n'existe que pour les commandes livrées
// par l'établissement.
func uberCustomer(order *ueModels.UberOrder, merchantID string) *models.CustomerRequest {
	first := strings.TrimSpace(order.Eater.FirstName)
	last := strings.TrimSpace(order.Eater.LastName)
	name := helpers.TruncateRunes(strings.TrimSpace(first+" "+last), customerNameMax)
	first = helpers.TruncateRunes(first, customerNameMax)
	last = helpers.TruncateRunes(last, customerNameMax)

	c := &models.CustomerRequest{
		CustomerBrand: models.BrandUberEats,
		FirstName:     &first,
		LastName:      &last,
		Name:          &name,
		MerchantID:    &merchantID,
	}

	// Plusieurs convives dans une commande groupée : le premier identifie le client.
	if id := eaterID(order); id != "" {
		c.BrandCustomerID = &id
	}

	if phone := helpers.TruncateRunes(strings.TrimSpace(order.Eater.Phone), customerPhoneMax); phone != "" {
		c.TemporaryPhone = &phone
		if code := helpers.TruncateRunes(strings.TrimSpace(order.Eater.PhoneCode), customerPhoneMax); code != "" {
			c.TemporaryPhoneCode = &code
		}
	}

	loc := order.Eater.Delivery.Location
	c.Lat, c.Lng, c.GooglePlaceID = loc.Lat, loc.Lng, loc.GooglePlaceID
	if addr := helpers.TruncateRunes(deliveryAddress(loc.StreetAddress, loc.Title, loc.UnitNumber, loc.BusinessName), customerAddrMax); addr != "" {
		c.Address = &addr
	}
	return c
}

// eaterID renvoie l'identifiant du premier convive. Une commande sans convive (payload
// anormal) ne doit pas faire échouer l'import : il n'y a alors pas d'identifiant.
func eaterID(order *ueModels.UberOrder) string {
	if len(order.Eaters) > 0 && order.Eaters[0].ID != "" {
		return order.Eaters[0].ID
	}
	return order.Eater.ID
}

// deliveryAddress : l'adresse sur une ligne (street_address, sinon la première ligne
// saisie par le client), suivie de l'appartement et de l'immeuble s'ils existent.
func deliveryAddress(street, title, unit, business string) string {
	first := strings.TrimSpace(street)
	if first == "" {
		first = strings.TrimSpace(title)
	}
	var parts []string
	for _, p := range []string{first, strings.TrimSpace(unit), strings.TrimSpace(business)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}
