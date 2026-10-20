package scannorder

import (
	"testing"

	"welloresto-api/internal/models"
)

func TestMinimumCartNotReached(t *testing.T) {
	cases := []struct {
		name    string
		pricing *models.PricingResponse
		want    bool
	}{
		{"minimum atteint", &models.PricingResponse{OrderRequest: &models.PricingRequest{IsOrderable: true}}, false},
		{"minimum non atteint (commande 40494)", &models.PricingResponse{OrderRequest: &models.PricingRequest{
			IsOrderable:        false,
			NotOrderableReason: StatusMinimumCartNotReached,
		}}, true},
		// Hors zone : PricingResponse.IsOrderable est false mais ce n'est pas
		// le minimum — la zone est contrôlée ailleurs ("address_too_far").
		{"hors zone seul ne compte pas", &models.PricingResponse{
			IsOrderable:  false,
			OrderRequest: &models.PricingRequest{IsOrderable: true},
		}, false},
		{"pricing nil", nil, false},
		{"order_request nil", &models.PricingResponse{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := minimumCartNotReached(tc.pricing); got != tc.want {
				t.Fatalf("minimumCartNotReached = %v, want %v", got, tc.want)
			}
		})
	}
}
