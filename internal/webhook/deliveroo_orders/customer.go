package deliveroo_orders

import (
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// Longueurs des colonnes de customer qui reçoivent ces textes. Deliveroo n'en limite
// pas la taille : une valeur trop longue faisait échouer l'insertion du client, donc
// la commande entière.
const (
	customerNameMax  = 50
	customerPhoneMax = 20
	customerAddrMax  = 255
)

// deliverooCustomer construit le client d'une commande Deliveroo : nom, numéro et — pour
// les commandes que l'établissement livre lui-même — adresse et coordonnées. Les
// commandes à emporter ou livrées par Deliveroo n'ont pas d'adresse.
//
// Le numéro de Deliveroo est un numéro intermédiaire : il faut le code d'accès
// (contact_access_code) pour joindre le client. Le code est enregistré avec le numéro
// (colonnes temporaires), le numéro reste aussi dans customer_tel comme avant.
func deliverooCustomer(merchantID string, ord DeliverooOrder) *models.CustomerRequest {
	var name, phone, address, accessCode string
	var lat, lng float64

	isRestaurantFulfillment := ord.FulfillmentType == models.FulfillmentTypeRestaurant

	if isRestaurantFulfillment && ord.Delivery != nil {
		d := ord.Delivery
		name = d.CustomerName
		phone = d.ContactNumber
		accessCode = d.ContactAccessCode
		address = joinNonEmpty(", ", d.Line1, d.Line2, d.Postcode, d.City)
		lat = d.Location.Latitude
		lng = d.Location.Longitude
	} else if ord.Customer != nil {
		// Takeaway ou Delivery by Deliveroo
		name = ord.Customer.FirstName
		phone = ord.Customer.ContactNumber
		accessCode = ord.Customer.ContactAccessCode
		// Pas d'adresse précise pour le client dans ce cas (géré par Deliveroo)
	}

	name = helpers.TruncateRunes(strings.TrimSpace(name), customerNameMax)
	phone = helpers.TruncateRunes(strings.TrimSpace(phone), customerPhoneMax)
	address = helpers.TruncateRunes(address, customerAddrMax)

	c := &models.CustomerRequest{
		MerchantID:    &merchantID,
		Name:          &name,
		Tel:           &phone,
		Address:       &address,
		Lat:           &lat,
		Lng:           &lng,
		CustomerBrand: models.BrandDeliveroo,
	}
	if code := helpers.TruncateRunes(strings.TrimSpace(accessCode), customerPhoneMax); code != "" && phone != "" {
		c.TemporaryPhone = &phone
		c.TemporaryPhoneCode = &code
	}
	return c
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
