//go:build postgres_integration

package menu

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Vérification réelle du module menu contre le Postgres de dev — chaque
// variante de SQL dynamique (IN (...), SET construits, multi-VALUES, upserts)
// est exécutée réellement, pas seulement compilée.
func TestMenuRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure_convert WHERE id_from IN (SELECT id FROM unit_of_measure_desc WHERE uom_desc LIKE 'itest-menu%')`)
		_, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure WHERE id IN (SELECT id FROM unit_of_measure_desc WHERE uom_desc LIKE 'itest-menu%')`)
		_, _ = db.ExecContext(ctx, `DELETE FROM unit_of_measure_desc WHERE uom_desc LIKE 'itest-menu%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE 'itest-menu%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM delays WHERE short_description = 'IT-MENU'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM allergens WHERE allergen_id LIKE 'itest-menu%'`)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM product_marketing_categories WHERE merchant_id = $1`,
			`DELETE FROM marketing_categories WHERE merchant_id = $1`,
			`DELETE FROM product_tags WHERE tag_id IN (SELECT tag_id FROM tags WHERE merchant_id = $1)`,
			`DELETE FROM tags WHERE merchant_id = $1`,
			`DELETE FROM product_allergens WHERE product_id IN (SELECT product_id::text FROM products WHERE merchant_Id = $1)`,
			`DELETE FROM requires WHERE recipe_id IN (SELECT recipe_id FROM recipes WHERE merchant_id = $1)`,
			`DELETE FROM recipes WHERE merchant_id = $1`,
			`DELETE FROM product_configurable_attribute WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM productcateg WHERE merchant_id = $1`,
			`DELETE FROM components WHERE merchant_id = $1`,
			`DELETE FROM component_category WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-menu' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	// --- seeds ---
	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Menu Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-menu', 'https://x', '06', 'mtok-menu', 'Europe/Paris')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	var unitG, unitKG int64
	if err := db.QueryRowContext(ctx, `INSERT INTO unit_of_measure (UOM) VALUES ('g') RETURNING id`).Scan(&unitG); err != nil {
		t.Fatalf("seed unit g: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO unit_of_measure (UOM) VALUES ('kg') RETURNING id`).Scan(&unitKG); err != nil {
		t.Fatalf("seed unit kg: %v", err)
	}
	for _, row := range []struct {
		id   int64
		desc string
	}{{unitG, "itest-menu-gramme"}, {unitKG, "itest-menu-kilo"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO unit_of_measure_desc (id, lang, uom_desc, uom_short_desc) VALUES ($1, 'FR', $2, $3)`, row.id, row.desc, row.desc); err != nil {
			t.Fatalf("seed unit desc: %v", err)
		}
	}
	// ratio : il faut 1000 g pour 1 kg
	if _, err := db.ExecContext(ctx, `INSERT INTO unit_of_measure_convert (id_from, id_to, ratio) VALUES ($1, $2, 1000)`, unitG, unitKG); err != nil {
		t.Fatalf("seed unit convert: %v", err)
	}

	// une ligne TVA par type de livraison (le COUNT(*) de la validation de
	// CreateProduct exige 3 lignes distinctes, comme le référentiel réel)
	newTva := func(deliveryType, title string) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
			VALUES ($1, $2, 'd', 10) RETURNING tva_id`, deliveryType, title).Scan(&id); err != nil {
			t.Fatalf("seed tva %s: %v", title, err)
		}
		return strconv.FormatInt(id, 10)
	}
	tvaStr := newTva("0", "itest-menu-tva-in")
	tvaTakeAway := newTva("3", "itest-menu-tva-ta")
	tvaDelivery := newTva("1", "itest-menu-tva-del")

	if _, err := db.ExecContext(ctx, `INSERT INTO delays (description, short_description, duration) VALUES ('itest menu', 'IT-MENU', 7)`); err != nil {
		t.Fatalf("seed delays: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO allergens (allergen_id, name, code, icon, color) VALUES ('itest-menu-alg', 'Gluten itest', 'GLU', 'ic', '#fff')`); err != nil {
		t.Fatalf("seed allergens: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tags (tag_id, merchant_id, name, color) VALUES ('itest-menu-tag', $1, 'Tag itest', '#000')`, merchantID); err != nil {
		t.Fatalf("seed tags: %v", err)
	}

	repo := NewMenuRepository(db)

	// --- GetUnitsOfMeasures (casts texte + conversions) ---
	units, err := repo.GetUnitsOfMeasures(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetUnitsOfMeasures: %v", err)
	}
	gID := strconv.FormatInt(unitG, 10)
	foundG := false
	for _, u := range units {
		if u.ID == gID {
			foundG = true
			hasConv := false
			for _, c := range u.Conversions {
				if c.ToUnitID == strconv.FormatInt(unitKG, 10) && c.Multiplier == 1.0/1000.0 {
					hasConv = true
				}
			}
			if !hasConv {
				t.Fatalf("conversion g->kg absente: %+v", u.Conversions)
			}
		}
	}
	if !foundG {
		t.Fatalf("unité g absente de GetUnitsOfMeasures (%d unités)", len(units))
	}

	// --- catégories + produits ---
	catID, err := repo.CreateProductCategory(ctx, &CreateProductCategoryPayload{Name: "entrees itest", MerchantID: merchantID})
	if err != nil || catID == "0" {
		t.Fatalf("CreateProductCategory = (%q, %v)", catID, err)
	}
	var mcatID string
	if err := db.QueryRowContext(ctx, `SELECT merchant_categ_id FROM productcateg WHERE categ_id = $1`, catID).Scan(&mcatID); err != nil || mcatID != catID {
		t.Fatalf("merchant_categ_id = (%q, %v), want %q", mcatID, err, catID)
	}

	newProduct := func(name string) string {
		t.Helper()
		id, err := repo.CreateProduct(ctx, &CreateProductPayload{
			MerchantID: merchantID, Name: name, ProductDesc: "desc " + name,
			Price: 1000.4, PriceTakeAway: 900, PriceDelivery: 1100,
			TvaInID: tvaStr, TvaDeliveryID: tvaDelivery, TvaTakeAwayID: tvaTakeAway,
			CategoryID: catID,
		})
		if err != nil || id == "0" {
			t.Fatalf("CreateProduct(%s) = (%q, %v)", name, id, err)
		}
		return id
	}
	prodA := newProduct("itest-menu-plat")
	prodB := newProduct("itest-menu-dessert")
	prodC := newProduct("itest-menu-sous-produit")

	// arrondi float64 -> int vérifié (1000.4 -> 1000)
	var storedPrice int
	if err := db.QueryRowContext(ctx, `SELECT price FROM products WHERE product_id = $1`, prodA).Scan(&storedPrice); err != nil || storedPrice != 1000 {
		t.Fatalf("price stocké = (%d, %v), want 1000", storedPrice, err)
	}

	// tva non numérique -> refus identique à MySQL (0 = aucune correspondance)
	if _, err := repo.CreateProduct(ctx, &CreateProductPayload{
		MerchantID: merchantID, Name: "x", Price: 100,
		TvaInID: "abc", TvaDeliveryID: tvaDelivery, TvaTakeAwayID: tvaTakeAway, CategoryID: catID,
	}); err == nil {
		t.Fatalf("CreateProduct(tva non numérique) devrait échouer")
	}

	// prodC devient sous-produit de prodA (by_product_of, COALESCE dynamiques)
	if err := repo.UpdateProduct(ctx, merchantID, prodC, ProductUpdatePayload{ByProductOf: &prodA}); err != nil {
		t.Fatalf("UpdateProduct(sous-produit): %v", err)
	}

	// --- composants ---
	compCatID, err := repo.CreateComponentCategory(ctx, &UpsertComponentCategoryPayload{Name: "frais itest", MerchantID: merchantID})
	if err != nil || compCatID == "0" {
		t.Fatalf("CreateComponentCategory = (%q, %v)", compCatID, err)
	}
	unitGStr := strconv.FormatInt(unitG, 10)
	unitKGStr := strconv.FormatInt(unitKG, 10)
	compName := "olives itest"
	compPrice := 200
	purchaseCost := 850
	compID, err := repo.CreateComponent(ctx, &UpdateComponentPayload{
		MerchantID: merchantID, Name: &compName, Price: &compPrice,
		CategoryID: &compCatID, UnitID: &unitGStr, PurchaseUnitID: &unitKGStr,
		PurchaseCost: &purchaseCost,
	})
	if err != nil || compID == "0" {
		t.Fatalf("CreateComponent = (%q, %v)", compID, err)
	}

	// unité non numérique -> refus identique à MySQL
	badUnit := "abc"
	if _, err := repo.CreateComponent(ctx, &UpdateComponentPayload{
		MerchantID: merchantID, Name: &compName, Price: &compPrice, CategoryID: &compCatID, UnitID: &badUnit,
	}); err == nil {
		t.Fatalf("CreateComponent(unité non numérique) devrait échouer")
	}

	// UpdateComponent : SET dynamique
	newCompName := "olives noires itest"
	qty := 430.0
	tempMin := 2.5
	if err := repo.UpdateComponent(ctx, merchantID, compID, &UpdateComponentPayload{
		Name: &newCompName, PurchaseCostQty: &qty, StorageTempMin: &tempMin,
	}); err != nil {
		t.Fatalf("UpdateComponent: %v", err)
	}
	comp, err := repo.GetComponent(ctx, merchantID, compID)
	if err != nil || comp.Name != "Olives noires itest" || comp.StorageTempMin == nil || *comp.StorageTempMin != 2.5 {
		t.Fatalf("GetComponent = (%+v, %v)", comp, err)
	}
	if comp.PurchaseUnitOfMeasureID != unitKGStr || comp.PurchaseUnitOfMeasure != "itest-menu-kilo" {
		t.Fatalf("GetComponent unités achat = %+v", comp)
	}

	// --- attributs configurables ---
	enabledTrue := true
	maxQ := 2
	attrID, err := repo.CreateAttribute(ctx, merchantID, &UpdateAttributePayload{
		Type: "CHECK", Name: "sauce-itest", Title: "Sauce ?", Min: 0, Max: 2,
		Options: []UpdateAttributeOptionPayload{
			{Title: "Ketchup", Price: 50, MaxQuantity: &maxQ, Enabled: &enabledTrue},
			{Title: "Mayo", Price: 0, Enabled: &enabledTrue},
		},
	})
	if err != nil || attrID == "" {
		t.Fatalf("CreateAttribute = (%q, %v)", attrID, err)
	}
	attr, err := repo.GetAttribute(ctx, merchantID, attrID)
	if err != nil || len(attr.Options) != 2 {
		t.Fatalf("GetAttribute = (%+v, %v), want 2 options", attr, err)
	}
	attrs, err := repo.GetAttributes(ctx, merchantID)
	if err != nil || len(attrs) != 1 || attrs[0].ID != attrID {
		t.Fatalf("GetAttributes = (%d, %v)", len(attrs), err)
	}

	// UpdateAttribute : maj d'une option existante + création d'une nouvelle
	opt0 := attr.Options[0]
	if err := repo.UpdateAttribute(ctx, merchantID, attrID, &UpdateAttributePayload{
		Type: "CHECK", Name: "sauce-itest", Title: "Sauces ?", Min: 0, Max: 3,
		Options: []UpdateAttributeOptionPayload{
			{ID: &opt0.ID, Title: "Ketchup bio", Price: 60, Enabled: &enabledTrue},
			{Title: "Harissa", Price: 30, Enabled: &enabledTrue},
		},
	}); err != nil {
		t.Fatalf("UpdateAttribute: %v", err)
	}
	attr, err = repo.GetAttribute(ctx, merchantID, attrID)
	if err != nil || attr.Title != "Sauces ?" || len(attr.Options) != 2 {
		t.Fatalf("GetAttribute après update = (%+v, %v)", attr, err)
	}

	// image d'option (UPDATE ... FROM + id non numérique)
	if err := repo.UpdateAttributeOptionImageURL(ctx, merchantID, opt0.ID, "https://img/itest.png"); err != nil {
		t.Fatalf("UpdateAttributeOptionImageURL: %v", err)
	}
	if url, err := repo.GetAttributeOptionImageURL(ctx, merchantID, opt0.ID); err != nil || url != "https://img/itest.png" {
		t.Fatalf("GetAttributeOptionImageURL = (%q, %v)", url, err)
	}
	if err := repo.UpdateAttributeOptionImageURL(ctx, merchantID, "cao-legacy-id", "x"); err == nil {
		t.Fatalf("UpdateAttributeOptionImageURL(id non numérique) devrait échouer comme MySQL (0 ligne)")
	}
	if url, err := repo.GetAttributeOptionImageURL(ctx, merchantID, "cao-legacy-id"); err != nil || url != "" {
		t.Fatalf("GetAttributeOptionImageURL(id non numérique) = (%q, %v), want vide", url, err)
	}

	// --- UpdateProduct complet (intégrations, configuration, composition, tags, allergènes) ---
	priceOverride := 1250
	if err := repo.UpdateProduct(ctx, merchantID, prodA, ProductUpdatePayload{
		Integrations: models.ProductIntegrations{
			UberEats: models.ProductIntegrationItem{Enabled: true, PriceOverride: &priceOverride},
		},
		Configuration: []string{attrID},
		Components:    []ProductComponentUpdate{{ComponentID: compID, Quantity: 100, UnitID: unitGStr}},
		Tags:          []string{"itest-menu-tag"},
		Allergens:     []string{"itest-menu-alg"},
	}); err != nil {
		t.Fatalf("UpdateProduct(prodA complet): %v", err)
	}

	p, err := repo.GetProduct(ctx, merchantID, prodA)
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if len(p.Components) != 1 || p.Components[0].ComponentID != compID {
		t.Fatalf("GetProduct components = %+v", p.Components)
	}
	if len(p.Configuration.Attributes) != 1 || len(p.Configuration.Attributes[0].Options) != 2 {
		t.Fatalf("GetProduct configuration = %+v", p.Configuration)
	}
	if len(p.Allergens) != 1 || p.Allergens[0].ID != "itest-menu-alg" {
		t.Fatalf("GetProduct allergens = %+v", p.Allergens)
	}
	if len(p.Tags) != 1 || p.Tags[0].ID != "itest-menu-tag" {
		t.Fatalf("GetProduct tags = %+v", p.Tags)
	}
	if !p.Integrations.UberEats.Enabled || p.Integrations.UberEats.PriceOverride == nil || *p.Integrations.UberEats.PriceOverride != 1250 {
		t.Fatalf("GetProduct integrations = %+v", p.Integrations)
	}

	// --- GetMenu (assemblage complet + no_update_required) ---
	menu, err := repo.GetMenu(ctx, merchantID, nil)
	if err != nil || menu.Status != "ok" {
		t.Fatalf("GetMenu = (%+v, %v)", menu.Status, err)
	}
	var menuCat *models.ProductCategory
	for i := range menu.ProductsTypes {
		if menu.ProductsTypes[i].CategoryID != nil && *menu.ProductsTypes[i].CategoryID == catID {
			menuCat = &menu.ProductsTypes[i]
		}
	}
	if menuCat == nil || len(menuCat.Products) != 2 {
		t.Fatalf("GetMenu catégorie itest absente ou mauvais compte de produits racines: %+v", menuCat)
	}
	foundSub := false
	for _, mp := range menuCat.Products {
		if mp.ProductID == prodA {
			if len(mp.Components) != 1 || len(mp.Configuration.Attributes) != 1 {
				t.Fatalf("GetMenu prodA compo/config = %+v", mp)
			}
			for _, sp := range mp.SubProducts {
				if sp.ProductID == prodC {
					foundSub = true
				}
			}
		}
	}
	if !foundSub {
		t.Fatalf("GetMenu: sous-produit prodC non rattaché à prodA")
	}
	foundDelay := false
	for _, d := range menu.Delays {
		if d.ShortDescription == "IT-MENU" {
			foundDelay = true
		}
	}
	if !foundDelay {
		t.Fatalf("GetMenu: delay itest absent")
	}

	var lastMenu time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_menu_update FROM merchant_parameters WHERE merchant_id = $1`, merchantID).Scan(&lastMenu); err != nil {
		t.Fatalf("read last_menu_update: %v", err)
	}
	menu2, err := repo.GetMenu(ctx, merchantID, &lastMenu)
	if err != nil || menu2.Status != "no_update_required" {
		t.Fatalf("GetMenu(lastMenu à jour) = (%q, %v), want no_update_required", menu2.Status, err)
	}

	// --- GetAllProducts / GetAllComponents (foodcost, jointures conversions) ---
	allProds, err := repo.GetAllProducts(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetAllProducts: %v", err)
	}
	foundFoodcost := false
	for _, cat := range allProds {
		for _, ap := range cat.Products {
			if ap.ProductID == prodA {
				// conv id_from=g id_to=unité de VENTE (g) -> aucune ligne, ratio 1 :
				// (100 / 1) * (850 / 430) = 197.67 (arrondi 2 déc.)
				if ap.CostPrice == nil || *ap.CostPrice != 197.67 {
					got := -1.0
					if ap.CostPrice != nil {
						got = *ap.CostPrice
					}
					t.Fatalf("GetAllProducts foodcost prodA = %v", got)
				}
				foundFoodcost = true
			}
		}
	}
	if !foundFoodcost {
		t.Fatalf("GetAllProducts: prodA absent")
	}

	allComps, err := repo.GetAllComponents(ctx, merchantID)
	if err != nil || len(allComps) != 1 || len(allComps[0].Components) != 1 {
		t.Fatalf("GetAllComponents = (%+v, %v)", allComps, err)
	}

	// --- upsell (by_product_of = 0/NULL, booléens) ---
	ups, err := repo.ListAvailableProductsForUpsell(ctx, merchantID)
	if err != nil || len(ups) != 2 {
		t.Fatalf("ListAvailableProductsForUpsell = (%d, %v), want 2 racines", len(ups), err)
	}

	// --- statuts / disponibilités ---
	if n, err := repo.SetProductStatus(ctx, merchantID, prodB, "unavailable_today"); err != nil || n != 1 {
		t.Fatalf("SetProductStatus = (%d, %v)", n, err)
	}
	if n, err := repo.SetComponentStatus(ctx, merchantID, compID, "0"); err != nil || n != 1 {
		t.Fatalf("SetComponentStatus = (%d, %v)", n, err)
	}
	if n, err := repo.SetProductAvailability(ctx, merchantID, prodB, "0"); err != nil || n != 1 {
		t.Fatalf("SetProductAvailability = (%d, %v)", n, err)
	}
	if n, err := repo.SetProductCategoryAvailability(ctx, merchantID, catID, "true"); err != nil {
		t.Fatalf("SetProductCategoryAvailability = (%d, %v)", n, err)
	}
	if err := repo.UpdateProductCategory(ctx, merchantID, catID, "entrees itest v2"); err != nil {
		t.Fatalf("UpdateProductCategory: %v", err)
	}

	// --- BulkAssignProductsToCategory (IN dynamique ×2 : racines + sous-produits) ---
	catID2, err := repo.CreateProductCategory(ctx, &CreateProductCategoryPayload{Name: "plats itest", MerchantID: merchantID})
	if err != nil {
		t.Fatalf("CreateProductCategory 2: %v", err)
	}
	if err := repo.BulkAssignProductsToCategory(ctx, merchantID, catID2, []string{prodA, prodB}); err != nil {
		t.Fatalf("BulkAssignProductsToCategory: %v", err)
	}
	var subCat string
	if err := db.QueryRowContext(ctx, `SELECT category FROM products WHERE product_id = $1`, prodC).Scan(&subCat); err != nil || subCat != catID2 {
		t.Fatalf("catégorie du sous-produit après bulk = (%q, %v), want %q", subCat, err, catID2)
	}

	// --- tags en masse (IN + multi-VALUES) ---
	if err := repo.BulkAssignTag(ctx, merchantID, "itest-menu-tag", []string{prodB}); err != nil {
		t.Fatalf("BulkAssignTag: %v", err)
	}
	if err := repo.BulkAssignProductsToTag(ctx, merchantID, "itest-menu-tag", []string{prodA, prodC}); err != nil {
		t.Fatalf("BulkAssignProductsToTag: %v", err)
	}
	var tagCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_tags WHERE tag_id = 'itest-menu-tag'`).Scan(&tagCount); err != nil || tagCount != 2 {
		t.Fatalf("product_tags après remplacement = (%d, %v), want 2", tagCount, err)
	}

	// --- allergènes en masse (IN + INSERT IGNORE / ON CONFLICT DO NOTHING, idempotent) ---
	if err := repo.BulkAssignAllergen(ctx, merchantID, "itest-menu-alg", []string{prodA, prodB}); err != nil {
		t.Fatalf("BulkAssignAllergen: %v", err) // prodA a déjà l'allergène -> ignoré
	}
	var algCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_allergens WHERE allergen_id = 'itest-menu-alg'`).Scan(&algCount); err != nil || algCount != 2 {
		t.Fatalf("product_allergens = (%d, %v), want 2", algCount, err)
	}

	// --- prix en masse (SET dynamique) ---
	newPrice := 1111
	newUber := 1333
	if err := repo.BulkUpdateProductPrices(ctx, merchantID, []BulkUpdateProductPrice{
		{ProductID: prodA, Price: &newPrice, PriceUberEats: &newUber},
	}); err != nil {
		t.Fatalf("BulkUpdateProductPrices: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT price FROM products WHERE product_id = $1`, prodA).Scan(&storedPrice); err != nil || storedPrice != 1111 {
		t.Fatalf("prix après bulk = (%d, %v)", storedPrice, err)
	}

	// --- UpdateProductAttributes (upsert ON CONFLICT, idempotent) ---
	if err := repo.UpdateProductAttributes(ctx, merchantID, prodA, []string{attrID}); err != nil {
		t.Fatalf("UpdateProductAttributes: %v", err)
	}
	if err := repo.UpdateProductAttributes(ctx, merchantID, prodA, []string{attrID}); err != nil {
		t.Fatalf("UpdateProductAttributes (2e, upsert): %v", err)
	}

	// --- display order ---
	if err := repo.UpdateDisplayOrder(ctx, merchantID, DisplayOrderPayload{
		DisplayOrder: []DisplayOrderItem{{CategoryID: catID2, Products: []string{prodB, prodA}}},
	}); err != nil {
		t.Fatalf("UpdateDisplayOrder: %v", err)
	}

	// --- catégories marketing (string_agg, upserts ON CONFLICT) ---
	mcID, err := repo.CreateMarketingCategory(ctx, merchantID, "promo itest")
	if err != nil || len(mcID) < 10 {
		t.Fatalf("CreateMarketingCategory = (%q, %v) — doit retourner l'ID généré", mcID, err)
	}
	mcName := "promos itest"
	mcAvail := true
	if err := repo.UpdateMarketingCategory(ctx, merchantID, mcID, UpdateMarketingCategoryPayload{Name: &mcName, Available: &mcAvail}); err != nil {
		t.Fatalf("UpdateMarketingCategory: %v", err)
	}
	if err := repo.AssignProductMarketingCategory(ctx, merchantID, prodA, mcID); err != nil {
		t.Fatalf("AssignProductMarketingCategory: %v", err)
	}
	// ré-assignation -> chemin ON DUPLICATE / ON CONFLICT
	if err := repo.AssignProductMarketingCategory(ctx, merchantID, prodA, mcID); err != nil {
		t.Fatalf("AssignProductMarketingCategory (2e, upsert): %v", err)
	}
	if err := repo.BulkAssignProductsToMarketingCategory(ctx, merchantID, mcID, []string{prodA, prodB}); err != nil {
		t.Fatalf("BulkAssignProductsToMarketingCategory: %v", err)
	}
	mcs, err := repo.GetMarketingCategories(ctx, merchantID)
	if err != nil || len(mcs) != 1 {
		t.Fatalf("GetMarketingCategories = (%+v, %v)", mcs, err)
	}
	if mcs[0].Name != "Promos itest" || mcs[0].ProductCount != 2 || len(mcs[0].ProductIDs) != 2 {
		t.Fatalf("GetMarketingCategories row = %+v", mcs[0])
	}

	mkMenu, err := repo.GetMenuWithMarketingCategories(ctx, merchantID)
	if err != nil || mkMenu.Status != "ok" {
		t.Fatalf("GetMenuWithMarketingCategories = (%v, %v)", mkMenu.Status, err)
	}
	if len(mkMenu.ProductsTypes) == 0 || mkMenu.ProductsTypes[0].CategoryID == nil || *mkMenu.ProductsTypes[0].CategoryID != mcID {
		t.Fatalf("GetMenuWithMarketingCategories: la catégorie marketing devrait être en tête: %+v", mkMenu.ProductsTypes)
	}

	if err := repo.UnassignProductMarketingCategory(ctx, merchantID, prodB); err != nil {
		t.Fatalf("UnassignProductMarketingCategory: %v", err)
	}
	if err := repo.DeleteMarketingCategory(ctx, merchantID, mcID); err != nil {
		t.Fatalf("DeleteMarketingCategory: %v", err)
	}

	// --- ListTags : anomalie préexistante (colonne id inexistante dans les deux
	// dialectes) — l'erreur doit rester une erreur, pas un plantage silencieux ---
	if _, err := repo.ListTags(ctx, merchantID); err == nil {
		t.Fatalf("ListTags devrait échouer (colonne id inexistante, bug préexistant identique aux deux dialectes)")
	}

	// --- produit externe (Uber Eats temp) ---
	extID, err := repo.CreateExternalProductTx(ctx, merchantID, "itest-menu-ext", "desc", 500)
	if err != nil || extID == 0 {
		t.Fatalf("CreateExternalProductTx = (%d, %v)", extID, err)
	}

	// --- soft deletes ---
	if err := repo.DeleteProduct(ctx, merchantID, prodB); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}
	if err := repo.DeleteComponent(ctx, merchantID, compID); err != nil {
		t.Fatalf("DeleteComponent: %v", err)
	}
	if err := repo.DeleteComponentCategory(ctx, merchantID, compCatID); err != nil {
		t.Fatalf("DeleteComponentCategory: %v", err)
	}
	if err := repo.DeleteProductCategory(ctx, merchantID, catID2); err != nil {
		t.Fatalf("DeleteProductCategory: %v", err)
	}
	if err := repo.DeleteAttribute(ctx, merchantID, attrID); err != nil {
		t.Fatalf("DeleteAttribute: %v", err)
	}
	if _, err := repo.GetAttribute(ctx, merchantID, attrID); err == nil {
		t.Fatalf("GetAttribute après delete devrait échouer (enabled = FALSE)")
	}
}
