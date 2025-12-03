package models

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
