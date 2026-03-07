package order_life_cycle

type DeliveredOrderMetadata struct {
	Brand           string
	BrandOrderID    *string
	MerchantID      int
	FulfillmentType string
}

type ProductionStatusProduct struct {
	OrderItemID      string `json:"order_item_id"`
	OrderID          string `json:"order_id"`
	ProductionStatus string `json:"production_status"`
}

type UpdateProductionStatusRequest struct {
	Products []ProductionStatusProduct `json:"products"`
}
