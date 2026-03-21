package models

const (
	OperationTypeSale   = "SALE"
	OperationTypeRefund = "REFUND"

	DeliverooMOP = "DELIVEROO"
)

type Payment struct {
	OrderID        string  `json:"order_id"`
	CashRegisterID string  `json:"cash_register_id,omitempty"`
	PaymentID      string  `json:"payment_id"`
	MOP            string  `json:"mop"`
	Amount         int     `json:"amount"`
	PaymentDate    int64   `json:"payment_date"`
	MerchantID     string  `json:"merchant_id"`
	UserID         string  `json:"user_id"`
	Enabled        bool    `json:"enabled"`
	IntentID       *string `json:"intent_id,omitempty"`
	AccountID      *string `json:"account_id,omitempty"`
	OperationType  string  `db:"operation_type"` // 'SALE' ou 'REFUND'
	Comment        *string `json:"comment,omitempty"`
	StatusCheck    *string `json:"status_check,omitempty"`
	Code           *string `json:"code,omitempty"`
}
