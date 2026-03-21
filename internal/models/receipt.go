package models

import "time"

type Receipt struct {
	ReceiptID        string    `json:"receipt_id" db:"receipt_id"`
	MerchantID       string    `json:"merchant_id" db:"merchant_id"` // Adapté selon ton type (string ou int)
	OrderID          string    `json:"order_id" db:"order_id"`
	ReceiptNumber    string    `json:"receipt_number" db:"receipt_number"`
	TotalTTC         int64     `json:"total_ttc" db:"total_ttc"`
	TotalHT          int64     `json:"total_ht" db:"total_ht"`
	TaxDetails       []byte    `json:"tax_details" db:"tax_details"`             // Stocké en JSON
	ItemsSnapshot    []byte    `json:"items_snapshot" db:"items_snapshot"`       // Stocké en JSON
	PaymentsSnapshot []byte    `json:"payments_snapshot" db:"payments_snapshot"` // Stocké en JSON
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	PrevHash         string    `json:"prev_hash" db:"prev_hash"`
	Hash             string    `json:"hash" db:"hash"`
	Signature        string    `json:"signature" db:"signature"`
}

// Structures pour faciliter la génération du JSON
type SnapshotItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	PriceTTC int64  `json:"price_ttc"`
	TaxRate  int64  `json:"tax_rate"` // Taux en points de base (ex: 1000 pour 10%)
}

type SnapshotPayment struct {
	Amount int64  `json:"amount"`
	MOP    string `json:"mop"`
}
