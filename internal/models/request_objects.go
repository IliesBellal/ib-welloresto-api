package models

type PaymentRequest struct {
	DeviceID        string        `json:"device_id"`
	OrderID         string        `json:"order_id"`
	MOP             string        `json:"mop"`
	Amount          int           `json:"amount"`
	Items           []PaymentItem `json:"items"`
	DiscountComment string        `json:"discount_comment"`
	StatusCheck     string        `json:"status_check"`
	Code            string        `json:"tr_code"`
}

type OpenCashRegisterRequest struct {
	CashRegister struct {
		CashRegisterID *string `json:"cash_register_id"` // peut être null
		CashDeskID     string  `json:"cash_desk_id"`
		CashFund       float64 `json:"cash_fund"`
		UserID         string  `json:"user_id"`
	} `json:"cash_register"`
	DeviceID string `json:"device_id"`
}

type CloseCashRegisterRequest struct {
	CashFund float64 `json:"cash_fund"`
	UserID   string  `json:"user_id"`
	DeviceID string  `json:"device_id"`
}

type CashRegisterSummaryResponse struct {
	Status       string               `json:"status"`
	CashRegister *CashRegisterSummary `json:"cash_register"`
}

type CashRegisterSummary struct {
	CashRegisterID string         `json:"cash_register_id"`
	StartDate      string         `json:"start_date"`
	EndDate        *string        `json:"end_date"`
	CashDesk       CashDeskInfo   `json:"cash_desk"`
	CashFund       float64        `json:"cash_fund"`
	Currency       string         `json:"currency"`
	Closed         int            `json:"closed"`
	ClosureComment *string        `json:"closure_comment"`
	OpenedBy       UserBaseInfo   `json:"opened_by"`
	ClosedBy       UserBaseInfo   `json:"closed_by"`
	Orders         []CROrder      `json:"orders"`
	Payments       []CRPayment    `json:"payments"`
	Items          []CRItem       `json:"items"`
	CustomItems    []CRCustomItem `json:"custom_items"`
}

type CashDeskInfo struct {
	CashDeskID   string `json:"cash_desk_id"`
	CashDeskName string `json:"cash_desk_name"`
}

type UserBaseInfo struct {
	UserID    *string `json:"user_id"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
}

type CRItem struct {
	ItemID   string  `json:"item_id"`
	MOP      string  `json:"mop"`
	Label    *string `json:"label"`
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

type CRCustomItem struct {
	ItemID   string  `json:"item_id"`
	Label    string  `json:"label"`
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

type CROrder struct {
	OrderID      string       `json:"order_id"`
	OrderNum     string       `json:"order_num"`
	OrderedBy    UserBaseInfo `json:"ordered_by"`
	CreationDate string       `json:"creation_date"`
	Currency     string       `json:"currency"`
	IsDelivery   int          `json:"isDelivery"`
	Status       string       `json:"status"`
	Price        float64      `json:"price"`
}

type CRPayment struct {
	OrderID     string       `json:"order_id"`
	OrderNum    string       `json:"order_num"`
	PaymentID   string       `json:"payment_id"`
	CollectedBy UserBaseInfo `json:"collected_by"`
	PaymentDate string       `json:"payment_date"`
	Amount      float64      `json:"amount"`
	Enabled     int          `json:"enabled"`
	Currency    string       `json:"currency"`
	MOP         string       `json:"mop"`
}

type CashRegisterReport struct {
	Status         int                       `json:"status"`
	CashReportID   string                    `json:"cash_report_id"`
	PeriodFrom     string                    `json:"period_from"`
	PeriodTo       string                    `json:"period_to"`
	CashFund       float64                   `json:"cash_fund"`
	HT             int                       `json:"HT"`
	TTC            int                       `json:"TTC"`
	TVA            int                       `json:"TVA"`
	CashReport     []CashReportDeliveryGroup `json:"cash_report"`
	MOP            []MOPLine                 `json:"mop"`
	CashReportType string                    `json:"cash_report_type"`
}

type CashReportDeliveryGroup struct {
	DeliveryTypeID    string            `json:"delivery_type_id"`
	DeliveryTypeLabel string            `json:"delivery_type_label"`
	TVACategories     []TVACategoryLine `json:"tva_categories"`
}

type TVACategoryLine struct {
	TVATitle string `json:"tva_title"`
	HT       int    `json:"HT"`
	TTC      int    `json:"TTC"`
	TVA      int    `json:"TVA"`
}

type MOPLine struct {
	MOP    string  `json:"mop"`
	Amount float64 `json:"amount"`
	Label  string  `json:"label,omitempty"`
}

type AddCustomItemRequest struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type EncloseCashRegisterRequest struct {
	UserID  string `json:"user_id"`
	Comment string `json:"comment"`
}

type BookingObjectRequest struct {
	MerchantID      string   `json:"merchant_id"`
	BookingID       *string  `json:"booking_id"`
	BookingNumber   *string  `json:"booking_number"`
	BookingDateFrom *string  `json:"booking_date_from"`
	BookingDateTo   *string  `json:"booking_date_to"`
	CreatedBy       string   `json:"created_by"`
	Customer        Customer `json:"customer"`
	Booking         Booking  `json:"booking"`
}

type OrderHistoryRequest struct {
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}
