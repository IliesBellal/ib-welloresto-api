package service

import (
	"context"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// knownUberFulfillmentTypes are the order.type values Uber Eats is known to
// send for a self-delivery (BYOC) order. Anything else is passed through
// as-is (courier-delivered order, or a future Uber value we haven't mapped
// yet) but logged, so a drift doesn't silently break the delivery_sessions
// pool filter (which matches on FulfillmentTypeRestaurant).
const knownUberFulfillmentTypeRestaurant = "DELIVERY_BY_RESTAURANT"

func MapUberOrderToRequest(
	ctx context.Context,
	order *ueModels.UberOrder,
	merchantID string,
) *ordersModels.RequestObject {
	fulfillmentType := order.Type
	if fulfillmentType == knownUberFulfillmentTypeRestaurant {
		fulfillmentType = models.FulfillmentTypeRestaurant
	} else if fulfillmentType != "" {
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

	customerName := order.Eater.FirstName + " " + order.Eater.LastName
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
			Customer: &ordersModels.CustomerRequest{
				BrandCustomerID:    &order.Eaters[0].ID,
				CustomerBrand:      models.BrandUberEats,
				FirstName:          &order.Eater.FirstName,
				LastName:           &order.Eater.LastName,
				Name:               &customerName,
				TemporaryPhone:     &order.Eater.Phone,
				TemporaryPhoneCode: &order.Eater.PhoneCode,
				Lat:                order.Eater.Delivery.Location.Lat,
				Lng:                order.Eater.Delivery.Location.Lng,
				GooglePlaceID:      order.Eater.Delivery.Location.GooglePlaceID,
				MerchantID:         &merchantID,
			},
			Comment:          order.StoreInstructions,
			MerchantApproval: models.MerchantApprovalPendingApproval,
			Payments: []ordersModels.PaymentPayload{
				{Amount: total, MOP: models.BrandUberEats},
			},
		},
	}
}
