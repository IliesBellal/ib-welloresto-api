package menu

import (
	"reflect"
	"testing"

	"welloresto-api/internal/models"
)

func TestToDeliverooFormat_SkipsProductsNotMarkedForSync(t *testing.T) {
	yes, no := true, false
	menu := &models.MenuResponse{ProductsTypes: []models.ProductCategory{{
		Category: "Plats",
		Products: []models.ProductEntry{
			{ProductID: "1", Name: "Burger", Price: 1200, SyncDeliveroo: &yes},
			{ProductID: "2", Name: "Salade", Price: 1100, SyncDeliveroo: &no},
			// Drapeau absent (NULL en base) : non envoyé, comme sync_uber_eats.
			{ProductID: "3", Name: "Wrap", Price: 900},
		},
	}}}

	payload, err := ToDeliverooFormat(menu)
	if err != nil {
		t.Fatalf("ToDeliverooFormat: %v", err)
	}
	var sent []string
	for _, item := range payload.Menus[0].Items {
		sent = append(sent, item.ID)
	}
	if want := []string{"1"}; !reflect.DeepEqual(sent, want) {
		t.Errorf("articles envoyés = %v, want %v", sent, want)
	}
}

func TestToDeliverooFormat_NothingMarkedForSync(t *testing.T) {
	no := false
	menu := &models.MenuResponse{ProductsTypes: []models.ProductCategory{{
		Category: "Plats",
		Products: []models.ProductEntry{{ProductID: "1", Name: "Burger", Price: 1200, SyncDeliveroo: &no}},
	}}}
	if _, err := ToDeliverooFormat(menu); err == nil {
		t.Fatalf("ToDeliverooFormat sans produit marqué Deliveroo devrait échouer")
	}
}
