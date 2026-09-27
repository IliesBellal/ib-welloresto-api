package kiosk

import (
	"reflect"
	"testing"
)

func TestPromoProducts(t *testing.T) {
	code := "BIENVENUE"
	// Ordre = prefered_order (tel que renvoyé par Repository.GetDiscounts).
	discounts := []KioskDiscount{
		{DiscountID: "burger-in", DiscountName: "Burger du midi", DiscountOrderType: "IN", Available: true},
		{DiscountID: "dessert-all", DiscountName: "Dessert offert", DiscountOrderType: "", Available: true},
		{DiscountID: "boisson-take-away", DiscountName: "Boisson à emporter", DiscountOrderType: "TAKE_AWAY", Available: true},
		{DiscountID: "code", DiscountName: "Code bienvenue", DiscountOrderType: "", DiscountCode: &code, Available: true},
		{DiscountID: "panier-entier", DiscountName: "-10% sur tout", DiscountOrderType: "", Available: true},
		{DiscountID: "inactive", DiscountName: "Inactive", DiscountOrderType: "", Available: false},
	}
	productsByDiscount := map[string][]string{
		"burger-in":         {"2", "1"},
		"dessert-all":       {"3", "1"}, // "1" déjà porté par une promo prioritaire en IN
		"boisson-take-away": {"4"},
		"code":              {"5"},
		"inactive":          {"6"},
		// "panier-entier" : aucun produit ciblé (remise sur tout le panier).
	}

	cases := []struct {
		name      string
		orderType string
		want      []KioskPromoProduct
	}{
		{"sur place", "IN", []KioskPromoProduct{
			{ProductID: "1", DiscountID: "burger-in", DiscountName: "Burger du midi"},
			{ProductID: "2", DiscountID: "burger-in", DiscountName: "Burger du midi"},
			{ProductID: "3", DiscountID: "dessert-all", DiscountName: "Dessert offert"},
		}},
		{"a emporter", "TAKE_AWAY", []KioskPromoProduct{
			{ProductID: "1", DiscountID: "dessert-all", DiscountName: "Dessert offert"},
			{ProductID: "3", DiscountID: "dessert-all", DiscountName: "Dessert offert"},
			{ProductID: "4", DiscountID: "boisson-take-away", DiscountName: "Boisson à emporter"},
		}},
		{"mode inconnu", "", []KioskPromoProduct{
			{ProductID: "1", DiscountID: "burger-in", DiscountName: "Burger du midi"},
			{ProductID: "2", DiscountID: "burger-in", DiscountName: "Burger du midi"},
			{ProductID: "3", DiscountID: "dessert-all", DiscountName: "Dessert offert"},
			{ProductID: "4", DiscountID: "boisson-take-away", DiscountName: "Boisson à emporter"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := promoProducts(discounts, productsByDiscount, tc.orderType)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("promoProducts = %+v, want %+v", got, tc.want)
			}
		})
	}

	if got := promoProducts(nil, nil, "IN"); got == nil || len(got) != 0 {
		t.Fatalf("no discount must yield an empty (non-nil) list, got %#v", got)
	}
}
