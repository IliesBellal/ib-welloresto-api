//go:build postgres_integration

package menu

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/menu/importer"
	tagsModule "welloresto-api/internal/modules/tags"
)

// Vérification réelle du commit d'import contre Postgres : c'est la seule
// phase qui écrit, et son contrat — tout ou rien, idempotent — ne se prouve
// pas avec un mock SQL.
//
//	DB_DIALECT=postgres POSTGRES_URL=postgres://…:5433/welloresto_dev \
//	  go test -tags postgres_integration ./internal/modules/menu/...
//
// Nécessite les migrations 080 (tables import_*_mapping) et 081
// (configurable_attribute_options.title en varchar(80) — deux libellés de la
// fixture 2026 dépassent 25 caractères).
//
// Chaque cas travaille sur son propre marchand et nettoie derrière lui : rien
// n'est partagé, et aucune donnée existante n'est touchée.

const (
	itestImportSiretPrefix = "siret-import-"
	itestImportTvaPrefix   = "itest-import-"
)

// itestImportMerchant crée un marchand isolé avec son référentiel de TVA.
func itestImportMerchant(t *testing.T, db *sql.DB, suffix string) (merchantID string, tvaIDs map[string]int) {
	t.Helper()
	ctx := context.Background()

	siret := itestImportSiretPrefix + suffix
	tvaTitlePrefix := itestImportTvaPrefix + suffix + "-"

	cleanup := func(mid string) {
		if mid != "" {
			for _, q := range []string{
				`DELETE FROM import_attribute_options_mapping WHERE merchant_id = $1`,
				`DELETE FROM import_attributes_mapping WHERE merchant_id = $1`,
				`DELETE FROM import_tags_mapping WHERE merchant_id = $1`,
				`DELETE FROM import_categories_mapping WHERE merchant_id = $1`,
				`DELETE FROM import_products_mapping WHERE merchant_id = $1`,
				`DELETE FROM product_tags WHERE tag_id IN (SELECT tag_id FROM tags WHERE merchant_id = $1)`,
				`DELETE FROM tags WHERE merchant_id = $1`,
				`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
				`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
				`DELETE FROM products WHERE merchant_Id = $1`,
				`DELETE FROM productcateg WHERE merchant_id = $1`,
				`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
				`DELETE FROM merchant WHERE id = $1`,
			} {
				_, _ = db.ExecContext(ctx, q, mid)
			}
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_title LIKE $1`, tvaTitlePrefix+"%")
	}

	// Résidu d'une exécution précédente interrompue.
	var previous int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&previous); err == nil {
		cleanup(strconv.FormatInt(previous, 10))
	} else {
		cleanup("")
	}

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Import Merchant', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $2, 'Europe/Paris')
		RETURNING id`, siret, "mtok-import-"+suffix).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	if _, err := db.ExecContext(ctx,
		`INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	// Le référentiel réel : chaque taux sur chacun des trois canaux.
	// delivery_type porte 'IN' / 'TAKE_AWAY' / 'DELIVERY', et non les valeurs
	// numériques qu'annonce le commentaire de la colonne.
	tvaIDs = make(map[string]int, 9)
	for _, channel := range []string{"IN", "TAKE_AWAY", "DELIVERY"} {
		for _, rate := range []string{"5.5", "10", "20"} {
			title := tvaTitlePrefix + channel + "-" + rate
			var tvaID int
			if err := db.QueryRowContext(ctx, `
				INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
				VALUES ($1, $2, 'itest', $3) RETURNING tva_id`, channel, title, rate).Scan(&tvaID); err != nil {
				t.Fatalf("seed tva %s: %v", title, err)
			}
			tvaIDs[channel+"/"+rate] = tvaID
		}
	}

	return merchantID, tvaIDs
}

func itestImportService(db *sql.DB, store importPreviewStore) *ImportService {
	repo := NewMenuRepository(db, nil)
	return NewImportService(repo, repo, importer.DefaultRegistry(), store, tagsModule.NewRepository(db), NewMenuChangeNotifier(nil, nil))
}

func itestImportContext(merchantID string) context.Context {
	return middleware.WithUser(context.Background(), &authpkg.UserLoginRow{
		UserID:     "u-itest-import",
		MerchantID: merchantID,
	})
}

func itestOpenFixture(t *testing.T, name string) *os.File {
	t.Helper()

	f, err := os.Open(filepath.Join("importer", "testdata", name))
	if err != nil {
		t.Fatalf("ouverture de la fixture %q: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func itestCount(t *testing.T, db *sql.DB, query string, args ...interface{}) int {
	t.Helper()

	var count int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("comptage (%s): %v", query, err)
	}
	return count
}

// Commit de bout en bout de l'export 2026, puis re-commit pour prouver
// l'idempotence.
func TestImportCommit_Postgres_EndToEndAndIdempotent(t *testing.T) {
	db := pgtest.Open(t)
	merchantID, tvaIDs := itestImportMerchant(t, db, "e2e")

	ctx := itestImportContext(merchantID)
	store := newFakePreviewStore()
	service := itestImportService(db, store)

	preview, err := service.PreviewImportFile(ctx, importer.ZeltySlug, itestOpenFixture(t, zeltyFixture2026))
	if err != nil {
		t.Fatalf("PreviewImportFile: %v", err)
	}
	if preview.Summary.UnresolvedTvaRates != 0 {
		t.Fatalf("UnresolvedTvaRates = %d, want 0", preview.Summary.UnresolvedTvaRates)
	}

	resp, err := service.CommitImport(ctx, &ImportCommitRequest{Token: preview.Token})
	if err != nil {
		t.Fatalf("CommitImport: %v", err)
	}

	// --- comptes rapportés ---
	if resp.Summary.Products.Created != 141 {
		t.Fatalf("produits créés = %d, want 141", resp.Summary.Products.Created)
	}
	if resp.Summary.Attributes.Created != 12 {
		t.Fatalf("groupes d'options créés = %d, want 12", resp.Summary.Attributes.Created)
	}
	if resp.Summary.OptionsCreated != 78 {
		t.Fatalf("options créées = %d, want 78", resp.Summary.OptionsCreated)
	}
	if got := resp.Summary.Categories.Created + resp.Summary.Tags.Created; got != 19 {
		t.Fatalf("catégories (%d) + tags (%d) = %d, want 19 libellés",
			resp.Summary.Categories.Created, resp.Summary.Tags.Created, got)
	}

	// --- comptes réels en base ---
	if got := itestCount(t, db, `SELECT count(*) FROM products WHERE merchant_Id = $1`, merchantID); got != 141 {
		t.Fatalf("produits en base = %d, want 141", got)
	}
	if got := itestCount(t, db, `SELECT count(*) FROM configurable_attributes WHERE merchant_id = $1`, merchantID); got != 12 {
		t.Fatalf("attributs en base = %d, want 12", got)
	}
	if got := itestCount(t, db,
		`SELECT count(*) FROM configurable_attribute_options o
		 JOIN configurable_attributes a ON a.id = o.configurable_attribute_id
		 WHERE a.merchant_id = $1`, merchantID); got != 78 {
		t.Fatalf("options en base = %d, want 78", got)
	}
	if got := itestCount(t, db, `SELECT count(*) FROM productcateg WHERE merchant_id = $1`, merchantID) +
		itestCount(t, db, `SELECT count(*) FROM tags WHERE merchant_id = $1`, merchantID); got != 19 {
		t.Fatalf("catégories + tags en base = %d, want 19", got)
	}

	// --- les cinq tables de correspondance ---
	mappings := map[string]int{
		importProductsMappingTable:         141,
		importAttributesMappingTable:       12,
		importAttributeOptionsMappingTable: 78,
	}
	for table, want := range mappings {
		got := itestCount(t, db,
			`SELECT count(*) FROM `+table+` WHERE merchant_id = $1 AND provider = $2`, merchantID, importer.ZeltySlug)
		if got != want {
			t.Fatalf("%s = %d lignes, want %d", table, got, want)
		}
	}
	if got := itestCount(t, db, `SELECT count(*) FROM `+importCategoriesMappingTable+` WHERE merchant_id = $1`, merchantID) +
		itestCount(t, db, `SELECT count(*) FROM `+importTagsMappingTable+` WHERE merchant_id = $1`, merchantID); got != 19 {
		t.Fatalf("correspondances catégories + tags = %d, want 19", got)
	}

	// --- Carbonara : 10 sur place, 0 ailleurs ---
	var (
		carbonaraID                           int
		tvaIn, tvaTakeAway, tvaDelivery       int
		availIn, availTakeAway, availDelivery bool
		status, category                      string
	)
	if err := db.QueryRowContext(context.Background(), `
		SELECT product_id, tva_in_id, tva_take_away_id, tva_delivery_id,
		       available_in, available_take_away, available_delivery, status, category
		FROM products WHERE merchant_Id = $1 AND name LIKE 'Carbonara%'`, merchantID).
		Scan(&carbonaraID, &tvaIn, &tvaTakeAway, &tvaDelivery,
			&availIn, &availTakeAway, &availDelivery, &status, &category); err != nil {
		t.Fatalf("lecture Carbonara: %v", err)
	}

	if tvaIn != tvaIDs["IN/10"] {
		t.Fatalf("tva_in_id = %d, want %d (10%% sur place)", tvaIn, tvaIDs["IN/10"])
	}
	if tvaTakeAway != tvaIDs["TAKE_AWAY/10"] {
		t.Fatalf("tva_take_away_id = %d, want %d (backfill du taux 10 sur le canal emporté)", tvaTakeAway, tvaIDs["TAKE_AWAY/10"])
	}
	if tvaDelivery != tvaIDs["DELIVERY/10"] {
		t.Fatalf("tva_delivery_id = %d, want %d (backfill du taux 10 sur le canal livraison)", tvaDelivery, tvaIDs["DELIVERY/10"])
	}
	if !availIn || availTakeAway || availDelivery {
		t.Fatalf("available = (%v, %v, %v), want (true, false, false)", availIn, availTakeAway, availDelivery)
	}
	if status != importer.ProductStatusAvailable {
		t.Fatalf("status = %q, want %q", status, importer.ProductStatusAvailable)
	}

	// --- rattachement catégorie et tags ---
	var categoryName string
	if err := db.QueryRowContext(context.Background(),
		`SELECT categ_name FROM productcateg WHERE merchant_id = $1 AND merchant_categ_id = $2`,
		merchantID, category).Scan(&categoryName); err != nil {
		t.Fatalf("lecture de la catégorie de Carbonara: %v", err)
	}
	if categoryName != "NOS PIZZA" {
		t.Fatalf("catégorie = %q, want %q", categoryName, "NOS PIZZA")
	}

	tagNames := itestCount(t, db, `
		SELECT count(*) FROM product_tags pt
		JOIN tags t ON t.tag_id = pt.tag_id
		WHERE pt.product_id = $1 AND t.merchant_id = $2`, strconv.Itoa(carbonaraID), merchantID)
	if tagNames != 2 {
		t.Fatalf("tags rattachés à Carbonara = %d, want 2 (BASE CRÈME, SIGNATURE)", tagNames)
	}

	// --- les groupes d'options restent non rattachés (V1a) ---
	if got := itestCount(t, db, `
		SELECT count(*) FROM product_configurable_attribute
		WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
		merchantID); got != 0 {
		t.Fatalf("rattachements produit↔attribut = %d, want 0 (V1a : attributs standalone)", got)
	}
	if got := itestCount(t, db,
		`SELECT count(*) FROM configurable_attributes WHERE merchant_id = $1 AND product_id <> 0`, merchantID); got != 0 {
		t.Fatalf("attributs avec product_id non nul = %d, want 0", got)
	}

	// --- un libellé de plus de 25 caractères a survécu entier (migration 081) ---
	if got := itestCount(t, db, `
		SELECT count(*) FROM configurable_attribute_options o
		JOIN configurable_attributes a ON a.id = o.configurable_attribute_id
		WHERE a.merchant_id = $1 AND o.title = 'Jambon de Parme 24 mois AOP'`, merchantID); got != 1 {
		t.Fatalf("option au titre long = %d, want 1 (titre intact)", got)
	}

	// --- le token est consommé ---
	if _, err := service.CommitImport(ctx, &ImportCommitRequest{Token: preview.Token}); err != ErrImportPreviewNotFound {
		t.Fatalf("second commit du même token = %v, want %v", err, ErrImportPreviewNotFound)
	}

	// --- idempotence : nouvelle preview du même fichier, rien n'est recréé ---
	secondPreview, err := service.PreviewImportFile(ctx, importer.ZeltySlug, itestOpenFixture(t, zeltyFixture2026))
	if err != nil {
		t.Fatalf("PreviewImportFile (2e) : %v", err)
	}
	if secondPreview.Summary.ProductsAlreadyImported != 141 {
		t.Fatalf("produits déjà importés = %d, want 141", secondPreview.Summary.ProductsAlreadyImported)
	}

	secondResp, err := service.CommitImport(ctx, &ImportCommitRequest{Token: secondPreview.Token})
	if err != nil {
		t.Fatalf("CommitImport (2e) : %v", err)
	}
	if secondResp.Summary.Products.Created != 0 ||
		secondResp.Summary.Categories.Created != 0 ||
		secondResp.Summary.Tags.Created != 0 ||
		secondResp.Summary.Attributes.Created != 0 ||
		secondResp.Summary.OptionsCreated != 0 {
		t.Fatalf("re-commit a créé %+v, want 0 partout", secondResp.Summary)
	}

	if got := itestCount(t, db, `SELECT count(*) FROM products WHERE merchant_Id = $1`, merchantID); got != 141 {
		t.Fatalf("produits après re-commit = %d, want 141", got)
	}
	if got := itestCount(t, db,
		`SELECT count(*) FROM `+importProductsMappingTable+` WHERE merchant_id = $1`, merchantID); got != 141 {
		t.Fatalf("correspondances produits après re-commit = %d, want 141", got)
	}
}

// L'export 2025 porte quatre lignes de frais sans prix ni libellé : elles
// doivent être importées, en removed_from_menu, une fois leur catégorie imposée.
func TestImportCommit_Postgres_ZeroPricedProductsAreRemovedFromMenu(t *testing.T) {
	db := pgtest.Open(t)
	merchantID, _ := itestImportMerchant(t, db, "zero")

	ctx := itestImportContext(merchantID)
	store := newFakePreviewStore()
	service := itestImportService(db, store)

	preview, err := service.PreviewImportFile(ctx, importer.ZeltySlug, itestOpenFixture(t, "Zelty Menu OK PIZZA DLP - 2025-09-24.xlsx"))
	if err != nil {
		t.Fatalf("PreviewImportFile: %v", err)
	}
	if preview.Summary.ProductsNeedingCategory != 4 {
		t.Fatalf("produits sans catégorie = %d, want 4", preview.Summary.ProductsNeedingCategory)
	}

	// Sans arbitrage, le lot est refusé.
	if _, err := service.CommitImport(ctx, &ImportCommitRequest{Token: preview.Token}); err == nil {
		t.Fatal("CommitImport a accepté un lot avec des produits sans catégorie")
	} else if _, ok := err.(*ImportNotCommittableError); !ok {
		t.Fatalf("erreur = %T (%v), want *ImportNotCommittableError", err, err)
	}
	if got := itestCount(t, db, `SELECT count(*) FROM products WHERE merchant_Id = $1`, merchantID); got != 0 {
		t.Fatalf("produits écrits malgré le refus = %d, want 0", got)
	}

	// On impose une catégorie aux produits qui en manquent.
	decisions := preview.Decisions
	var fallback string
	for _, tag := range preview.Tags {
		if tag.Class == string(importer.TagClassCategory) {
			fallback = tag.ExternalID
			break
		}
	}
	if fallback == "" {
		t.Fatal("aucun libellé classé catégorie")
	}
	for _, product := range preview.Products {
		if product.NeedsCategory {
			decisions.CategoryPerProduct[product.ExternalID] = fallback
		}
	}

	resp, err := service.CommitImport(ctx, &ImportCommitRequest{Token: preview.Token, Decisions: &decisions})
	if err != nil {
		t.Fatalf("CommitImport: %v", err)
	}
	if resp.Summary.Products.Created != 107 {
		t.Fatalf("produits créés = %d, want 107", resp.Summary.Products.Created)
	}

	if got := itestCount(t, db,
		`SELECT count(*) FROM products WHERE merchant_Id = $1 AND status = $2`,
		merchantID, importer.ProductStatusRemovedFromMenu); got != 4 {
		t.Fatalf("produits en %s = %d, want 4", importer.ProductStatusRemovedFromMenu, got)
	}
	if got := itestCount(t, db,
		`SELECT count(*) FROM products WHERE merchant_Id = $1 AND status = $2 AND price <> 0`,
		merchantID, importer.ProductStatusRemovedFromMenu); got != 0 {
		t.Fatalf("produits sans prix mais valorisés = %d, want 0", got)
	}
}

// Tout ou rien : une erreur au milieu du lot doit tout annuler, correspondances
// comprises. Un lot à moitié écrit laisserait des mappings qui feraient sauter
// les entités manquantes au ré-import.
func TestImportCommit_Postgres_RollsBackEntireBatchOnError(t *testing.T) {
	db := pgtest.Open(t)
	merchantID, tvaIDs := itestImportMerchant(t, db, "rollback")

	repo := NewMenuRepository(db, nil)
	tagsRepo := tagsModule.NewRepository(db)

	product := func(externalID, name, categoryExternalID string) importer.PlannedProduct {
		return importer.PlannedProduct{
			ExternalID:         externalID,
			Name:               name,
			Status:             importer.ProductStatusAvailable,
			CategoryExternalID: categoryExternalID,
			TagExternalIDs:     []string{"EXT-TAG"},
			PriceIn:            990,
			PriceTakeAway:      990,
			PriceDelivery:      990,
			TvaInID:            tvaIDs["IN/10"],
			TvaTakeAwayID:      tvaIDs["TAKE_AWAY/10"],
			TvaDeliveryID:      tvaIDs["DELIVERY/10"],
			AvailableIn:        true,
			AvailableTakeAway:  true,
			AvailableDelivery:  true,
		}
	}

	plan := &importer.CommitPlan{
		Provider:   importer.ZeltySlug,
		Categories: []importer.PlannedCategory{{ExternalID: "EXT-CAT", Name: "Pizzas itest"}},
		Tags:       []importer.PlannedTag{{ExternalID: "EXT-TAG", Name: "Signature itest"}},
		Attributes: []importer.PlannedAttribute{{
			ExternalID: "EXT-ATTR", Name: "Suppléments itest", Type: importer.AttributeTypeCheck,
			MinOptions: 0, MaxOptions: 2,
			Options: []importer.PlannedOption{
				{ExternalID: "EXT-OPT-1", Title: "Chèvre", ExtraPrice: 200},
				{ExternalID: "EXT-OPT-2", Title: "Miel", ExtraPrice: 100},
			},
		}},
		Products: []importer.PlannedProduct{
			product("EXT-P1", "Margherita itest", "EXT-CAT"),
			// La catégorie de ce second produit ne fait pas partie du plan :
			// l'échec survient après que tout le reste a été écrit.
			product("EXT-P2", "Calzone itest", "EXT-CAT-INCONNUE"),
		},
	}

	outcome, err := repo.MaterializeImportTx(context.Background(), merchantID, plan, tagsRepo)
	if err == nil {
		t.Fatalf("MaterializeImportTx a réussi malgré une catégorie non résolue: %+v", outcome)
	}

	tables := []struct {
		label string
		query string
	}{
		{"produits", `SELECT count(*) FROM products WHERE merchant_Id = $1`},
		{"catégories", `SELECT count(*) FROM productcateg WHERE merchant_id = $1`},
		{"tags", `SELECT count(*) FROM tags WHERE merchant_id = $1`},
		{"attributs", `SELECT count(*) FROM configurable_attributes WHERE merchant_id = $1`},
		{"options", `SELECT count(*) FROM configurable_attribute_options o
			JOIN configurable_attributes a ON a.id = o.configurable_attribute_id WHERE a.merchant_id = $1`},
		{"corr. produits", `SELECT count(*) FROM ` + importProductsMappingTable + ` WHERE merchant_id = $1`},
		{"corr. catégories", `SELECT count(*) FROM ` + importCategoriesMappingTable + ` WHERE merchant_id = $1`},
		{"corr. tags", `SELECT count(*) FROM ` + importTagsMappingTable + ` WHERE merchant_id = $1`},
		{"corr. attributs", `SELECT count(*) FROM ` + importAttributesMappingTable + ` WHERE merchant_id = $1`},
		{"corr. options", `SELECT count(*) FROM ` + importAttributeOptionsMappingTable + ` WHERE merchant_id = $1`},
	}
	for _, table := range tables {
		if got := itestCount(t, db, table.query, merchantID); got != 0 {
			t.Fatalf("%s après rollback = %d, want 0", table.label, got)
		}
	}
}
