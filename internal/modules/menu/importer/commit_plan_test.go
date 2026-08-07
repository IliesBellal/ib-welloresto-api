package importer

import (
	"strings"
	"testing"
)

// defaultDecisions rejoue les propositions de la preview : c'est le corps que
// le wizard renvoie quand l'utilisateur accepte tout.
func defaultDecisions(t *testing.T, imp *IntermediateImport, lk PreviewLookups) ImportDecisions {
	t.Helper()

	res, err := BuildPreview(imp, lk)
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	return res.Decisions
}

func planFixture(t *testing.T, fixture string, lk PreviewLookups) *CommitPlan {
	t.Helper()

	imp := parseZeltyFixture(t, fixture)
	plan, blockers := BuildCommitPlan(imp, defaultDecisions(t, imp, lk), lk)
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	return plan
}

func plannedProduct(t *testing.T, plan *CommitPlan, externalID string) PlannedProduct {
	t.Helper()

	for _, p := range plan.Products {
		if p.ExternalID == externalID {
			return p
		}
	}
	t.Fatalf("produit %q absent du plan", externalID)
	return PlannedProduct{}
}

func hasBlocker(blockers []CommitBlocker, code, ref string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code && (ref == "" || blocker.Ref == ref) {
			return true
		}
	}
	return false
}

// Le lot 2026 accepté tel quel doit produire un plan entièrement résolu.
func TestBuildCommitPlanResolvesFixture(t *testing.T) {
	plan := planFixture(t, fixtureZelty2026, defaultLookups())

	if plan.Provider != ZeltySlug {
		t.Fatalf("Provider = %q, want %q", plan.Provider, ZeltySlug)
	}
	if len(plan.Products) != 141 {
		t.Fatalf("produits planifiés = %d, want 141", len(plan.Products))
	}
	if len(plan.Attributes) != 12 {
		t.Fatalf("groupes d'options = %d, want 12", len(plan.Attributes))
	}
	if len(plan.Categories)+len(plan.Tags) != 19 {
		t.Fatalf("catégories (%d) + tags (%d) = %d, want 19 libellés",
			len(plan.Categories), len(plan.Tags), len(plan.Categories)+len(plan.Tags))
	}

	// Tout produit matérialisable porte une catégorie et trois tva_id.
	for _, p := range plan.Products {
		if !p.Materializable() {
			continue
		}
		if p.CategoryExternalID == "" {
			t.Fatalf("produit %q sans catégorie", p.ExternalID)
		}
		if p.TvaInID == 0 || p.TvaTakeAwayID == 0 || p.TvaDeliveryID == 0 {
			t.Fatalf("produit %q : tva_id = (%d, %d, %d), want tous résolus",
				p.ExternalID, p.TvaInID, p.TvaTakeAwayID, p.TvaDeliveryID)
		}
		if p.Status != ProductStatusAvailable && p.Status != ProductStatusRemovedFromMenu {
			t.Fatalf("produit %q : statut %q inattendu", p.ExternalID, p.Status)
		}
	}
}

// Carbonara : 10 sur place, 0 ailleurs. Les deux canaux fermés reçoivent quand
// même un tva_id, re-résolu depuis le taux le plus haut du produit.
func TestBuildCommitPlanResolvesChannelsAndBackfill(t *testing.T) {
	plan := planFixture(t, fixtureZelty2026, defaultLookups())

	carbonara := plannedProduct(t, plan, productCarbonara26)

	if !carbonara.AvailableIn || carbonara.TvaInID != 2 {
		t.Fatalf("sur place = (available %v, tva %d), want (true, 2)", carbonara.AvailableIn, carbonara.TvaInID)
	}
	if carbonara.AvailableTakeAway || carbonara.TvaTakeAwayID != 5 {
		t.Fatalf("emporté = (available %v, tva %d), want (false, 5)", carbonara.AvailableTakeAway, carbonara.TvaTakeAwayID)
	}
	if carbonara.AvailableDelivery || carbonara.TvaDeliveryID != 8 {
		t.Fatalf("livraison = (available %v, tva %d), want (false, 8)", carbonara.AvailableDelivery, carbonara.TvaDeliveryID)
	}
	if carbonara.PriceIn != 1390 || carbonara.PriceTakeAway != 1390 || carbonara.PriceDelivery != 1390 {
		t.Fatalf("prix = (%d, %d, %d), want 1390 partout",
			carbonara.PriceIn, carbonara.PriceTakeAway, carbonara.PriceDelivery)
	}
}

// Une ligne sans prix est importée, en statut removed_from_menu.
func TestBuildCommitPlanMarksZeroPricedProducts(t *testing.T) {
	lk := defaultLookups()

	imp := parseZeltyFixture(t, fixtureZelty2025)
	decisions := defaultDecisions(t, imp, lk)

	// Les lignes de frais n'ont aucun libellé : la catégorie doit être imposée.
	frais := findCanonicalProduct(t, imp, productFrais2025)
	decisions.CategoryPerProduct[frais.ExternalID] = firstCategoryLabel(t, imp, decisions)
	for _, p := range imp.Products {
		if len(p.TagExternalIDs) == 0 {
			decisions.CategoryPerProduct[p.ExternalID] = decisions.CategoryPerProduct[frais.ExternalID]
		}
	}

	plan, blockers := BuildCommitPlan(imp, decisions, lk)
	if len(blockers) > 0 {
		t.Fatalf("blocages inattendus : %s", BlockersMessage(blockers))
	}

	planned := plannedProduct(t, plan, productFrais2025)
	if planned.Status != ProductStatusRemovedFromMenu {
		t.Fatalf("statut = %q, want %q", planned.Status, ProductStatusRemovedFromMenu)
	}
	if !planned.Materializable() {
		t.Fatal("une ligne sans prix doit tout de même être créée")
	}
}

// Règle de rejet : un produit sans catégorie bloque tout le lot.
func TestBuildCommitPlanRejectsProductWithoutCategory(t *testing.T) {
	lk := defaultLookups()
	imp := parseZeltyFixture(t, fixtureZelty2025)

	plan, blockers := BuildCommitPlan(imp, defaultDecisions(t, imp, lk), lk)

	if plan != nil {
		t.Fatal("un plan a été produit malgré des produits sans catégorie")
	}
	if !hasBlocker(blockers, BlockerProductNeedsCategory, productFrais2025) {
		t.Fatalf("blocage %q attendu sur %q, obtenu : %s",
			BlockerProductNeedsCategory, productFrais2025, BlockersMessage(blockers))
	}
}

// Règle de rejet : un taux absent de la configuration du marchand bloque.
func TestBuildCommitPlanRejectsUnresolvedTvaRate(t *testing.T) {
	lk := defaultLookups()
	lk.TvaRates = tvaRatesWithout(20)

	imp := parseZeltyFixture(t, fixtureZelty2026)

	plan, blockers := BuildCommitPlan(imp, ImportDecisions{}, lk)

	if plan != nil {
		t.Fatal("un plan a été produit malgré un taux non résolu")
	}
	if !hasBlocker(blockers, BlockerTvaRateUnresolved, productMonaco2026) {
		t.Fatalf("blocage %q attendu sur %q, obtenu : %s",
			BlockerTvaRateUnresolved, productMonaco2026, BlockersMessage(blockers))
	}
}

// Règle de rejet : un canal fermé dont aucun taux de repli ne se résout bloque
// aussi — tva_*_id est NOT NULL.
func TestBuildCommitPlanRejectsUnresolvableBackfill(t *testing.T) {
	zero, ten := 0.0, 10.0

	imp := &IntermediateImport{
		Provider: ZeltySlug,
		Tags:     []CanonicalTag{{ExternalID: "ZT1", Name: "PIZZAS"}},
		Products: []CanonicalProduct{{
			ExternalID: "ZD1", Name: "Margherita", TagExternalIDs: []string{"ZT1"},
			PriceIn: 990, PriceTakeAway: 990, PriceDelivery: 990,
			TvaRateIn: &ten, TvaRateTakeAway: &zero, TvaRateDelivery: &zero,
		}},
	}

	lk := defaultLookups()
	// Le taux 10 n'existe que sur place : le repli des canaux fermés échoue.
	lk.TvaRates = []TvaRateRow{{TvaID: 2, Channel: TvaChannelIn, Rate: 10}}

	plan, blockers := BuildCommitPlan(imp, ImportDecisions{}, lk)

	if plan != nil {
		t.Fatal("un plan a été produit malgré un repli irrésolvable")
	}
	if !hasBlocker(blockers, BlockerTvaRateUnresolved, "ZD1") {
		t.Fatalf("blocage %q attendu, obtenu : %s", BlockerTvaRateUnresolved, BlockersMessage(blockers))
	}
	if !strings.Contains(BlockersMessage(blockers), BlockerTvaRateUnresolved) {
		t.Fatalf("message = %s", BlockersMessage(blockers))
	}
}

// Règle de rejet : une collision non tranchée bloque.
func TestBuildCommitPlanRejectsUnresolvedCollision(t *testing.T) {
	lk := defaultLookups()
	lk.ExistingProducts = []ExistingProduct{{ProductID: 7788, Name: "margherita 🍅🧀"}}

	imp := parseZeltyFixture(t, fixtureZelty2026)

	// On repart des défauts de la preview mais on efface l'arbitrage, comme le
	// ferait un client qui renverrait des décisions incomplètes.
	decisions := defaultDecisions(t, imp, lk)
	delete(decisions.NameCollisions, productMargherita)

	plan, blockers := BuildCommitPlan(imp, decisions, lk)

	if plan != nil {
		t.Fatal("un plan a été produit malgré une collision non tranchée")
	}
	if !hasBlocker(blockers, BlockerNameCollisionUnresolved, productMargherita) {
		t.Fatalf("blocage %q attendu sur %q, obtenu : %s",
			BlockerNameCollisionUnresolved, productMargherita, BlockersMessage(blockers))
	}
}

// Collision tranchée : « ignorer » sort le produit du lot, « importer quand
// même » le laisse dedans.
func TestBuildCommitPlanAppliesCollisionResolutions(t *testing.T) {
	lk := defaultLookups()
	lk.ExistingProducts = []ExistingProduct{{ProductID: 7788, Name: "margherita 🍅🧀"}}

	imp := parseZeltyFixture(t, fixtureZelty2026)

	cases := []struct {
		name             string
		resolution       NameCollisionResolution
		wantMaterialized bool
	}{
		{"ignorer", CollisionSkip, false},
		{"importer quand même", CollisionImportAnyway, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decisions := defaultDecisions(t, imp, lk)
			decisions.NameCollisions[productMargherita] = tc.resolution

			plan, blockers := BuildCommitPlan(imp, decisions, lk)
			if len(blockers) > 0 {
				t.Fatalf("blocages inattendus : %s", BlockersMessage(blockers))
			}

			planned := plannedProduct(t, plan, productMargherita)
			if planned.Materializable() != tc.wantMaterialized {
				t.Fatalf("Materializable() = %v, want %v", planned.Materializable(), tc.wantMaterialized)
			}
		})
	}
}

// Les décisions du client ne sont pas crues sur parole : un tva_id qui ne
// correspond pas au canal annoncé est refusé.
func TestBuildCommitPlanRejectsTvaMappingFromWrongChannel(t *testing.T) {
	lk := defaultLookups()
	imp := parseZeltyFixture(t, fixtureZelty2026)

	decisions := defaultDecisions(t, imp, lk)
	// 2 est le tva_id de 10 % sur place, pas en livraison (qui vaut 8).
	decisions.TvaMapping[TvaRateKey{Rate: 10, Channel: TvaChannelDelivery}] = 2

	plan, blockers := BuildCommitPlan(imp, decisions, lk)

	if plan != nil {
		t.Fatal("un plan a été produit malgré un tva_id du mauvais canal")
	}
	if !hasBlocker(blockers, BlockerInvalidTvaMapping, "10:1") {
		t.Fatalf("blocage %q attendu, obtenu : %s", BlockerInvalidTvaMapping, BlockersMessage(blockers))
	}
}

// Une catégorie imposée à un produit doit réellement être classée catégorie.
func TestBuildCommitPlanRejectsCategoryDecisionOnATag(t *testing.T) {
	lk := defaultLookups()
	imp := parseZeltyFixture(t, fixtureZelty2026)

	decisions := defaultDecisions(t, imp, lk)
	// BASE TOMATE est classé tag : il ne peut pas devenir la catégorie.
	decisions.CategoryPerProduct[productCreeTa2026] = tagBaseTomate2026

	plan, blockers := BuildCommitPlan(imp, decisions, lk)

	if plan != nil {
		t.Fatal("un plan a été produit malgré une catégorie qui est un tag")
	}
	if !hasBlocker(blockers, BlockerInvalidCategoryDecision, productCreeTa2026) {
		t.Fatalf("blocage %q attendu, obtenu : %s", BlockerInvalidCategoryDecision, BlockersMessage(blockers))
	}
}

// Reclasser un tag en catégorie via le wizard doit fonctionner : c'est ce que
// l'écran de classification permet, et ce dont l'export 2025 a besoin.
func TestBuildCommitPlanHonoursReclassification(t *testing.T) {
	lk := defaultLookups()
	imp := parseZeltyFixture(t, fixtureZelty2026)

	decisions := defaultDecisions(t, imp, lk)
	decisions.TagClassification[tagBaseTomate2026] = TagClassCategory
	decisions.CategoryPerProduct[productCreeTa2026] = tagBaseTomate2026

	plan, blockers := BuildCommitPlan(imp, decisions, lk)
	if len(blockers) > 0 {
		t.Fatalf("blocages inattendus : %s", BlockersMessage(blockers))
	}

	planned := plannedProduct(t, plan, productCreeTa2026)
	if planned.CategoryExternalID != tagBaseTomate2026 {
		t.Fatalf("catégorie = %q, want %q", planned.CategoryExternalID, tagBaseTomate2026)
	}
	for _, id := range planned.TagExternalIDs {
		if id == tagBaseTomate2026 {
			t.Fatal("le libellé promu en catégorie reste attaché comme tag")
		}
	}

	// Il doit maintenant figurer parmi les catégories du plan, pas les tags.
	if !hasPlannedCategory(plan, tagBaseTomate2026) {
		t.Fatal("le libellé reclassé n'apparaît pas dans les catégories du plan")
	}
	for _, tag := range plan.Tags {
		if tag.ExternalID == tagBaseTomate2026 {
			t.Fatal("le libellé reclassé apparaît encore dans les tags")
		}
	}
}

// Les produits déjà importés sont exclus des contrôles : ils ne seront pas
// créés, leur réclamer une catégorie bloquerait un lot pour rien.
func TestBuildCommitPlanExcludesAlreadyImportedFromChecks(t *testing.T) {
	lk := defaultLookups()
	// Les quatre lignes de frais de 2025 sont sans catégorie ; on les déclare
	// déjà importées pour qu'elles cessent de bloquer.
	lk.Imported.Products = map[string]int{
		"ZD1557688": 101,
		"ZD1676576": 102,
		"ZD1717009": 103,
		"ZD1717011": 104,
	}

	imp := parseZeltyFixture(t, fixtureZelty2025)

	plan, blockers := BuildCommitPlan(imp, defaultDecisions(t, imp, lk), lk)
	if len(blockers) > 0 {
		t.Fatalf("blocages inattendus : %s", BlockersMessage(blockers))
	}

	frais := plannedProduct(t, plan, productFrais2025)
	if !frais.AlreadyImported {
		t.Fatal("AlreadyImported = false sur un produit déjà mappé")
	}
	if frais.Materializable() {
		t.Fatal("un produit déjà importé ne doit pas être recréé")
	}

	created := 0
	for _, p := range plan.Products {
		if p.Materializable() {
			created++
		}
	}
	if created != 103 {
		t.Fatalf("produits à créer = %d, want 103 (107 - 4 déjà importés)", created)
	}
}

// Idempotence : tout ce qui est déjà mappé est sauté. Un ré-import du même
// fichier ne crée rien.
func TestBuildCommitPlanIsIdempotent(t *testing.T) {
	lk := defaultLookups()
	imp := parseZeltyFixture(t, fixtureZelty2026)

	lk.Imported.Products = map[string]int{}
	for i, p := range imp.Products {
		lk.Imported.Products[p.ExternalID] = 1000 + i
	}
	lk.Imported.Tags = map[string]string{}
	lk.Imported.Categories = map[string]int{}
	for i, tag := range imp.Tags {
		lk.Imported.Tags[tag.ExternalID] = "tag-existing-" + tag.ExternalID
		lk.Imported.Categories[tag.ExternalID] = 2000 + i
		lk.ExistingTags = append(lk.ExistingTags, ExistingTag{TagID: "tag-existing-" + tag.ExternalID, Name: tag.Name})
		lk.ExistingCategories = append(lk.ExistingCategories, ExistingCategory{
			CategID: 2000 + i, MerchantCategID: "2000", Name: tag.Name,
		})
	}
	lk.Imported.Attributes = map[string]string{}
	for _, attribute := range imp.Attributes {
		lk.Imported.Attributes[attribute.ExternalID] = "attribute-existing"
	}

	plan, blockers := BuildCommitPlan(imp, defaultDecisions(t, imp, lk), lk)
	if len(blockers) > 0 {
		t.Fatalf("blocages inattendus : %s", BlockersMessage(blockers))
	}

	for _, p := range plan.Products {
		if p.Materializable() {
			t.Fatalf("produit %q serait recréé", p.ExternalID)
		}
	}
	for _, category := range plan.Categories {
		if !category.AlreadyImported {
			t.Fatalf("catégorie %q serait recréée", category.ExternalID)
		}
	}
	for _, attribute := range plan.Attributes {
		if !attribute.AlreadyImported {
			t.Fatalf("groupe d'options %q serait recréé", attribute.ExternalID)
		}
	}
}

// Une catégorie créée par un import précédent puis désactivée ne peut plus être
// référencée, et le mapping empêche de la recréer : il faut le dire, pas écrire
// un produit qui pointe dans le vide. C'est le pendant en mémoire de la
// validation 2 de validateProductForCreate.
func TestBuildCommitPlanRejectsDisabledAlreadyImportedCategory(t *testing.T) {
	ten := 10.0
	imp := &IntermediateImport{
		Provider: ZeltySlug,
		Tags:     []CanonicalTag{{ExternalID: "ZT1", Name: "PIZZAS"}},
		Products: []CanonicalProduct{{
			ExternalID: "ZD1", Name: "Margherita", TagExternalIDs: []string{"ZT1"},
			PriceIn: 990, PriceTakeAway: 990, PriceDelivery: 990,
			TvaRateIn: &ten, TvaRateTakeAway: &ten, TvaRateDelivery: &ten,
		}},
	}

	lk := defaultLookups()
	// Mappée, mais absente de ExistingCategories (qui ne liste que les actives).
	lk.Imported.Categories = map[string]int{"ZT1": 55}

	plan, blockers := BuildCommitPlan(imp, ImportDecisions{}, lk)

	if plan != nil {
		t.Fatal("un plan a été produit avec une catégorie désactivée")
	}
	if !hasBlocker(blockers, BlockerProductNeedsCategory, "ZD1") {
		t.Fatalf("blocage %q attendu, obtenu : %s", BlockerProductNeedsCategory, BlockersMessage(blockers))
	}
}

// Un tag mappé mais physiquement supprimé est simplement écarté : contrairement
// à la catégorie, il est facultatif, et faire échouer le produit entier pour
// lui serait disproportionné.
func TestBuildCommitPlanDropsDeletedAlreadyImportedTag(t *testing.T) {
	ten := 10.0
	imp := &IntermediateImport{
		Provider: ZeltySlug,
		Tags: []CanonicalTag{
			{ExternalID: "ZT1", Name: "PIZZAS"},
			{ExternalID: "ZT2", Name: "VEGE"},
		},
		Products: []CanonicalProduct{{
			ExternalID: "ZD1", Name: "Margherita", TagExternalIDs: []string{"ZT1", "ZT2"},
			PriceIn: 990, PriceTakeAway: 990, PriceDelivery: 990,
			TvaRateIn: &ten, TvaRateTakeAway: &ten, TvaRateDelivery: &ten,
		}},
	}

	lk := defaultLookups()
	lk.Imported.Tags = map[string]string{"ZT2": "tag-disparu"}

	plan, blockers := BuildCommitPlan(imp, ImportDecisions{}, lk)
	if len(blockers) > 0 {
		t.Fatalf("blocages inattendus : %s", BlockersMessage(blockers))
	}

	planned := plannedProduct(t, plan, "ZD1")
	if len(planned.TagExternalIDs) != 0 {
		t.Fatalf("TagExternalIDs = %v, want vide (le tag mappé n'existe plus)", planned.TagExternalIDs)
	}
}

// La déduplication par nom se retrouve dans le plan sous forme de
// réutilisation, pas de création.
func TestBuildCommitPlanReusesExistingEntities(t *testing.T) {
	lk := defaultLookups()
	lk.ExistingCategories = []ExistingCategory{{CategID: 41, MerchantCategID: "41", Name: "nos pizza"}}
	lk.ExistingTags = []ExistingTag{{TagID: "tag-vege", Name: "VÉGÉ"}}

	plan := planFixture(t, fixtureZelty2026, lk)

	reusedCategory := false
	for _, category := range plan.Categories {
		if category.ExternalID == tagNosPizza2026 {
			reusedCategory = true
			if category.ReuseMerchantCategID != "41" || category.ReuseCategID != 41 {
				t.Fatalf("catégorie réutilisée = %+v, want categ_id 41 / merchant_categ_id \"41\"", category)
			}
		}
	}
	if !reusedCategory {
		t.Fatal("NOS PIZZA absent des catégories du plan")
	}

	reusedTag := false
	for _, tag := range plan.Tags {
		if tag.ExternalID == tagVege2026 {
			reusedTag = true
			if tag.ReuseTagID != "tag-vege" {
				t.Fatalf("tag réutilisé = %+v, want tag-vege", tag)
			}
		}
	}
	if !reusedTag {
		t.Fatal("VÉGÉ absent des tags du plan")
	}
}

// Équivalence avec validateProductForCreate : chacun des cas que la validation
// unitaire rejette doit produire un blocage ici. Le commentaire de
// assignChannels rappelle que les deux doivent rester synchronisées.
func TestBuildCommitPlanMirrorsValidateProductForCreate(t *testing.T) {
	ten := 10.0
	newImport := func(rateIn, rateTakeAway, rateDelivery *float64) *IntermediateImport {
		return &IntermediateImport{
			Provider: ZeltySlug,
			Tags:     []CanonicalTag{{ExternalID: "ZT1", Name: "PIZZAS"}},
			Products: []CanonicalProduct{{
				ExternalID: "ZD1", Name: "Margherita", TagExternalIDs: []string{"ZT1"},
				PriceIn: 990, PriceTakeAway: 990, PriceDelivery: 990,
				TvaRateIn: rateIn, TvaRateTakeAway: rateTakeAway, TvaRateDelivery: rateDelivery,
			}},
		}
	}

	cases := []struct {
		name      string
		unitaire  string
		imp       *IntermediateImport
		lookups   func() PreviewLookups
		decisions ImportDecisions
		wantCode  string
	}{
		{
			name:     "taux absent",
			unitaire: "validation 1 : tva_*_id obligatoire",
			imp:      newImport(&ten, nil, &ten),
			lookups:  defaultLookups,
			wantCode: BlockerTvaRateUnresolved,
		},
		{
			name:     "taux inconnu de tva_categories",
			unitaire: "validations 3 et 4 : le taux doit exister et être actif",
			imp:      newImport(&ten, &ten, &ten),
			lookups: func() PreviewLookups {
				lk := defaultLookups()
				lk.TvaRates = tvaRatesWithout(10)
				return lk
			},
			wantCode: BlockerTvaRateUnresolved,
		},
		{
			name:     "catégorie désactivée",
			unitaire: "validation 2 : la catégorie doit exister et être activée",
			imp:      newImport(&ten, &ten, &ten),
			lookups: func() PreviewLookups {
				lk := defaultLookups()
				lk.Imported.Categories = map[string]int{"ZT1": 55}
				return lk
			},
			wantCode: BlockerProductNeedsCategory,
		},
		{
			name:     "tva_id du mauvais canal",
			unitaire: "validation 4 : chaque taux est vérifié individuellement",
			imp:      newImport(&ten, &ten, &ten),
			lookups:  defaultLookups,
			decisions: ImportDecisions{
				TvaMapping: map[TvaRateKey]int{{Rate: 10, Channel: TvaChannelDelivery}: 2},
			},
			wantCode: BlockerInvalidTvaMapping,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, blockers := BuildCommitPlan(tc.imp, tc.decisions, tc.lookups())

			if plan != nil {
				t.Fatalf("plan produit alors que %s rejetterait (%s)", "validateProductForCreate", tc.unitaire)
			}
			if !hasBlocker(blockers, tc.wantCode, "") {
				t.Fatalf("blocage %q attendu (%s), obtenu : %s", tc.wantCode, tc.unitaire, BlockersMessage(blockers))
			}
		})
	}
}

func TestBuildCommitPlanRejectsNilImport(t *testing.T) {
	plan, blockers := BuildCommitPlan(nil, ImportDecisions{}, defaultLookups())
	if plan != nil || len(blockers) == 0 {
		t.Fatal("BuildCommitPlan(nil) doit bloquer")
	}
}

func findCanonicalProduct(t *testing.T, imp *IntermediateImport, externalID string) CanonicalProduct {
	t.Helper()

	for _, p := range imp.Products {
		if p.ExternalID == externalID {
			return p
		}
	}
	t.Fatalf("produit %q absent du canonique", externalID)
	return CanonicalProduct{}
}

// firstCategoryLabel rend un libellé classé catégorie, pour imposer une
// catégorie à un produit qui n'en a pas.
func firstCategoryLabel(t *testing.T, imp *IntermediateImport, decisions ImportDecisions) string {
	t.Helper()

	for _, tag := range imp.Tags {
		if decisions.TagClassification[tag.ExternalID] == TagClassCategory {
			return tag.ExternalID
		}
	}
	t.Fatal("aucun libellé classé catégorie")
	return ""
}

func hasPlannedCategory(plan *CommitPlan, externalID string) bool {
	for _, category := range plan.Categories {
		if category.ExternalID == externalID {
			return true
		}
	}
	return false
}
