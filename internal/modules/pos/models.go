package pos

type DeletionReason struct {
	DeletionReasonID     string  `json:"deletion_reason_id"`
	DeletionReasonType   *string `json:"deletion_reason_type"`
	DeletionReasonObject string  `json:"deletion_reason_object"`
	DeletionReasonDesc   string  `json:"deletion_reason_desc"`
	Label                string  `json:"label"`
	RequiresComment      bool    `json:"requires_comment"`
}

type DeletionReasonResponse struct {
	Status          string           `json:"status"`
	DeletionReasons []DeletionReason `json:"deletions_reasons"`
}

type POSStatus struct {
	Wello struct {
		IsOpen    int    `json:"is_open"`
		Status    string `json:"status"`
		NextStart string `json:"next_start"`
		NextEnd   string `json:"next_end"`
	} `json:"wello_resto_status"`

	Uber struct {
		EstimatedPrepTime string      `json:"estimated_preparation_time"`
		DelayDuration     string      `json:"busy_mode_delay_duration"`
		DelayUntil        interface{} `json:"busy_mode_delay_until"`
		ClosedUntil       interface{} `json:"closed_until"`
	} `json:"uber_eats_status"`
}

type DeliveryMan struct {
	UserID    string   `json:"user_id"`
	FirstName string   `json:"first_name"`
	LastName  string   `json:"last_name"`
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
	Status    string   `json:"status"`
}

type DeliveryMenResponse struct {
	Users []DeliveryMan `json:"users"`
}

type Rate struct {
	ID    int     `json:"id"`
	Value float64 `json:"value"`
	Label string  `json:"label"`
}

type ConsumptionType struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Rates []Rate `json:"rates"`
}
