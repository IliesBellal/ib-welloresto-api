package cash_registers

type CashRegisterHistoryItem struct {
	CashRegisterID string              `json:"cash_register_id"`
	StartDate      string              `json:"start_date"`
	EndDate        string              `json:"end_date"`
	Closed         bool                `json:"closed"`
	CashDesk       CashRegisterDeskRef `json:"cash_desk"`
}

type CashRegisterDeskRef struct {
	CashDeskID   string `json:"cash_desk_id"`
	CashDeskName string `json:"cash_desk_name"`
}

type CashRegisterHistoryResponse struct {
	Status        string                    `json:"status"`
	CashRegisters []CashRegisterHistoryItem `json:"cash_registers"`
}

type CashRegisterDetails struct {
	Status         int                       `json:"status"`
	CashReportID   string                    `json:"cash_report_id"`
	PeriodFrom     string                    `json:"period_from"`
	PeriodTo       string                    `json:"period_to"`
	CashFund       int                       `json:"cash_fund"`
	HT             int                       `json:"HT"`
	TTC            int                       `json:"TTC"`
	TVA            int                       `json:"TVA"`
	CashReport     []CashReportDeliveryGroup `json:"cash_report"`
	MOP            []MOPLine                 `json:"mop"`
	CashReportType string                    `json:"cash_report_type"`
}

type CashReportLine struct {
	DeliveryType string `json:"delivery_type"`
	Label        string `json:"label"`
	TVATitle     string `json:"tva_title"`
	HT           int    `json:"HT"`
	TTC          int    `json:"TTC"`
	TVA          int    `json:"TVA"`
}

type CashReportDeliveryGroup struct {
	DeliveryTypeID    string            `json:"delivery_type_id"`
	DeliveryTypeLabel string            `json:"delivery_type_label"`
	TVACategories     []TVACategoryLine `json:"tva_categories"`
}

type MOPLine struct {
	MOP    string  `json:"mop"`
	Amount float64 `json:"amount"`
	Label  string  `json:"label,omitempty"`
}

type TVACategoryLine struct {
	TVATitle string `json:"tva_title"`
	HT       int    `json:"HT"`
	TTC      int    `json:"TTC"`
	TVA      int    `json:"TVA"`
}
