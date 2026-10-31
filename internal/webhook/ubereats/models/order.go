package models

type UberOrder struct {
	ID                string  `json:"id"`
	DisplayID         string  `json:"display_id"`
	CurrentState      string  `json:"current_state"`
	Type              string  `json:"type"`
	StoreInstructions *string `json:"store_instructions"`

	Eaters []UberEater `json:"eaters"`
	Eater  UberEater   `json:"eater"`

	Cart UberCart `json:"cart"`

	Payment UberPayment `json:"payment"`

	Packaging struct {
		DisposableItems struct {
			// ShouldInclude : le client veut (true) ou ne veut pas (false) les
			// couverts et emballages jetables ; nil = au choix de l'établissement.
			ShouldInclude *bool `json:"should_include"`
		} `json:"disposable_items"`
	} `json:"packaging"`
}

type UberEater struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	PhoneCode string `json:"phone_code"`

	Delivery struct {
		// Notes : consignes de livraison du client, pour le livreur (commandes
		// livrées par l'établissement).
		Notes    string `json:"notes"`
		Location struct {
			// Type : STREET_ADDRESS (adresse et coordonnées) ou GOOGLE_PLACE (seulement
			// google_place_id). Title est la première ligne d'adresse saisie par le
			// client, UnitNumber l'appartement ou la suite, BusinessName l'immeuble ou
			// l'entreprise. Livraison par l'établissement seulement.
			Type          string   `json:"type"`
			Title         string   `json:"title"`
			UnitNumber    string   `json:"unit_number"`
			BusinessName  string   `json:"business_name"`
			StreetAddress string   `json:"street_address"`
			Lat           *float64 `json:"latitude"`
			Lng           *float64 `json:"longitude"`
			GooglePlaceID *string  `json:"google_place_id"`
		} `json:"location"`
	} `json:"delivery"`
}

type UberCart struct {
	Items []UberCartItem `json:"items"`

	SpecialInstructions *string `json:"special_instructions"`
}

type UberCartItem struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Quantity    int     `json:"quantity"`

	Price struct {
		UnitPrice struct {
			Amount int `json:"amount"`
		} `json:"unit_price"`
		TaxInfoCamel *UberTaxInfo `json:"taxInfo"`
	} `json:"price"`

	// Étiquettes fiscales (CAT_ALCOHOL…) : Uber ne les envoie qu'aux
	// développeurs qui en font la demande, et les place tantôt sur l'article,
	// tantôt dans son prix, en snake_case ou en camelCase.
	TaxInfo      *UberTaxInfo `json:"tax_info"`
	TaxInfoCamel *UberTaxInfo `json:"taxInfo"`

	// SpecialInstructions : consignes de préparation du client pour l'article.
	SpecialInstructions *string `json:"special_instructions"`

	// SpecialRequests : demandes particulières (allergies) ; désactivées par
	// défaut pour les établissements connectés, Uber doit les activer.
	SpecialRequests []UberSpecialRequest `json:"special_requests"`

	SelectedModifierGroups []UberModifierGroup `json:"selected_modifier_groups"`

	// CustomerRequest : ancien format, absent de la documentation actuelle.
	CustomerRequest *UberCustomerRequest `json:"customer_request"`
}

type UberSpecialRequest struct {
	Allergy *UberAllergyRequest `json:"allergy"`
}

type UberAllergyRequest struct {
	AllergensToExclude  []UberAllergen `json:"allergens_to_exclude"`
	AllergyInstructions string         `json:"allergy_instructions"`
}

// UberAllergen : Type vaut DAIRY, EGGS, FISH, SHELLFISH, TREENUTS, PEANUTS,
// GLUTEN, SOY ou OTHER ; FreeformText n'est renseigné que pour OTHER.
type UberAllergen struct {
	Type         string `json:"type"`
	FreeformText string `json:"freeform_text"`
}

// UberTaxInfo : étiquettes qui définissent un article pour le calcul de la TVA.
type UberTaxInfo struct {
	Labels []string `json:"labels"`
}

var uberAlcoholLabels = map[string]bool{
	"CAT_ALCOHOL": true, "CAT_BEER": true, "CAT_WINE": true,
	"CAT_LIQUOR": true, "CAT_SPARKLING_WINE": true,
}

// TaxLabels renvoie les étiquettes fiscales de l'article, où qu'Uber les ait mises.
func (i UberCartItem) TaxLabels() []string {
	var labels []string
	for _, t := range []*UberTaxInfo{i.TaxInfo, i.TaxInfoCamel, i.Price.TaxInfoCamel} {
		if t != nil {
			labels = append(labels, t.Labels...)
		}
	}
	return labels
}

// IsAlcoholByLabels dit si Uber classe l'article parmi les boissons alcoolisées.
// known vaut false quand Uber n'envoie aucune étiquette : le nom doit alors
// servir de repère.
func (i UberCartItem) IsAlcoholByLabels() (alcohol, known bool) {
	labels := i.TaxLabels()
	if len(labels) == 0 {
		return false, false
	}
	for _, l := range labels {
		if uberAlcoholLabels[l] {
			return true, true
		}
	}
	return false, true
}

type UberCustomerRequest struct {
	Allergy *UberAllergy `json:"allergy"`

	SpecialInstructions *string `json:"special_instructions"`
}

type UberAllergy struct {
	Allergens    []string `json:"allergens"`
	Instructions *string  `json:"instructions"`
}

type UberModifierGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	SelectedItems []UberModifierOption `json:"selected_items"`
}

type UberModifierOption struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Quantity int    `json:"quantity"`

	Price struct {
		UnitPrice struct {
			Amount int `json:"amount"`
		} `json:"unit_price"`
	} `json:"price"`
}

type UberMoney struct {
	Amount int `json:"amount"`
}

type UberPayment struct {
	Charges struct {
		// SubTotal : total des lignes avant promotion.
		SubTotal *UberMoney `json:"sub_total"`
		// SubTotalPromoApplied : total des lignes une fois déduites les
		// promotions financées par l'établissement ; nil sans promotion.
		SubTotalPromoApplied *UberMoney `json:"sub_total_promo_applied"`
	} `json:"charges"`

	Promotions *struct {
		Promotions []UberPromotion `json:"promotions"`
	} `json:"promotions"`
}

// UberPromotion : promotion appliquée à la commande (BOGO, FLATOFF, PERCENTOFF…).
type UberPromotion struct {
	PromoType          string             `json:"promo_type"`
	DiscountItems      []UberDiscountItem `json:"discount_items"`
	PromoDiscountValue int                `json:"promo_discount_value"`
	FundingSplits      []UberFundingSplit `json:"promo_funding_splits"`
}

// UberDiscountItem : articles remisés ; external_id est l'id de l'article du
// panier (cart.items[].id).
type UberDiscountItem struct {
	ExternalID         string `json:"external_id"`
	DiscountedQuantity int    `json:"discounted_quantity"`
}

type UberFundingSplit struct {
	FundingSource string    `json:"funding_source"` // ORGANIZATION (l'établissement) ou UBER
	AmountPaid    UberMoney `json:"amount_paid"`
}

// MerchantFunded dit si l'établissement finance tout ou partie de la promotion.
// Sans détail de financement, on suppose que oui.
func (p UberPromotion) MerchantFunded() bool {
	if len(p.FundingSplits) == 0 {
		return true
	}
	for _, s := range p.FundingSplits {
		if s.FundingSource == "ORGANIZATION" && s.AmountPaid.Amount != 0 {
			return true
		}
	}
	return false
}
