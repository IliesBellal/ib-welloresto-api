package scannorder

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/models"
)

func TestOnlineDiscounts_KeepsTakeAwayAndDeliveryOnly(t *testing.T) {
	got := onlineDiscounts([]Discount{
		{DiscountID: "all", DiscountOrderType: ""},
		{DiscountID: "in", DiscountOrderType: "IN"},
		{DiscountID: "ta", DiscountOrderType: "TAKE_AWAY"},
		{DiscountID: "mix", DiscountOrderType: "IN,DELIVERY"},
	})
	ids := []string{}
	for _, d := range got {
		ids = append(ids, d.DiscountID)
	}
	if len(ids) != 3 || ids[0] != "all" || ids[1] != "ta" || ids[2] != "mix" {
		t.Fatalf("onlineDiscounts = %v, want [all ta mix]", ids)
	}
}

func TestRecordFranchiseHubEvent_RejectsInvalidInputBeforeAnyQuery(t *testing.T) {
	s := &Service{} // pas de repo : la validation doit refuser avant toute requête
	for _, req := range []FranchiseHubEventRequest{
		{EventType: "purchase"},
		{EventType: ""},
		{EventType: HubEventOrderClick},
		{EventType: HubEventReservationClick, MerchantID: "  "},
	} {
		if err := s.RecordFranchiseHubEvent(context.Background(), "x", req); !errors.Is(err, models.ErrInvalidInput) {
			t.Fatalf("RecordFranchiseHubEvent(%+v) = %v, want ErrInvalidInput", req, err)
		}
	}
}
