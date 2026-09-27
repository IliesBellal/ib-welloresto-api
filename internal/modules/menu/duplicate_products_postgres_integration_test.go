//go:build postgres_integration

package menu

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Copie groupée de produits exécutée contre le Postgres de dev : chaque table
// d'association est réellement recopiée, pas seulement compilée.
func TestDuplicateProducts_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		ofMerchant := `(SELECT product_id::text FROM products WHERE merchant_id = $1)`
		for _, q := range []string{
			`DELETE FROM product_configurable_attribute WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM product_tags WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM product_allergens WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM product_marketing_categories WHERE merchant_id = $1`,
			`DELETE FROM product_production_profiles WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM availabilities_products WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM discounts_products WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM discounts_products_options WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM discounts WHERE merchant_id = $1`,
			`DELETE FROM customer_loyalty_program_target_products WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM customer_loyalty_program_reward_products WHERE product_id::text IN ` + ofMerchant,
			`DELETE FROM requires WHERE recipe_id IN (SELECT recipe_id FROM recipes WHERE merchant_id = $1)`,
			`DELETE FROM recipes WHERE merchant_id = $1`,
			`DELETE FROM printers WHERE merchant_id = $1`,
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
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-menu-dup' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	// --- seeds ---
	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Menu Dup', 'a', '1', 's', '75001', 'Paris', 'siret-menu-dup', 'https://x', '06', 'mtok-menu-dup', 'Europe/Paris')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	repo := NewMenuRepository(db, nil)

	srcCat, err := repo.CreateProductCategory(ctx, &CreateProductCategoryPayload{Name: "dup source itest", MerchantID: merchantID})
	if err != nil {
		t.Fatalf("CreateProductCategory(source): %v", err)
	}
	dstCat, err := repo.CreateProductCategory(ctx, &CreateProductCategoryPayload{Name: "dup cible itest", MerchantID: merchantID})
	if err != nil {
		t.Fatalf("CreateProductCategory(cible): %v", err)
	}

	type productSeed struct {
		name, category, status, imageURL string
		isGroup, onKiosk, enabled        bool
		displayOrder                     int
		parentID                         string
	}
	newProduct := func(p productSeed) string {
		t.Helper()
		var parent interface{}
		if p.parentID != "" {
			parent = p.parentID
		}
		var image interface{}
		if p.imageURL != "" {
			image = p.imageURL
		}
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_id, name, product_desc, image_url, bg_color, price, price_take_away, price_delivery,
				tva_in_id, tva_delivery_id, tva_take_away_id, category, status, is_product_group, is_available_on_kiosk,
				enabled, display_order, by_product_of)
			VALUES ($1, $2, 'desc', $3, '#123456', 1200, 1100, 1300, 1, 2, 3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING product_id`,
			merchantID, p.name, image, p.category, p.status, p.isGroup, p.onKiosk, p.enabled, p.displayOrder, parent,
		).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", p.name, err)
		}
		return strconv.FormatInt(id, 10)
	}

	plat := newProduct(productSeed{name: "Burger itest", category: srcCat, status: "available", imageURL: "https://cdn.example.com/burger.jpg", enabled: true, displayOrder: 3})
	group := newProduct(productSeed{name: "Boissons itest", category: srcCat, status: "available", isGroup: true, onKiosk: true, enabled: true, displayOrder: 1})
	sub1 := newProduct(productSeed{name: "Coca itest", category: srcCat, status: "available", onKiosk: true, enabled: true, displayOrder: 0, parentID: group})
	sub2 := newProduct(productSeed{name: "Fanta itest", category: srcCat, status: "out_of_stock", onKiosk: true, enabled: true, displayOrder: 1, parentID: group})
	newProduct(productSeed{name: "Sprite supprimé itest", category: srcCat, status: "available", onKiosk: true, enabled: false, displayOrder: 2, parentID: group})
	newProduct(productSeed{name: "Déjà là itest", category: dstCat, status: "available", onKiosk: true, enabled: true, displayOrder: 7})

	exec := func(label, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %s: %v", label, err)
		}
	}
	exec("options", `INSERT INTO product_configurable_attribute (product_id, configurable_attribute_id, num_order, enabled) VALUES ($1, 'ca-dup-1', 2, TRUE), ($1, 'ca-dup-off', 0, FALSE)`, plat)
	exec("tags", `INSERT INTO product_tags (product_id, tag_id) VALUES ($1, 'tag-dup'), ($2, 'tag-dup-sub')`, plat, sub1)
	exec("allergens", `INSERT INTO product_allergens (product_id, allergen_id) VALUES ($1, 'alg-dup')`, plat)
	exec("marketing", `INSERT INTO product_marketing_categories (product_id, marketing_category_id, merchant_id) VALUES ($1, 'mc-dup', $2)`, plat, merchantID)
	exec("production", `INSERT INTO product_production_profiles (production_profile_id, product_id, should_produce, should_monitor) VALUES ('pp-dup', $1, TRUE, FALSE)`, plat)
	exec("availability", `INSERT INTO availabilities_products (availability_product_id, availability_id, product_id) VALUES ('avail-prod-dup-1', 'avail-dup', $1), ('avail-prod-dup-off', 'avail-dup-off', $1)`, plat)
	exec("availability off", `UPDATE availabilities_products SET enabled = FALSE WHERE availability_product_id = 'avail-prod-dup-off'`)
	var discountIDNew int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_unit, discounted_quantity, is_cumulative, is_time_limited)
		VALUES ('disc-dup', $1, 'Promo itest', 'd', 'PERCENT', 1, FALSE, FALSE)
		RETURNING discount_id_new`, merchantID).Scan(&discountIDNew); err != nil {
		t.Fatalf("seed discount: %v", err)
	}
	exec("discount product", `INSERT INTO discounts_products (discount_id, discount_id_new, product_id, new_price, enabled) VALUES ('disc-dup', $1, $2, 500, TRUE)`, discountIDNew, plat)
	exec("discount option", `INSERT INTO discounts_products_options (discount_id, discount_id_new, product_id, option_id, new_price, is_option_mandatory) VALUES ('disc-dup', $1, $2, 'opt-dup', 0, FALSE)`, discountIDNew, plat)
	exec("loyalty target", `INSERT INTO customer_loyalty_program_target_products (id, product_id, loyalty_program_id) VALUES ('target-prod-dup', $1, 'lp-dup')`, plat)
	exec("loyalty reward", `INSERT INTO customer_loyalty_program_reward_products (id, product_id, loyalty_program_id) VALUES ('reward-prod-dup', $1, 'lp-dup')`, plat)
	var recipeID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO recipes (product_id, merchant_id, preparation_time) VALUES ($1, $2, 5) RETURNING recipe_id`, plat, merchantID).Scan(&recipeID); err != nil {
		t.Fatalf("seed recipe: %v", err)
	}
	exec("requires", `INSERT INTO requires (recipe_id, component_id, quantity, unit_of_measure, in_orders, enabled) VALUES ($1, 42, 1.5, 1, FALSE, TRUE), ($1, 43, 1, 1, TRUE, FALSE)`, recipeID)
	filter, _ := json.Marshal([]string{plat, "999"})
	exec("printer filtré", `INSERT INTO printers (printer_id, merchant_id, name, connection_type, language, role, production_product_ids) VALUES ('prn-dup-1', $1, 'Cuisine', 'NETWORK', 'fr', 'PRODUCTION', $2)`, merchantID, string(filter))
	exec("printer sans filtre", `INSERT INTO printers (printer_id, merchant_id, name, connection_type, language, role) VALUES ('prn-dup-2', $1, 'Bar', 'NETWORK', 'fr', 'PRODUCTION')`, merchantID)

	// --- refus ---
	if _, err := repo.DuplicateProducts(ctx, merchantID, dstCat, "available", []string{plat, "999999999"}); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("DuplicateProducts(produit étranger) = %v, want ErrForbidden", err)
	}
	if _, err := repo.DuplicateProducts(ctx, merchantID, "999999999", "available", []string{plat}); err == nil {
		t.Fatalf("DuplicateProducts(catégorie inconnue) devrait échouer")
	}

	// --- copie : sub1 est sélectionné avec son groupe, plat en double ---
	copies, err := repo.DuplicateProducts(ctx, merchantID, dstCat, "removed_from_menu", []string{plat, group, sub1, " ", plat})
	if err != nil {
		t.Fatalf("DuplicateProducts: %v", err)
	}
	if len(copies) != 4 {
		t.Fatalf("copies = %+v, want 4 (plat, groupe, 2 sous-produits actifs)", copies)
	}
	copyOf := map[string]DuplicatedProduct{}
	for _, c := range copies {
		copyOf[c.SourceID] = c
	}
	platCopy, groupCopy, sub1Copy, sub2Copy := copyOf[plat], copyOf[group], copyOf[sub1], copyOf[sub2]
	if !platCopy.Root || !groupCopy.Root || sub1Copy.Root || sub2Copy.Root {
		t.Fatalf("Root = plat %v, groupe %v, sub1 %v, sub2 %v ; want true, true, false, false",
			platCopy.Root, groupCopy.Root, sub1Copy.Root, sub2Copy.Root)
	}
	if platCopy.SourceImageURL != "https://cdn.example.com/burger.jpg" {
		t.Fatalf("SourceImageURL = %q", platCopy.SourceImageURL)
	}

	type productRow struct {
		name, category, status, bgColor string
		imageURL, parentID              sql.NullString
		price, displayOrder             int
		isGroup, onKiosk, enabled       bool
	}
	readProduct := func(id string) productRow {
		t.Helper()
		var p productRow
		if err := db.QueryRowContext(ctx, `
			SELECT name, category, status, bg_color, image_url, by_product_of::text, price, display_order,
				is_product_group, is_available_on_kiosk, enabled
			FROM products WHERE product_id = $1`, id,
		).Scan(&p.name, &p.category, &p.status, &p.bgColor, &p.imageURL, &p.parentID, &p.price, &p.displayOrder,
			&p.isGroup, &p.onKiosk, &p.enabled); err != nil {
			t.Fatalf("read product %s: %v", id, err)
		}
		return p
	}

	// Les sources sont copiées dans leur ordre (groupe display_order 1, plat 3),
	// à la suite du produit déjà présent dans la cible (display_order 7).
	pc := readProduct(platCopy.NewID)
	if pc.name != "Burger itest" || pc.category != dstCat || pc.status != "removed_from_menu" || pc.displayOrder != 9 ||
		pc.imageURL.Valid || pc.parentID.Valid || pc.price != 1200 || pc.bgColor != "#123456" || pc.onKiosk || !pc.enabled {
		t.Fatalf("copie du plat = %+v", pc)
	}
	gc := readProduct(groupCopy.NewID)
	if !gc.isGroup || gc.category != dstCat || gc.status != "removed_from_menu" || gc.displayOrder != 8 || gc.parentID.Valid {
		t.Fatalf("copie du groupe = %+v", gc)
	}
	// Un sous-produit garde son statut et son ordre dans le groupe.
	for _, tc := range []struct {
		copy   DuplicatedProduct
		name   string
		status string
		order  int
	}{{sub1Copy, "Coca itest", "available", 0}, {sub2Copy, "Fanta itest", "out_of_stock", 1}} {
		sc := readProduct(tc.copy.NewID)
		if sc.name != tc.name || sc.parentID.String != groupCopy.NewID || sc.category != dstCat || sc.status != tc.status || sc.displayOrder != tc.order {
			t.Fatalf("copie de %s = %+v", tc.name, sc)
		}
	}
	var subCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE by_product_of = $1`, groupCopy.NewID).Scan(&subCount); err != nil || subCount != 2 {
		t.Fatalf("sous-produits de la copie du groupe = (%d, %v), want 2 (le sous-produit supprimé n'est pas copié)", subCount, err)
	}
	if src := readProduct(plat); src.category != srcCat || src.status != "available" {
		t.Fatalf("source modifiée : %+v", src)
	}

	// --- associations ---
	assertValue := func(label, q string, want string, args ...interface{}) {
		t.Helper()
		var got sql.NullString
		if err := db.QueryRowContext(ctx, q, args...).Scan(&got); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if got.String != want {
			t.Fatalf("%s = %q, want %q", label, got.String, want)
		}
	}
	nc := platCopy.NewID
	assertValue("options", `SELECT string_agg(configurable_attribute_id || ':' || num_order || ':' || enabled, ',') FROM product_configurable_attribute WHERE product_id = $1`, "ca-dup-1:2:true", nc)
	assertValue("tags", `SELECT string_agg(tag_id, ',') FROM product_tags WHERE product_id = $1`, "tag-dup", nc)
	assertValue("tags du sous-produit", `SELECT string_agg(tag_id, ',') FROM product_tags WHERE product_id = $1`, "tag-dup-sub", sub1Copy.NewID)
	assertValue("allergènes", `SELECT string_agg(allergen_id, ',') FROM product_allergens WHERE product_id = $1`, "alg-dup", nc)
	assertValue("catégorie marketing", `SELECT marketing_category_id || ':' || merchant_id FROM product_marketing_categories WHERE product_id = $1`, "mc-dup:"+merchantID, nc)
	assertValue("profils de production", `SELECT string_agg(production_profile_id || ':' || should_produce || ':' || should_monitor, ',') FROM product_production_profiles WHERE product_id = $1`, "pp-dup:true:false", nc)
	assertValue("disponibilités", `SELECT string_agg(availability_id || ':' || (availability_product_id LIKE 'avail-prod%') || ':' || (availability_product_id <> 'avail-prod-dup-1'), ',') FROM availabilities_products WHERE product_id = $1`, "avail-dup:true:true", nc)
	assertValue("promos", `SELECT string_agg(discount_id || ':' || discount_id_new || ':' || new_price, ',') FROM discounts_products WHERE product_id = $1`, "disc-dup:"+strconv.FormatInt(discountIDNew, 10)+":500", nc)
	assertValue("options de promo", `SELECT string_agg(option_id || ':' || new_price || ':' || is_option_mandatory, ',') FROM discounts_products_options WHERE product_id = $1`, "opt-dup:0:false", nc)
	assertValue("fidélité (cible)", `SELECT string_agg(loyalty_program_id, ',') FROM customer_loyalty_program_target_products WHERE product_id = $1`, "lp-dup", nc)
	assertValue("fidélité (récompense)", `SELECT string_agg(loyalty_program_id, ',') FROM customer_loyalty_program_reward_products WHERE product_id = $1`, "lp-dup", nc)
	assertValue("composition", `
		SELECT string_agg(r.preparation_time || ':' || q.component_id || ':' || q.quantity || ':' || q.in_orders, ',')
		FROM recipes r JOIN requires q ON q.recipe_id = r.recipe_id
		WHERE r.product_id = $1 AND r.merchant_id = $2`, "5:42:1.5:false", nc, merchantID)

	var filterRaw sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT production_product_ids FROM printers WHERE printer_id = 'prn-dup-1'`).Scan(&filterRaw); err != nil {
		t.Fatalf("read printer filter: %v", err)
	}
	var filterIDs []string
	_ = json.Unmarshal([]byte(filterRaw.String), &filterIDs)
	if strings.Join(filterIDs, ",") != plat+",999,"+nc {
		t.Fatalf("filtre imprimante = %v, want [%s 999 %s]", filterIDs, plat, nc)
	}
	var noFilter sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT production_product_ids FROM printers WHERE printer_id = 'prn-dup-2'`).Scan(&noFilter); err != nil || noFilter.Valid {
		t.Fatalf("imprimante sans filtre = (%v, %v), want NULL", noFilter, err)
	}

	// --- images ---
	if err := repo.SetProductImages(ctx, merchantID, map[string]string{nc: "https://cdn.example.com/copy.jpg"}); err != nil {
		t.Fatalf("SetProductImages: %v", err)
	}
	if got := readProduct(nc).imageURL.String; got != "https://cdn.example.com/copy.jpg" {
		t.Fatalf("image de la copie = %q", got)
	}
	if got := readProduct(plat).imageURL.String; got != "https://cdn.example.com/burger.jpg" {
		t.Fatalf("image de la source modifiée : %q", got)
	}
}
