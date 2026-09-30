package importer

import (
	"fmt"
	"sort"
)

// Blocages propres à la porte IA.
const (
	// BlockerTvaNotConfirmed : les taux de TVA de la porte IA sont des
	// propositions déduites de la nature des produits ; le restaurateur doit
	// avoir validé l'étape « Nature / TVA » (ImportDecisions.TvaConfirmed).
	BlockerTvaNotConfirmed = "tva_not_confirmed"
	// BlockerInvalidKindDecision : nature inconnue, ou produit absent du lot.
	BlockerInvalidKindDecision = "invalid_kind_decision"
	// BlockerInvalidGroupDecision : rattachement à autre chose qu'un produit
	// groupe du lot, ou rattachement d'un groupe.
	BlockerInvalidGroupDecision = "invalid_group_decision"
	// BlockerInvalidPriceDecision : prix négatif ou aberrant, produit absent
	// du lot, ou prix posé sur un produit groupe.
	BlockerInvalidPriceDecision = "invalid_price_decision"
)

// maxDecidedPriceCents borne un prix saisi en relecture (10 000 €), comme la
// lecture des photos (aiMaxPriceCents).
const maxDecidedPriceCents = aiMaxPriceCents

// applyAIDecisions rend une copie du canonique où les décisions de relecture
// produit par produit sont appliquées : une nature changée recalcule les
// trois taux de TVA (KindTvaRates), un rattachement changé réécrit
// ParentExternalID, des prix saisis remplacent ceux de la source. Le snapshot
// d'origine n'est pas modifié. La TVA choisie par canal (TvaPerProduct) est
// appliquée plus loin, par assignChannels, qui vérifie chaque tva_id.
//
// Comme le reste du plan, rien n'est cru sur parole : une décision qui cite un
// produit absent, une nature inconnue, un groupe qui n'en est pas un ou un
// prix hors bornes produit un blocage.
func applyAIDecisions(imp *IntermediateImport, decisions ImportDecisions) (*IntermediateImport, []CommitBlocker) {
	if len(decisions.KindPerProduct) == 0 && len(decisions.GroupPerProduct) == 0 &&
		len(decisions.PricePerProduct) == 0 && len(decisions.TvaPerProduct) == 0 {
		return imp, nil
	}

	out := *imp
	out.Products = make([]CanonicalProduct, len(imp.Products))
	copy(out.Products, imp.Products)

	index := make(map[string]int, len(out.Products))
	for i, p := range out.Products {
		index[p.ExternalID] = i
	}

	var blockers []CommitBlocker
	block := func(code, ref, message string) {
		blockers = append(blockers, CommitBlocker{Code: code, Ref: ref, Message: message})
	}

	for externalID, kind := range decisions.KindPerProduct {
		i, ok := index[externalID]
		if !ok {
			block(BlockerInvalidKindDecision, externalID, fmt.Sprintf("produit %q absent de l'import", externalID))
			continue
		}
		if !kind.IsKnown() {
			block(BlockerInvalidKindDecision, externalID, fmt.Sprintf("nature %q inconnue pour %q", kind, out.Products[i].Name))
			continue
		}
		p := &out.Products[i]
		p.Kind = kind
		p.TvaRateIn, p.TvaRateTakeAway, p.TvaRateDelivery = KindTvaRates(kind)
	}

	for externalID, parentID := range decisions.GroupPerProduct {
		i, ok := index[externalID]
		if !ok {
			block(BlockerInvalidGroupDecision, externalID, fmt.Sprintf("produit %q absent de l'import", externalID))
			continue
		}
		p := &out.Products[i]
		if parentID == "" {
			p.ParentExternalID = ""
			continue
		}
		if p.IsGroup {
			block(BlockerInvalidGroupDecision, externalID, fmt.Sprintf("%q est un groupe : il ne peut pas être rattaché à un autre groupe", p.Name))
			continue
		}
		parent, ok := index[parentID]
		if !ok || !out.Products[parent].IsGroup {
			block(BlockerInvalidGroupDecision, externalID, fmt.Sprintf("%q ne peut être rattaché qu'à un groupe de l'import (%q n'en est pas un)", p.Name, parentID))
			continue
		}
		p.ParentExternalID = parentID
	}

	for externalID, prices := range decisions.PricePerProduct {
		i, ok := index[externalID]
		if !ok {
			block(BlockerInvalidPriceDecision, externalID, fmt.Sprintf("produit %q absent de l'import", externalID))
			continue
		}
		p := &out.Products[i]
		if p.IsGroup {
			block(BlockerInvalidPriceDecision, externalID, fmt.Sprintf("%q est un groupe : il n'a pas de prix", p.Name))
			continue
		}
		valid := true
		for _, cents := range []int{prices.In, prices.TakeAway, prices.Delivery} {
			if cents < 0 || cents > maxDecidedPriceCents {
				valid = false
			}
		}
		if !valid {
			block(BlockerInvalidPriceDecision, externalID, fmt.Sprintf("prix hors bornes pour %q (0 à 10 000 €)", p.Name))
			continue
		}
		p.PriceIn, p.PriceTakeAway, p.PriceDelivery = prices.In, prices.TakeAway, prices.Delivery
		// Un prix saisi fait sortir la ligne du statut removed_from_menu.
		p.AllPricesZero = prices.In == 0 && prices.TakeAway == 0 && prices.Delivery == 0
	}

	// Les tva_id eux-mêmes sont vérifiés par assignChannels ; ici, seul le
	// produit cité.
	for externalID := range decisions.TvaPerProduct {
		if _, ok := index[externalID]; !ok {
			block(BlockerInvalidTvaMapping, externalID, fmt.Sprintf("produit %q absent de l'import", externalID))
		}
	}

	sort.SliceStable(blockers, func(i, j int) bool { return blockers[i].Ref < blockers[j].Ref })
	return &out, blockers
}

// tvaOverride rend le tva_id choisi en relecture pour ce produit et ce canal,
// ou nil (TvaPerProduct).
func (b *commitPlanner) tvaOverride(externalID string, channel TvaChannel) *int {
	chosen, ok := b.decisions.TvaPerProduct[externalID]
	if !ok {
		return nil
	}
	switch channel {
	case TvaChannelIn:
		return chosen.In
	case TvaChannelTakeAway:
		return chosen.TakeAway
	case TvaChannelDelivery:
		return chosen.Delivery
	}
	return nil
}

// resolveGroups finalise les groupes du plan (porte IA), une fois connu le
// sort de chaque produit :
//   - un produit dont le groupe ne sera pas créé (exclu, homonyme ignoré…)
//     est créé à la racine ;
//   - un groupe qui garde moins de deux déclinaisons créées n'est pas créé et
//     sa déclinaison éventuelle passe à la racine — même règle que
//     BuildAIMenuImport (« un seul Coca reste à la racine »), réappliquée
//     après les exclusions de la relecture ;
//   - les groupes passent en tête du plan : chaque parent est inséré avant
//     ses enfants, qui référencent son product_id.
func (b *commitPlanner) resolveGroups() {
	products := b.plan.Products
	index := make(map[string]int, len(products))
	for i, p := range products {
		index[p.ExternalID] = i
	}

	children := make(map[string][]int)
	for i := range products {
		p := &products[i]
		if p.ParentExternalID == "" {
			continue
		}
		parent, ok := index[p.ParentExternalID]
		if !ok || !products[parent].IsGroup || !products[parent].Materializable() {
			p.ParentExternalID = ""
			continue
		}
		if p.Materializable() {
			children[p.ParentExternalID] = append(children[p.ParentExternalID], i)
		}
	}

	for i := range products {
		g := &products[i]
		if !g.IsGroup || !g.Materializable() {
			continue
		}
		kids := children[g.ExternalID]
		if len(kids) < 2 {
			g.EmptyGroup = true
			for _, k := range kids {
				products[k].ParentExternalID = ""
			}
			continue
		}
		// Le groupe n'est jamais vendu : il prend la TVA de sa première
		// déclinaison, telle que choisie en relecture (tva_*_id NOT NULL).
		first := products[kids[0]]
		g.TvaInID, g.TvaTakeAwayID, g.TvaDeliveryID = first.TvaInID, first.TvaTakeAwayID, first.TvaDeliveryID
	}

	sort.SliceStable(products, func(i, j int) bool { return products[i].IsGroup && !products[j].IsGroup })
}
