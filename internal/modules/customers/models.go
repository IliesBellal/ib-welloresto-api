package customers

// --- GetCustomerLoyalty response ---
type LoyaltyProgress struct {
	LoyaltyProgramID string `json:"loyalty_program_id"`
	CurrentValue     int    `json:"current_value"`
	LastUpdate       string `json:"last_update"`
	Type             string `json:"type"`
	TargetValue      int    `json:"target_value"`
	Name             string `json:"name"`
	Description      string `json:"description"`
}

type LoyaltyReward struct {
	RewardID         string `json:"reward_id"`
	LoyaltyProgramID string `json:"loyalty_program_id"`
	CreationDate     string `json:"creation_date"`
	RewardType       string `json:"reward_type"`
	RewardValue      int    `json:"reward_value"`
	IsUsed           bool   `json:"is_used"`
}

type CustomerLoyalty struct {
	LoyaltyProgress  []LoyaltyProgress `json:"loyalty_progress"`
	AvailableRewards []LoyaltyReward   `json:"available_rewards"`
}

// --- update progress ---
type LoyaltyProgressUpdateRequest struct {
	CustomerID       string `json:"customer_id"`
	LoyaltyProgramID string `json:"loyalty_program_id"`
	CurrentValue     int    `json:"current_value"`
}

// --- update reward ---
type LoyaltyRewardUpdateRequest struct {
	RewardID string `json:"reward_id"`
	IsUsed   int    `json:"is_used"`
}

type CustomerSearchRequest struct {
	Name    string `json:"name"`
	Tel     string `json:"tel"`
	Address string `json:"address"`
	Code    string `json:"code"`
}

type CustomerSearchResult struct {
	CustomerID         string  `json:"customer_id"`
	CustomerName       string  `json:"customer_name"`
	CustomerTel        string  `json:"customer_tel"`
	CustomerAddress    string  `json:"customer_address"`
	CustomerEmail      string  `json:"customer_email"`
	CustomerNbOrders   int     `json:"customer_nb_orders"`
	CustomerTotalSpent float64 `json:"customer_total_spent"`
	CreationDate       string  `json:"creation_date"`
	CustomerCode       string  `json:"customer_code"`
	MatchScore         int     `json:"match_score"`
}

type Customer struct {
	CustomerID                         *string  `json:"customer_id"`
	CustomerCode                       *string  `json:"customer_code"`
	CustomerName                       *string  `json:"customer_name"`
	CustomerTel                        *string  `json:"customer_tel"`
	CustomerEmail                      *string  `json:"customer_email"`
	CustomerTemporaryPhone             *string  `json:"customer_temporary_phone"`
	CustomerTemporaryPhoneCode         *string  `json:"customer_temporary_phone_code"`
	CustomerNbOrders                   *int     `json:"customer_nb_orders"`
	CustomerNbBookings                 *int     `json:"customer_nb_bookings"`
	CustomerTotalSpent                 *int     `json:"customer_total_spent"`
	MatchScore                         *int     `json:"match_score"`
	CustomerAdditionalInfo             *string  `json:"customer_additional_info"`
	CustomerZoneCode                   *string  `json:"customer_zone_code"`
	CustomerAddress                    *string  `json:"customer_address"`
	CustomerLat                        *float64 `json:"customer_lat"`
	CustomerLng                        *float64 `json:"customer_lng"`
	CustomerFloorNumber                *string  `json:"customer_floor_number"`
	CustomerDoorNumber                 *string  `json:"customer_door_number"`
	CustomerAdditionalAddress          *string  `json:"customer_additional_address"`
	MerchantID                         string   `json:"merchant_id"`
	CustomerBusinessName               *string  `json:"customer_business_name"`
	CustomerBirthdate                  *string  `json:"customer_birthdate"`
	CustomerTemporaryAddress           *string  `json:"customer_temporary_address"`
	CustomerTemporaryLat               *string  `json:"customer_temporary_lat"`
	CustomerTemporaryLng               *string  `json:"customer_temporary_lng"`
	CustomerTemporaryDoorNumber        *string  `json:"customer_temporary_door_number"`
	CustomerTemporaryFloorNumber       *string  `json:"customer_temporary_floor_number"`
	CustomerTemporaryAdditionalAddress *string  `json:"customer_temporary_additional_address"`
	CreationDate                       *string  `json:"creation_date"`
}
