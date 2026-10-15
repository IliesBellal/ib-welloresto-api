package importer

import (
	"reflect"
	"strings"
	"testing"
)

// aiPromoPages : une carte de pizzas (31 cm à 12 € et 8 €, 26 cm à 10 €) et les
// promotions données par le test.
func aiPromoPages(promos ...AIPromotion) []AIMenuPage {
	return []AIMenuPage{{
		Categories: []AICategory{{Ref: "c1", Name: "Pizzas"}},
		Products: []AIProduct{
			{Ref: "p1", CategoryRef: strPtr("c1"), Name: "Reine 31 cm", PriceCents: intPtr(1200), Kind: "food", Confidence: "high"},
			{Ref: "p2", CategoryRef: strPtr("c1"), Name: "Margherita 31 cm", PriceCents: intPtr(800), Kind: "food", Confidence: "high"},
			{Ref: "p3", CategoryRef: strPtr("c1"), Name: "Reine 26 cm", PriceCents: intPtr(1000), Kind: "food", Confidence: "high"},
		},
		Promotions: promos,
	}}
}

func buildAIPromoImport(t *testing.T, promos ...AIPromotion) *IntermediateImport {
	t.Helper()
	imp, err := BuildAIMenuImport(aiPromoPages(promos...), nil)
	if err != nil {
		t.Fatalf("BuildAIMenuImport: %v", err)
	}
	return imp
}

func mondayNinePromo() AIPromotion {
	return AIPromotion{
		Name: "Lundi pizza 9 €", Description: "Pizzas 31 cm à 9 € le lundi", Kind: "new_price",
		PriceCents: intPtr(900), Days: []int{1}, ProductRefs: []string{"p1", "p2"},
		SourceText: "Tous les lundis, 9 €, valable sur nos pizzas 31cm",
	}
}

func fourthFreePromo(name string, orderType string, buy int) AIPromotion {
	return AIPromotion{
		Name: name, Description: "Une pizza offerte", Kind: "nth_free", BuyQuantity: intPtr(buy),
		OrderTypes: []string{orderType}, AllProducts: true,
		SourceText: name,
	}
}

func promotionByName(t *testing.T, imp *IntermediateImport, name string) CanonicalPromotion {
	t.Helper()
	for _, p := range imp.Promotions {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("promotion %q absente de l'import", name)
	return CanonicalPromotion{}
}

func TestBuildAIMenuImport_PromotionsFromCard(t *testing.T) {
	imp := buildAIPromoImport(t,
		mondayNinePromo(),
		fourthFreePromo("Sur place 4e offerte", "IN", 3),
		fourthFreePromo("Livraison 5e offerte", "DELIVERY", 4),
	)
	if len(imp.Promotions) != 3 {
		t.Fatalf("promotions = %d, want 3 (avertissements : %+v)", len(imp.Promotions), imp.SourceWarnings)
	}

	monday := promotionByName(t, imp, "Lundi pizza 9 €")
	reine, margherita := productByName(t, imp, "Reine 31 cm"), productByName(t, imp, "Margherita 31 cm")
	if monday.Kind != PromotionNewPrice || monday.NewPriceCents != 900 || !reflect.DeepEqual(monday.Days, []int{1}) {
		t.Errorf("promotion du lundi = %+v", monday)
	}
	if !reflect.DeepEqual(monday.ProductExternalIDs, []string{reine.ExternalID, margherita.ExternalID}) {
		t.Errorf("produits ciblés = %v, want les deux pizzas 31 cm", monday.ProductExternalIDs)
	}

	inFourth := promotionByName(t, imp, "Sur place 4e offerte")
	if inFourth.BuyQuantity != 3 || !reflect.DeepEqual(inFourth.OrderTypes, []string{"IN"}) || !inFourth.AllProducts {
		t.Errorf("4e offerte sur place = %+v", inFourth)
	}
	if len(inFourth.Notes) != 1 || !strings.Contains(inFourth.Notes[0], "une seule fois par commande") {
		t.Errorf("la limite du moteur doit être signalée, notes = %v", inFourth.Notes)
	}
}

func TestBuildAIMenuImport_PromotionsNotCreated(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p *AIPromotion)
		reason string
	}{
		{"produit inconnu", func(p *AIPromotion) { p.ProductRefs = []string{"p1", "p9"} }, "introuvable"},
		{"aucun produit identifié", func(p *AIPromotion) { p.ProductRefs = nil }, "non identifiés"},
		{"prix illisible", func(p *AIPromotion) { p.PriceCents = nil }, "prix promotionnel"},
		{"type inconnu", func(p *AIPromotion) { p.Kind = "deux_pour_un" }, "non pris en charge"},
		{"mode inconnu", func(p *AIPromotion) { p.OrderTypes = []string{"DRIVE"} }, "mode de commande"},
		{"jour hors bornes", func(p *AIPromotion) { p.Days = []int{8} }, "jour 8"},
		{"plage horaire incomplète", func(p *AIPromotion) { p.FromTime = strPtr("11:00") }, "incomplète"},
		{"plage horaire à l'envers", func(p *AIPromotion) { p.FromTime, p.ToTime = strPtr("14:00"), strPtr("11:00") }, "à l'envers"},
		{"période terminée", func(p *AIPromotion) { p.ValidUntil = strPtr("2020-01-01") }, "déjà terminée"},
		{"prix fixe sur toute la carte", func(p *AIPromotion) { p.AllProducts, p.ProductRefs = true, nil }, "toute la carte"},
		{"nom illisible", func(p *AIPromotion) { p.Name = " " }, "nom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			promo := mondayNinePromo()
			tc.mutate(&promo)
			pages := aiPromoPages(promo)
			imp, err := BuildAIMenuImport(pages, nil)
			if err != nil {
				t.Fatalf("BuildAIMenuImport: %v", err)
			}
			if len(imp.Promotions) != 0 {
				t.Fatalf("promotions = %+v, want aucune", imp.Promotions)
			}
			w := sourceWarning(t, imp, WarningAIPromotionNotCreated)
			if !strings.Contains(w.Message, tc.reason) || !strings.Contains(w.Message, "Tous les lundis") {
				t.Errorf("avertissement = %q, want la raison %q et le texte de la carte", w.Message, tc.reason)
			}
		})
	}
}

func TestBuildAIMenuImport_PromotionNthFreeCannotCombineWithMinOrder(t *testing.T) {
	promo := fourthFreePromo("4e offerte", "IN", 3)
	promo.MinOrderCents = intPtr(2000)
	imp := buildAIPromoImport(t, promo)
	if len(imp.Promotions) != 0 {
		t.Fatalf("une promotion ambiguë ne doit pas être créée : %+v", imp.Promotions)
	}
	sourceWarning(t, imp, WarningAIPromotionNotCreated)
}

func TestBuildAIMenuImport_PromotionsReportedByTheModelAsNotCreated(t *testing.T) {
	pages := aiPromoPages()
	pages[0].PromotionsNotCreated = []AIPromotionNotCreated{
		{SourceText: "Le plus petit des 3 offert", Reason: "règle non exprimable"},
	}
	imp, err := BuildAIMenuImport(pages, nil)
	if err != nil {
		t.Fatalf("BuildAIMenuImport: %v", err)
	}
	w := sourceWarning(t, imp, WarningAIPromotionNotCreated)
	if !strings.Contains(w.Message, "règle non exprimable") || !strings.Contains(w.Message, "Le plus petit des 3 offert") {
		t.Errorf("avertissement = %q", w.Message)
	}
}

func TestBuildAIMenuImport_PromotionSeenOnTwoPhotosKeptOnce(t *testing.T) {
	pages := append(aiPromoPages(fourthFreePromo("Sur place 4e offerte", "IN", 3)),
		aiPromoPages(fourthFreePromo("sur place 4E offerte", "IN", 3))...)
	imp, err := BuildAIMenuImport(pages, nil)
	if err != nil {
		t.Fatalf("BuildAIMenuImport: %v", err)
	}
	if len(imp.Promotions) != 1 {
		t.Fatalf("promotions = %d, want 1", len(imp.Promotions))
	}
	sourceWarning(t, imp, WarningAIPromotionDuplicate)
}

func TestBuildAIMenuImport_PromotionTextsAreBounded(t *testing.T) {
	promo := mondayNinePromo()
	promo.Name = strings.Repeat("é", 80)
	promo.Description = strings.Repeat("d", 300)
	imp := buildAIPromoImport(t, promo)
	got := imp.Promotions[0]
	if n := len([]rune(got.Name)); n > aiPromotionNameMax {
		t.Errorf("nom = %d caractères, want ≤ %d", n, aiPromotionNameMax)
	}
	if n := len([]rune(got.Description)); n > aiPromotionDescMax {
		t.Errorf("description = %d caractères, want ≤ %d", n, aiPromotionDescMax)
	}
}

func TestDecodeAIMenuPage_Promotions(t *testing.T) {
	page, err := DecodeAIMenuPage(`{"categories":[],"product_groups":[],"products":[],"option_groups":[],"formulas":[],"warnings":[],
		"promotions":[{"name":"N","description":"D","kind":"percent_off","price_cents":null,"percent":20,"amount_cents":null,
		"buy_quantity":null,"max_units":null,"order_types":["IN"],"days":[1,2],"from_time":"11:00","to_time":"14:00",
		"valid_from":null,"valid_until":null,"min_order_cents":null,"product_refs":[],"all_products":true,"source_text":"-20%"}],
		"promotions_not_created":[{"source_text":"x","reason":"y"}]}`)
	if err != nil {
		t.Fatalf("DecodeAIMenuPage: %v", err)
	}
	if len(page.Promotions) != 1 || *page.Promotions[0].Percent != 20 || *page.Promotions[0].FromTime != "11:00" || len(page.PromotionsNotCreated) != 1 {
		t.Errorf("page décodée = %+v", page)
	}
}

func TestDescribePromotion(t *testing.T) {
	imp := buildAIPromoImport(t, mondayNinePromo(), fourthFreePromo("Livraison 5e offerte", "DELIVERY", 4))
	names := map[string]string{}
	for _, p := range imp.Products {
		names[p.ExternalID] = p.Name
	}

	got := DescribePromotion(promotionByName(t, imp, "Lundi pizza 9 €"), names)
	for _, want := range []string{"prix à 9,00 €", "tous les modes de commande", "lundi", "Reine 31 cm", "Margherita 31 cm"} {
		if !strings.Contains(got, want) {
			t.Errorf("description = %q, want %q", got, want)
		}
	}
	got = DescribePromotion(promotionByName(t, imp, "Livraison 5e offerte"), names)
	for _, want := range []string{"4 acheté(s), le suivant offert", "en livraison", "toute la carte"} {
		if !strings.Contains(got, want) {
			t.Errorf("description = %q, want %q", got, want)
		}
	}
}

func TestBuildPreview_Promotions(t *testing.T) {
	imp := buildAIPromoImport(t, mondayNinePromo(), fourthFreePromo("Sur place 4e offerte", "IN", 3))
	lk := defaultLookups()
	lk.ExistingPromotions = []ExistingPromotion{{Name: "SUR PLACE 4E OFFERTE"}}

	res, err := BuildPreview(imp, lk)
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	if len(res.Promotions) != 2 || res.Summary.PromotionsToCreate != 1 {
		t.Fatalf("promotions = %+v, à créer = %d, want 2 dont 1 à créer", res.Promotions, res.Summary.PromotionsToCreate)
	}
	for _, p := range res.Promotions {
		switch p.Name {
		case "Lundi pizza 9 €":
			if p.Action != ActionCreate || p.Summary == "" {
				t.Errorf("promotion du lundi = %+v", p)
			}
			// Margherita 31 cm à 8 € : un prix de 9 € ne le baisserait pas.
			if !containsSubstring(p.Notes, "Margherita 31 cm") {
				t.Errorf("notes = %v, want l'alerte prix non inférieur pour Margherita", p.Notes)
			}
		case "Sur place 4e offerte":
			if p.Action != ActionAlreadyExists {
				t.Errorf("promotion existante : action = %q, want %q", p.Action, ActionAlreadyExists)
			}
		}
	}
}

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func aiPromoPlan(t *testing.T, imp *IntermediateImport, mutate func(d *ImportDecisions, lk *PreviewLookups)) *CommitPlan {
	t.Helper()
	lk := defaultLookups()
	decisions := defaultDecisions(t, imp, lk)
	decisions.TvaConfirmed = true
	if mutate != nil {
		mutate(&decisions, &lk)
	}
	plan, blockers := BuildCommitPlan(imp, decisions, lk)
	if len(blockers) > 0 {
		t.Fatalf("BuildCommitPlan bloqué : %s", BlockersMessage(blockers))
	}
	return plan
}

func plannedPromotion(t *testing.T, plan *CommitPlan, name string) PlannedPromotion {
	t.Helper()
	for _, p := range plan.Promotions {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("promotion %q absente du plan", name)
	return PlannedPromotion{}
}

func TestBuildCommitPlan_PromotionNewPriceOnSelectedDays(t *testing.T) {
	imp := buildAIPromoImport(t, mondayNinePromo())
	plan := aiPromoPlan(t, imp, nil)

	got := plannedPromotion(t, plan, "Lundi pizza 9 €")
	if !got.Materializable() {
		t.Fatalf("promotion non créée : %s", got.SkipReason)
	}
	if got.DiscountUnit != "NEWPRICE" || got.NewPriceCents != 900 || got.DiscountedQuantity != aiPromotionAllUnits {
		t.Errorf("promotion = %+v, want NEWPRICE à 900 sur toutes les unités", got)
	}
	// Margherita 31 cm (8 €) : 9 € ne baisse pas son prix, elle sort de la promotion.
	reine := productByName(t, imp, "Reine 31 cm")
	if !reflect.DeepEqual(got.ProductExternalIDs, []string{reine.ExternalID}) {
		t.Errorf("produits = %v, want seulement Reine 31 cm", got.ProductExternalIDs)
	}
	if !containsSubstring(got.Notes, "Margherita 31 cm") {
		t.Errorf("notes = %v, want le produit retiré expliqué", got.Notes)
	}
	// Lundi toute la journée : 00:00 → 00:00 (jusqu'à minuit).
	if want := []PlannedPromotionSchedule{{DayOfWeek: 1, From: "00:00", To: "00:00"}}; !reflect.DeepEqual(got.Schedules, want) {
		t.Errorf("créneaux = %+v, want %+v", got.Schedules, want)
	}
}

func TestBuildCommitPlan_PromotionNthFreeMapsToEngineFields(t *testing.T) {
	imp := buildAIPromoImport(t, fourthFreePromo("Sur place 4e offerte", "IN", 3), fourthFreePromo("Livraison 5e offerte", "DELIVERY", 4))
	plan := aiPromoPlan(t, imp, nil)

	inPlan := plannedPromotion(t, plan, "Sur place 4e offerte")
	if inPlan.DiscountUnit != "PERCENTAGE" || inPlan.DiscountValue != 100 || inPlan.DiscountedQuantity != 1 ||
		inPlan.MinOrderUnit != "QUANTITY" || inPlan.MinOrderValue != 4 || !inPlan.AllProducts || len(inPlan.Schedules) != 0 {
		t.Errorf("4e offerte sur place = %+v", inPlan)
	}
	if delivery := plannedPromotion(t, plan, "Livraison 5e offerte"); delivery.MinOrderValue != 5 || !reflect.DeepEqual(delivery.OrderTypes, []string{"DELIVERY"}) {
		t.Errorf("5e offerte en livraison = %+v", delivery)
	}
}

func TestBuildCommitPlan_PromotionPercentAndTimeWindow(t *testing.T) {
	promo := AIPromotion{
		Name: "Midi -20 %", Description: "-20 % le midi", Kind: "percent_off", Percent: intPtr(20), MaxUnits: intPtr(2),
		Days: []int{1, 2, 3, 4, 5}, FromTime: strPtr("11:30"), ToTime: strPtr("14:00"), MinOrderCents: intPtr(1500),
		ProductRefs: []string{"p3"}, SourceText: "Midi -20 %",
	}
	plan := aiPromoPlan(t, buildAIPromoImport(t, promo), nil)

	got := plannedPromotion(t, plan, "Midi -20 %")
	if got.DiscountUnit != "PERCENTAGE" || got.DiscountValue != 20 || got.DiscountedQuantity != 2 ||
		got.MinOrderUnit != "CURRENCY" || got.MinOrderValue != 1500 {
		t.Errorf("promotion = %+v", got)
	}
	if len(got.Schedules) != 5 || got.Schedules[0] != (PlannedPromotionSchedule{DayOfWeek: 1, From: "11:30", To: "14:00"}) {
		t.Errorf("créneaux = %+v, want lundi→vendredi 11:30–14:00", got.Schedules)
	}
}

func TestBuildCommitPlan_PromotionSkipped(t *testing.T) {
	imp := buildAIPromoImport(t, mondayNinePromo(), fourthFreePromo("Sur place 4e offerte", "IN", 3))
	reine, margherita := productByName(t, imp, "Reine 31 cm"), productByName(t, imp, "Margherita 31 cm")

	t.Run("écartée à la relecture", func(t *testing.T) {
		plan := aiPromoPlan(t, imp, func(d *ImportDecisions, _ *PreviewLookups) {
			d.ExcludedPromotions = map[string]bool{promotionByName(t, imp, "Lundi pizza 9 €").ExternalID: true}
		})
		if got := plannedPromotion(t, plan, "Lundi pizza 9 €"); got.Materializable() {
			t.Errorf("promotion écartée mais créée : %+v", got)
		}
		if got := plannedPromotion(t, plan, "Sur place 4e offerte"); !got.Materializable() {
			t.Errorf("l'autre promotion doit rester créée : %s", got.SkipReason)
		}
	})

	t.Run("déjà existante", func(t *testing.T) {
		plan := aiPromoPlan(t, imp, func(_ *ImportDecisions, lk *PreviewLookups) {
			lk.ExistingPromotions = []ExistingPromotion{{Name: "sur place 4e offerte"}}
		})
		if got := plannedPromotion(t, plan, "Sur place 4e offerte"); got.Materializable() || !strings.Contains(got.SkipReason, "existe déjà") {
			t.Errorf("promotion existante = %+v", got)
		}
	})

	t.Run("produits ciblés écartés", func(t *testing.T) {
		plan := aiPromoPlan(t, imp, func(d *ImportDecisions, _ *PreviewLookups) {
			d.ExcludedProducts = map[string]bool{reine.ExternalID: true, margherita.ExternalID: true}
		})
		if got := plannedPromotion(t, plan, "Lundi pizza 9 €"); got.Materializable() || !strings.Contains(got.SkipReason, "aucun des produits") {
			t.Errorf("promotion sans produit = %+v", got)
		}
	})

	t.Run("prix saisi en relecture : plus de baisse", func(t *testing.T) {
		plan := aiPromoPlan(t, imp, func(d *ImportDecisions, _ *PreviewLookups) {
			d.PricePerProduct = map[string]ChannelPrices{reine.ExternalID: {In: 850, TakeAway: 850, Delivery: 850}}
		})
		if got := plannedPromotion(t, plan, "Lundi pizza 9 €"); got.Materializable() {
			t.Errorf("les deux pizzas coûtent ≤ 9 € : la promotion ne doit pas être créée, %+v", got)
		}
	})
}
