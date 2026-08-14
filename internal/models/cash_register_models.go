package models

type CashRegister struct {
	CashRegisterID string   `json:"cash_register_id"`
	StartDate      *int     `json:"start_date"`
	EndDate        *int     `json:"end_date"`
	Closed         bool     `json:"closed"`   // colonne closed
	Enclosed       bool     `json:"enclosed"` // colonne enclosed
	CashDesk       CashDesk `json:"cash_desk"`
}

type CashDesk struct {
	CashDeskID   string `json:"cash_desk_id"`
	CashDeskName string `json:"cash_desk_name"`
}

type CashRegisterHistoryResponse struct {
	Status        string              `json:"status"`
	Metadata      *PaginationMetadata `json:"metadata,omitempty"`
	CashRegisters []CashRegister      `json:"cash_registers"`
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
	DeliveryType string  `json:"delivery_type"`
	Label        string  `json:"label"`
	TVATitle     string  `json:"tva_title"`
	Rate         float64 `json:"tva_rate"`
	HT           int     `json:"HT"`
	TTC          int     `json:"TTC"`
	TVA          int     `json:"TVA"`
}
