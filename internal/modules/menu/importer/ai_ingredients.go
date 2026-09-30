package importer

import (
	"strings"

	"welloresto-api/internal/importutil"
)

// Ingrédients de la porte IA : lus dans la description écrite sous chaque
// produit, seulement si le restaurateur l'a demandé à l'envoi des photos.
// Ils deviennent des composants (components) et des lignes de composition
// (requires) à quantité 0 : la composition décrit le produit sans toucher au
// stock, puisqu'une carte n'indique jamais de quantités.
const (
	aiIngredientPrefix         = "ai-i"
	aiIngredientCategoryPrefix = "ai-ic"
	// aiDefaultUnit : unité de repli d'un ingrédient dont l'unité proposée
	// n'existe pas dans unit_of_measure.
	aiDefaultUnit = "PCE"
)

// WarningAIIngredientsSkipped : ingrédients demandés mais non repris, faute
// d'unité de mesure connue (unit_of_measure vide ou sans PCE).
const WarningAIIngredientsSkipped = "ai_ingredients_skipped"

// AIIngredient est un ingrédient cité dans la description d'un produit.
type AIIngredient struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	// Unit est le code unit_of_measure.uom le plus naturel pour cet
	// ingrédient (G pour un fromage, CL pour une crème, PCE pour un œuf).
	Unit string `json:"unit"`
}

// aiIngredientCategory est une catégorie d'ingrédient de la liste fixe
// proposée au modèle. Le libellé devient une component_category ; une
// catégorie du même nom chez le marchand est réutilisée (preview, commit).
type aiIngredientCategory struct {
	Code  string
	Label string
}

// AIIngredientCategories est la liste fixe des catégories d'ingrédient. Son
// ordre et ses codes sont ceux de l'enum de AIMenuPageSchema (vérifié par
// test).
var AIIngredientCategories = []aiIngredientCategory{
	{"meat", "Viandes"},
	{"fish", "Poissons et fruits de mer"},
	{"dairy", "Fromages et crèmerie"},
	{"vegetables", "Légumes"},
	{"fruits", "Fruits"},
	{"grocery", "Épicerie"},
	{"sauces", "Sauces et condiments"},
	{"bakery", "Boulangerie"},
	{"other", "Autres"},
}

// AIIngredientUnits sont les codes d'unité proposés au modèle (enum de
// AIMenuPageSchema), résolus en unit_of_measure.id par BuildAIMenuImport.
var AIIngredientUnits = []string{"PCE", "G", "KG", "L", "ML", "CL"}

func aiIngredientCategoryLabel(code string) string {
	for _, c := range AIIngredientCategories {
		if c.Code == code {
			return c.Label
		}
	}
	return AIIngredientCategories[len(AIIngredientCategories)-1].Label
}

// aiIngredientKey est l'identité d'un ingrédient pour la fusion : casse,
// accents et pluriel simple (s, x final de chaque mot) ignorés. « Tomates »,
// « tomate » et « TOMATE » sont un seul ingrédient ; « Pommes de terre » et
// « pomme de terre » aussi.
func aiIngredientKey(name string) string {
	words := strings.Fields(importutil.FoldHeader(name))
	for i, w := range words {
		if len(w) > 3 && (strings.HasSuffix(w, "s") || strings.HasSuffix(w, "x")) {
			words[i] = w[:len(w)-1]
		}
	}
	return strings.Join(words, " ")
}

// ingredients rend la composition d'un produit : un composant par ingrédient
// distinct, créé à sa première apparition dans le lot (nom, catégorie et
// unité de cette première apparition), réutilisé ensuite.
func (m *aiMerger) ingredients(p AIProduct) []CanonicalProductComponent {
	if len(p.Ingredients) == 0 {
		return nil
	}

	var out []CanonicalProductComponent
	seen := make(map[string]struct{}, len(p.Ingredients))
	for _, ing := range p.Ingredients {
		name := strings.TrimSpace(ing.Name)
		key := aiIngredientKey(name)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}

		component, ok := m.componentByKey[key]
		if !ok {
			unitID, known := m.unitIDs[ing.Unit]
			if !known {
				if unitID, known = m.unitIDs[aiDefaultUnit]; !known {
					m.ingredientsSkipped = true
					continue
				}
			}
			component = &CanonicalComponent{
				ExternalID:         importutil.GeneratedExternalID(aiIngredientPrefix, key),
				Name:               name,
				CategoryExternalID: m.ingredientCategory(ing.Category),
				UnitOfMeasureID:    unitID,
			}
			m.componentByKey[key] = component
			m.out.Components = append(m.out.Components, *component)
		}

		seen[key] = struct{}{}
		out = append(out, CanonicalProductComponent{
			ComponentExternalID: component.ExternalID,
			Quantity:            0,
			UnitOfMeasureID:     component.UnitOfMeasureID,
			InOrders:            true,
			TakeAwayOrders:      true,
			DeliveryOrders:      true,
		})
	}
	return out
}

// ingredientCategory déclare la catégorie d'ingrédient à sa première
// utilisation : le lot ne contient que les catégories réellement servies.
func (m *aiMerger) ingredientCategory(code string) string {
	label := aiIngredientCategoryLabel(code)
	id := importutil.GeneratedExternalID(aiIngredientCategoryPrefix, label)
	if _, ok := m.ingredientCategories[id]; !ok {
		m.ingredientCategories[id] = struct{}{}
		m.out.ComponentCategories = append(m.out.ComponentCategories, CanonicalComponentCategory{ExternalID: id, Name: label})
	}
	return id
}
