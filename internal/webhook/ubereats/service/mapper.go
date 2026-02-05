package service

import (
	"time"

	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func MapUberOrderToRequest(
	order *ueModels.UberOrder,
	merchantID string,
) *ordersModels.RequestObject {

	total := 0
	for _, item := range order.Cart.Items {
		total += item.Price.UnitPrice.Amount * item.Quantity
	}
	if order.Payment.Charges.SubTotalPromoApplied != nil {
		total = order.Payment.Charges.SubTotalPromoApplied.Amount
	}

	orderType := "DELIVERY"
	if order.Type == "PICK_UP" {
		orderType = "TAKE_AWAY"
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

	return &ordersModels.RequestObject{
		MerchantID: merchantID,
		Order: ordersModels.OrderRequest{
			OrderID:     &order.ID,
			OrderNum:    &order.DisplayID,
			TTC:         total,
			Products:    products,
			OrderType:   orderType,
			BrandStatus: order.CurrentState,
			Customer: &ordersModels.CustomerRequest{
				BrandCustomerID:    &order.Eaters[0].ID,
				FirstName:          &order.Eater.FirstName,
				LastName:           &order.Eater.LastName,
				TemporaryPhone:     &order.Eater.Phone,
				TemporaryPhoneCode: &order.Eater.PhoneCode,
				Lat:                order.Eater.Delivery.Location.Lat,
				Lng:                order.Eater.Delivery.Location.Lng,
				GooglePlaceID:      order.Eater.Delivery.Location.GooglePlaceID,
				MerchantID:         &merchantID,
			},
			MerchantApproval: "PENDING_APPROVAL",
			Payments: []ordersModels.PaymentPayload{
				{Amount: total, MOP: "UBER_EATS"},
			},
		},
	}
}
