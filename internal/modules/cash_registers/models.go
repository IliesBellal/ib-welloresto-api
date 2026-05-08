package cash_registers

import "welloresto-api/internal/models"

type CashRegisterHistoryItem struct {
	CashRegisterID   string              `json:"cash_register_id"`
	StartDate        string              `json:"start_date"`
	EndDate          string              `json:"end_date"`
	CashFund         int                 `json:"cash_fund"`
	FinalCashFund    int                 `json:"final_cash_fund"`
	ClosureComment   *string             `json:"closure_comment,omitempty"`
	ClosedByName     *string             `json:"closed_by_name,omitempty"`
	Closed           bool                `json:"closed"`
	Enclosed         bool                `json:"enclosed"`
	TotalRevenu      int                 `json:"total_revenu"`
	TransactionCount int                 `json:"transaction_count"`
	PaymentMethods   []MOPLine           `json:"payment_methods"`
	HashPrefix       *string             `json:"hash_prefix,omitempty"`
	CashDesk         CashRegisterDeskRef `json:"cash_desk"`
}

type CashRegisterDeskRef struct {
	CashDeskID   string `json:"cash_desk_id"`
	CashDeskName string `json:"cash_desk_name"`
}

type CashRegisterHistoryResponse struct {
	Status        string                     `json:"status"`
	Metadata      *models.PaginationMetadata `json:"metadata,omitempty"`
	CashRegisters []CashRegisterHistoryItem  `json:"cash_registers"`
}

type CashRegisterHistoryRequest struct {
	Page     *int    `json:"page"`
	Limit    *int    `json:"limit"`
	DateFrom *string `json:"date_from"`
	DateTo   *string `json:"date_to"`
}

func (r CashRegisterHistoryRequest) NormalizedPagination() (int, int) {
	page := 1
	if r.Page != nil && *r.Page > 0 {
		page = *r.Page
	}

	limit := 50
	if r.Limit != nil && *r.Limit > 0 {
		limit = *r.Limit
	}

	return page, limit
}

type CashRegisterHistoryResult struct {
	CashRegisters []CashRegisterHistoryItem
	Metadata      models.PaginationMetadata
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

type DeviceLinkRequest struct {
	DeviceID   string `json:"device_id" binding:"required"`
	OnBehalfOf string `json:"on_behalf_of"`
}

type DeviceLink struct {
	DeviceID     string `json:"device_id"`
	UserID       string `json:"user_id"`
	OnBehalfOf   string `json:"on_behalf_of"`
	CreationDate string `json:"creation_date"`
}
