package menu

import (
	"reflect"
	"testing"

	"welloresto-api/internal/models"
)

// platformTestMenu construit une catégorie « Boissons » : un produit simple,
// un groupe Coca-Cola (prix 0, comme les groupes en base) avec trois
// sous-produits — 12 non marqué pour Deliveroo, 13 non marqué pour Uber
// Eats — puis un second produit simple. Les autres produits sont marqués
// pour les deux plateformes.
func platformTestMenu() *models.MenuResponse {
	yes, no := true, false
	product := func(id, name string, price int64, syncUber, syncDeliveroo bool) models.ProductEntry {
		return models.ProductEntry{
			ProductID: id, Name: name, Price: price,
			SyncUberEats: &syncUber, SyncDeliveroo: &syncDeliveroo,
		}
	}

	group := product("10", "Coca-Cola", 0, true, true)
	group.IsProductGroup = &yes
	group.SubProducts = []models.ProductEntry{
		product("11", "Coca-Cola Zero", 350, true, true),
		product("12", "Coca-Cola Cherry", 380, true, false),
		product("13", "Coca-Cola Vanille", 390, false, true),
	}

	notGroup := product("1", "Eau plate", 200, true, true)
	notGroup.IsProductGroup = &no

	catID := "cat-boissons"
	return &models.MenuResponse{
		ProductsTypes: []models.ProductCategory{{
			Category:   "Boissons",
			CategoryID: &catID,
			Products: []models.ProductEntry{
				notGroup,
				group,
				product("2", "Limonade", 300, true, true),
			},
		}},
	}
}

func TestPlatformProducts_GroupReplacedByItsSubProductsInPlace(t *testing.T) {
	products := platformTestMenu().ProductsTypes[0].Products

	var got []string
	for _, p := range platformProducts(products) {
		got = append(got, p.ProductID)
	}
	want := []string{"1", "11", "12", "13", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("produits aplatis = %v, want %v", got, want)
	}
}

func TestPlatformProducts_NonGroupWithSubProductsKeepsBoth(t *testing.T) {
	// Donnée incohérente (sous-produits sous un produit non groupe) : même
	// règle que le kiosk, le produit ET ses sous-produits sont envoyés.
	parent := models.ProductEntry{ProductID: "1", SubProducts: []models.ProductEntry{{ProductID: "2"}}}

	var got []string
	for _, p := range platformProducts([]models.ProductEntry{parent}) {
		got = append(got, p.ProductID)
	}
	if want := []string{"1", "2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("produits aplatis = %v, want %v", got, want)
	}
}

func TestToUberEatsFormat_SendsSubProductsNotGroups(t *testing.T) {
	payload, err := ToUberEatsFormat(platformTestMenu())
	if err != nil {
		t.Fatalf("ToUberEatsFormat: %v", err)
	}

	// Le sous-produit 13 n'est pas marqué pour Uber Eats : il reste filtré.
	if len(payload.Categories) != 1 {
		t.Fatalf("catégories = %d, want 1", len(payload.Categories))
	}
	var entities []string
	for _, e := range payload.Categories[0].Entities {
		entities = append(entities, e.ID)
	}
	if want := []string{"1", "11", "12", "2"}; !reflect.DeepEqual(entities, want) {
		t.Errorf("articles de la catégorie = %v, want %v", entities, want)
	}

	prices := map[string]int64{}
	for _, item := range payload.Items {
		prices[item.ID] = item.PriceInfo.Price
	}
	if _, sent := prices["10"]; sent {
		t.Errorf("le groupe 10 ne doit pas être envoyé comme article")
	}
	if prices["11"] != 350 || prices["12"] != 380 {
		t.Errorf("prix des sous-produits = %v, want 11:350 12:380", prices)
	}
}

func TestToDeliverooFormat_SendsSubProductsNotGroups(t *testing.T) {
	payload, err := ToDeliverooFormat(platformTestMenu())
	if err != nil {
		t.Fatalf("ToDeliverooFormat: %v", err)
	}

	entry := payload.Menus[0]
	if len(entry.Categories) != 1 {
		t.Fatalf("catégories = %d, want 1", len(entry.Categories))
	}
	// Le sous-produit 12 n'est pas marqué pour Deliveroo : il est filtré.
	if want := []string{"1", "11", "13", "2"}; !reflect.DeepEqual(entry.Categories[0].ItemIDs, want) {
		t.Errorf("articles de la catégorie = %v, want %v", entry.Categories[0].ItemIDs, want)
	}
	for _, item := range entry.Items {
		if item.ID == "10" {
			t.Errorf("le groupe 10 ne doit pas être envoyé comme article")
		}
		if item.ID == "13" && item.PriceMoney.Amount != 390 {
			t.Errorf("prix du sous-produit 13 = %d, want 390", item.PriceMoney.Amount)
		}
	}
}
