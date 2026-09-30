package scannorder

import (
	"testing"

	"welloresto-api/internal/models"
)

func TestIsOrderTypeEnabled(t *testing.T) {
	table := "loc-1"
	empty := ""
	// Configuration type du signalement : à emporter + livraison uniquement.
	takeAwayDelivery := models.MerchantRow{TakeawayEnabled: true, DeliveryEnabled: true}
	tableQR := models.MerchantRow{TakeawayEnabled: true, LocationID: &table}
	emptyLocation := models.MerchantRow{TakeawayEnabled: true, LocationID: &empty}
	inOnly := models.MerchantRow{InEnabled: true}

	cases := []struct {
		name      string
		merchant  *models.MerchantRow
		orderType string
		want      bool
	}{
		{"IN forcé dans l'URL, non proposé", &takeAwayDelivery, "IN", false},
		{"TAKE_AWAY proposé", &takeAwayDelivery, "TAKE_AWAY", true},
		{"DELIVERY proposé", &takeAwayDelivery, "DELIVERY", true},
		{"DELIVERY non proposé", &inOnly, "DELIVERY", false},
		{"TAKE_AWAY non proposé", &inOnly, "TAKE_AWAY", false},
		{"IN proposé", &inOnly, "IN", true},
		{"IN via QR de table", &tableQR, "IN", true},
		{"location_id vide ne vaut pas QR de table", &emptyLocation, "IN", false},
		{"mode vide refusé", &takeAwayDelivery, "", false},
		{"mode inconnu refusé", &takeAwayDelivery, "FOO", false},
		{"casse non normalisée refusée", &takeAwayDelivery, "take_away", false},
		{"merchant nil", nil, "TAKE_AWAY", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOrderTypeEnabled(tc.merchant, tc.orderType); got != tc.want {
				t.Fatalf("isOrderTypeEnabled(%q) = %v, want %v", tc.orderType, got, tc.want)
			}
		})
	}
}
