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

func TestRecordEvents_RejectsMalformedBatchBeforeAnyQuery(t *testing.T) {
	s := &Service{} // pas de repo : la validation doit refuser avant toute requête
	tooMany := make([]SNOEvent, maxEventsPerBatch+1)
	for i := range tooMany {
		tooMany[i] = SNOEvent{Type: EventMenuView, MerchantID: "1"}
	}
	for name, req := range map[string]SNOEventsRequest{
		"vide":                    {},
		"trop d'événements":       {Events: tooMany},
		"type inconnu":            {Events: []SNOEvent{{Type: "purchase", MerchantID: "1"}}},
		"visit_id invalide":       {VisitID: "abc", Events: []SNOEvent{{Type: EventMenuView, MerchantID: "1"}}},
		"menu sans établissement": {Events: []SNOEvent{{Type: EventMenuView}}},
		"produit sans produit":    {Events: []SNOEvent{{Type: EventProductView, MerchantID: "1"}}},
		"clic hub sans franchise": {Events: []SNOEvent{{Type: EventHubOrderClick, MerchantID: "1"}}},
		"vue hub sans franchise":  {Events: []SNOEvent{{Type: EventHubView}}},
	} {
		if err := s.RecordEvents(context.Background(), req); !errors.Is(err, models.ErrInvalidInput) {
			t.Fatalf("%s : RecordEvents = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestNormalizeSource(t *testing.T) {
	cases := map[string]string{
		"qr_table":           "qr_table",
		" Instagram ":        "instagram",
		"newsletter-2026":    "newsletter-2026",
		"<script>":           "",
		"":                   "",
		"un nom avec espace": "",
	}
	for in, want := range cases {
		if got := normalizeSource(in); got != want {
			t.Fatalf("normalizeSource(%q) = %q, want %q", in, got, want)
		}
	}
}
