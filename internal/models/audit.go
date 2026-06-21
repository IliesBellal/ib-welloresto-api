package models

import (
	"encoding/json"
	"time"
)

// Définition de quelques constantes pour standardiser tes logs
const (
	ActionOrderUpdate = "ORDER_UPDATE"
	ActionOrderReopen = "ORDER_REOPEN"
	ActionOrderClose  = "ORDER_CLOSE"
	ActionOrderDelete = "ORDER_DELETE"
	ActionOrderRefund = "ORDER_REFUND"

	ActionPaymentAdded   = "PAYMENT_ADDED"
	ActionPaymentDeleted = "PAYMENT_DELETED"

	ActionCustomerInvoiceLink = "CUSTOMER_INVOICE_LINK"

	ResourceOrder    = "orders"
	ResourcePayment  = "payments"
	ResourceCustomer = "customers"
)

type AuditLog struct {
	ID           string          `json:"id" db:"id"`
	UserID       string          `json:"user_id" db:"user_id"`
	MerchantID   string          `json:"merchant_id" db:"merchant_id"`
	Action       string          `json:"action" db:"action"`
	ResourceType string          `json:"resource_type" db:"resource_type"`
	ResourceID   string          `json:"resource_id" db:"resource_id"`
	OldValues    json.RawMessage `json:"old_values" db:"old_values"`
	NewValues    json.RawMessage `json:"new_values" db:"new_values"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}
