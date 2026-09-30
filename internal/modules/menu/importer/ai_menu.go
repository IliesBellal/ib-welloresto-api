package importer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"welloresto-api/internal/importutil"
)

// AIPhotoSlug identifie la lecture de carte par photo : quatrième porte du
// pipeline. Un modèle d'IA lit chaque photo et produit une AIMenuPage ;
// BuildAIMenuImport les fusionne en canonique. Comme pour la saisie manuelle,
// l'IA ne décide rien : elle propose (catégories, groupes, nature/TVA), la
// preview et le restaurateur tranchent.
const AIPhotoSlug = "ai_photo"

// Préfixes des identifiants externes. Comme le template et la saisie
// manuelle, l'identité d'une ligne est son nom (qualifié par sa catégorie) :
// réimporter une photo de la même carte retrouve les mêmes produits.
const (
	aiCategoryPrefix     = "ai-c"
	aiGroupPrefix        = "ai-g"
	aiProductPrefix      = "ai-p"
	aiAttributePrefix    = "ai-o"
	aiOptionPrefix       = "ai-oo"
	aiMaxPriceCents      = 1_000_000 // 10 000 € : au-delà, prix jugé illisible
	aiConfidenceHigh     = "high"
	aiConfidenceMedium   = "medium"
	aiConfidenceLow      = "low"
	aiKeySeparator       = " / "
	aiOptionKeySeparator = " | "
)

// Codes des signalements de la porte IA (SourceWarning → PreviewWarning).
const (
	WarningAIFormulaNotCreated = "ai_formula_not_created"
	WarningAIPhoto             = "ai_photo_warning"
	WarningAIDuplicateMerged   = "ai_duplicate_merged"
	WarningAIGroupDissolved    = "ai_group_dissolved"
	WarningAILowConfidence     = "ai_low_confidence"
)

// AIMenuPage est la sortie du modèle pour une photo. Le JSON est contraint
// par AIMenuPageSchema côté API, puis revalidé ici (BuildAIMenuImport) : les
// références croisées et les bornes ne sont pas exprimables dans le schéma.
type AIMenuPage struct {
	Categories    []AICategory     `json:"categories"`
	ProductGroups []AIProductGroup `json:"product_groups"`
	Products      []AIProduct      `json:"products"`
	OptionGroups  []AIOptionGroup  `json:"option_groups"`
	Formulas      []AIFormula      `json:"formulas"`
	Warnings      []string         `json:"warnings"`
}

type AICategory struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

type AIProductGroup struct {
	Ref         string  `json:"ref"`
	CategoryRef *string `json:"category_ref"`
	Name        string  `json:"name"`
}

type AIProduct struct {
	Ref             string   `json:"ref"`
	CategoryRef     *string  `json:"category_ref"`
	GroupRef        *string  `json:"group_ref"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	PriceCents      *int     `json:"price_cents"`
	OptionGroupRefs []string `json:"option_group_refs"`
	Kind            string   `json:"kind"`
	Confidence      string   `json:"confidence"`
	Issues          []string `json:"issues"`
}

type AIOptionGroup struct {
	Ref     string     `json:"ref"`
	Name    string     `json:"name"`
	Min     int        `json:"min"`
	Max     int        `json:"max"`
	Options []AIOption `json:"options"`
}

type AIOption struct {
	Title           string `json:"title"`
	ExtraPriceCents int    `json:"extra_price_cents"`
}

type AIFormula struct {
	Name        string `json:"name"`
	PriceCents  *int   `json:"price_cents"`
	Description string `json:"description"`
}

// AIMenuPageSchema est le schéma JSON (sorties structurées Anthropic) d'une
// AIMenuPage. Contraintes de l'API : additionalProperties false sur chaque
// objet, champ facultatif = anyOf avec null, pas de bornes numériques ni de
// longueur (vérifiées en Go). Transmis octet pour octet : un schéma stable
// garde en cache sa grammaire compilée côté API.
var AIMenuPageSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "categories": {"type": "array", "items": {
      "type": "object",
      "properties": {"ref": {"type": "string"}, "name": {"type": "string"}},
      "required": ["ref", "name"], "additionalProperties": false}},
    "product_groups": {"type": "array", "items": {
      "type": "object",
      "properties": {
        "ref": {"type": "string"},
        "category_ref": {"anyOf": [{"type": "string"}, {"type": "null"}]},
        "name": {"type": "string"}},
      "required": ["ref", "category_ref", "name"], "additionalProperties": false}},
    "products": {"type": "array", "items": {
      "type": "object",
      "properties": {
        "ref": {"type": "string"},
        "category_ref": {"anyOf": [{"type": "string"}, {"type": "null"}]},
        "group_ref": {"anyOf": [{"type": "string"}, {"type": "null"}]},
        "name": {"type": "string"},
        "description": {"type": "string"},
        "price_cents": {"anyOf": [{"type": "integer"}, {"type": "null"}]},
        "option_group_refs": {"type": "array", "items": {"type": "string"}},
        "kind": {"type": "string", "enum": ["food", "hot_drink", "soft_drink_served", "soft_drink_sealed", "packaged_food", "alcohol", "other"]},
        "confidence": {"type": "string", "enum": ["high", "medium", "low"]},
        "issues": {"type": "array", "items": {"type": "string"}}},
      "required": ["ref", "category_ref", "group_ref", "name", "description", "price_cents", "option_group_refs", "kind", "confidence", "issues"],
      "additionalProperties": false}},
    "option_groups": {"type": "array", "items": {
      "type": "object",
      "properties": {
        "ref": {"type": "string"},
        "name": {"type": "string"},
        "min": {"type": "integer"},
        "max": {"type": "integer"},
        "options": {"type": "array", "items": {
          "type": "object",
          "properties": {"title": {"type": "string"}, "extra_price_cents": {"type": "integer"}},
          "required": ["title", "extra_price_cents"], "additionalProperties": false}}},
      "required": ["ref", "name", "min", "max", "options"], "additionalProperties": false}},
    "formulas": {"type": "array", "items": {
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "price_cents": {"anyOf": [{"type": "integer"}, {"type": "null"}]},
        "description": {"type": "string"}},
      "required": ["name", "price_cents", "description"], "additionalProperties": false}},
    "warnings": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["categories", "product_groups", "products", "option_groups", "formulas", "warnings"],
  "additionalProperties": false
}`)

// DecodeAIMenuPage relit la réponse du modèle pour une photo.
func DecodeAIMenuPage(raw string) (*AIMenuPage, error) {
	var page AIMenuPage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return nil, fmt.Errorf("réponse IA illisible: %w", err)
	}
	return &page, nil
}

// aiGroup est un groupe en cours d'assemblage : il ne devient un produit
// groupe que s'il garde au moins deux déclinaisons.
type aiGroup struct {
	externalID  string
	name        string
	categoryID  string
	sourcePhoto int
	children    []int // index dans products
}

type aiMerger struct {
	out *IntermediateImport

	categoryNames  map[string]string // externalID -> nom
	categoryByKey  map[string]string // nom normalisé -> externalID
	attributeByKey map[string]string // clé normalisée -> externalID

	groups      []*aiGroup
	groupByID   map[string]*aiGroup
	products    []CanonicalProduct
	productByID map[string]int
}

// BuildAIMenuImport fusionne les pages lues (une par photo, dans l'ordre des
// photos) en un import canonique.
//
// Fusion entre photos, par identité de nom :
//   - catégories : même nom → même catégorie ;
//   - groupes d'options : même nom ET mêmes options → même groupe ; deux
//     « Suppléments » différents (burgers, pizzas) restent distincts ;
//   - produits : même catégorie et même nom → doublon (photos qui se
//     chevauchent), le premier est gardé et un avertissement donne les deux prix.
//
// Groupes : un groupe proposé n'est créé que s'il garde au moins deux
// déclinaisons (« s'il n'y a qu'un seul Coca, on le laisse à la racine »).
// Les produits groupes précèdent tous les autres dans Products, pour que le
// commit crée chaque parent avant ses enfants.
func BuildAIMenuImport(pages []AIMenuPage) (*IntermediateImport, error) {
	m := &aiMerger{
		out:            &IntermediateImport{Provider: AIPhotoSlug},
		categoryNames:  make(map[string]string),
		categoryByKey:  make(map[string]string),
		attributeByKey: make(map[string]string),
		groupByID:      make(map[string]*aiGroup),
		productByID:    make(map[string]int),
	}

	for i := range pages {
		m.addPage(&pages[i], i+1)
	}

	m.finishGroups()

	if len(m.out.Products) == 0 {
		return nil, ErrNoProducts
	}
	return m.out, nil
}

func (m *aiMerger) warn(code, ref, message string) {
	m.out.SourceWarnings = append(m.out.SourceWarnings, SourceWarning{Code: code, Ref: ref, Message: message})
}

func (m *aiMerger) addPage(page *AIMenuPage, photo int) {
	photoRef := fmt.Sprintf("photo %d", photo)

	categoryByRef := make(map[string]string)
	for _, c := range page.Categories {
		if id := m.category(c.Name); id != "" {
			categoryByRef[c.Ref] = id
		}
	}
	resolveCategory := func(ref *string) string {
		if ref == nil {
			return ""
		}
		return categoryByRef[*ref]
	}

	attributeByRef := make(map[string]string)
	for _, og := range page.OptionGroups {
		if id := m.attribute(og); id != "" {
			attributeByRef[og.Ref] = id
		}
	}

	groupByRef := make(map[string]*aiGroup)
	for _, g := range page.ProductGroups {
		if grp := m.group(g, resolveCategory(g.CategoryRef), photo); grp != nil {
			groupByRef[g.Ref] = grp
		}
	}

	for _, p := range page.Products {
		var grp *aiGroup
		if p.GroupRef != nil {
			grp = groupByRef[*p.GroupRef]
		}
		m.product(p, resolveCategory(p.CategoryRef), grp, attributeByRef, photo)
	}

	for _, f := range page.Formulas {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		price := ""
		if f.PriceCents != nil && *f.PriceCents > 0 {
			price = fmt.Sprintf(" à %s €", formatCents(*f.PriceCents))
		}
		// Seules les formules composées de produits de la carte arrivent ici :
		// une formule à choix simples est lue comme un produit à options
		// (menuOCRSystemPrompt).
		// Message affiché tel quel au restaurateur : on dit quoi faire, pas ce
		// qui manque.
		m.warn(WarningAIFormulaNotCreated, photoRef, fmt.Sprintf(
			"formule « %s »%s (photo %d) : à configurer manuellement, par exemple en promotion "+
				"ou en produit « %s » à prix fixe",
			name, price, photo, name))
	}

	for _, w := range page.Warnings {
		if w = strings.TrimSpace(w); w != "" {
			m.warn(WarningAIPhoto, photoRef, fmt.Sprintf("photo %d : %s", photo, w))
		}
	}
}

func (m *aiMerger) category(rawName string) string {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return ""
	}
	key := importutil.NormalizeLabel(name)
	if id, ok := m.categoryByKey[key]; ok {
		return id
	}
	id := importutil.GeneratedExternalID(aiCategoryPrefix, name)
	m.categoryByKey[key] = id
	m.categoryNames[id] = name
	m.out.Categories = append(m.out.Categories, CanonicalCategory{ExternalID: id, Name: name})
	return id
}

func (m *aiMerger) attribute(og AIOptionGroup) string {
	name := strings.TrimSpace(og.Name)
	if name == "" {
		return ""
	}

	options := make([]CanonicalOption, 0, len(og.Options))
	titles := make([]string, 0, len(og.Options))
	seen := make(map[string]struct{}, len(og.Options))
	for _, o := range og.Options {
		title := strings.TrimSpace(o.Title)
		if title == "" {
			continue
		}
		key := importutil.NormalizeLabel(title)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		extra := o.ExtraPriceCents
		if extra < 0 || extra > aiMaxPriceCents {
			extra = 0
		}
		options = append(options, CanonicalOption{Title: title, ExtraPrice: extra})
		titles = append(titles, key)
	}
	if len(options) == 0 {
		return ""
	}

	sort.Strings(titles)
	groupKey := importutil.NormalizeLabel(name) + aiOptionKeySeparator + strings.Join(titles, ", ")
	if id, ok := m.attributeByKey[groupKey]; ok {
		return id
	}
	id := importutil.GeneratedExternalID(aiAttributePrefix, groupKey)
	m.attributeByKey[groupKey] = id

	for i := range options {
		options[i].ExternalID = importutil.GeneratedExternalID(aiOptionPrefix, groupKey+aiOptionKeySeparator+options[i].Title)
	}

	// Bornes proposées par le modèle, ramenées dans l'intervalle valide :
	// 0 ≤ min ≤ max ≤ nombre d'options, max à 0 ouvrant à toutes les options.
	maxOptions := og.Max
	if maxOptions <= 0 || maxOptions > len(options) {
		maxOptions = len(options)
	}
	minOptions := og.Min
	if minOptions < 0 {
		minOptions = 0
	}
	if minOptions > maxOptions {
		minOptions = maxOptions
	}

	m.out.Attributes = append(m.out.Attributes, CanonicalAttribute{
		ExternalID: id,
		Name:       name,
		Type:       AttributeTypeCheck,
		MinOptions: minOptions,
		MaxOptions: maxOptions,
		IsRequired: minOptions > 0,
		Options:    options,
	})
	return id
}

func (m *aiMerger) group(g AIProductGroup, categoryID string, photo int) *aiGroup {
	name := strings.TrimSpace(g.Name)
	if name == "" {
		return nil
	}
	id := importutil.GeneratedExternalID(aiGroupPrefix, m.categoryNames[categoryID]+aiKeySeparator+name)
	if grp, ok := m.groupByID[id]; ok {
		return grp
	}
	grp := &aiGroup{externalID: id, name: name, categoryID: categoryID, sourcePhoto: photo}
	m.groupByID[id] = grp
	m.groups = append(m.groups, grp)
	return grp
}

func (m *aiMerger) product(p AIProduct, categoryID string, grp *aiGroup, attributeByRef map[string]string, photo int) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return
	}
	// Une déclinaison vit dans la catégorie de son groupe.
	if grp != nil && grp.categoryID != "" {
		categoryID = grp.categoryID
	}

	id := importutil.GeneratedExternalID(aiProductPrefix, m.categoryNames[categoryID]+aiKeySeparator+name)
	if idx, dup := m.productByID[id]; dup {
		first := m.products[idx]
		m.warn(WarningAIDuplicateMerged, id, fmt.Sprintf(
			"« %s » apparaît sur les photos %d et %d : gardé une seule fois, au prix de la photo %d (%s ; photo %d : %s)",
			name, first.SourcePhoto, photo, first.SourcePhoto,
			describePrice(first.PriceIn, first.AllPricesZero), photo, describeAIPrice(p.PriceCents)))
		return
	}

	issues := make([]string, 0, len(p.Issues))
	for _, issue := range p.Issues {
		if issue = strings.TrimSpace(issue); issue != "" {
			issues = append(issues, issue)
		}
	}

	price := 0
	if p.PriceCents != nil && *p.PriceCents > 0 && *p.PriceCents <= aiMaxPriceCents {
		price = *p.PriceCents
	} else {
		issues = append(issues, "prix absent ou illisible")
	}

	kind := ProductKind(p.Kind)
	if !kind.IsKnown() {
		kind = KindOther
	}
	rateIn, rateTakeAway, rateDelivery := KindTvaRates(kind)

	confidence := p.Confidence
	if confidence != aiConfidenceHigh && confidence != aiConfidenceMedium {
		confidence = aiConfidenceLow
	}

	var attributes []string
	seen := make(map[string]struct{})
	for _, ref := range p.OptionGroupRefs {
		if attrID, ok := attributeByRef[ref]; ok {
			if _, dup := seen[attrID]; !dup {
				seen[attrID] = struct{}{}
				attributes = append(attributes, attrID)
			}
		}
	}

	product := CanonicalProduct{
		ExternalID:           id,
		Name:                 name,
		Description:          strings.TrimSpace(p.Description),
		PriceIn:              price,
		PriceTakeAway:        price,
		PriceDelivery:        price,
		TvaRateIn:            rateIn,
		TvaRateTakeAway:      rateTakeAway,
		TvaRateDelivery:      rateDelivery,
		CategoryExternalID:   categoryID,
		AllPricesZero:        price == 0,
		AttributeExternalIDs: attributes,
		Kind:                 kind,
		Confidence:           confidence,
		Issues:               issues,
		SourcePhoto:          photo,
	}

	m.productByID[id] = len(m.products)
	m.products = append(m.products, product)
	if grp != nil {
		grp.children = append(grp.children, len(m.products)-1)
	}
}

// finishGroups crée les produits groupes qui gardent au moins deux
// déclinaisons, rattache leurs enfants, puis ordonne Products : groupes
// d'abord, produits ensuite, chacun dans l'ordre de lecture.
func (m *aiMerger) finishGroups() {
	for _, grp := range m.groups {
		switch len(grp.children) {
		case 0:
			continue
		case 1:
			child := m.products[grp.children[0]]
			m.warn(WarningAIGroupDissolved, child.ExternalID, fmt.Sprintf(
				"le groupe « %s » n'a qu'une déclinaison (« %s ») : produit laissé seul, sans groupe", grp.name, child.Name))
			continue
		}

		// Le groupe prend la nature (donc la TVA) de sa première déclinaison :
		// tva_*_id est NOT NULL, y compris pour un produit groupe sans prix.
		kind := m.products[grp.children[0]].Kind
		rateIn, rateTakeAway, rateDelivery := KindTvaRates(kind)
		m.out.Products = append(m.out.Products, CanonicalProduct{
			ExternalID:         grp.externalID,
			Name:               grp.name,
			CategoryExternalID: grp.categoryID,
			TvaRateIn:          rateIn,
			TvaRateTakeAway:    rateTakeAway,
			TvaRateDelivery:    rateDelivery,
			IsGroup:            true,
			Kind:               kind,
			Confidence:         aiConfidenceHigh,
			SourcePhoto:        grp.sourcePhoto,
		})
		for _, idx := range grp.children {
			m.products[idx].ParentExternalID = grp.externalID
		}
	}
	m.out.Products = append(m.out.Products, m.products...)
}

func formatCents(cents int) string {
	return strings.Replace(fmt.Sprintf("%.2f", float64(cents)/100), ".", ",", 1)
}

func describePrice(cents int, missing bool) string {
	if missing {
		return "absent"
	}
	return formatCents(cents) + " €"
}

func describeAIPrice(cents *int) string {
	if cents == nil || *cents <= 0 {
		return "absent"
	}
	return formatCents(*cents) + " €"
}
