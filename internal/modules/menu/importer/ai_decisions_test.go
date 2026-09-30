package importer

import "testing"

// aiReadyDecisions : décisions proposées par la preview, complétées comme le
// ferait le restaurateur (nature du Veggie précisée, TVA confirmée).
func aiReadyDecisions(t *testing.T, imp *IntermediateImport) ImportDecisions {
	t.Helper()
	decisions := defaultDecisions(t, imp, defaultLookups())
	decisions.KindPerProduct[productByName(t, imp, "Veggie").ExternalID] = KindFood
	decisions.TvaConfirmed = true
	return decisions
}

func plannedByName(t *testing.T, plan *CommitPlan, name string) PlannedProduct {
	t.Helper()
	for _, p := range plan.Products {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("produit %q absent du plan", name)
	return PlannedProduct{}
}

func blockerCodes(blockers []CommitBlocker) map[string]int {
	codes := make(map[string]int)
	for _, b := range blockers {
		codes[b.Code]++
	}
	return codes
}

func TestBuildCommitPlan_AIDoorRequiresTvaConfirmation(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := aiReadyDecisions(t, imp)
	decisions.TvaConfirmed = false

	_, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if codes := blockerCodes(blockers); codes[BlockerTvaNotConfirmed] != 1 || len(blockers) != 1 {
		t.Fatalf("blocages = %s, want uniquement %s", BlockersMessage(blockers), BlockerTvaNotConfirmed)
	}
}

func TestBuildCommitPlan_AIDoorKindOtherBlocksUntilDecided(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := defaultDecisions(t, imp, defaultLookups())
	decisions.TvaConfirmed = true // Veggie reste « other », sans taux

	_, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if blockerCodes(blockers)[BlockerTvaRateUnresolved] != 3 {
		t.Fatalf("blocages = %s, want les 3 canaux de Veggie sans TVA", BlockersMessage(blockers))
	}
}

func TestBuildCommitPlan_AIDoorGroupsAndTva(t *testing.T) {
	imp := buildAITestImport(t)
	plan, blockers := BuildCommitPlan(imp, aiReadyDecisions(t, imp), defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}

	if first := plan.Products[0]; !first.IsGroup || first.Name != "Coca-Cola" || !first.Materializable() {
		t.Fatalf("premier produit du plan = %+v, want le groupe Coca-Cola, créé", first)
	}
	group := plan.Products[0]
	if group.Status != ProductStatusAvailable {
		t.Errorf("statut du groupe = %q, want available malgré les prix nuls", group.Status)
	}

	zero := plannedByName(t, plan, "Coca-Cola Zero")
	if zero.ParentExternalID != group.ExternalID {
		t.Errorf("Coca-Cola Zero rattaché à %q, want le groupe", zero.ParentExternalID)
	}
	// fullTvaRates : IN 10 -> 2, TAKE_AWAY 5.5 -> 4, DELIVERY 5.5 -> 7.
	if zero.TvaInID != 2 || zero.TvaTakeAwayID != 4 || zero.TvaDeliveryID != 7 {
		t.Errorf("TVA Coca-Cola Zero = %d/%d/%d, want 2/4/7", zero.TvaInID, zero.TvaTakeAwayID, zero.TvaDeliveryID)
	}
	if veggie := plannedByName(t, plan, "Veggie"); veggie.TvaTakeAwayID != 5 {
		t.Errorf("TVA emporter de Veggie devenu « food » = %d, want 5 (10 %%)", veggie.TvaTakeAwayID)
	}
}

func TestBuildCommitPlan_AIDoorKindDecisionRecomputesTva(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := aiReadyDecisions(t, imp)
	zero := productByName(t, imp, "Coca-Cola Zero")
	decisions.KindPerProduct[zero.ExternalID] = KindSoftDrinkServed // servi au verre : 10 % partout

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	if got := plannedByName(t, plan, "Coca-Cola Zero"); got.TvaTakeAwayID != 5 || got.TvaDeliveryID != 8 {
		t.Errorf("TVA après changement de nature = emporter %d livraison %d, want 5/8 (10 %%)", got.TvaTakeAwayID, got.TvaDeliveryID)
	}
	// Le snapshot d'origine n'est pas modifié.
	if *productByName(t, imp, "Coca-Cola Zero").TvaRateTakeAway != 5.5 {
		t.Errorf("applyAIDecisions a modifié le canonique d'origine")
	}
}

func TestBuildCommitPlan_AIDoorGroupWithOneVariantLeftIsNotCreated(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := aiReadyDecisions(t, imp)
	decisions.ExcludedProducts[productByName(t, imp, "Coca-Cola Cherry").ExternalID] = true
	decisions.ExcludedProducts[productByName(t, imp, "Coca-Cola Light").ExternalID] = true

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	if group := plannedByName(t, plan, "Coca-Cola"); !group.EmptyGroup || group.Materializable() {
		t.Errorf("groupe à une seule déclinaison = %+v, want non créé", group)
	}
	if zero := plannedByName(t, plan, "Coca-Cola Zero"); zero.ParentExternalID != "" || !zero.Materializable() {
		t.Errorf("dernière déclinaison = %+v, want créée à la racine", zero)
	}
}

func TestBuildCommitPlan_AIDoorUngroupDecision(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := aiReadyDecisions(t, imp)
	decisions.GroupPerProduct[productByName(t, imp, "Coca-Cola Light").ExternalID] = ""

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	if light := plannedByName(t, plan, "Coca-Cola Light"); light.ParentExternalID != "" {
		t.Errorf("Coca-Cola Light dégroupé rattaché à %q", light.ParentExternalID)
	}
	if group := plannedByName(t, plan, "Coca-Cola"); !group.Materializable() {
		t.Errorf("le groupe garde deux déclinaisons : il doit être créé")
	}
}

func TestBuildCommitPlan_AIDoorRejectsInvalidDecisions(t *testing.T) {
	imp := buildAITestImport(t)
	group := imp.Products[0].ExternalID
	classique := productByName(t, imp, "Classique").ExternalID
	zero := productByName(t, imp, "Coca-Cola Zero").ExternalID

	cases := map[string]func(d *ImportDecisions){
		"nature inconnue":              func(d *ImportDecisions) { d.KindPerProduct[zero] = "boisson" },
		"nature d'un produit absent":   func(d *ImportDecisions) { d.KindPerProduct["ai-p-fantome"] = KindFood },
		"rattachement à un non-groupe": func(d *ImportDecisions) { d.GroupPerProduct[zero] = classique },
		"groupe rattaché à un groupe":  func(d *ImportDecisions) { d.GroupPerProduct[group] = group },
	}
	for name, mutate := range cases {
		decisions := aiReadyDecisions(t, imp)
		mutate(&decisions)
		_, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
		codes := blockerCodes(blockers)
		if codes[BlockerInvalidKindDecision]+codes[BlockerInvalidGroupDecision] != 1 {
			t.Errorf("%s : blocages = %s, want un blocage de décision invalide", name, BlockersMessage(blockers))
		}
	}
}

func TestBuildCommitPlan_AIDoorTvaDecisionPerProduct(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := defaultDecisions(t, imp, defaultLookups())
	decisions.TvaConfirmed = true
	// Veggie reste « other » (aucun taux proposé) : la TVA choisie produit
	// par produit suffit à débloquer ses trois canaux.
	decisions.TvaPerProduct = map[string]ChannelTvaIDs{
		productByName(t, imp, "Veggie").ExternalID: {In: intPtr(2), TakeAway: intPtr(5), Delivery: intPtr(8)},
	}
	for _, name := range []string{"Coca-Cola Zero", "Coca-Cola Light", "Coca-Cola Cherry"} {
		decisions.TvaPerProduct[productByName(t, imp, name).ExternalID] = ChannelTvaIDs{In: intPtr(3), TakeAway: intPtr(6), Delivery: intPtr(9)}
	}

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	if veggie := plannedByName(t, plan, "Veggie"); veggie.TvaInID != 2 || veggie.TvaTakeAwayID != 5 || veggie.TvaDeliveryID != 8 {
		t.Errorf("TVA Veggie = %d/%d/%d, want 2/5/8", veggie.TvaInID, veggie.TvaTakeAwayID, veggie.TvaDeliveryID)
	}
	// Le groupe reprend la TVA de ses déclinaisons, pas celle de sa nature.
	if group := plannedByName(t, plan, "Coca-Cola"); group.TvaInID != 3 || group.TvaTakeAwayID != 6 || group.TvaDeliveryID != 9 {
		t.Errorf("TVA du groupe = %d/%d/%d, want 3/6/9", group.TvaInID, group.TvaTakeAwayID, group.TvaDeliveryID)
	}
}

func TestBuildCommitPlan_AIDoorPartialTvaDecisionKeepsProposedRates(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := aiReadyDecisions(t, imp)
	decisions.TvaPerProduct = map[string]ChannelTvaIDs{
		productByName(t, imp, "Coca-Cola Zero").ExternalID: {TakeAway: intPtr(6)},
	}

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	if zero := plannedByName(t, plan, "Coca-Cola Zero"); zero.TvaInID != 2 || zero.TvaTakeAwayID != 6 || zero.TvaDeliveryID != 7 {
		t.Errorf("TVA Coca-Cola Zero = %d/%d/%d, want 2/6/7", zero.TvaInID, zero.TvaTakeAwayID, zero.TvaDeliveryID)
	}
}

func TestBuildCommitPlan_AIDoorPriceDecision(t *testing.T) {
	imp := buildAITestImport(t)
	decisions := aiReadyDecisions(t, imp)
	decisions.PricePerProduct = map[string]ChannelPrices{
		productByName(t, imp, "Classique").ExternalID: {In: 1270, TakeAway: 1190, Delivery: 1450},
	}

	plan, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	got := plannedByName(t, plan, "Classique")
	if got.PriceIn != 1270 || got.PriceTakeAway != 1190 || got.PriceDelivery != 1450 || got.Status != ProductStatusAvailable {
		t.Errorf("Classique = %d/%d/%d (%s), want 1270/1190/1450, disponible", got.PriceIn, got.PriceTakeAway, got.PriceDelivery, got.Status)
	}
	if productByName(t, imp, "Classique").PriceIn == 1270 {
		t.Errorf("applyAIDecisions a modifié le canonique d'origine")
	}
}

func TestBuildCommitPlan_AIDoorRejectsInvalidPriceAndTvaDecisions(t *testing.T) {
	imp := buildAITestImport(t)
	group := imp.Products[0].ExternalID
	classique := productByName(t, imp, "Classique").ExternalID

	cases := map[string]struct {
		mutate func(d *ImportDecisions)
		code   string
	}{
		"prix négatif": {func(d *ImportDecisions) { d.PricePerProduct = map[string]ChannelPrices{classique: {In: -1}} }, BlockerInvalidPriceDecision},
		"prix aberrant": {func(d *ImportDecisions) {
			d.PricePerProduct = map[string]ChannelPrices{classique: {In: maxDecidedPriceCents + 1}}
		}, BlockerInvalidPriceDecision},
		"prix d'un groupe":         {func(d *ImportDecisions) { d.PricePerProduct = map[string]ChannelPrices{group: {In: 300}} }, BlockerInvalidPriceDecision},
		"prix d'un produit absent": {func(d *ImportDecisions) { d.PricePerProduct = map[string]ChannelPrices{"ai-p-fantome": {In: 300}} }, BlockerInvalidPriceDecision},
		// 4 est un taux du canal « à emporter ».
		"TVA d'un autre canal": {func(d *ImportDecisions) { d.TvaPerProduct = map[string]ChannelTvaIDs{classique: {In: intPtr(4)}} }, BlockerInvalidTvaMapping},
		"TVA inconnue": {func(d *ImportDecisions) {
			d.TvaPerProduct = map[string]ChannelTvaIDs{classique: {Delivery: intPtr(999)}}
		}, BlockerInvalidTvaMapping},
		"TVA d'un produit absent": {func(d *ImportDecisions) { d.TvaPerProduct = map[string]ChannelTvaIDs{"ai-p-fantome": {In: intPtr(2)}} }, BlockerInvalidTvaMapping},
	}
	for name, tc := range cases {
		decisions := aiReadyDecisions(t, imp)
		tc.mutate(&decisions)
		_, blockers := BuildCommitPlan(imp, decisions, defaultLookups())
		if codes := blockerCodes(blockers); codes[tc.code] != 1 || len(blockers) != 1 {
			t.Errorf("%s : blocages = %s, want un seul %s", name, BlockersMessage(blockers), tc.code)
		}
	}
}
