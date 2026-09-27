package orders

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/models"
)

// Couvre les contraintes et prix d'options des promotions
// (discounts_products_options) et l'application des prix d'options officiels
// (applyConfigurationOptionPrices). Voir docs/KIOSK_DECISIONS.md (2026-09-27).

// withOptions construit une configuration avec les options données
// (sélectionnées, prix d'option donné).
func withOptions(extraPrice int, selectedIDs ...string) *models.ProductConfiguration {
	options := make([]models.ConfigurationOption, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		options = append(options, models.ConfigurationOption{ID: id, Selected: true, ExtraPrice: extraPrice})
	}
	return &models.ProductConfiguration{Attributes: []models.ConfigurationAttribute{{ID: "attr", Options: options}}}
}

func TestOptionsMatch(t *testing.T) {
	svc := &OrdersService{}
	mandatoryX := []models.DiscountOptionInfo{{OptionID: "X", IsOptionMandatory: true}}
	optionalX := []models.DiscountOptionInfo{{OptionID: "X", IsOptionMandatory: false}}

	cases := []struct {
		name   string
		config *models.ProductConfiguration
		promo  []models.DiscountOptionInfo
		want   bool
	}{
		{"aucune contrainte", nil, nil, true},
		{"option obligatoire sélectionnée", withOptions(0, "X"), mandatoryX, true},
		{"option obligatoire absente", withOptions(0, "Y"), mandatoryX, false},
		{"option obligatoire, produit sans configuration", nil, mandatoryX, false},
		{"option obligatoire, configuration vide", &models.ProductConfiguration{}, mandatoryX, false},
		{"option facultative absente", nil, optionalX, true},
		{"sélection par quantité", &models.ProductConfiguration{Attributes: []models.ConfigurationAttribute{{
			Options: []models.ConfigurationOption{{ID: "X", Quantity: 2}},
		}}}, mandatoryX, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sp := models.OrderProductPayload{ProductID: "A", Config: tc.config}
			if got := svc.optionsMatch(sp, tc.promo); got != tc.want {
				t.Fatalf("optionsMatch = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestApplyDiscounts_OptionConstraintsAndPrices : promotion -50 % sur A
// réservée à l'option obligatoire X, qui offre en plus le supplément X
// (new_price 0). Deux unités d'une même ligne partagent la configuration
// (comme buildSelectedProducts) : seule l'unité remisée voit son supplément
// passer à 0.
func TestApplyDiscounts_OptionConstraintsAndPrices(t *testing.T) {
	svc := &OrdersService{}
	discount := &models.DBDiscount{DiscountID: "d1", DiscountUnit: "PERCENTAGE", DiscountValue: 50, DiscountedQuantity: 1, Available: true}
	dp := map[string]map[string]*models.DiscountProductInfo{"d1": targeted("A")}
	do := map[string]map[string][]models.DiscountOptionInfo{"d1": {
		"A": {{OptionID: "X", IsOptionMandatory: true, NewPrice: intPtr(0)}},
	}}

	t.Run("option obligatoire choisie : remise et supplément offert sur une seule unité", func(t *testing.T) {
		shared := withOptions(150, "X")
		products := []models.OrderProductPayload{
			{ProductID: "A", Price: 1000, Quantity: 1, Config: shared},
			{ProductID: "A", Price: 1000, Quantity: 1, Config: shared},
		}
		req := &models.PricingRequest{Order: &models.OrderRequest{OrderType: "IN"}}

		applied := svc.applyDiscounts(req, products, []*models.DBDiscount{discount}, dp, do, 2000)

		if len(applied) != 1 || products[0].DiscountedPrice == nil || *products[0].DiscountedPrice != 500 || products[1].DiscountedPrice != nil {
			t.Fatalf("remise attendue sur la seule première unité, got applied=%v prices=%v", applied, unitPrices(products))
		}
		if got := products[0].Config.Attributes[0].Options[0].ExtraPrice; got != 0 {
			t.Fatalf("supplément de l'unité remisée = %d, want 0 (prix promo de l'option)", got)
		}
		if got := products[1].Config.Attributes[0].Options[0].ExtraPrice; got != 150 {
			t.Fatalf("supplément de l'unité non remisée = %d, want 150 (configuration partagée non modifiée)", got)
		}
	})

	t.Run("option obligatoire non choisie : pas de remise", func(t *testing.T) {
		products := []models.OrderProductPayload{{ProductID: "A", Price: 1000, Quantity: 1}}
		req := &models.PricingRequest{Order: &models.OrderRequest{OrderType: "IN"}}

		applied := svc.applyDiscounts(req, products, []*models.DBDiscount{discount}, dp, do, 1000)

		if len(applied) != 0 || products[0].DiscountedPrice != nil {
			t.Fatalf("aucune remise attendue sans l'option obligatoire, got applied=%v prices=%v", applied, unitPrices(products))
		}
	})
}

func TestGetDiscountProductOptions_IndexedByDiscount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`FROM discounts_products_options dpo`).
		WithArgs("42").
		WillReturnRows(sqlmock.NewRows([]string{"discount_id", "product_id", "option_id", "new_price", "is_option_mandatory"}).
			AddRow("d1", "A", "X", 0, true).
			AddRow("d1", "A", "Y", nil, false))

	got, err := NewOrdersRepository(db, nil).GetDiscountProductOptions(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetDiscountProductOptions: %v", err)
	}
	opts := got["d1"]["A"]
	if len(opts) != 2 || opts[0].OptionID != "X" || !opts[0].IsOptionMandatory || opts[0].NewPrice == nil || *opts[0].NewPrice != 0 ||
		opts[1].OptionID != "Y" || opts[1].IsOptionMandatory || opts[1].NewPrice != nil {
		t.Fatalf("options de d1/A inattendues : %+v", got)
	}
}

// TestApplyConfigurationOptionPrices_UsesDatabasePrice : le prix d'option
// envoyé par le client (ici 1, falsifié) est remplacé par le prix officiel
// lu en base, pour toutes les unités de la ligne.
func TestApplyConfigurationOptionPrices_UsesDatabasePrice(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	svc := &OrdersService{ordersRepo: NewOrdersRepository(db, nil)}

	mock.ExpectQuery(`FROM configurable_attribute_options`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "extra_price"}).AddRow("X", 150))

	shared := withOptions(1, "X")
	products := []models.OrderProductPayload{
		{ProductID: "A", Price: 1000, Quantity: 1, Config: shared},
		{ProductID: "A", Price: 1000, Quantity: 1, Config: shared},
	}
	if err := svc.applyConfigurationOptionPrices(context.Background(), products); err != nil {
		t.Fatalf("applyConfigurationOptionPrices: %v", err)
	}
	for i, p := range products {
		if got := p.Config.Attributes[0].Options[0].ExtraPrice; got != 150 {
			t.Fatalf("unité %d : prix d'option = %d, want 150 (prix officiel)", i, got)
		}
	}
}
