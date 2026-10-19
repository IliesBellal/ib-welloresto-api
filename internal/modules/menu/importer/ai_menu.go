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
	aiColorGroupPrefix   = "ai-k"
	aiMaxPriceCents      = 1_000_000 // 10 000 € : au-delà, prix jugé illisible
	aiConfidenceHigh     = "high"
	aiConfidenceMedium   = "medium"
	aiConfidenceLow      = "low"
	aiKeySeparator       = " / "
	aiOptionKeySeparator = " | "
)

// aiColorPalette fixe les couleurs proposées pour products.bg_color : une
// couleur par famille de produits (color_group_ref), jamais par recette.
// Sept teintes fortement distinctes, pensées pour un écran de caisse (fond
// derrière un libellé noir ou blanc), dans l'ordre où elles sont attribuées à
// la première famille rencontrée, puis la deuxième, etc. Au-delà de sept
// familles sur un même menu (le modèle ne devrait pas en proposer plus,
// menuOCRSystemPrompt), les suivantes réutilisent le cycle plutôt que de
// bloquer l'import — un avertissement le signale (WarningAIColorPalette).
var aiColorPalette = []string{
	"#E53935", // rouge — viande rouge
	"#FB8C00", // orange — volaille
	"#2E7D32", // vert — végétarien, salades
	"#1565C0", // bleu — poisson, fruits de mer
	"#6D4C41", // brun — pizza, four
	"#8E24AA", // violet — dessert
	"#00897B", // bleu-vert — boissons
}

// Codes des signalements de la porte IA (SourceWarning → PreviewWarning).
const (
	WarningAIFormulaNotCreated = "ai_formula_not_created"
	WarningAIPhoto             = "ai_photo_warning"
	WarningAIDuplicateMerged   = "ai_duplicate_merged"
	WarningAIGroupDissolved    = "ai_group_dissolved"
	WarningAILowConfidence     = "ai_low_confidence"
	// WarningAIColorPalette : plus de familles de couleur (color_groups) que
	// la palette n'a de teintes distinctes — les suivantes réutilisent une
	// couleur déjà posée. Non bloquant : le restaurateur peut changer
	// n'importe quelle couleur après import.
	WarningAIColorPalette = "ai_color_palette_exceeded"
)

// AIMenuPage est la sortie du modèle pour une photo. Le JSON est contraint
// par AIMenuPageSchema côté API, puis revalidé ici (BuildAIMenuImport) : les
// références croisées et les bornes ne sont pas exprimables dans le schéma.
type AIMenuPage struct {
	Categories    []AICategory     `json:"categories"`
	ProductGroups []AIProductGroup `json:"product_groups"`
	// ColorGroups : familles de produits utilisées pour proposer une couleur
	// (products.bg_color), au sens de menuOCRSystemPrompt — jamais une par
	// recette. Fusionnées entre photos par nom, comme Categories ; chacune
	// reçoit une teinte de aiColorPalette à sa première apparition.
	ColorGroups  []AIColorGroup  `json:"color_groups"`
	Products     []AIProduct     `json:"products"`
	OptionGroups []AIOptionGroup `json:"option_groups"`
	Formulas     []AIFormula     `json:"formulas"`
	Warnings     []string        `json:"warnings"`

	// Offers : le texte de chaque offre ou promotion vue sur la photo, recopié.
	// C'est tout ce que la lecture principale en dit : la structurer demande un
	// schéma trop gros pour cette même requête (la grammaire compilée par l'API
	// est limitée). Une seconde requête, par photo qui en montre, les lit à part
	// (AIPromotionsSchema) ; ses résultats sont rangés dans les deux champs
	// suivants avant le stockage de la page.
	Offers []string `json:"offers"`
	// Promotions : lues par la seconde requête et restituables exactement ;
	// PromotionsNotCreated : vues, mais hors de la structure (ai_promotions.go).
	Promotions           []AIPromotion           `json:"promotions"`
	PromotionsNotCreated []AIPromotionNotCreated `json:"promotions_not_created"`
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

// AIColorGroup est une famille de produits proposée pour la couleur
// (menuOCRSystemPrompt) : viande rouge, volaille, poisson, végétarien,
// pizza, dessert, boisson… Jamais une par recette ni par catégorie de carte.
type AIColorGroup struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

type AIProduct struct {
	Ref             string   `json:"ref"`
	CategoryRef     *string  `json:"category_ref"`
	GroupRef        *string  `json:"group_ref"`
	ColorGroupRef   *string  `json:"color_group_ref"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	PriceCents      *int     `json:"price_cents"`
	OptionGroupRefs []string `json:"option_group_refs"`
	Kind            string   `json:"kind"`
	Confidence      string   `json:"confidence"`
	Issues          []string `json:"issues"`
	// Ingredients : vide sauf si les ingrédients ont été demandés
	// (menuOCRUserPrompt). Absent des lectures antérieures : nil.
	Ingredients []AIIngredient `json:"ingredients"`
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
    "color_groups": {"type": "array", "items": {
      "type": "object",
      "properties": {"ref": {"type": "string"}, "name": {"type": "string"}},
      "required": ["ref", "name"], "additionalProperties": false}},
    "products": {"type": "array", "items": {
      "type": "object",
      "properties": {
        "ref": {"type": "string"},
        "category_ref": {"anyOf": [{"type": "string"}, {"type": "null"}]},
        "group_ref": {"anyOf": [{"type": "string"}, {"type": "null"}]},
        "color_group_ref": {"anyOf": [{"type": "string"}, {"type": "null"}]},
        "name": {"type": "string"},
        "description": {"type": "string"},
        "price_cents": {"anyOf": [{"type": "integer"}, {"type": "null"}]},
        "option_group_refs": {"type": "array", "items": {"type": "string"}},
        "kind": {"type": "string", "enum": ["food", "hot_drink", "soft_drink_served", "soft_drink_sealed", "packaged_food", "alcohol", "other"]},
        "confidence": {"type": "string", "enum": ["high", "medium", "low"]},
        "issues": {"type": "array", "items": {"type": "string"}},
        "ingredients": {"type": "array", "items": {
          "type": "object",
          "properties": {
            "name": {"type": "string"},
            "category": {"type": "string", "enum": ["meat", "fish", "dairy", "vegetables", "fruits", "grocery", "sauces", "bakery", "other"]},
            "unit": {"type": "string", "enum": ["PCE", "G", "KG", "L", "ML", "CL"]}},
          "required": ["name", "category", "unit"], "additionalProperties": false}}},
      "required": ["ref", "category_ref", "group_ref", "color_group_ref", "name", "description", "price_cents", "option_group_refs", "kind", "confidence", "issues", "ingredients"],
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
    "warnings": {"type": "array", "items": {"type": "string"}},
    "offers": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["categories", "product_groups", "color_groups", "products", "option_groups", "formulas", "warnings", "offers"],
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

	// Couleur (color_groups) : même mécanique que les catégories, une famille
	// par nom normalisé, fusionnée entre photos. colorHexByID fixe la teinte
	// à la première apparition de la famille (ordre de colorOrder), cyclée
	// au-delà de aiColorPalette.
	colorGroupByKey map[string]string // nom normalisé -> externalID
	colorHexByID    map[string]string // externalID -> hex
	colorOrder      []string          // externalID, dans l'ordre d'apparition
	colorOverflowed bool

	groups      []*aiGroup
	groupByID   map[string]*aiGroup
	products    []CanonicalProduct
	productByID map[string]int

	// Promotions (ai_promotions.go).
	promotions     []CanonicalPromotion
	promotionByKey map[string]int

	// Ingrédients (ai_ingredients.go).
	unitIDs              map[string]string // unit_of_measure.uom -> id
	componentByKey       map[string]*CanonicalComponent
	ingredientCategories map[string]struct{}
	ingredientsSkipped   bool
}

// BuildAIMenuImport fusionne les pages lues (une par photo, dans l'ordre des
// photos) en un import canonique.
//
// Fusion entre photos, par identité de nom :
//   - catégories : même nom → même catégorie ;
//   - groupes d'options : même nom ET mêmes options (titres et suppléments) →
//     même groupe ; deux « Suppléments » différents (burgers, pizzas) ou deux
//     « Quantité » aux prix différents restent distincts ;
//   - produits : même catégorie et même nom → doublon (photos qui se
//     chevauchent), le premier est gardé et un avertissement donne les deux prix.
//
// Groupes : un groupe proposé n'est créé que s'il garde au moins deux
// déclinaisons (« s'il n'y a qu'un seul Coca, on le laisse à la racine »).
// Les produits groupes précèdent tous les autres dans Products, pour que le
// commit crée chaque parent avant ses enfants.
//
// Ingrédients : même nom (casse, accents et pluriel ignorés) → même
// composant. unitIDs traduit les codes d'unité du modèle (unit_of_measure.uom)
// en identifiants ; il ne sert que si des ingrédients ont été lus.
func BuildAIMenuImport(pages []AIMenuPage, unitIDs map[string]string) (*IntermediateImport, error) {
	m := &aiMerger{
		out:                  &IntermediateImport{Provider: AIPhotoSlug},
		categoryNames:        make(map[string]string),
		categoryByKey:        make(map[string]string),
		attributeByKey:       make(map[string]string),
		colorGroupByKey:      make(map[string]string),
		colorHexByID:         make(map[string]string),
		groupByID:            make(map[string]*aiGroup),
		productByID:          make(map[string]int),
		promotionByKey:       make(map[string]int),
		unitIDs:              unitIDs,
		componentByKey:       make(map[string]*CanonicalComponent),
		ingredientCategories: make(map[string]struct{}),
	}

	for i := range pages {
		m.addPage(&pages[i], i+1)
	}

	m.finishGroups()
	m.out.Promotions = m.promotions
	if m.ingredientsSkipped {
		m.warn(WarningAIIngredientsSkipped, "", "des ingrédients n'ont pas pu être repris : aucune unité de mesure n'est configurée")
	}
	if m.colorOverflowed {
		m.warn(WarningAIColorPalette, "", fmt.Sprintf(
			"plus de %d familles de couleur proposées : certaines partagent la même couleur, à ajuster au besoin",
			len(aiColorPalette)))
	}

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

	colorGroupByRef := make(map[string]string)
	for _, c := range page.ColorGroups {
		if id := m.colorGroup(c.Name); id != "" {
			colorGroupByRef[c.Ref] = id
		}
	}
	resolveColorHex := func(ref *string) string {
		if ref == nil {
			return ""
		}
		return m.colorHexByID[colorGroupByRef[*ref]]
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

	productIDByRef := make(map[string]string)
	for _, p := range page.Products {
		var grp *aiGroup
		if p.GroupRef != nil {
			grp = groupByRef[*p.GroupRef]
		}
		colorHex := resolveColorHex(p.ColorGroupRef)
		if id := m.product(p, resolveCategory(p.CategoryRef), grp, attributeByRef, colorHex, photo); id != "" && p.Ref != "" {
			productIDByRef[p.Ref] = id
		}
	}

	for _, promo := range page.Promotions {
		m.promotion(promo, productIDByRef, photo)
	}
	for _, skipped := range page.PromotionsNotCreated {
		reason := strings.TrimSpace(skipped.Reason)
		if reason == "" {
			reason = "promotion non exprimable exactement"
		}
		m.promotionNotCreated(truncateRunes(strings.TrimSpace(skipped.SourceText), 40), skipped.SourceText, reason, photo)
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

// colorGroup rend l'identifiant externe d'une famille de couleur, fusionnée
// entre photos par nom normalisé comme une catégorie. Contrairement à une
// catégorie, elle ne devient aucune entité Wello : seule sa teinte
// (colorHexByID) sert, posée à sa première apparition.
func (m *aiMerger) colorGroup(rawName string) string {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return ""
	}
	key := importutil.NormalizeLabel(name)
	if id, ok := m.colorGroupByKey[key]; ok {
		return id
	}
	id := importutil.GeneratedExternalID(aiColorGroupPrefix, name)
	m.colorGroupByKey[key] = id
	m.colorOrder = append(m.colorOrder, id)
	if len(m.colorOrder) > len(aiColorPalette) {
		m.colorOverflowed = true
	}
	m.colorHexByID[id] = aiColorPalette[(len(m.colorOrder)-1)%len(aiColorPalette)]
	return id
}

func (m *aiMerger) attribute(og AIOptionGroup) string {
	name := strings.TrimSpace(og.Name)
	if name == "" {
		return ""
	}

	options := make([]CanonicalOption, 0, len(og.Options))
	// Un titre et son supplément : deux groupes « Quantité » de mêmes titres
	// (x4, x6, x9) mais de suppléments différents (nuggets, ailes) sont deux
	// groupes distincts.
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
		titles = append(titles, fmt.Sprintf("%s=%d", key, extra))
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

// product ajoute un produit et rend son identifiant externe (celui du premier
// quand la ligne en double un autre), "" si la ligne est ignorée.
func (m *aiMerger) product(p AIProduct, categoryID string, grp *aiGroup, attributeByRef map[string]string, colorHex string, photo int) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return ""
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
		return id
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
		Components:           m.ingredients(p),
		ColorHex:             colorHex,
	}

	m.productByID[id] = len(m.products)
	m.products = append(m.products, product)
	if grp != nil {
		grp.children = append(grp.children, len(m.products)-1)
	}
	return id
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

		// Le groupe prend la nature (donc la TVA) et la couleur de sa première
		// déclinaison : tva_*_id est NOT NULL, y compris pour un produit groupe
		// sans prix, et les déclinaisons d'un même groupe partagent la même
		// famille de couleur en pratique (ce sont les mêmes produits).
		first := m.products[grp.children[0]]
		kind := first.Kind
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
			ColorHex:           first.ColorHex,
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
