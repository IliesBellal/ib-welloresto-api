package order_life_cycle

type DeliveredOrderMetadata struct {
	Brand           string
	BrandOrderID    *string
	MerchantID      int
	FulfillmentType string
}
