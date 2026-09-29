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
)

// applyAIDecisions rend une copie du canonique où les décisions de nature et
// de groupe de la porte IA sont appliquées : une nature changée recalcule les
// trois taux de TVA (KindTvaRates), un rattachement changé réécrit
// ParentExternalID. Le snapshot d'origine n'est pas modifié.
//
// Comme le reste du plan, rien n'est cru sur parole : une décision qui cite un
// produit absent, une nature inconnue ou un groupe qui n'en est pas un produit
// un blocage.
func applyAIDecisions(imp *IntermediateImport, decisions ImportDecisions) (*IntermediateImport, []CommitBlocker) {
	if len(decisions.KindPerProduct) == 0 && len(decisions.GroupPerProduct) == 0 {
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

	sort.SliceStable(blockers, func(i, j int) bool { return blockers[i].Ref < blockers[j].Ref })
	return &out, blockers
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
		if kids := children[g.ExternalID]; len(kids) < 2 {
			g.EmptyGroup = true
			for _, k := range kids {
				products[k].ParentExternalID = ""
			}
		}
	}

	sort.SliceStable(products, func(i, j int) bool { return products[i].IsGroup && !products[j].IsGroup })
}
