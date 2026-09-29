package importer

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// aiTestPages simule deux photos d'une même carte :
//   - photo 1 : Boissons (groupe Coca-Cola à 3 déclinaisons, un Orangina seul
//     dans un groupe « Orangina », une formule) et Burgers (Classique, avec
//     suppléments) ;
//   - photo 2 : la catégorie Boissons à nouveau, une Limonade sans prix, le
//     Burger Classique en doublon (autre prix), et d'autres « Suppléments »
//     (options différentes).
func aiTestPages() []AIMenuPage {
	return []AIMenuPage{
		{
			Categories: []AICategory{{Ref: "c1", Name: "Boissons"}, {Ref: "c2", Name: "Burgers"}},
			ProductGroups: []AIProductGroup{
				{Ref: "g1", CategoryRef: strPtr("c1"), Name: "Coca-Cola"},
				{Ref: "g2", CategoryRef: strPtr("c1"), Name: "Orangina"},
			},
			Products: []AIProduct{
				{Ref: "p1", CategoryRef: strPtr("c1"), GroupRef: strPtr("g1"), Name: "Coca-Cola Zero", PriceCents: intPtr(350), Kind: "soft_drink_sealed", Confidence: "high"},
				{Ref: "p2", CategoryRef: strPtr("c1"), GroupRef: strPtr("g1"), Name: "Coca-Cola Cherry", PriceCents: intPtr(380), Kind: "soft_drink_sealed", Confidence: "high"},
				{Ref: "p3", CategoryRef: strPtr("c1"), GroupRef: strPtr("g1"), Name: "Coca-Cola Light", PriceCents: intPtr(350), Kind: "soft_drink_sealed", Confidence: "medium"},
				{Ref: "p4", CategoryRef: strPtr("c1"), GroupRef: strPtr("g2"), Name: "Orangina", PriceCents: intPtr(350), Kind: "soft_drink_sealed", Confidence: "high"},
				{Ref: "p5", CategoryRef: strPtr("c2"), Name: "Classique", Description: "Steak, cheddar", PriceCents: intPtr(1250),
					OptionGroupRefs: []string{"o1", "inconnu"}, Kind: "food", Confidence: "high"},
			},
			OptionGroups: []AIOptionGroup{{Ref: "o1", Name: "Suppléments", Min: -1, Max: 9, Options: []AIOption{
				{Title: "Cheddar", ExtraPriceCents: 100}, {Title: "Bacon", ExtraPriceCents: 150}, {Title: "cheddar", ExtraPriceCents: 100},
			}}},
			Formulas: []AIFormula{{Name: "Menu midi", PriceCents: intPtr(1590), Description: "Burger + boisson"}},
		},
		{
			Categories: []AICategory{{Ref: "c1", Name: "boissons"}, {Ref: "c2", Name: "Burgers"}},
			Products: []AIProduct{
				{Ref: "p1", CategoryRef: strPtr("c1"), Name: "Limonade", Kind: "soft_drink_served", Confidence: "low", Issues: []string{"bas de ligne coupé"}},
				{Ref: "p2", CategoryRef: strPtr("c2"), Name: "Classique", PriceCents: intPtr(1290), Kind: "food", Confidence: "high"},
				{Ref: "p3", CategoryRef: strPtr("c2"), Name: "Veggie", PriceCents: intPtr(1190), OptionGroupRefs: []string{"o9"}, Kind: "inconnu", Confidence: "bof"},
			},
			OptionGroups: []AIOptionGroup{{Ref: "o9", Name: "Suppléments", Min: 0, Max: 0, Options: []AIOption{{Title: "Avocat", ExtraPriceCents: 200}}}},
			Warnings:     []string{"reflet sur le bas de la carte"},
		},
	}
}

func buildAITestImport(t *testing.T) *IntermediateImport {
	t.Helper()
	imp, err := BuildAIMenuImport(aiTestPages())
	if err != nil {
		t.Fatalf("BuildAIMenuImport: %v", err)
	}
	return imp
}

func productByName(t *testing.T, imp *IntermediateImport, name string) CanonicalProduct {
	t.Helper()
	for _, p := range imp.Products {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("produit %q absent", name)
	return CanonicalProduct{}
}

func TestAIMenuPageSchema_RespectsStructuredOutputConstraints(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(AIMenuPageSchema, &schema); err != nil {
		t.Fatalf("schéma JSON invalide: %v", err)
	}

	// Chaque objet : additionalProperties false et toutes ses propriétés
	// requises (un champ facultatif s'exprime par anyOf avec null).
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch v := node.(type) {
		case map[string]any:
			if v["type"] == "object" {
				if v["additionalProperties"] != false {
					t.Errorf("%s : additionalProperties doit valoir false", path)
				}
				props, _ := v["properties"].(map[string]any)
				required, _ := v["required"].([]any)
				if len(required) != len(props) {
					t.Errorf("%s : %d propriétés, %d requises", path, len(props), len(required))
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
	walk("$", schema)

	// L'enum des natures du schéma suit AllProductKinds.
	products := schema["properties"].(map[string]any)["products"].(map[string]any)
	kindEnum := products["items"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
	var want []any
	for _, k := range AllProductKinds {
		want = append(want, string(k))
	}
	if !reflect.DeepEqual(kindEnum, want) {
		t.Errorf("enum kind du schéma = %v, want %v", kindEnum, want)
	}
}

func TestBuildAIMenuImport_MergesCategoriesAcrossPhotos(t *testing.T) {
	imp := buildAITestImport(t)

	var names []string
	for _, c := range imp.Categories {
		names = append(names, c.Name)
	}
	if want := []string{"Boissons", "Burgers"}; !reflect.DeepEqual(names, want) {
		t.Errorf("catégories = %v, want %v (« boissons » de la photo 2 fusionnée)", names, want)
	}
	if productByName(t, imp, "Limonade").CategoryExternalID != imp.Categories[0].ExternalID {
		t.Errorf("Limonade (photo 2) doit être dans la catégorie Boissons de la photo 1")
	}
}

func TestBuildAIMenuImport_GroupsNeedTwoVariantsAndComeFirst(t *testing.T) {
	imp := buildAITestImport(t)

	group := imp.Products[0]
	if !group.IsGroup || group.Name != "Coca-Cola" {
		t.Fatalf("premier produit = %+v, want le groupe Coca-Cola en tête", group)
	}
	if group.AllPricesZero || group.PriceIn != 0 {
		t.Errorf("groupe : prix %d, AllPricesZero %v ; want prix nul sans statut removed_from_menu", group.PriceIn, group.AllPricesZero)
	}
	if group.Kind != KindSoftDrinkSealed || *group.TvaRateTakeAway != 5.5 {
		t.Errorf("groupe : nature %q, TVA emporter %v ; want celle de sa première déclinaison", group.Kind, *group.TvaRateTakeAway)
	}
	for _, name := range []string{"Coca-Cola Zero", "Coca-Cola Cherry", "Coca-Cola Light"} {
		if productByName(t, imp, name).ParentExternalID != group.ExternalID {
			t.Errorf("%s doit être rattaché au groupe Coca-Cola", name)
		}
	}

	// Orangina seul dans son groupe : pas de groupe, produit à la racine.
	if productByName(t, imp, "Orangina").ParentExternalID != "" {
		t.Errorf("Orangina, seule déclinaison, doit rester à la racine")
	}
	for _, p := range imp.Products {
		if p.IsGroup && p.Name == "Orangina" {
			t.Errorf("le groupe Orangina ne doit pas être créé")
		}
	}
	if !hasSourceWarning(imp, WarningAIGroupDissolved) {
		t.Errorf("groupe dissous non signalé")
	}
}

func TestBuildAIMenuImport_DuplicateAcrossPhotosKeptOnce(t *testing.T) {
	imp := buildAITestImport(t)

	count := 0
	for _, p := range imp.Products {
		if p.Name == "Classique" {
			count++
			if p.PriceIn != 1250 || p.SourcePhoto != 1 {
				t.Errorf("Classique gardé = prix %d photo %d, want la première lecture (1250, photo 1)", p.PriceIn, p.SourcePhoto)
			}
		}
	}
	if count != 1 {
		t.Errorf("Classique présent %d fois, want 1", count)
	}
	w := sourceWarning(t, imp, WarningAIDuplicateMerged)
	if !strings.Contains(w.Message, "12,50 €") || !strings.Contains(w.Message, "12,90 €") {
		t.Errorf("avertissement de doublon = %q, want les deux prix", w.Message)
	}
}

func TestBuildAIMenuImport_PricesKindsAndConfidence(t *testing.T) {
	imp := buildAITestImport(t)

	zero := productByName(t, imp, "Coca-Cola Zero")
	if zero.PriceIn != 350 || zero.PriceTakeAway != 350 || zero.PriceDelivery != 350 {
		t.Errorf("prix unique recopié sur les trois canaux : %d/%d/%d", zero.PriceIn, zero.PriceTakeAway, zero.PriceDelivery)
	}
	if *zero.TvaRateIn != 10 || *zero.TvaRateTakeAway != 5.5 || *zero.TvaRateDelivery != 5.5 {
		t.Errorf("TVA boisson fermée = %v/%v/%v, want 10/5.5/5.5", *zero.TvaRateIn, *zero.TvaRateTakeAway, *zero.TvaRateDelivery)
	}

	limonade := productByName(t, imp, "Limonade")
	if !limonade.AllPricesZero || !contains(limonade.Issues, "prix absent ou illisible") || !contains(limonade.Issues, "bas de ligne coupé") {
		t.Errorf("Limonade sans prix = %+v, want AllPricesZero et les deux problèmes signalés", limonade)
	}

	veggie := productByName(t, imp, "Veggie")
	if veggie.Kind != KindOther || veggie.TvaRateIn != nil {
		t.Errorf("nature inconnue = %q / %v, want other sans taux", veggie.Kind, veggie.TvaRateIn)
	}
	if veggie.Confidence != "low" {
		t.Errorf("confiance inconnue = %q, want low", veggie.Confidence)
	}
}

func TestBuildAIMenuImport_OptionGroups(t *testing.T) {
	imp := buildAITestImport(t)

	if len(imp.Attributes) != 2 {
		t.Fatalf("groupes d'options = %d, want 2 (même nom, options différentes)", len(imp.Attributes))
	}
	first := imp.Attributes[0]
	if len(first.Options) != 2 || first.MinOptions != 0 || first.MaxOptions != 2 || first.IsRequired {
		t.Errorf("Suppléments burgers = %+v, want 2 options dédoublonnées, min 0, max ramené à 2", first)
	}

	classique := productByName(t, imp, "Classique")
	if !reflect.DeepEqual(classique.AttributeExternalIDs, []string{first.ExternalID}) {
		t.Errorf("options de Classique = %v, want seulement le groupe connu", classique.AttributeExternalIDs)
	}
	if productByName(t, imp, "Veggie").AttributeExternalIDs[0] != imp.Attributes[1].ExternalID {
		t.Errorf("Veggie doit porter son propre groupe Suppléments")
	}
}

func TestBuildAIMenuImport_FormulasAndPhotoWarnings(t *testing.T) {
	imp := buildAITestImport(t)

	formula := sourceWarning(t, imp, WarningAIFormulaNotCreated)
	if !strings.Contains(formula.Message, "Menu midi") || !strings.Contains(formula.Message, "15,90 €") {
		t.Errorf("formule = %q", formula.Message)
	}
	if w := sourceWarning(t, imp, WarningAIPhoto); w.Ref != "photo 2" {
		t.Errorf("avertissement de photo = %+v, want ref photo 2", w)
	}
}

func TestBuildAIMenuImport_NoProducts(t *testing.T) {
	if _, err := BuildAIMenuImport([]AIMenuPage{{Warnings: []string{"photo illisible"}}}); err != ErrNoProducts {
		t.Fatalf("err = %v, want ErrNoProducts", err)
	}
}

func TestBuildPreview_AIDoorProposesKindsGroupsAndWarnings(t *testing.T) {
	imp := buildAITestImport(t)
	res, err := BuildPreview(imp, defaultLookups())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}

	group := imp.Products[0]
	zero := productByName(t, imp, "Coca-Cola Zero")
	if res.Decisions.KindPerProduct[zero.ExternalID] != KindSoftDrinkSealed {
		t.Errorf("nature proposée = %q", res.Decisions.KindPerProduct[zero.ExternalID])
	}
	if res.Decisions.GroupPerProduct[zero.ExternalID] != group.ExternalID {
		t.Errorf("groupe proposé = %q", res.Decisions.GroupPerProduct[zero.ExternalID])
	}
	if res.Decisions.TvaConfirmed {
		t.Errorf("la TVA ne doit jamais être proposée comme confirmée")
	}

	entry := previewProduct(t, res, group.ExternalID)
	if !entry.IsGroup || entry.Status != ProductStatusAvailable {
		t.Errorf("groupe en preview = %+v, want is_group et statut available", entry)
	}
	if limonade := previewProduct(t, res, productByName(t, imp, "Limonade").ExternalID); limonade.SourcePhoto != 2 || limonade.Confidence != "low" {
		t.Errorf("Limonade en preview = photo %d confiance %q", limonade.SourcePhoto, limonade.Confidence)
	}
	for _, code := range []string{WarningAILowConfidence, WarningAIFormulaNotCreated, WarningAIDuplicateMerged, WarningAIPhoto} {
		if !hasWarning(res, code) {
			t.Errorf("avertissement %s absent de la preview", code)
		}
	}
}

func hasSourceWarning(imp *IntermediateImport, code string) bool {
	for _, w := range imp.SourceWarnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func sourceWarning(t *testing.T, imp *IntermediateImport, code string) SourceWarning {
	t.Helper()
	for _, w := range imp.SourceWarnings {
		if w.Code == code {
			return w
		}
	}
	t.Fatalf("signalement %s absent", code)
	return SourceWarning{}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
