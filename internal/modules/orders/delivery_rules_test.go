package orders

import (
	"testing"

	"welloresto-api/internal/models"
)

// Paramètres du marchand 226 (commande 40494) : minimum 15 €, livraison
// offerte dès 25 €, frais 3 €.
func TestApplyDeliveryRules_MinimumCart(t *testing.T) {
	merchant := &models.MerchantPricingInfo{
		DeliveryFees:                300,
		DeliveryFeesLimit:           2500,
		MinimumCartForDeliveryOrder: 1500,
	}

	cases := []struct {
		name          string
		orderType     string
		isSNO         bool
		ttc           int
		wantOrderable bool
		wantFees      int
	}{
		{"SNO livraison sous le minimum (40494)", "DELIVERY", true, 1200, false, 300},
		{"SNO livraison au minimum exact", "DELIVERY", true, 1500, true, 300},
		{"SNO livraison offerte au-delà du seuil", "DELIVERY", true, 2500, true, 0},
		{"minimum ignoré hors SNO", "DELIVERY", false, 1200, true, 300},
		{"minimum ignoré à emporter", "TAKE_AWAY", true, 1200, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &models.PricingRequest{
				IsSNO: tc.isSNO,
				Order: &models.OrderRequest{OrderType: tc.orderType, TTC: tc.ttc},
			}
			(&OrdersService{}).applyDeliveryRules(req, merchant, 10)

			if req.IsOrderable != tc.wantOrderable {
				t.Fatalf("IsOrderable = %v, want %v", req.IsOrderable, tc.wantOrderable)
			}
			if !tc.wantOrderable && req.NotOrderableReason != "minimum_cart_for_delivery_not_reached" {
				t.Fatalf("NotOrderableReason = %q", req.NotOrderableReason)
			}
			if req.Order.DeliveryFees != tc.wantFees {
				t.Fatalf("DeliveryFees = %d, want %d", req.Order.DeliveryFees, tc.wantFees)
			}
		})
	}
}
