package importer

import (
	"encoding/json"
	"reflect"
	"testing"
)

var aiTestUnitIDs = map[string]string{"PCE": "1", "G": "2", "KG": "3", "L": "4", "ML": "5", "CL": "6"}

// aiIngredientPages : deux photos dont les descriptions citent des
// ingrédients communs, écrits différemment.
func aiIngredientPages() []AIMenuPage {
	return []AIMenuPage{
		{
			Categories: []AICategory{{Ref: "c1", Name: "Pizzas"}},
			Products: []AIProduct{
				{Ref: "p1", CategoryRef: strPtr("c1"), Name: "Reine", Description: "Tomates, mozzarella, jambon", PriceCents: intPtr(1200), Kind: "food", Confidence: "high",
					Ingredients: []AIIngredient{
						{Name: "Tomates", Category: "vegetables", Unit: "G"},
						{Name: "Mozzarella", Category: "dairy", Unit: "G"},
						{Name: "Jambon", Category: "meat", Unit: "G"},
					}},
			},
		},
		{
			Categories: []AICategory{{Ref: "c1", Name: "Salades"}},
			Products: []AIProduct{
				{Ref: "p1", CategoryRef: strPtr("c1"), Name: "Salade du chef", Description: "tomate, œufs, crème", PriceCents: intPtr(1100), Kind: "food", Confidence: "high",
					Ingredients: []AIIngredient{
						{Name: "tomate", Category: "vegetables", Unit: "PCE"},
						{Name: "Œufs", Category: "dairy", Unit: "PCE"},
						{Name: "œuf", Category: "dairy", Unit: "PCE"},
						{Name: "Crème", Category: "dairy", Unit: "KG"},
					}},
			},
		},
	}
}

func buildAIIngredientImport(t *testing.T, units map[string]string) *IntermediateImport {
	t.Helper()
	imp, err := BuildAIMenuImport(aiIngredientPages(), units)
	if err != nil {
		t.Fatalf("BuildAIMenuImport: %v", err)
	}
	return imp
}

func componentByName(t *testing.T, imp *IntermediateImport, name string) CanonicalComponent {
	t.Helper()
	for _, c := range imp.Components {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("ingrédient %q absent", name)
	return CanonicalComponent{}
}

func TestAIMenuPageSchema_IngredientEnums(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(AIMenuPageSchema, &schema); err != nil {
		t.Fatalf("schéma JSON invalide: %v", err)
	}
	products := schema["properties"].(map[string]any)["products"].(map[string]any)
	ingredient := products["items"].(map[string]any)["properties"].(map[string]any)["ingredients"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)

	var categories []any
	for _, c := range AIIngredientCategories {
		categories = append(categories, c.Code)
	}
	if got := ingredient["category"].(map[string]any)["enum"].([]any); !reflect.DeepEqual(got, categories) {
		t.Errorf("enum category = %v, want %v", got, categories)
	}
	var units []any
	for _, u := range AIIngredientUnits {
		units = append(units, u)
	}
	if got := ingredient["unit"].(map[string]any)["enum"].([]any); !reflect.DeepEqual(got, units) {
		t.Errorf("enum unit = %v, want %v", got, units)
	}
}

func TestAIIngredientKey(t *testing.T) {
	same := [][2]string{
		{"Tomates", "tomate"},
		{"Œufs", "œuf"},
		{"Pommes de terre", "pomme de terre"},
		{"Maïs", "mais"},
		{"  Crème   fraîche ", "crème fraiche"},
	}
	for _, pair := range same {
		if aiIngredientKey(pair[0]) != aiIngredientKey(pair[1]) {
			t.Errorf("%q et %q doivent être le même ingrédient (%q / %q)", pair[0], pair[1], aiIngredientKey(pair[0]), aiIngredientKey(pair[1]))
		}
	}
	if aiIngredientKey("Riz") == aiIngredientKey("Ri") {
		t.Errorf("un mot court ne perd pas sa dernière lettre")
	}
}

func TestBuildAIMenuImport_IngredientsMergedAcrossProducts(t *testing.T) {
	imp := buildAIIngredientImport(t, aiTestUnitIDs)

	// Tomate (2 écritures), Mozzarella, Jambon, Œuf (2 écritures), Crème.
	if len(imp.Components) != 5 {
		t.Fatalf("ingrédients = %d, want 5 : %+v", len(imp.Components), imp.Components)
	}
	tomato := componentByName(t, imp, "Tomates")
	// La première apparition fixe le nom, la catégorie et l'unité.
	if tomato.UnitOfMeasureID != "2" {
		t.Errorf("unité de la tomate = %q, want 2 (G, première apparition)", tomato.UnitOfMeasureID)
	}
	if crème := componentByName(t, imp, "Crème"); crème.UnitOfMeasureID != "3" {
		t.Errorf("unité de la crème = %q, want 3 (KG)", crème.UnitOfMeasureID)
	}

	// Seules les catégories servies sont déclarées, au libellé de la liste fixe.
	var categoryNames []string
	for _, c := range imp.ComponentCategories {
		categoryNames = append(categoryNames, c.Name)
	}
	if want := []string{"Légumes", "Fromages et crèmerie", "Viandes"}; !reflect.DeepEqual(categoryNames, want) {
		t.Errorf("catégories = %v, want %v", categoryNames, want)
	}

	salad := productByName(t, imp, "Salade du chef")
	if len(salad.Components) != 3 {
		t.Fatalf("composition de la salade = %+v, want tomate, œuf, crème", salad.Components)
	}
	first := salad.Components[0]
	if first.ComponentExternalID != tomato.ExternalID || first.Quantity != 0 || first.UnitOfMeasureID != "2" ||
		!first.InOrders || !first.TakeAwayOrders || !first.DeliveryOrders {
		t.Errorf("ligne de composition = %+v, want la tomate de la Reine, quantité 0, unité de l'ingrédient, trois canaux", first)
	}
}

func TestBuildAIMenuImport_UnknownUnitFallsBackToPiece(t *testing.T) {
	imp := buildAIIngredientImport(t, map[string]string{"PCE": "1", "G": "2"})
	if crème := componentByName(t, imp, "Crème"); crème.UnitOfMeasureID != "1" {
		t.Errorf("unité de la crème (KG inconnu) = %q, want 1 (PCE)", crème.UnitOfMeasureID)
	}
}

func TestBuildAIMenuImport_IngredientsSkippedWithoutUnits(t *testing.T) {
	imp := buildAIIngredientImport(t, nil)
	if len(imp.Components) != 0 || len(imp.ComponentCategories) != 0 || len(productByName(t, imp, "Reine").Components) != 0 {
		t.Errorf("sans unités, aucun ingrédient ne doit être créé")
	}
	if w := sourceWarning(t, imp, WarningAIIngredientsSkipped); w.Message == "" {
		t.Errorf("avertissement attendu")
	}
}

func TestBuildAIMenuImport_NoIngredientsWhenNotRead(t *testing.T) {
	imp, err := BuildAIMenuImport(aiTestPages(), aiTestUnitIDs)
	if err != nil {
		t.Fatalf("BuildAIMenuImport: %v", err)
	}
	if len(imp.Components) != 0 || len(imp.ComponentCategories) != 0 {
		t.Errorf("aucune description lue : aucun ingrédient attendu")
	}
}

// ---- relecture et plan de commit ----

func aiIngredientDecisions(t *testing.T, imp *IntermediateImport) ImportDecisions {
	t.Helper()
	decisions := defaultDecisions(t, imp, defaultLookups())
	decisions.TvaConfirmed = true
	return decisions
}

func plannedComponentNames(plan *CommitPlan) []string {
	var names []string
	for _, c := range plan.Components {
		names = append(names, c.Name)
	}
	return names
}

func TestBuildCommitPlan_AIDoorWritesIngredients(t *testing.T) {
	imp := buildAIIngredientImport(t, aiTestUnitIDs)
	plan, blockers := BuildCommitPlan(imp, aiIngredientDecisions(t, imp), defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	if len(plan.Components) != 5 || len(plan.ComponentCategories) != 3 {
		t.Errorf("plan = %d ingrédients, %d catégories, want 5 et 3", len(plan.Components), len(plan.ComponentCategories))
	}
	if reine := plannedByName(t, plan, "Reine"); len(reine.Components) != 3 {
		t.Errorf("composition de la Reine = %+v", reine.Components)
	}
}

func TestBuildCommitPlan_AIDoorIngredientRemovedEverywhereIsNotCreated(t *testing.T) {
	imp := buildAIIngredientImport(t, aiTestUnitIDs)
	tomato := componentByName(t, imp, "Tomates").ExternalID
	ham := componentByName(t, imp, "Jambon").ExternalID
	mozzarella := componentByName(t, imp, "Mozzarella").ExternalID
	salad := productByName(t, imp, "Salade du chef")

	decisions := aiIngredientDecisions(t, imp)
	decisions.IngredientsPerProduct = map[string][]string{
		// Reine : tomate et jambon retirés.
		productByName(t, imp, "Reine").ExternalID: {mozzarella},
		// Salade : tomate retirée, le reste gardé.
		salad.ExternalID: {salad.Components[1].ComponentExternalID, salad.Components[2].ComponentExternalID},
	}

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	for _, c := range plan.Components {
		if c.ExternalID == tomato || c.ExternalID == ham {
			t.Errorf("%s n'est plus utilisé : il ne doit pas être créé (%v)", c.Name, plannedComponentNames(plan))
		}
	}
	// Plus d'ingrédient en Légumes ni en Viandes : catégories non créées.
	if len(plan.ComponentCategories) != 1 || plan.ComponentCategories[0].Name != "Fromages et crèmerie" {
		t.Errorf("catégories = %+v, want Fromages et crèmerie seule", plan.ComponentCategories)
	}
	if reine := plannedByName(t, plan, "Reine"); len(reine.Components) != 1 {
		t.Errorf("composition de la Reine = %+v, want la mozzarella seule", reine.Components)
	}
}

func TestBuildCommitPlan_AIDoorExcludedProductIngredientsNotCreated(t *testing.T) {
	imp := buildAIIngredientImport(t, aiTestUnitIDs)
	decisions := aiIngredientDecisions(t, imp)
	decisions.ExcludedProducts[productByName(t, imp, "Salade du chef").ExternalID] = true

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	// Œuf et crème ne servaient qu'à la salade exclue.
	if names := plannedComponentNames(plan); !reflect.DeepEqual(names, []string{"Tomates", "Mozzarella", "Jambon"}) {
		t.Errorf("ingrédients créés = %v, want ceux de la Reine", names)
	}
}

func TestBuildCommitPlan_AIDoorRejectsInvalidIngredientDecisions(t *testing.T) {
	imp := buildAIIngredientImport(t, aiTestUnitIDs)
	reine := productByName(t, imp, "Reine").ExternalID
	egg := componentByName(t, imp, "Œufs").ExternalID

	cases := map[string]map[string][]string{
		"ingrédient d'un autre produit": {reine: {egg}},
		"ingrédient inconnu":            {reine: {"ai-i-fantome"}},
		"produit absent":                {"ai-p-fantome": {}},
	}
	for name, ingredients := range cases {
		decisions := aiIngredientDecisions(t, imp)
		decisions.IngredientsPerProduct = ingredients
		_, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
		if codes := blockerCodes(blockers); codes[BlockerInvalidIngredientDecision] != 1 || len(blockers) != 1 {
			t.Errorf("%s : blocages = %s, want un seul %s", name, BlockersMessage(blockers), BlockerInvalidIngredientDecision)
		}
	}
}
