package models

import "time"

type Receipt struct {
	ReceiptID        string    `json:"receipt_id" db:"receipt_id"`
	MerchantID       string    `json:"merchant_id" db:"merchant_id"` // Adapté selon ton type (string ou int)
	OrderID          string    `json:"order_id" db:"order_id"`
	ReceiptNumber    string    `json:"receipt_number" db:"receipt_number"`
	TotalTTC         int       `json:"total_ttc" db:"total_ttc"`
	TotalHT          int       `json:"total_ht" db:"total_ht"`
	TaxDetails       []byte    `json:"tax_details" db:"tax_details"`             // Stocké en JSON
	ItemsSnapshot    []byte    `json:"items_snapshot" db:"items_snapshot"`       // Stocké en JSON
	PaymentsSnapshot []byte    `json:"payments_snapshot" db:"payments_snapshot"` // Stocké en JSON
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	PrevHash         string    `json:"prev_hash" db:"prev_hash"`
	Hash             string    `json:"hash" db:"hash"`
	Signature        string    `json:"signature" db:"signature"`
	HashVersion      int       `json:"-" db:"hash_version"` // 0 à l'insertion = 1 (formule historique)
}

// SnapshotItem est une ligne figée d'un ticket (receipts.items_snapshot).
//
// Les cinq premiers champs existent depuis l'origine : prix TTC et TVA
// unitaires. Les tickets les plus anciens portent le montant de TVA dans
// TaxRate et 0 dans TaxAmount.
//
// Ticket complet (lot D conformité caisse, fiscal.BuildReceiptItems) : une
// ligne par article, option payante, supplément, frais de livraison et remise
// de caisse, avec Kind et les totaux de la ligne. La somme des TotalTTC vaut
// la TVA ventilée du ticket (tax_details), la somme des TotalHT son HT.
type SnapshotItem struct {
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	PriceTTC  int64  `json:"price_ttc"`
	TaxRate   int64  `json:"tax_rate"` // Taux en points de base (ex: 1000 pour 10%)
	TaxAmount int64  `json:"tax_amount"`

	Kind     string `json:"kind,omitempty"` // article, option, supplement, livraison, remise
	TotalTTC int64  `json:"total_ttc,omitempty"`
	TotalHT  int64  `json:"total_ht,omitempty"`
	TotalTVA int64  `json:"total_tva,omitempty"`
	Parent   *int   `json:"parent,omitempty"` // rang (base 0) de l'article d'une option ou d'un supplément
}

// Natures d'une ligne de ticket complet.
const (
	SnapshotKindArticle    = "article"
	SnapshotKindOption     = "option"
	SnapshotKindSupplement = "supplement"
	SnapshotKindDelivery   = "livraison"
	SnapshotKindDiscount   = "remise"
)

type SnapshotPayment struct {
	Amount int    `json:"amount"`
	MOP    string `json:"mop"`
}
