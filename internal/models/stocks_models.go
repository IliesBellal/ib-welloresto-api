package models

type ComponentBarcodeInfo struct {
	ComponentID   string  `json:"component_id"`
	ComponentName string  `json:"component_name"`
	UOM           string  `json:"uom"`
	UOMDesc       string  `json:"uom_desc"`
	Price         float64 `json:"price"`
	Quantity      float64 `json:"quantity"`
}

type AvailableUOM struct {
	UOMID   string `json:"uom_id"`
	UOMDesc string `json:"uom_desc"`
}

type BarcodeInfoResponse struct {
	Status       string                `json:"status"`
	Component    *ComponentBarcodeInfo `json:"component,omitempty"`
	AvailableUOM []AvailableUOM        `json:"available_uom,omitempty"`
	Code         string                `json:"code,omitempty"`
}

// AddStockBarcodeRequest représente le payload envoyé lors d'un scan / ajout de stock via barcode.
type AddStockBarcodeRequest struct {
	Barcode string       `json:"barcode"` // code scanné
	Specs   BarcodeSpecs `json:"specs"`
}

// BarcodeSpecs contient les infos du scan (issues du payload d'origine)
type BarcodeSpecs struct {
	ComponentID string  `json:"component_id"`  // id du composant
	BCPrice     float64 `json:"bc_price"`      // prix par unité barcode (float)
	BCQuantity  float64 `json:"bc_quantity"`   // quantité contenue dans le barcode (float)
	BCUOM       string  `json:"bc_uom"`        // unité du barcode (id)
	CQuantity   float64 `json:"c_quantity"`    // multiplication (nombre d'unités)
	DLC         *string `json:"dlc,omitempty"` // date de péremption (format 'YYYY-MM-DD' ou vide) - optional
}

type StockLossRequest struct {
	Type     string  `json:"type"`      // COMPONENT or PRODUCT
	ObjectID string  `json:"object_id"` // component_id or product_id
	Qty      float64 `json:"qty"`
	UOM      string  `json:"uom"`
	Comment  string  `json:"comment"`
}

type CreateBarcodePayload struct {
	Code        string `json:"code"`
	ComponentID string `json:"component_id"`
}

type StockCategory struct {
	CategoryID   string        `json:"category_id"`
	CategoryName string        `json:"category_name"`
	Objects      []StockObject `json:"objects"`
}

type StockObject struct {
	ObjectID   string     `json:"object_id"`
	ObjectName string     `json:"object_name"`
	UOM        []UOMEntry `json:"object_UOM"`
}

type UOMEntry struct {
	UOMID   string `json:"UOM_id"`
	UOMDesc string `json:"UOM_desc"`
}
