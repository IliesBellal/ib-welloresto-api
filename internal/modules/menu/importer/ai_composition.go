package importer

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Composition par IA d'un produit existant, à partir de sa description
// (docs/composition-ia.md). Même vocabulaire fermé que les ingrédients de la
// porte photo (AIIngredientCategories, AIIngredientUnits) et même identité
// d'ingrédient (IngredientKey) : un ingrédient lu ici retrouve celui créé par
// un import photo.

// AICompositionResult est la sortie du modèle pour un lot de produits.
type AICompositionResult struct {
	Products []AICompositionProduct `json:"products"`
}

// AICompositionProduct : ref est celle donnée au modèle dans la consigne
// utilisateur (p1, p2…), jamais un identifiant Wello.
type AICompositionProduct struct {
	Ref         string         `json:"ref"`
	Ingredients []AIIngredient `json:"ingredients"`
}

// AICompositionSchema contraint la réponse du modèle. L'objet ingrédient est
// celui de AIMenuPageSchema (vérifié par test). Transmis octet pour octet,
// comme AIMenuPageSchema, pour garder en cache sa grammaire compilée.
var AICompositionSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "products": {"type": "array", "items": {
      "type": "object",
      "properties": {
        "ref": {"type": "string"},
        "ingredients": {"type": "array", "items": {
          "type": "object",
          "properties": {
            "name": {"type": "string"},
            "category": {"type": "string", "enum": ["meat", "fish", "dairy", "vegetables", "fruits", "grocery", "sauces", "bakery", "other"]},
            "unit": {"type": "string", "enum": ["PCE", "G", "KG", "L", "ML", "CL"]}},
          "required": ["name", "category", "unit"], "additionalProperties": false}}},
      "required": ["ref", "ingredients"], "additionalProperties": false}}
  },
  "required": ["products"],
  "additionalProperties": false
}`)

// DecodeAIComposition relit la réponse du modèle pour un lot.
func DecodeAIComposition(raw string) (*AICompositionResult, error) {
	var out AICompositionResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("réponse IA illisible: %w", err)
	}
	return &out, nil
}

// IngredientKey est l'identité d'un ingrédient : casse, accents et pluriel
// simple ignorés (« Tomates » = « tomate »).
func IngredientKey(name string) string { return aiIngredientKey(name) }

// IngredientCategoryLabel rend le nom de la catégorie d'ingrédient d'un code
// (« vegetables » → « Légumes ») ; « Autres » pour un code inconnu.
func IngredientCategoryLabel(code string) string { return aiIngredientCategoryLabel(code) }

// CleanIngredients rend les ingrédients d'un produit nettoyés : nom
// rogné, doublons (même IngredientKey) retirés, catégorie inconnue ramenée à
// « other », unité inconnue ramenée à « PCE ». Au plus max ingrédients.
func CleanIngredients(in []AIIngredient, max int) []AIIngredient {
	out := make([]AIIngredient, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, ing := range in {
		name := strings.TrimSpace(ing.Name)
		key := IngredientKey(name)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, AIIngredient{Name: name, Category: knownIngredientCategory(ing.Category), Unit: knownIngredientUnit(ing.Unit)})
		if len(out) == max {
			break
		}
	}
	return out
}

func knownIngredientCategory(code string) string {
	for _, c := range AIIngredientCategories {
		if c.Code == code {
			return code
		}
	}
	return AIIngredientCategories[len(AIIngredientCategories)-1].Code
}

func knownIngredientUnit(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	for _, u := range AIIngredientUnits {
		if u == code {
			return code
		}
	}
	return aiDefaultUnit
}
