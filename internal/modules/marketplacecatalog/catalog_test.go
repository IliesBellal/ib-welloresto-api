package marketplacecatalog

import (
	"errors"
	"reflect"
	"testing"

	"welloresto-api/internal/models"
)

func ptr[T any](v T) *T { return &v }

// testMenu : une catégorie « Boissons » avec un produit simple, un groupe
// Coca-Cola et ses trois sous-produits (12 non coché Deliveroo, 13 non coché
// Uber Eats), puis un produit simple ; une catégorie « Plats » avec une pizza
// à options, une salade épuisée et un dessert masqué.
func testMenu() *models.MenuResponse {
	product := func(id, name string, price int64, uber, deliveroo bool) models.ProductEntry {
		return models.ProductEntry{
			ProductID: id, Name: name, Price: price, Status: "available",
			SyncUberEats: ptr(uber), SyncDeliveroo: ptr(deliveroo),
			TVADelivery: ptr(10.0),
		}
	}

	group := product("10", "Coca-Cola", 0, true, true)
	group.IsProductGroup = ptr(true)
	group.SubProducts = []models.ProductEntry{
		product("11", "Coca-Cola Zero", 350, true, true),
		product("12", "Coca-Cola Cherry", 380, true, false),
		product("13", "Coca-Cola Vanille", 390, false, true),
	}

	pizza := product("20", "Pizza 4 fromages", 1200, true, true)
	pizza.PriceDelivery = ptr(int64(1300))
	pizza.Description = ptr("  Mozzarella, gorgonzola  ")
	pizza.Configuration = models.ConfigurableResponse{Attributes: []models.ConfigurableAttribute{
		{ID: "attr-1", Title: "Suppléments", MinOptions: 0, MaxOptions: 0, Options: []models.ConfigurableOption{
			{ID: "501", Title: "Olives", ExtraPrice: 100, MaxQuantity: 1},
			{ID: "502", Title: "Basilic", ExtraPrice: 50, MaxQuantity: 3},
		}},
		{ID: "attr-vide", Title: "Sans option"},
	}}

	salade := product("21", "Salade", 900, true, true)
	salade.Status = "out_of_stock"
	dessert := product("22", "Tiramisu", 600, true, true)
	dessert.Status = "not_available"
	legacy := product("23", "Ancien statut", 500, true, true)
	legacy.Status = "1"

	return &models.MenuResponse{ProductsTypes: []models.ProductCategory{
		{Category: "Boissons", CategoryName: "Boissons", CategoryID: ptr("c1"), Products: []models.ProductEntry{
			product("1", "Eau plate", 200, true, true), group, product("2", "Limonade", 300, true, true),
		}},
		{Category: "Plats", CategoryName: "Plats", CategoryID: ptr("c2"), Products: []models.ProductEntry{
			pizza, salade, dessert, legacy,
		}},
	}}
}

func TestBuild_GroupReplacedBySubProducts_AndSyncFlagPerPlatform(t *testing.T) {
	cat, err := Build(testMenu(), Deliveroo, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if want := []string{"1", "11", "13", "2"}; !reflect.DeepEqual(cat.Categories[0].ProductIDs, want) {
		t.Errorf("Deliveroo Boissons = %v, want %v", cat.Categories[0].ProductIDs, want)
	}
	if _, ok := cat.Products["10"]; ok {
		t.Error("le groupe 10 ne doit jamais être publié")
	}

	uber, err := Build(testMenu(), UberEats, nil)
	if err != nil {
		t.Fatalf("Build uber: %v", err)
	}
	if want := []string{"1", "11", "12", "2"}; !reflect.DeepEqual(uber.Categories[0].ProductIDs, want) {
		t.Errorf("Uber Eats Boissons = %v, want %v", uber.Categories[0].ProductIDs, want)
	}
}

func TestBuild_PriceFallback(t *testing.T) {
	extras := map[string]ProductExtras{
		"1":  {PlatformPriceCents: ptr(int64(250))}, // prix Deliveroo renseigné
		"2":  {PlatformPriceCents: ptr(int64(0))},   // 0 = non renseigné
		"20": {ContainsAlcohol: true},
	}
	cat, err := Build(testMenu(), Deliveroo, extras)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	cases := map[string]int64{"1": 250, "2": 300, "20": 1300, "11": 350}
	for id, want := range cases {
		if got := cat.Products[id].PriceCents; got != want {
			t.Errorf("prix %s = %d, want %d", id, got, want)
		}
	}
	if !cat.Products["20"].ContainsAlcohol || cat.Products["1"].ContainsAlcohol {
		t.Error("contains_alcohol doit venir des extras, produit par produit")
	}
	if cat.Products["20"].Description != "Mozzarella, gorgonzola" {
		t.Errorf("description = %q", cat.Products["20"].Description)
	}
	if cat.Products["20"].TaxRate != 10 {
		t.Errorf("TVA = %v, want 10 (taux livraison)", cat.Products["20"].TaxRate)
	}
}

func TestBuild_Availability(t *testing.T) {
	cat, err := Build(testMenu(), Deliveroo, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := map[string]Availability{"20": Available, "21": SoldOut, "22": Hidden, "23": Available}
	for id, a := range want {
		if got := cat.Products[id].Availability; got != a {
			t.Errorf("disponibilité %s = %s, want %s", id, got, a)
		}
	}

	only := cat.Availabilities([]string{"21", "inconnu"})
	if !reflect.DeepEqual(only, map[string]Availability{"21": SoldOut}) {
		t.Errorf("Availabilities limité = %v", only)
	}
}

func TestAvailabilityFromStatus(t *testing.T) {
	cases := map[string]Availability{
		"available": Available, "1": Available, " AVAILABLE ": Available, "true": Available,
		"out_of_stock": SoldOut,
		"not_available": Hidden, "0": Hidden, "false": Hidden, "": Hidden, "bizarre": Hidden,
	}
	for status, want := range cases {
		if got := AvailabilityFromStatus(status); got != want {
			t.Errorf("AvailabilityFromStatus(%q) = %s, want %s", status, got, want)
		}
	}
}

func TestBuild_OptionGroups(t *testing.T) {
	cat, err := Build(testMenu(), Deliveroo, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	groups := cat.Products["20"].OptionGroups
	if len(groups) != 1 {
		t.Fatalf("groupes = %d, want 1 (le groupe sans option est ignoré)", len(groups))
	}
	g := groups[0]
	if !g.Repeatable || g.Min != 0 || g.Max != 99 {
		t.Errorf("groupe = %+v, want repeatable, min 0, max 99 (max_options 0 = sans limite)", g)
	}
	if len(g.Options) != 2 || g.Options[0].PriceCents != 100 {
		t.Errorf("options = %+v", g.Options)
	}
}

func TestBuild_NothingToPublish(t *testing.T) {
	menu := &models.MenuResponse{ProductsTypes: []models.ProductCategory{{
		CategoryID: ptr("c"), Products: []models.ProductEntry{{ProductID: "1", Name: "X", SyncDeliveroo: ptr(false)}},
	}}}
	if _, err := Build(menu, Deliveroo, nil); !errors.Is(err, ErrNothingToPublish) {
		t.Fatalf("err = %v, want ErrNothingToPublish", err)
	}
}
