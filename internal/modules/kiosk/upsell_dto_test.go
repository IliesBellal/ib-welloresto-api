package kiosk

import (
	"encoding/json"
	"testing"

	"welloresto-api/internal/models"
)

// Régression : GetUpsellSuggestions exposait autrefois SuggestedItem.Product
// (un models.ProductEntry brut, seulement débarrassé de quelques champs
// internes) directement au client Kiosk. Le modèle Flutter Product
// (upsell_suggestion.dart -> product) attend le contrat KioskProduct — même
// forme que /kiosk/menu ("id"/"price_cents"/"available_on_kiosk") — jamais
// la forme ProductEntry ("product_id"/"price", pas d'available_on_kiosk).
// Product.fromJson côté Flutter (champs requis sans valeur par défaut)
// levait systématiquement sur "id" manquant, avalée silencieusement par
// UpsellController.loadSuggestions : l'upsell ne s'affichait jamais, sans
// aucune erreur visible côté serveur (la réponse HTTP restait 200).
//
// Ce test verrouille la forme JSON réellement envoyée au client, au niveau
// octets — pas seulement l'état Go interne — pour que cette classe de bug
// (nom de champ qui diverge silencieusement entre Go et le modèle Dart généré)
// ne puisse plus repasser inaperçue.
func TestKioskUpsellSuggestion_ProductJSONMatchesKioskProductContract(t *testing.T) {
	product := &models.ProductEntry{
		ProductID: "prod_123",
		Name:      "Frites",
		Price:     350,
	}
	kioskProduct := mapProductEntryToKioskProduct(product, models.OrderTypeIn)

	suggestion := KioskUpsellSuggestion{
		ProductID: product.ProductID,
		Name:      product.Name,
		Price:     kioskProduct.PriceCents,
		Product:   &kioskProduct,
	}

	raw, err := json.Marshal(suggestion)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	productJSON, ok := decoded["product"].(map[string]any)
	if !ok {
		t.Fatalf("decoded[%q] is not a JSON object: %#v (payload: %s)", "product", decoded["product"], raw)
	}

	// Clés exigées par Product.fromJson côté Flutter (requises, sans
	// @JsonKey(defaultValue: ...)) — leur absence fait planter le parsing.
	for _, requiredKey := range []string{"id", "name", "price_cents", "available", "available_on_kiosk"} {
		if _, present := productJSON[requiredKey]; !present {
			t.Fatalf("product JSON is missing required key %q (payload: %s)", requiredKey, raw)
		}
	}

	// La forme cassée (ProductEntry brut, jamais converti) exposait ces clés
	// à la place — leur présence signalerait une régression vers cette forme.
	for _, brokenKey := range []string{"product_id", "price"} {
		if _, present := productJSON[brokenKey]; present {
			t.Fatalf("product JSON still carries the raw ProductEntry key %q — the nested product must always go through mapProductEntryToKioskProduct (payload: %s)", brokenKey, raw)
		}
	}

	if productJSON["id"] != "prod_123" {
		t.Fatalf(`product["id"] = %v, want "prod_123"`, productJSON["id"])
	}
	if priceCents, ok := productJSON["price_cents"].(float64); !ok || int64(priceCents) != 350 {
		t.Fatalf(`product["price_cents"] = %v, want 350`, productJSON["price_cents"])
	}

	// Champ racine (lu par UpsellSuggestion.priceCents côté Flutter, via
	// @JsonKey(name: 'price')) : doit rester "price", jamais "price_cents"
	// — contrat partagé avec le POS (upsell_item_dto.dart y lit aussi
	// json['price']), à ne pas casser en "corrigeant" ce nom côté Go.
	if _, present := decoded["price_cents"]; present {
		t.Fatalf("top-level suggestion JSON must use \"price\", never \"price_cents\" (payload: %s)", raw)
	}
	if rootPrice, ok := decoded["price"].(float64); !ok || int64(rootPrice) != 350 {
		t.Fatalf(`suggestion["price"] = %v, want 350`, decoded["price"])
	}
}
