package service

import (
	"context"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// Uber Eats order.type values we know about. DELIVERY_BY_RESTAURANT is a
// self-delivery (BYOC) order and is mapped to FulfillmentTypeRestaurant;
// DELIVERY_BY_UBER is a courier-delivered order and is passed through as-is
// (it never enters the delivery_sessions pool, which matches on
// FulfillmentTypeRestaurant). Any other value is passed through too but
// logged, so a drift doesn't silently break that pool filter.
const (
	knownUberFulfillmentTypeRestaurant = "DELIVERY_BY_RESTAURANT"
	knownUberFulfillmentTypeUber       = "DELIVERY_BY_UBER"
)

func MapUberOrderToRequest(
	ctx context.Context,
	order *ueModels.UberOrder,
	merchantID string,
) *ordersModels.RequestObject {
	fulfillmentType := order.Type
	if fulfillmentType == knownUberFulfillmentTypeRestaurant {
		fulfillmentType = models.FulfillmentTypeRestaurant
	} else if fulfillmentType != "" && fulfillmentType != knownUberFulfillmentTypeUber {
		logger.FromContext(ctx).Warn("Uber Eats order.type not recognized as a known fulfillment type: " + fulfillmentType)
	}

	total := 0
	for _, item := range order.Cart.Items {
		total += item.Price.UnitPrice.Amount * item.Quantity
	}
	if order.Payment.Charges.SubTotalPromoApplied != nil {
		total = order.Payment.Charges.SubTotalPromoApplied.Amount
	}

	orderType := models.OrderTypeDelivery
	if order.Type == "PICK_UP" {
		orderType = models.OrderTypeTakeAway
	}

	var products []ordersModels.OrderProductPayload
	for _, item := range order.Cart.Items {
		products = append(products, ordersModels.OrderProductPayload{
			ProductID:   item.ID, // ⚠️ mapping réel viendra plus tard
			ProductName: item.Title,
			Quantity:    item.Quantity,
			Price:       item.Price.UnitPrice.Amount,
			OrderedDate: time.Now().Format(time.RFC3339),
		})
	}

	cashRegisterID := models.BrandUberEats

	return &ordersModels.RequestObject{
		MerchantID: merchantID,
		Order: ordersModels.OrderRequest{
			BrandOrderID:    &order.ID,
			BrandOrderNum:   &order.DisplayID,
			Brand:           models.BrandUberEats,
			TTC:             total,
			Products:        products,
			OrderType:       orderType,
			BrandStatus:     order.CurrentState,
			FulfillmentType: &fulfillmentType,
			CashRegisterId:  &cashRegisterID,
			Customer:         uberCustomer(order, merchantID),
			Comment:          orderComment(order),
			MerchantApproval: models.MerchantApprovalPendingApproval,
			Payments: []ordersModels.PaymentPayload{
				{Amount: total, MOP: models.BrandUberEats},
			},
		},
	}
}
