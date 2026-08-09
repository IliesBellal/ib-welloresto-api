package importer

import (
	"fmt"
	"testing"
)

// Identifiants relevés dans l'export 2026, utilisés comme ancres de test.
const (
	tagNosPizza2026    = "ZT541858" // ouvre la liste de toutes les pizzas
	tagVege2026        = "ZT541863"
	tagBaseTomate2026  = "ZT541866"
	productCreeTa2026  = "ZD1676511" // 9,90 - TVA 10/10/10
	productCarbonara26 = "ZD1676517" // 13,90 - TVA 10/0/0
	productMargherita  = "ZD1676524" // "Margherita 🍅🧀"
	productMonaco2026  = "ZD2112900" // 4,50 - TVA 20/0/0
	productFrais2025   = "ZD1557688" // "Frais de livraison", sans prix ni libellé
)

// fullTvaRates simule un tva_categories complet : trois taux sur les trois
// canaux. Les identifiants sont déterministes pour pouvoir être assertés.
//
//	in (0)        : 5.5 -> 1, 10 -> 2, 20 -> 3
//	take_away (3) : 5.5 -> 4, 10 -> 5, 20 -> 6
//	delivery (1)  : 5.5 -> 7, 10 -> 8, 20 -> 9
func fullTvaRates() []TvaRateRow {
	var rows []TvaRateRow
	tvaID := 1
	for _, channel := range AllTvaChannels {
		for _, rate := range []float64{5.5, 10, 20} {
			rows = append(rows, TvaRateRow{TvaID: tvaID, Channel: channel, Rate: rate})
			tvaID++
		}
	}
	return rows
}

// tvaRatesWithout retire un taux de tous les canaux, pour simuler un taux
// présent dans le fichier mais absent de la configuration du marchand.
func tvaRatesWithout(dropped float64) []TvaRateRow {
	var rows []TvaRateRow
	for _, row := range fullTvaRates() {
		if row.Rate == dropped {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func emptyImported() ImportedEntities {
	return ImportedEntities{
		Products:   map[string]int{},
		Categories: map[string]int{},
		Tags:       map[string]string{},
		Attributes: map[string]string{},
	}
}

func defaultLookups() PreviewLookups {
	return PreviewLookups{TvaRates: fullTvaRates(), Imported: emptyImported()}
}

func previewFixture(t *testing.T, fixture string, lk PreviewLookups) *PreviewResult {
	t.Helper()

	imp := parseZeltyFixture(t, fixture)
	res, err := BuildPreview(imp, lk)
	if err != nil {
		t.Fatalf("BuildPreview(%s): %v", fixture, err)
	}
	return res
}

func previewProduct(t *testing.T, res *PreviewResult, externalID string) PreviewProduct {
	t.Helper()

	for _, p := range res.Products {
		if p.ExternalID == externalID {
			return p
		}
	}
	t.Fatalf("produit %q absent de la preview", externalID)
	return PreviewProduct{}
}

func previewTag(t *testing.T, res *PreviewResult, externalID string) PreviewTag {
	t.Helper()

	for _, tag := range res.Tags {
		if tag.ExternalID == externalID {
			return tag
		}
	}
	t.Fatalf("libellé %q absent de la preview", externalID)
	return PreviewTag{}
}

// Sur un marchand dont les trois taux sont configurés sur les trois canaux,
// aucun couple ne doit rester non résolu, sur aucun des deux exports.
func TestBuildPreviewResolvesEveryTvaCouple(t *testing.T) {
	for _, fixture := range []string{fixtureZelty2025, fixtureZelty2026} {
		t.Run(fixture, func(t *testing.T) {
			res := previewFixture(t, fixture, defaultLookups())

			if res.Summary.UnresolvedTvaRates != 0 {
				t.Fatalf("UnresolvedTvaRates = %d, want 0", res.Summary.UnresolvedTvaRates)
			}
			if len(res.TvaRates) == 0 {
				t.Fatal("aucun couple (taux, canal) relevé")
			}
			for _, couple := range res.TvaRates {
				if !couple.Resolved || couple.TvaID == 0 {
					t.Fatalf("couple %v%% / %s non résolu", couple.Rate, couple.ChannelLabel)
				}
				if couple.ProductCount == 0 {
					t.Fatalf("couple %v%% / %s sans produit", couple.Rate, couple.ChannelLabel)
				}
			}

			// Le mapping compact doit refléter la liste détaillée.
			if got := res.Decisions.TvaMapping[TvaRateKey{Rate: 10, Channel: TvaChannelIn}]; got != 2 {
				t.Fatalf("TvaMapping[10, in] = %d, want 2", got)
			}
		})
	}
}

// Un taux du fichier absent de tva_categories doit ressortir explicitement,
// avec un warning — c'est ce que le wizard fera corriger.
func TestBuildPreviewFlagsUnresolvedTvaRate(t *testing.T) {
	lk := defaultLookups()
	lk.TvaRates = tvaRatesWithout(20)

	res := previewFixture(t, fixtureZelty2026, lk)

	if res.Summary.UnresolvedTvaRates == 0 {
		t.Fatal("UnresolvedTvaRates = 0, want > 0 (le taux 20 n'est pas configuré)")
	}

	for _, couple := range res.TvaRates {
		if couple.Rate == 20 && couple.Resolved {
			t.Fatalf("couple 20%% / %s résolu alors que le taux est absent", couple.ChannelLabel)
		}
	}

	// Monaco : 20 sur place, 0 ailleurs. Le canal sur place est irrésolvable,
	// et le backfill des deux autres l'est aussi puisqu'il repose sur ce même
	// taux 20.
	monaco := previewProduct(t, res, productMonaco2026)
	if monaco.Channels.In.Resolved {
		t.Fatal("Monaco: canal sur place résolu, want non résolu")
	}
	if monaco.Channels.TakeAway.Resolved {
		t.Fatal("Monaco: backfill emporté résolu alors que le taux source est absent")
	}

	if !hasWarning(res, WarningTvaRateUnresolved) {
		t.Fatalf("warning %q absent", WarningTvaRateUnresolved)
	}
}

// Le taux 0 est la façon dont Zelty exprime « pas vendu sur ce canal ». Le
// canal est désactivé, mais tva_*_id reste NOT NULL : il est rempli avec le
// taux le plus haut du produit, re-résolu sur le canal concerné.
func TestBuildPreviewDisablesZeroRatedChannelsAndBackfills(t *testing.T) {
	res := previewFixture(t, fixtureZelty2026, defaultLookups())

	carbonara := previewProduct(t, res, productCarbonara26)

	if !carbonara.Channels.In.Available {
		t.Fatal("canal sur place désactivé alors que le taux vaut 10")
	}
	if carbonara.Channels.In.TvaID != 2 || carbonara.Channels.In.Backfilled {
		t.Fatalf("canal sur place = %+v, want tva_id 2 sans backfill", carbonara.Channels.In)
	}

	cases := []struct {
		channel string
		got     PreviewChannel
		wantTva int
	}{
		{"emporté", carbonara.Channels.TakeAway, 5},
		{"livraison", carbonara.Channels.Delivery, 8},
	}
	for _, tc := range cases {
		if tc.got.Available {
			t.Fatalf("canal %s : available = true, want false (taux 0)", tc.channel)
		}
		if !tc.got.Backfilled || !tc.got.Resolved {
			t.Fatalf("canal %s = %+v, want backfillé et résolu", tc.channel, tc.got)
		}
		if tc.got.TvaID != tc.wantTva {
			t.Fatalf("canal %s : tva_id = %d, want %d (taux 10 re-résolu sur ce canal)", tc.channel, tc.got.TvaID, tc.wantTva)
		}
		// Zelty n'a qu'un prix : le backfill de prix est un no-op ici.
		if tc.got.Price != 1390 || tc.got.PriceBackfilled {
			t.Fatalf("canal %s : prix = %d (backfillé=%v), want 1390 sans backfill", tc.channel, tc.got.Price, tc.got.PriceBackfilled)
		}
	}

	// Le couple ajouté par le backfill est signalé comme tel.
	found := false
	for _, couple := range res.TvaRates {
		if couple.Rate == 10 && couple.Channel == TvaChannelTakeAway {
			found = true
		}
	}
	if !found {
		t.Fatal("le couple 10%% / take_away n'apparaît pas dans la liste à résoudre")
	}
}

// La classification par défaut : un libellé qui ouvre la liste d'au moins un
// produit devient une catégorie, les autres restent des tags.
func TestBuildPreviewClassifiesFirstLabelAsCategory(t *testing.T) {
	res := previewFixture(t, fixtureZelty2026, defaultLookups())

	if got := previewTag(t, res, tagNosPizza2026); got.Class != string(TagClassCategory) {
		t.Fatalf("NOS PIZZA classé %q, want %q", got.Class, TagClassCategory)
	}
	if got := previewTag(t, res, tagBaseTomate2026); got.Class != string(TagClassTag) {
		t.Fatalf("BASE TOMATE classé %q, want %q", got.Class, TagClassTag)
	}

	// Une pizza reçoit NOS PIZZA en catégorie et garde ses autres libellés.
	pizza := previewProduct(t, res, productCreeTa2026)
	if pizza.CategoryExternalID != tagNosPizza2026 {
		t.Fatalf("catégorie = %q, want %q", pizza.CategoryExternalID, tagNosPizza2026)
	}
	if pizza.CategorySource != CategorySourceFirstTag {
		t.Fatalf("CategorySource = %q, want %q", pizza.CategorySource, CategorySourceFirstTag)
	}
	if pizza.NeedsCategory {
		t.Fatal("NeedsCategory = true alors que la catégorie est déterminée")
	}
	for _, id := range pizza.TagExternalIDs {
		if id == tagNosPizza2026 {
			t.Fatal("la catégorie retenue figure aussi dans les tags")
		}
	}
	if len(pizza.TagExternalIDs) == 0 {
		t.Fatal("aucun tag conservé alors que le produit en porte plusieurs")
	}

	// Le mapping compact porte la même décision.
	if got := res.Decisions.CategoryPerProduct[productCreeTa2026]; got != tagNosPizza2026 {
		t.Fatalf("CategoryPerProduct[%s] = %q, want %q", productCreeTa2026, got, tagNosPizza2026)
	}
	if got := res.Decisions.TagClassification[tagVege2026]; got != TagClassTag {
		t.Fatalf("TagClassification[VÉGÉ] = %q, want %q", got, TagClassTag)
	}
}

// Un produit sans aucun libellé n'a pas de catégorie, et la catégorie est
// obligatoire : le cas doit remonter, pas passer.
func TestBuildPreviewFlagsProductsNeedingCategory(t *testing.T) {
	cases := []struct {
		fixture string
		want    int
	}{
		// Les quatre lignes de frais de 2025 n'ont ni prix ni libellé.
		{fixtureZelty2025, 4},
		// En 2026 chaque produit ouvre sa liste par un libellé, qui devient
		// donc une catégorie.
		{fixtureZelty2026, 0},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			res := previewFixture(t, tc.fixture, defaultLookups())

			if res.Summary.ProductsNeedingCategory != tc.want {
				t.Fatalf("ProductsNeedingCategory = %d, want %d", res.Summary.ProductsNeedingCategory, tc.want)
			}
		})
	}

	res := previewFixture(t, fixtureZelty2025, defaultLookups())
	frais := previewProduct(t, res, productFrais2025)

	if !frais.NeedsCategory {
		t.Fatal("NeedsCategory = false pour une ligne sans libellé")
	}
	if frais.CategorySource != CategorySourceNone || frais.CategoryExternalID != "" {
		t.Fatalf("catégorie = %q (source %q), want vide", frais.CategoryExternalID, frais.CategorySource)
	}
	if frais.Status != ProductStatusRemovedFromMenu {
		t.Fatalf("Status = %q, want %q", frais.Status, ProductStatusRemovedFromMenu)
	}
	if _, ok := res.Decisions.CategoryPerProduct[productFrais2025]; ok {
		t.Fatal("CategoryPerProduct contient un produit sans catégorie")
	}
	if !hasWarning(res, WarningProductNeedsCategory) {
		t.Fatalf("warning %q absent", WarningProductNeedsCategory)
	}
}

// Déduplication : aucune contrainte d'unicité n'existe en base, un import
// répété créerait des homonymes. La preview propose de réutiliser l'existant.
func TestBuildPreviewReusesExistingCategoriesAndTags(t *testing.T) {
	lk := defaultLookups()
	lk.ExistingCategories = []ExistingCategory{
		{CategID: 41, MerchantCategID: "41", Name: "nos pizza"}, // casse différente
	}
	lk.ExistingTags = []ExistingTag{
		{TagID: "tag-vege", Name: "VÉGÉ"},
	}

	res := previewFixture(t, fixtureZelty2026, lk)

	category := previewTag(t, res, tagNosPizza2026)
	if category.Action != ActionReuseExisting {
		t.Fatalf("NOS PIZZA action = %q, want %q", category.Action, ActionReuseExisting)
	}
	if category.ExistingCategoryID != "41" {
		t.Fatalf("ExistingCategoryID = %q, want %q (merchant_categ_id)", category.ExistingCategoryID, "41")
	}

	tag := previewTag(t, res, tagVege2026)
	if tag.Action != ActionReuseExisting {
		t.Fatalf("VÉGÉ action = %q, want %q", tag.Action, ActionReuseExisting)
	}
	if tag.ExistingTagID != "tag-vege" {
		t.Fatalf("ExistingTagID = %q, want %q", tag.ExistingTagID, "tag-vege")
	}

	if res.Summary.CategoriesReused != 1 || res.Summary.TagsReused != 1 {
		t.Fatalf("réutilisations = (%d catégories, %d tags), want (1, 1)",
			res.Summary.CategoriesReused, res.Summary.TagsReused)
	}
}

// Collision de nom : détectée et proposée en « skip ». C'est ce qui remplace le
// double-appel Redis du chemin unitaire, inutilisable en lot.
func TestBuildPreviewDetectsNameCollision(t *testing.T) {
	lk := defaultLookups()
	lk.ExistingProducts = []ExistingProduct{
		{ProductID: 7788, Name: "margherita 🍅🧀"}, // casse différente
	}

	res := previewFixture(t, fixtureZelty2026, lk)

	product := previewProduct(t, res, productMargherita)
	if product.NameCollision == nil {
		t.Fatal("aucune collision détectée sur un nom déjà pris")
	}
	if product.NameCollision.ExistingProductID != 7788 {
		t.Fatalf("ExistingProductID = %d, want 7788", product.NameCollision.ExistingProductID)
	}
	if product.NameCollision.Resolution != string(CollisionSkip) {
		t.Fatalf("Resolution = %q, want %q", product.NameCollision.Resolution, CollisionSkip)
	}
	if got := res.Decisions.NameCollisions[productMargherita]; got != CollisionSkip {
		t.Fatalf("Decisions.NameCollisions[%s] = %q, want %q", productMargherita, got, CollisionSkip)
	}
	if res.Summary.ProductsWithNameCollision != 1 {
		t.Fatalf("ProductsWithNameCollision = %d, want 1", res.Summary.ProductsWithNameCollision)
	}
	if !hasWarning(res, WarningProductNameCollision) {
		t.Fatalf("warning %q absent", WarningProductNameCollision)
	}
}

// Un produit Wello issu d'un import précédent du même provider n'entre pas en
// collision : c'est le mapping qui fait autorité, pas le nom.
func TestBuildPreviewIgnoresCollisionWithOwnImportedProduct(t *testing.T) {
	imp := &IntermediateImport{
		Provider: ZeltySlug,
		Products: []CanonicalProduct{{ExternalID: "ZD-NEW", Name: "Pizza", CategoryExternalID: "cat-1"}},
	}

	lk := defaultLookups()
	lk.ExistingProducts = []ExistingProduct{{ProductID: 7, Name: "Pizza"}}
	lk.Imported.Products = map[string]int{"ZD-OLD": 7}

	res, err := BuildPreview(imp, lk)
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}

	if res.Products[0].NameCollision != nil {
		t.Fatalf("collision signalée avec un produit issu du même import: %+v", res.Products[0].NameCollision)
	}
	if res.Summary.ProductsWithNameCollision != 0 {
		t.Fatalf("ProductsWithNameCollision = %d, want 0", res.Summary.ProductsWithNameCollision)
	}
}

// Idempotence : une entité déjà présente dans import_*_mapping est signalée et
// ne compte pas dans ce qui sera créé.
func TestBuildPreviewMarksAlreadyImportedEntities(t *testing.T) {
	lk := defaultLookups()
	lk.Imported.Products = map[string]int{productCreeTa2026: 4242}
	lk.Imported.Tags = map[string]string{tagVege2026: "tag-existing"}
	lk.Imported.Attributes = map[string]string{"ZO247656": "attribute-existing"}

	res := previewFixture(t, fixtureZelty2026, lk)

	product := previewProduct(t, res, productCreeTa2026)
	if product.Action != ActionAlreadyImported {
		t.Fatalf("action = %q, want %q", product.Action, ActionAlreadyImported)
	}
	if res.Summary.ProductsAlreadyImported != 1 {
		t.Fatalf("ProductsAlreadyImported = %d, want 1", res.Summary.ProductsAlreadyImported)
	}
	if res.Summary.ProductsToCreate != 140 {
		t.Fatalf("ProductsToCreate = %d, want 140 (141 - 1 déjà importé)", res.Summary.ProductsToCreate)
	}

	if got := previewTag(t, res, tagVege2026); got.Action != ActionAlreadyImported {
		t.Fatalf("tag VÉGÉ action = %q, want %q", got.Action, ActionAlreadyImported)
	}

	var attribute PreviewAttribute
	for _, a := range res.Attributes {
		if a.ExternalID == "ZO247656" {
			attribute = a
		}
	}
	if attribute.Action != ActionAlreadyImported {
		t.Fatalf("attribut action = %q, want %q", attribute.Action, ActionAlreadyImported)
	}
	if res.Summary.AttributesAlreadyImported != 1 || res.Summary.AttributesToCreate != 11 {
		t.Fatalf("attributs = (%d déjà importés, %d à créer), want (1, 11)",
			res.Summary.AttributesAlreadyImported, res.Summary.AttributesToCreate)
	}
	// Les options d'un groupe déjà importé ne sont pas recomptées.
	if res.Summary.OptionsToCreate != 72 {
		t.Fatalf("OptionsToCreate = %d, want 72 (78 - les 6 du groupe déjà importé)", res.Summary.OptionsToCreate)
	}
}

// Un produit déjà importé ne sera pas créé : lui réclamer une catégorie ou
// arbitrer une collision n'aurait pas de sens.
func TestBuildPreviewDoesNotInstructAlreadyImportedProducts(t *testing.T) {
	lk := defaultLookups()
	lk.Imported.Products = map[string]int{productFrais2025: 5150}

	res := previewFixture(t, fixtureZelty2025, lk)

	frais := previewProduct(t, res, productFrais2025)
	if frais.Action != ActionAlreadyImported {
		t.Fatalf("action = %q, want %q", frais.Action, ActionAlreadyImported)
	}
	if frais.NeedsCategory {
		t.Fatal("NeedsCategory = true sur un produit qui ne sera pas créé")
	}
	if res.Summary.ProductsNeedingCategory != 3 {
		t.Fatalf("ProductsNeedingCategory = %d, want 3 (4 lignes de frais - 1 déjà importée)", res.Summary.ProductsNeedingCategory)
	}
}

// Le résumé doit rester cohérent avec le détail : c'est lui que le wizard
// affiche avant de laisser valider.
func TestBuildPreviewSummaryMatchesDetail(t *testing.T) {
	res := previewFixture(t, fixtureZelty2026, defaultLookups())

	if got := res.Summary.ProductsToCreate + res.Summary.ProductsAlreadyImported; got != len(res.Products) {
		t.Fatalf("à créer + déjà importés = %d, want %d produits", got, len(res.Products))
	}
	if got := res.Summary.CategoriesToCreate + res.Summary.CategoriesReused +
		res.Summary.TagsToCreate + res.Summary.TagsReused; got != len(res.Tags) {
		t.Fatalf("somme des classifications = %d, want %d libellés", got, len(res.Tags))
	}
	if res.Summary.ProductsRemovedFromMenu != 0 {
		t.Fatalf("ProductsRemovedFromMenu = %d, want 0 en 2026", res.Summary.ProductsRemovedFromMenu)
	}
	if res.Summary.TagsSynthetic != 0 {
		t.Fatalf("TagsSynthetic = %d, want 0 (tous les libellés sont déclarés)", res.Summary.TagsSynthetic)
	}
	if res.Provider != ZeltySlug {
		t.Fatalf("Provider = %q, want %q", res.Provider, ZeltySlug)
	}
	// Token et expiration sont posés par le service, pas par le cœur pur.
	if res.Token != "" || res.ExpiresAt != "" {
		t.Fatalf("Token/ExpiresAt renseignés par BuildPreview: %q / %q", res.Token, res.ExpiresAt)
	}
}

// Un produit portant deux libellés classés catégorie n'en garde qu'une : le
// second doit être signalé, pas escamoté.
func TestBuildPreviewReportsDroppedSecondCategoryLabel(t *testing.T) {
	imp := &IntermediateImport{
		Provider: ZeltySlug,
		Tags: []CanonicalTag{
			{ExternalID: "ZT1", Name: "PIZZAS"},
			{ExternalID: "ZT2", Name: "SUGGESTIONS"},
		},
		Products: []CanonicalProduct{
			{ExternalID: "ZD1", Name: "Margherita", TagExternalIDs: []string{"ZT1", "ZT2"}},
			{ExternalID: "ZD2", Name: "Coup de cœur", TagExternalIDs: []string{"ZT2"}},
		},
	}

	res, err := BuildPreview(imp, defaultLookups())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}

	// ZT1 et ZT2 ouvrent chacun la liste d'un produit : tous deux catégories.
	margherita := res.Products[0]
	if margherita.CategoryExternalID != "ZT1" {
		t.Fatalf("catégorie = %q, want ZT1", margherita.CategoryExternalID)
	}
	if len(margherita.DroppedLabelExternalIDs) != 1 || margherita.DroppedLabelExternalIDs[0] != "ZT2" {
		t.Fatalf("DroppedLabelExternalIDs = %v, want [ZT2]", margherita.DroppedLabelExternalIDs)
	}
	if len(margherita.TagExternalIDs) != 0 {
		t.Fatalf("TagExternalIDs = %v, want vide (les deux libellés sont des catégories)", margherita.TagExternalIDs)
	}
	if !hasWarning(res, WarningLabelDropped) {
		t.Fatalf("warning %q absent", WarningLabelDropped)
	}
}

// Un libellé synthétisé par le parser doit être signalé : c'est souvent le
// symptôme d'un export tronqué.
func TestBuildPreviewReportsSyntheticLabels(t *testing.T) {
	imp, err := NewZeltyProvider().Parse(buildXLSX(t, [][]string{
		{"ID", "Type", "Nom"},
		{"ZT1", "Tag", "NOS PIZZA"},
		{},
		{"ID", "Type", "Nom", "Prix", "TVA", "TVA emporté", "TVA livraison", "Tags"},
		{"ZD1", "Produit", "Margherita", "9,9", "10", "10", "10", "NOS PIZZA, SIGNATURE"},
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	res, err := BuildPreview(imp, defaultLookups())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}

	if res.Summary.TagsSynthetic != 1 {
		t.Fatalf("TagsSynthetic = %d, want 1", res.Summary.TagsSynthetic)
	}
	if !hasWarning(res, WarningTagSynthesized) {
		t.Fatalf("warning %q absent", WarningTagSynthesized)
	}
}

// Un taux absent (colonne non fournie) n'est pas un taux nul : il ne désactive
// pas le canal, il réclame une décision.
func TestBuildPreviewFlagsMissingTvaRate(t *testing.T) {
	rate := 10.0
	imp := &IntermediateImport{
		Provider: WelloGenericSlug,
		Products: []CanonicalProduct{{
			ExternalID:         "wg-p-1",
			Name:               "Margherita",
			CategoryExternalID: "wg-c-1",
			PriceIn:            990,
			TvaRateIn:          &rate,
		}},
	}

	res, err := BuildPreview(imp, defaultLookups())
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}

	product := res.Products[0]
	if !product.Channels.TakeAway.Available {
		t.Fatal("canal emporté désactivé alors que le taux est absent, pas nul")
	}
	if product.Channels.TakeAway.Resolved || product.Channels.TakeAway.TvaID != 0 {
		t.Fatalf("canal emporté = %+v, want non résolu", product.Channels.TakeAway)
	}
	if !hasWarning(res, WarningTvaRateMissing) {
		t.Fatalf("warning %q absent", WarningTvaRateMissing)
	}
}

func TestBuildPreviewRejectsNilImport(t *testing.T) {
	if _, err := BuildPreview(nil, defaultLookups()); err == nil {
		t.Fatal("BuildPreview(nil) = nil, want une erreur")
	}
}

func hasWarning(res *PreviewResult, code string) bool {
	for _, warning := range res.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

// Référentiel de TVA tel qu'il existe réellement en base, relevé sur un
// marchand de production. Il sert à vérifier que la résolution fonctionne sur
// les valeurs qui arrivent vraiment, et non sur celles que décrit le
// commentaire — désormais faux — de tva_categories.delivery_type.
func productionTvaRates() []TvaRateRow {
	return []TvaRateRow{
		// tva_id -1 et 0 existent mais sont désactivés : la requête les écarte.
		{TvaID: 1, Channel: TvaChannelTakeAway, Rate: 5.5},
		{TvaID: 2, Channel: TvaChannelTakeAway, Rate: 10},
		{TvaID: 3, Channel: TvaChannelTakeAway, Rate: 20},
		{TvaID: 5, Channel: TvaChannelIn, Rate: 10},
		{TvaID: 6, Channel: TvaChannelIn, Rate: 20},
		{TvaID: 7, Channel: TvaChannelDelivery, Rate: 10},
		{TvaID: 8, Channel: TvaChannelDelivery, Rate: 5.5},
		{TvaID: 9, Channel: TvaChannelDelivery, Rate: 20},
	}
}

// Sur le référentiel réel, l'export 2026 doit se résoudre presque entièrement.
// Seul 5,5 % sur place reste ouvert : ce taux n'est tout simplement pas
// configuré pour ce canal, et c'est exactement ce que l'écran de vérification
// est là pour faire compléter.
func TestBuildPreviewAgainstProductionTvaReferential(t *testing.T) {
	lk := defaultLookups()
	lk.TvaRates = productionTvaRates()

	res := previewFixture(t, fixtureZelty2026, lk)

	unresolved := make(map[string]bool)
	for _, couple := range res.TvaRates {
		if !couple.Resolved {
			unresolved[string(couple.Channel)+"/"+fmt.Sprintf("%g", couple.Rate)] = true
			continue
		}
		if couple.TvaID == 0 {
			t.Fatalf("couple %v%% / %s marqué résolu sans tva_id", couple.Rate, couple.Channel)
		}
	}

	if len(unresolved) != 1 || !unresolved["IN/5.5"] {
		t.Fatalf("couples non résolus = %v, want uniquement IN/5.5", unresolved)
	}

	// Les taux qui existent bien doivent pointer sur le bon identifiant, canal
	// par canal — c'est là que la confusion sur delivery_type se voyait.
	cases := []struct {
		rate    float64
		channel TvaChannel
		want    int
	}{
		{10, TvaChannelIn, 5},
		{20, TvaChannelIn, 6},
		{10, TvaChannelTakeAway, 2},
		{20, TvaChannelTakeAway, 3},
		{10, TvaChannelDelivery, 7},
		{5.5, TvaChannelDelivery, 8},
		{20, TvaChannelDelivery, 9},
	}
	for _, tc := range cases {
		got, ok := res.Decisions.TvaMapping[TvaRateKey{Rate: tc.rate, Channel: tc.channel}]
		if !ok {
			t.Fatalf("couple %v%% / %s absent du mapping", tc.rate, tc.channel)
		}
		if got != tc.want {
			t.Fatalf("couple %v%% / %s = tva_id %d, want %d", tc.rate, tc.channel, got, tc.want)
		}
	}
}

// Un delivery_type inconnu est écarté sans faire tomber la preview, mais ne
// doit jamais résoudre un couple par accident.
func TestTvaResolverIgnoresUnknownChannels(t *testing.T) {
	resolver := newTvaResolver([]TvaRateRow{
		{TvaID: 42, Channel: TvaChannel("SNO"), Rate: 10},
		{TvaID: 5, Channel: TvaChannelIn, Rate: 10},
	})

	if id, ok := resolver.resolve(10, TvaChannelIn); !ok || id != 5 {
		t.Fatalf("résolution sur place = (%d, %v), want (5, true)", id, ok)
	}
	if _, ok := resolver.resolve(10, TvaChannelTakeAway); ok {
		t.Fatal("un canal sans taux configuré ne doit pas se résoudre")
	}
	if resolver.hasID(42, 10, TvaChannelIn) {
		t.Fatal("un tva_id d'un autre canal est accepté")
	}
}
