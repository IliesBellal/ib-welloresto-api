package importer

import (
	"encoding/json"
	"reflect"
	"testing"
)

// L'objet ingrédient du schéma de composition est exactement celui de la
// lecture des photos : mêmes enums, mêmes champs requis.
func TestAICompositionSchema_IngredientMatchesPhotoSchema(t *testing.T) {
	var composition, photo map[string]any
	if err := json.Unmarshal(AICompositionSchema, &composition); err != nil {
		t.Fatalf("schéma de composition invalide: %v", err)
	}
	if err := json.Unmarshal(AIMenuPageSchema, &photo); err != nil {
		t.Fatalf("schéma photo invalide: %v", err)
	}
	ingredientOf := func(products map[string]any) any {
		return products["items"].(map[string]any)["properties"].(map[string]any)["ingredients"]
	}
	got := ingredientOf(composition["properties"].(map[string]any)["products"].(map[string]any))
	want := ingredientOf(photo["properties"].(map[string]any)["products"].(map[string]any))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ingrédient du schéma de composition = %v, want %v", got, want)
	}

	// Contraintes des sorties structurées : additionalProperties false et
	// toutes les propriétés requises, à chaque niveau.
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch v := node.(type) {
		case map[string]any:
			if v["type"] == "object" {
				props, _ := v["properties"].(map[string]any)
				required, _ := v["required"].([]any)
				if v["additionalProperties"] != false || len(required) != len(props) {
					t.Errorf("%s : additionalProperties %v, %d propriétés, %d requises", path, v["additionalProperties"], len(props), len(required))
				}
			}
			for key, child := range v {
				walk(path+"."+key, child)
			}
		case []any:
			for _, child := range v {
				walk(path+"[]", child)
			}
		}
	}
	walk("$", composition)
}

func TestCleanIngredients(t *testing.T) {
	got := CleanIngredients([]AIIngredient{
		{Name: "  Tomates ", Category: "vegetables", Unit: "G"},
		{Name: "tomate", Category: "vegetables", Unit: "PCE"},
		{Name: "", Category: "dairy", Unit: "G"},
		{Name: "Crème", Category: "inconnue", Unit: "litre"},
		{Name: "Basilic", Category: "vegetables", Unit: "g"},
	}, 10)
	want := []AIIngredient{
		{Name: "Tomates", Category: "vegetables", Unit: "G"},
		{Name: "Crème", Category: "other", Unit: "PCE"},
		{Name: "Basilic", Category: "vegetables", Unit: "G"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CleanIngredients = %+v, want %+v", got, want)
	}

	if capped := CleanIngredients(want, 2); len(capped) != 2 {
		t.Errorf("plafond : %d ingrédients, want 2", len(capped))
	}
}

func TestDecodeAIComposition(t *testing.T) {
	res, err := DecodeAIComposition(`{"products":[{"ref":"p1","ingredients":[{"name":"Steak","category":"meat","unit":"G"}]}]}`)
	if err != nil || len(res.Products) != 1 || res.Products[0].Ingredients[0].Name != "Steak" {
		t.Fatalf("DecodeAIComposition = %+v, %v", res, err)
	}
	if _, err := DecodeAIComposition("pas du json"); err == nil {
		t.Errorf("réponse illisible acceptée")
	}
}
