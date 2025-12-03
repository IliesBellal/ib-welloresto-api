package models

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
