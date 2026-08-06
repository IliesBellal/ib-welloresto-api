package menu

import (
	"encoding/json"
	"testing"
)

func TestUpdateComponentPayloadUnmarshalAcceptsDecimalPurchaseCostQty(t *testing.T) {
	var payload UpdateComponentPayload

	if err := json.Unmarshal([]byte(`{"purchase_cost_qty":2.85}`), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.PurchaseCostQty == nil {
		t.Fatal("PurchaseCostQty = nil, want 2.85")
	}
	if got, want := *payload.PurchaseCostQty, 2.85; got != want {
		t.Fatalf("PurchaseCostQty = %v, want %v", got, want)
	}
}

func TestUpdateComponentPayloadUnmarshalAcceptsIntegerPurchaseCostQty(t *testing.T) {
	var payload UpdateComponentPayload

	if err := json.Unmarshal([]byte(`{"purchase_cost_qty":3}`), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.PurchaseCostQty == nil {
		t.Fatal("PurchaseCostQty = nil, want 3")
	}
	if got, want := *payload.PurchaseCostQty, 3.0; got != want {
		t.Fatalf("PurchaseCostQty = %v, want %v", got, want)
	}
}

// Les champs de lien ingrédient (ComponentID/Quantity/UnitOfMeasureID) sont
// non-pointeurs à dessein (cf. UpdateAttributeOptionPayload) : une clé JSON
// omise doit décoder vers le zero-value Go, exactement comme une clé envoyée
// explicitement vide — c'est ce qui permet au bouton "Aucun" du back-office
// (qui envoie `undefined`, éliminé par JSON.stringify) d'effacer un lien déjà
// posé sans qu'aucun changement frontend ne soit nécessaire.
func TestUpdateAttributeOptionPayloadUnmarshalOmittedComponentFieldsDecodeToZeroValue(t *testing.T) {
	var payload UpdateAttributeOptionPayload

	if err := json.Unmarshal([]byte(`{"title":"Ketchup bio","price":60}`), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.ComponentID != "" || payload.Quantity != 0 || payload.UnitOfMeasureID != "" {
		t.Fatalf("champs ingrédient omis = (%q, %v, %q), want zero values", payload.ComponentID, payload.Quantity, payload.UnitOfMeasureID)
	}
}
