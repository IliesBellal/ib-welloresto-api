package customers

import "welloresto-api/internal/modules/customers/importer"

// maxCustomerImportFileSize borne le fichier accepté par la preview d'import
// de clients. Relevé à 10 Mo (contre 5 Mo côté produit) : l'export Zelty
// clients réel avoisine les 18 500 lignes, sensiblement plus volumineux que
// les exports menu visés par la limite produit — décision §15 du plan.
const maxCustomerImportFileSize = 10 << 20

// Champs du formulaire multipart.
const (
	customerImportFormProviderField = "provider"
	customerImportFormFileField     = "file"
)

// ImportPreviewJSONRequest est le corps accepté en application/json : la
// porte de la saisie manuelle, qui n'a pas de fichier à parser.
type ImportPreviewJSONRequest struct {
	Customers []ImportPreviewJSONCustomer `json:"customers"`
}

// ImportPreviewJSONCustomer est une ligne de saisie manuelle, miroir JSON de
// importer.ManualCustomerInput.
type ImportPreviewJSONCustomer struct {
	Name              string `json:"name"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	Address           string `json:"address"`
	FloorNumber       string `json:"floor_number"`
	DoorNumber        string `json:"door_number"`
	AdditionalAddress string `json:"additional_address"`
	BusinessName      string `json:"business_name"`
	Birthdate         string `json:"birthdate"`
	AdditionalInfo    string `json:"additional_info"`
	DeliveryNotes     string `json:"delivery_notes"`

	AdvertisingConsent *bool `json:"advertising_consent"`
}

// toManualCustomerInputs traduit la requête vers l'entrée du constructeur
// canonique.
func (r *ImportPreviewJSONRequest) toManualCustomerInputs() []importer.ManualCustomerInput {
	inputs := make([]importer.ManualCustomerInput, 0, len(r.Customers))
	for _, c := range r.Customers {
		inputs = append(inputs, importer.ManualCustomerInput{
			Name:               c.Name,
			FirstName:          c.FirstName,
			LastName:           c.LastName,
			Email:              c.Email,
			Phone:              c.Phone,
			Address:            c.Address,
			FloorNumber:        c.FloorNumber,
			DoorNumber:         c.DoorNumber,
			AdditionalAddress:  c.AdditionalAddress,
			BusinessName:       c.BusinessName,
			Birthdate:          c.Birthdate,
			AdditionalInfo:     c.AdditionalInfo,
			DeliveryNotes:      c.DeliveryNotes,
			AdvertisingConsent: c.AdvertisingConsent,
		})
	}
	return inputs
}
