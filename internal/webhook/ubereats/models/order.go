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
}

type UberEater struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	PhoneCode string `json:"phone_code"`

	Delivery struct {
		Location struct {
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
	} `json:"price"`

	SpecialInstructions *string `json:"special_instructions"`

	SelectedModifierGroups []UberModifierGroup `json:"selected_modifier_groups"`

	CustomerRequest *UberCustomerRequest `json:"customer_request"`
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

type UberPayment struct {
	Charges struct {
		SubTotalPromoApplied *struct {
			Amount int `json:"amount"`
		} `json:"sub_total_promo_applied"`
	} `json:"charges"`
}
