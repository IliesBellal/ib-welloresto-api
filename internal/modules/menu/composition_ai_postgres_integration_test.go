//go:build postgres_integration

package menu

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Composition par IA contre Postgres : lecture des produits et de leur
// composition, puis application en mode add et replace. Vérifie qu'une ligne
// gardée conserve sa quantité, qu'un ingrédient existant est réutilisé par
// nom (pluriel, casse) et que les manquants sont créés avec leur catégorie.
func TestCompositionAI_Postgres_LoadAndApply(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM requires WHERE recipe_id IN (SELECT recipe_id FROM recipes WHERE merchant_id = $1)`,
			`DELETE FROM recipes WHERE merchant_id = $1`,
			`DELETE FROM components WHERE merchant_id = $1`,
			`DELETE FROM component_category WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_id = $1`,
			`DELETE FROM productcateg WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			if _, err := db.ExecContext(ctx, q, mid); err != nil {
				t.Logf("cleanup %q: %v", q, err)
			}
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-compo-ia' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Compo IA', 'a', '1', 's', '75001', 'Paris', 'siret-compo-ia', 'https://x', '06', 'mtok-compo-ia', 'Europe/Paris')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	repo := NewMenuRepository(db, nil)
	units, err := NewAIDraftRepository(db).UnitIDsByCode(ctx)
	if err != nil || units["G"] == "" {
		t.Fatalf("unit_of_measure : %v (G = %q)", err, units["G"])
	}

	cat, err := repo.CreateProductCategory(ctx, &CreateProductCategoryPayload{Name: "compo itest", MerchantID: merchantID})
	if err != nil {
		t.Fatalf("CreateProductCategory: %v", err)
	}
	newProduct := func(name, desc string) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_id, name, product_desc, price, price_take_away, price_delivery,
				tva_in_id, tva_delivery_id, tva_take_away_id, category, status, enabled)
			VALUES ($1, $2, $3, 1200, 1200, 1200, 1, 2, 3, $4, 'available', TRUE)
			RETURNING product_id`, merchantID, name, desc, cat).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return strconv.FormatInt(id, 10)
	}
	burger := newProduct("Burger itest", "Steak, tomate, cheddar")
	salade := newProduct("Salade itest", "Roquette")

	vegCat, err := repo.CreateComponentCategory(ctx, &UpsertComponentCategoryPayload{Name: "Légumes", MerchantID: merchantID})
	if err != nil {
		t.Fatalf("CreateComponentCategory: %v", err)
	}
	vegMerchantCateg, err := repo.readComponentCategoryMerchantIDTx(ctx, merchantID, mustAtoi(t, vegCat))
	if err != nil {
		t.Fatalf("merchant_categ_id: %v", err)
	}
	tomatoName, oignonName, zero, g := "Tomates", "Oignon", 0, units["G"]
	tomato, err := repo.CreateComponent(ctx, &UpdateComponentPayload{Name: &tomatoName, Price: &zero, MerchantID: merchantID, CategoryID: &vegMerchantCateg, UnitID: &g})
	if err != nil {
		t.Fatalf("CreateComponent(Tomates): %v", err)
	}
	oignon, err := repo.CreateComponent(ctx, &UpdateComponentPayload{Name: &oignonName, Price: &zero, MerchantID: merchantID, CategoryID: &vegMerchantCateg, UnitID: &g})
	if err != nil {
		t.Fatalf("CreateComponent(Oignon): %v", err)
	}
	// Composition saisie à la main : 30 g de tomates, 10 g d'oignon.
	if err := repo.SyncProductComponents(ctx, merchantID, burger, []ProductComponentUpdate{
		{ComponentID: tomato, Quantity: 30, UnitID: g},
		{ComponentID: oignon, Quantity: 10, UnitID: g},
	}); err != nil {
		t.Fatalf("SyncProductComponents: %v", err)
	}

	products, err := repo.LoadCompositionProducts(ctx, merchantID, []string{burger, salade, "999999999"})
	if err != nil {
		t.Fatalf("LoadCompositionProducts: %v", err)
	}
	if len(products) != 2 || len(products[0].Lines) != 2 || products[0].Description != "Steak, tomate, cheddar" {
		t.Fatalf("produits lus = %+v", products)
	}

	// Burger en replace : Tomate (existante, gardée avec ses 30 g), Steak et
	// Cheddar (créés), Oignon retiré. Salade en add : Roquette créée.
	res, err := repo.ApplyAICompositions(ctx, merchantID, []AICompositionApplyProduct{
		{ProductID: burger, Mode: compositionModeReplace, Ingredients: []AICompositionIngredient{
			{Name: "tomate", Category: "vegetables", Unit: "G"},
			{Name: "Steak", Category: "meat", Unit: "G"},
			{Name: "Cheddar", Category: "dairy", Unit: "G"},
		}},
		{ProductID: salade, Mode: compositionModeAdd, Ingredients: []AICompositionIngredient{
			{Name: "Roquette", Category: "vegetables", Unit: "G"},
		}},
	}, units)
	if err != nil {
		t.Fatalf("ApplyAICompositions: %v", err)
	}
	if res.ProductsUpdated != 2 || res.ComponentsCreated != 3 || res.ComponentCategoriesCreated != 2 {
		t.Errorf("résultat = %+v, want 2 produits, 3 ingrédients créés, 2 catégories (Viandes, Fromages et crèmerie)", res)
	}

	after, err := repo.LoadCompositionProducts(ctx, merchantID, []string{burger, salade})
	if err != nil {
		t.Fatalf("relecture: %v", err)
	}
	lines := map[string]float64{}
	for _, l := range after[0].Lines {
		lines[l.Name] = l.Quantity
	}
	if len(lines) != 3 || lines["Tomates"] != 30 || lines["Steak"] != 0 || lines["Cheddar"] != 0 {
		t.Errorf("burger après replace = %v, want Tomates 30 (gardée), Steak 0, Cheddar 0, sans Oignon", lines)
	}
	if len(after[1].Lines) != 1 || after[1].Lines[0].Name != "Roquette" {
		t.Errorf("salade après add = %+v", after[1].Lines)
	}

	// Rejouer la même application ne change rien.
	again, err := repo.ApplyAICompositions(ctx, merchantID, []AICompositionApplyProduct{
		{ProductID: salade, Mode: compositionModeAdd, Ingredients: []AICompositionIngredient{{Name: "Roquette", Category: "vegetables", Unit: "G"}}},
	}, units)
	if err != nil || again.ProductsUnchanged != 1 || again.ComponentsCreated != 0 {
		t.Errorf("rejeu = %+v, %v ; want inchangé, aucun ingrédient créé", again, err)
	}

	// Un produit d'un autre marchand refuse tout le lot.
	if _, err := repo.ApplyAICompositions(ctx, merchantID, []AICompositionApplyProduct{
		{ProductID: "999999999", Mode: compositionModeAdd},
	}, units); err == nil {
		t.Errorf("produit inconnu accepté")
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", s, err)
	}
	return n
}
