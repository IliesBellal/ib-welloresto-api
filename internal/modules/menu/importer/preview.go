package importer

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Statuts posés par l'import. `available` pour un produit normal (il est
// first-class dans le back-office), `removed_from_menu` pour les lignes sans
// prix, qui n'ont rien à faire sur la carte tant que personne ne les a
// valorisées.
const (
	ProductStatusAvailable       = "available"
	ProductStatusRemovedFromMenu = "removed_from_menu"
)

// Actions proposées pour une entité de l'import.
const (
	// ActionCreate : l'entité sera créée.
	ActionCreate = "create"
	// ActionReuseExisting : une entité du marchand porte déjà ce nom, on s'y
	// rattache au lieu d'en créer une homonyme (aucune contrainte d'unicité
	// n'existe en base, la déduplication est entièrement applicative).
	ActionReuseExisting = "reuse_existing"
	// ActionAlreadyImported : un import précédent du même provider a déjà créé
	// cette entité (import_*_mapping). Elle sera ignorée — c'est l'idempotence.
	ActionAlreadyImported = "already_imported"
)

// Origine de la catégorie proposée pour un produit.
const (
	CategorySourceExplicit = "explicit"  // la source la désigne (template, saisie)
	CategorySourceFirstTag = "first_tag" // 1er libellé du produit classé catégorie
	CategorySourceNone     = "none"      // indéterminée : le wizard doit trancher
)

// Codes de warning. Ils sont stables : le back-office s'en sert pour router
// l'utilisateur vers l'écran de correction correspondant.
const (
	WarningTvaRateUnresolved      = "tva_rate_unresolved"
	WarningTvaRateMissing         = "tva_rate_missing"
	WarningProductNeedsCategory   = "product_needs_category"
	WarningProductNameCollision   = "product_name_collision"
	WarningProductRemovedFromMenu = "product_removed_from_menu"
	WarningLabelDropped           = "label_dropped"
	WarningTagSynthesized         = "tag_synthesized"
)

// ErrNilImport protège BuildPreview d'un appel sans canonique.
var ErrNilImport = errors.New("import canonique absent")

// ---------------------------------------------------------------------------
// Données existantes injectées
// ---------------------------------------------------------------------------

// TvaRateRow est une ligne de tva_categories. La table est globale (pas de
// merchant_id) : un couple (taux, canal) suffit à désigner un tva_id.
type TvaRateRow struct {
	TvaID   int
	Channel TvaChannel
	Rate    float64
}

// ExistingCategory est une catégorie caisse du marchand. MerchantCategID est la
// valeur par laquelle products.category référence la catégorie — et non CategID,
// qui est la vraie PK.
type ExistingCategory struct {
	CategID         int
	MerchantCategID string
	Name            string
}

type ExistingTag struct {
	TagID string
	Name  string
}

type ExistingProduct struct {
	ProductID int
	Name      string
}

// ExistingAttribute est un groupe d'options actif du marchand. Seul son
// identifiant sert : il permet de savoir si un mapping d'import le designe
// encore, ou pointe dans le vide.
type ExistingAttribute struct {
	AttributeID string
}

// ImportedEntities est le contenu des tables import_*_mapping pour le couple
// (marchand, provider) : identifiant externe -> identifiant Wello.
type ImportedEntities struct {
	Products   map[string]int
	Categories map[string]int
	Tags       map[string]string
	Attributes map[string]string
}

// PreviewLookups porte tout ce que BuildPreview a besoin de savoir de la base.
// C'est ce qui garde le cœur pur : la fonction ne lit rien elle-même.
type PreviewLookups struct {
	TvaRates           []TvaRateRow
	ExistingCategories []ExistingCategory
	ExistingTags       []ExistingTag
	ExistingProducts   []ExistingProduct
	ExistingAttributes []ExistingAttribute
	Imported           ImportedEntities
}

// liveImportedEntities dit, pour chaque entite deja mappee, si l'entite Wello
// qu'elle designe existe toujours.
//
// Rien ne desactive un mapping quand le marchand supprime le produit
// correspondant : la correspondance survit a sa cible. Sans ce controle, un
// menu supprime puis reimporte donnait un commit sans effet — tout etait
// « deja importe », donc ignore, et aucun moyen de revenir en arriere.
type liveImportedEntities struct {
	products   map[string]bool
	categories map[string]bool
	tags       map[string]bool
	attributes map[string]bool
}

func newLiveImportedEntities(lk PreviewLookups) liveImportedEntities {
	liveProducts := make(map[int]struct{}, len(lk.ExistingProducts))
	for _, product := range lk.ExistingProducts {
		liveProducts[product.ProductID] = struct{}{}
	}
	liveCategories := make(map[int]struct{}, len(lk.ExistingCategories))
	for _, category := range lk.ExistingCategories {
		liveCategories[category.CategID] = struct{}{}
	}
	liveTags := make(map[string]struct{}, len(lk.ExistingTags))
	for _, tag := range lk.ExistingTags {
		liveTags[tag.TagID] = struct{}{}
	}
	liveAttributes := make(map[string]struct{}, len(lk.ExistingAttributes))
	for _, attribute := range lk.ExistingAttributes {
		liveAttributes[attribute.AttributeID] = struct{}{}
	}

	live := liveImportedEntities{
		products:   make(map[string]bool, len(lk.Imported.Products)),
		categories: make(map[string]bool, len(lk.Imported.Categories)),
		tags:       make(map[string]bool, len(lk.Imported.Tags)),
		attributes: make(map[string]bool, len(lk.Imported.Attributes)),
	}
	for externalID, welloID := range lk.Imported.Products {
		_, ok := liveProducts[welloID]
		live.products[externalID] = ok
	}
	for externalID, welloID := range lk.Imported.Categories {
		_, ok := liveCategories[welloID]
		live.categories[externalID] = ok
	}
	for externalID, welloID := range lk.Imported.Tags {
		_, ok := liveTags[welloID]
		live.tags[externalID] = ok
	}
	for externalID, welloID := range lk.Imported.Attributes {
		_, ok := liveAttributes[welloID]
		live.attributes[externalID] = ok
	}
	return live
}

// stale dit qu'une correspondance existe mais que sa cible a disparu.
func (l liveImportedEntities) stale(index map[string]bool, externalID string) bool {
	alive, mapped := index[externalID]
	return mapped && !alive
}

// ---------------------------------------------------------------------------
// Résultat
// ---------------------------------------------------------------------------

// PreviewResult est le dry-run complet renvoyé au wizard.
//
// Token et ExpiresAt sont renseignés par le service après stockage du snapshot :
// BuildPreview ne connaît ni Redis ni l'horloge.
type PreviewResult struct {
	Token     string `json:"token"`
	Provider  string `json:"provider"`
	ExpiresAt string `json:"expires_at"`

	Summary    PreviewSummary     `json:"summary"`
	TvaRates   []PreviewTvaRate   `json:"tva_rates"`
	Categories []PreviewCategory  `json:"categories"`
	Tags       []PreviewTag       `json:"tags"`
	Products   []PreviewProduct   `json:"products"`
	Attributes []PreviewAttribute `json:"attributes"`
	Warnings   []PreviewWarning   `json:"warnings"`

	// Decisions est la forme compacte des propositions ci-dessus. Le wizard
	// la renvoie amendée à la phase de commit ; c'est le contrat machine.
	Decisions ImportDecisions `json:"decisions"`
}

type PreviewSummary struct {
	ProductsToCreate          int `json:"products_to_create"`
	ProductsAlreadyImported   int `json:"products_already_imported"`
	ProductsRemovedFromMenu   int `json:"products_removed_from_menu"`
	ProductsNeedingCategory   int `json:"products_needing_category"`
	ProductsWithNameCollision int `json:"products_with_name_collision"`
	// ProductsMappingStale : deja importes, mais le produit Wello
	// correspondant n'existe plus. Ce sont ceux qu'un reimport repare.
	ProductsMappingStale int `json:"products_mapping_stale"`

	CategoriesToCreate int `json:"categories_to_create"`
	CategoriesReused   int `json:"categories_reused"`

	TagsToCreate  int `json:"tags_to_create"`
	TagsReused    int `json:"tags_reused"`
	TagsSynthetic int `json:"tags_synthetic"`

	AttributesToCreate        int `json:"attributes_to_create"`
	AttributesAlreadyImported int `json:"attributes_already_imported"`
	OptionsToCreate           int `json:"options_to_create"`

	UnresolvedTvaRates int `json:"unresolved_tva_rates"`
}

// PreviewTvaRate est un couple (taux, canal) à résoudre en tva_id.
type PreviewTvaRate struct {
	Rate float64 `json:"rate"`
	// Channel porte la valeur de tva_categories.delivery_type : « IN »,
	// « TAKE_AWAY » ou « DELIVERY ».
	Channel      TvaChannel `json:"channel"`
	ChannelLabel string     `json:"channel_label"`
	TvaID        int        `json:"tva_id"`
	Resolved     bool       `json:"resolved"`
	ProductCount int        `json:"product_count"`

	// NeededForBackfill marque un couple qui ne figure pas tel quel dans le
	// fichier : il est requis parce qu'un canal désactivé (taux 0) doit tout
	// de même recevoir un tva_id, tva_*_id étant NOT NULL.
	NeededForBackfill bool `json:"needed_for_backfill"`
}

type PreviewCategory struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Action     string `json:"action"`
	// ExistingCategoryID est le merchant_categ_id réutilisé, pas la PK.
	ExistingCategoryID string `json:"existing_category_id,omitempty"`
	ProductCount       int    `json:"product_count"`

	MappingStale bool `json:"mapping_stale,omitempty"`
}

type PreviewTag struct {
	ExternalID   string `json:"external_id"`
	Name         string `json:"name"`
	Class        string `json:"class"`
	Synthetic    bool   `json:"synthetic"`
	Action       string `json:"action"`
	ProductCount int    `json:"product_count"`

	ExistingTagID      string `json:"existing_tag_id,omitempty"`
	ExistingCategoryID string `json:"existing_category_id,omitempty"`

	// MappingStale : deja importe, mais l'entite Wello a disparu. Elle sera
	// recreee d'office — un contenant ne merite pas d'arbitrage.
	MappingStale bool `json:"mapping_stale,omitempty"`
}

type PreviewProduct struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Action     string `json:"action"`
	Status     string `json:"status"`

	CategoryExternalID string `json:"category_external_id"`
	CategorySource     string `json:"category_source"`
	NeedsCategory      bool   `json:"needs_category"`

	TagExternalIDs []string `json:"tag_external_ids"`

	// DroppedLabelExternalIDs recense les libellés classés catégorie qui ne
	// sont pas la catégorie retenue : un produit n'en porte qu'une, et ces
	// libellés ne deviendront pas non plus des tags. Explicités plutôt que
	// perdus en silence.
	DroppedLabelExternalIDs []string `json:"dropped_label_external_ids,omitempty"`

	Channels PreviewChannels `json:"channels"`

	NameCollision *PreviewNameCollision `json:"name_collision,omitempty"`

	// MappingStale : un import precedent a cree ce produit, mais il n'existe
	// plus dans Wello. Le reimporter le recree ; sans cette information, il
	// resterait ignore indefiniment.
	MappingStale bool `json:"mapping_stale,omitempty"`
	// Reimport est l'arbitrage propose pour un produit deja importe.
	Reimport string `json:"reimport,omitempty"`
}

type PreviewChannels struct {
	In       PreviewChannel `json:"in"`
	TakeAway PreviewChannel `json:"take_away"`
	Delivery PreviewChannel `json:"delivery"`
}

type PreviewChannel struct {
	Price int      `json:"price"`
	Rate  *float64 `json:"rate"`
	TvaID int      `json:"tva_id"`

	// Available passe à false quand le taux vaut 0 : c'est ainsi que la source
	// exprime « ce produit n'est pas vendu sur ce canal ».
	Available bool `json:"available"`
	Resolved  bool `json:"resolved"`

	// Backfilled : le tva_id ne vient pas du taux du canal (qui vaut 0) mais du
	// taux le plus haut du produit, re-résolu sur ce canal.
	Backfilled      bool `json:"backfilled"`
	PriceBackfilled bool `json:"price_backfilled"`
}

type PreviewAttribute struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	Action      string `json:"action"`
	OptionCount int    `json:"option_count"`
	MinOptions  int    `json:"min_options"`
	MaxOptions  int    `json:"max_options"`

	MappingStale bool `json:"mapping_stale,omitempty"`
}

type PreviewNameCollision struct {
	ExistingProductID int    `json:"existing_product_id"`
	ExistingName      string `json:"existing_name"`
	Resolution        string `json:"resolution"`
}

type PreviewWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Ref     string `json:"ref,omitempty"`
}

// ---------------------------------------------------------------------------
// Résolution de TVA
// ---------------------------------------------------------------------------

// tvaResolver résout un couple (taux, canal) en tva_id.
//
// Les taux sont comparés en centièmes de point entiers, pas en float :
// tva_categories.tva_rate est un `real` (float32) et le taux du fichier vient
// d'un parse texte. Un 5,5 des deux côtés doit se rencontrer, pas se rater sur
// une différence de représentation.
type tvaResolver struct {
	byRateChannel map[tvaLookupKey]int
	// keyByID retient le couple de chaque tva_id actif, pour pouvoir vérifier
	// qu'un identifiant renvoyé par le wizard correspond bien au canal annoncé.
	keyByID map[int]tvaLookupKey
}

type tvaLookupKey struct {
	rate    int
	channel TvaChannel
}

func newTvaResolver(rows []TvaRateRow) *tvaResolver {
	r := &tvaResolver{
		byRateChannel: make(map[tvaLookupKey]int, len(rows)),
		keyByID:       make(map[int]tvaLookupKey, len(rows)),
	}
	for _, row := range rows {
		key := tvaLookupKey{rate: rateToHundredths(row.Rate), channel: row.Channel}
		r.keyByID[row.TvaID] = key

		// Plusieurs lignes peuvent porter le même couple (titres différents) :
		// on retient le plus petit tva_id, pour que la preview soit stable.
		if existing, ok := r.byRateChannel[key]; ok && existing <= row.TvaID {
			continue
		}
		r.byRateChannel[key] = row.TvaID
	}
	return r
}

func (r *tvaResolver) resolve(rate float64, channel TvaChannel) (int, bool) {
	id, ok := r.byRateChannel[tvaLookupKey{rate: rateToHundredths(rate), channel: channel}]
	return id, ok
}

// describeID rend le canal et le taux d'un tva_id actif.
//
// Sert à valider ce que le wizard renvoie. Le contrôle porte sur le canal et
// non sur le taux : quand un taux du fichier n'existe pas chez le marchand,
// l'écran de vérification lui fait justement désigner un autre taux du même
// canal pour le remplacer. Exiger la correspondance exacte rendrait ce choix
// impossible — c'est le canal qui doit être respecté, tva_*_id devant matcher
// le delivery_type de la colonne.
func (r *tvaResolver) describeID(tvaID int) (TvaChannel, float64, bool) {
	key, ok := r.keyByID[tvaID]
	if !ok {
		return "", 0, false
	}
	return key.channel, float64(key.rate) / 100, true
}

func rateToHundredths(rate float64) int { return int(math.Round(rate * 100)) }

// channelOrder classe les canaux dans l'ordre d'affichage du back-office
// plutot que dans l'ordre alphabetique de leur valeur en base.
func channelOrder(channel TvaChannel) int {
	for i, known := range AllTvaChannels {
		if known == channel {
			return i
		}
	}
	return len(AllTvaChannels)
}

// ---------------------------------------------------------------------------
// BuildPreview
// ---------------------------------------------------------------------------

// BuildPreview calcule le dry-run complet d'un import.
//
// Fonction pure : elle ne lit ni base ni Redis, tout l'existant lui arrive par
// lookups. C'est ce qui la rend testable sur les fichiers réels sans
// infrastructure, et c'est aussi ce qui garantit que la preview et le commit
// raisonnent sur les mêmes données.
func BuildPreview(imp *IntermediateImport, lk PreviewLookups) (*PreviewResult, error) {
	if imp == nil {
		return nil, ErrNilImport
	}

	b := &previewBuilder{
		imp:      imp,
		lk:       lk,
		live:     newLiveImportedEntities(lk),
		resolver: newTvaResolver(lk.TvaRates),
		res: &PreviewResult{
			Provider: imp.Provider,
			Decisions: ImportDecisions{
				TagClassification:  make(map[string]TagClass),
				CategoryPerProduct: make(map[string]string),
				TvaMapping:         make(map[TvaRateKey]int),
				NameCollisions:     make(map[string]NameCollisionResolution),
				AlreadyImported:    make(map[string]ReimportResolution),
			},
		},
		tvaSeen: make(map[tvaLookupKey]int),
	}

	b.classifyLabels()
	b.buildCategories()
	b.buildTags()
	b.buildProducts()
	b.buildAttributes()
	b.buildTvaRates()

	return b.res, nil
}

type previewBuilder struct {
	imp      *IntermediateImport
	lk       PreviewLookups
	live     liveImportedEntities
	resolver *tvaResolver
	res      *PreviewResult

	labelClass map[string]TagClass
	labelUsage map[string]int

	// tvaSeen indexe les couples rencontrés vers leur position dans
	// res.TvaRates, pour n'en produire qu'une ligne et y cumuler les compteurs.
	tvaSeen map[tvaLookupKey]int
}

// classifyLabels propose une classification catégorie/tag pour chaque libellé.
//
// Un libellé est proposé « catégorie » s'il ouvre la liste d'au moins un produit
// dépourvu de catégorie explicite. C'est la convention de l'export 2026 (parent
// en tête) ; celui de 2025 a un ordre non fiable, d'où l'écran de validation.
// La règle ne s'applique pas aux sources qui désignent leur catégorie
// elles-mêmes : leurs libellés restent tous des tags.
func (b *previewBuilder) classifyLabels() {
	b.labelClass = make(map[string]TagClass, len(b.imp.Tags))
	b.labelUsage = make(map[string]int, len(b.imp.Tags))

	opensAProduct := make(map[string]bool)
	for _, p := range b.imp.Products {
		for i, id := range p.TagExternalIDs {
			b.labelUsage[id]++
			if i == 0 && p.CategoryExternalID == "" {
				opensAProduct[id] = true
			}
		}
	}

	for _, tag := range b.imp.Tags {
		class := TagClassTag
		if opensAProduct[tag.ExternalID] {
			class = TagClassCategory
		}
		b.labelClass[tag.ExternalID] = class
		b.res.Decisions.TagClassification[tag.ExternalID] = class
	}
}

// buildCategories traite les catégories que la source désigne explicitement.
func (b *previewBuilder) buildCategories() {
	usage := make(map[string]int)
	for _, p := range b.imp.Products {
		if p.CategoryExternalID != "" {
			usage[p.CategoryExternalID]++
		}
	}

	existing := indexCategoriesByName(b.lk.ExistingCategories)

	for _, category := range b.imp.Categories {
		entry := PreviewCategory{
			ExternalID:   category.ExternalID,
			Name:         category.Name,
			ProductCount: usage[category.ExternalID],
			Action:       ActionCreate,
		}

		entry.MappingStale = b.live.stale(b.live.categories, category.ExternalID)

		switch {
		// Un mapping périmé ne compte pas comme déjà importé : l'entité a
		// disparu, elle sera recréée. Le dire ici garde les compteurs en phase
		// avec ce que le commit fera réellement.
		case hasIntMapping(b.lk.Imported.Categories, category.ExternalID) && !entry.MappingStale:
			entry.Action = ActionAlreadyImported
		default:
			if match, ok := existing[normalizeLabel(category.Name)]; ok {
				entry.Action = ActionReuseExisting
				entry.ExistingCategoryID = match.MerchantCategID
				b.res.Summary.CategoriesReused++
			} else {
				b.res.Summary.CategoriesToCreate++
			}
		}

		b.res.Categories = append(b.res.Categories, entry)
	}
}

// buildTags traite les libellés source, dédupliqués contre l'existant du
// marchand selon la classe proposée : les catégories contre productcateg, les
// tags contre tags. Aucune contrainte d'unicité n'existant en base, sans cette
// étape un import répété créerait des homonymes en silence.
func (b *previewBuilder) buildTags() {
	existingCategories := indexCategoriesByName(b.lk.ExistingCategories)
	existingTags := indexTagsByName(b.lk.ExistingTags)

	for _, tag := range b.imp.Tags {
		class := b.labelClass[tag.ExternalID]

		entry := PreviewTag{
			ExternalID:   tag.ExternalID,
			Name:         tag.Name,
			Class:        string(class),
			Synthetic:    tag.Synthetic,
			ProductCount: b.labelUsage[tag.ExternalID],
			Action:       ActionCreate,
		}

		if tag.Synthetic {
			b.res.Summary.TagsSynthetic++
			b.warn(WarningTagSynthesized, tag.ExternalID,
				fmt.Sprintf("le libellé %q est utilisé par un produit mais n'est déclaré nulle part dans le fichier", tag.Name))
		}

		alreadyImported := hasStringMapping(b.lk.Imported.Tags, tag.ExternalID) ||
			hasIntMapping(b.lk.Imported.Categories, tag.ExternalID)
		entry.MappingStale = b.live.stale(b.live.tags, tag.ExternalID) ||
			b.live.stale(b.live.categories, tag.ExternalID)

		switch {
		case alreadyImported && !entry.MappingStale:
			entry.Action = ActionAlreadyImported
		case class == TagClassCategory:
			if match, ok := existingCategories[normalizeLabel(tag.Name)]; ok {
				entry.Action = ActionReuseExisting
				entry.ExistingCategoryID = match.MerchantCategID
				b.res.Summary.CategoriesReused++
			} else {
				b.res.Summary.CategoriesToCreate++
			}
		default:
			if match, ok := existingTags[normalizeLabel(tag.Name)]; ok {
				entry.Action = ActionReuseExisting
				entry.ExistingTagID = match.TagID
				b.res.Summary.TagsReused++
			} else {
				b.res.Summary.TagsToCreate++
			}
		}

		b.res.Tags = append(b.res.Tags, entry)
	}
}

func (b *previewBuilder) buildProducts() {
	existingByName := indexProductsByName(b.lk.ExistingProducts, b.lk.Imported.Products)

	for i := range b.imp.Products {
		p := &b.imp.Products[i]

		entry := PreviewProduct{
			ExternalID: p.ExternalID,
			Name:       p.Name,
			Action:     ActionCreate,
			Status:     ProductStatusAvailable,
		}
		if p.AllPricesZero {
			entry.Status = ProductStatusRemovedFromMenu
		}

		// Un produit déjà importé est ignoré par défaut : inutile de lui
		// réclamer une catégorie ou d'arbitrer une collision qui ne se
		// produira pas. On le liste, on ne l'instruit pas — l'écran de
		// vérification peut décider de le recréer, et c'est alors le plan de
		// commit qui l'instruira.
		if _, imported := b.lk.Imported.Products[p.ExternalID]; imported {
			entry.Action = ActionAlreadyImported
			entry.MappingStale = b.live.stale(b.live.products, p.ExternalID)
			entry.Reimport = string(ReimportSkip)
			b.res.Decisions.AlreadyImported[p.ExternalID] = ReimportSkip

			b.res.Summary.ProductsAlreadyImported++
			if entry.MappingStale {
				b.res.Summary.ProductsMappingStale++
			}

			entry.Channels = b.buildChannels(p, false)
			b.res.Products = append(b.res.Products, entry)
			continue
		}

		b.res.Summary.ProductsToCreate++
		if p.AllPricesZero {
			b.res.Summary.ProductsRemovedFromMenu++
			b.warn(WarningProductRemovedFromMenu, p.ExternalID,
				fmt.Sprintf("%q n'a aucun prix : importé en statut %s", p.Name, ProductStatusRemovedFromMenu))
		}

		b.assignCategory(p, &entry)
		entry.Channels = b.buildChannels(p, true)

		if match, ok := existingByName[normalizeLabel(p.Name)]; ok {
			entry.NameCollision = &PreviewNameCollision{
				ExistingProductID: match.ProductID,
				ExistingName:      match.Name,
				Resolution:        string(CollisionSkip),
			}
			b.res.Decisions.NameCollisions[p.ExternalID] = CollisionSkip
			b.res.Summary.ProductsWithNameCollision++
			b.warn(WarningProductNameCollision, p.ExternalID,
				fmt.Sprintf("un produit nommé %q existe déjà ; ignoré par défaut", p.Name))
		}

		b.res.Products = append(b.res.Products, entry)
	}
}

// assignCategory retient la catégorie du produit et répartit ses libellés.
func (b *previewBuilder) assignCategory(p *CanonicalProduct, entry *PreviewProduct) {
	if p.CategoryExternalID != "" {
		entry.CategoryExternalID = p.CategoryExternalID
		entry.CategorySource = CategorySourceExplicit
	} else {
		entry.CategorySource = CategorySourceNone
		for _, id := range p.TagExternalIDs {
			if b.labelClass[id] == TagClassCategory {
				entry.CategoryExternalID = id
				entry.CategorySource = CategorySourceFirstTag
				break
			}
		}
	}

	for _, id := range p.TagExternalIDs {
		switch {
		case b.labelClass[id] != TagClassCategory:
			entry.TagExternalIDs = append(entry.TagExternalIDs, id)
		case id != entry.CategoryExternalID:
			entry.DroppedLabelExternalIDs = append(entry.DroppedLabelExternalIDs, id)
			b.warn(WarningLabelDropped, p.ExternalID,
				fmt.Sprintf("%q porte plusieurs libellés classés catégorie ; seul le premier est retenu", p.Name))
		}
	}

	if entry.CategoryExternalID == "" {
		entry.NeedsCategory = true
		b.res.Summary.ProductsNeedingCategory++
		b.warn(WarningProductNeedsCategory, p.ExternalID,
			fmt.Sprintf("%q n'a aucune catégorie : la catégorie est obligatoire", p.Name))
		return
	}

	b.res.Decisions.CategoryPerProduct[p.ExternalID] = entry.CategoryExternalID
}

// buildChannels résout prix, taux et disponibilité des trois canaux.
//
// instruct distingue les produits qui seront réellement créés de ceux qui sont
// déjà importés : ces derniers ne doivent produire ni warning ni couple de TVA
// à résoudre, puisqu'ils ne partiront pas en base.
func (b *previewBuilder) buildChannels(p *CanonicalProduct, instruct bool) PreviewChannels {
	maxPrice := p.PriceIn
	if p.PriceTakeAway > maxPrice {
		maxPrice = p.PriceTakeAway
	}
	if p.PriceDelivery > maxPrice {
		maxPrice = p.PriceDelivery
	}

	// Taux candidats au backfill, du plus haut au plus bas.
	var candidates []float64
	for _, rate := range []*float64{p.TvaRateIn, p.TvaRateTakeAway, p.TvaRateDelivery} {
		if rate != nil && *rate > 0 {
			candidates = append(candidates, *rate)
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(candidates)))

	return PreviewChannels{
		In:       b.buildChannel(p, TvaChannelIn, p.PriceIn, p.TvaRateIn, maxPrice, candidates, instruct),
		TakeAway: b.buildChannel(p, TvaChannelTakeAway, p.PriceTakeAway, p.TvaRateTakeAway, maxPrice, candidates, instruct),
		Delivery: b.buildChannel(p, TvaChannelDelivery, p.PriceDelivery, p.TvaRateDelivery, maxPrice, candidates, instruct),
	}
}

func (b *previewBuilder) buildChannel(
	p *CanonicalProduct,
	channel TvaChannel,
	price int,
	rate *float64,
	maxPrice int,
	backfillCandidates []float64,
	instruct bool,
) PreviewChannel {
	out := PreviewChannel{Price: price, Rate: rate, Available: true}

	switch {
	case rate == nil:
		// Aucune information : tva_*_id est NOT NULL, le wizard devra choisir.
		if instruct {
			b.warn(WarningTvaRateMissing, p.ExternalID,
				fmt.Sprintf("%q n'a pas de taux de TVA sur le canal %s", p.Name, channel.Label()))
		}

	case *rate == 0:
		// Le canal est désactivé, mais tva_*_id et price_* restent NOT NULL :
		// on les remplit avec le taux le plus haut du produit et le prix le
		// plus haut défini, pour qu'une réactivation ultérieure parte d'un
		// état cohérent plutôt que d'un zéro.
		out.Available = false
		if price == 0 && maxPrice > 0 {
			out.Price = maxPrice
			out.PriceBackfilled = true
		}
		for _, candidate := range backfillCandidates {
			if tvaID, ok := b.resolver.resolve(candidate, channel); ok {
				out.TvaID = tvaID
				out.Resolved = true
				out.Backfilled = true
				if instruct {
					b.noteTvaCouple(candidate, channel, tvaID, true, true)
				}
				break
			}
		}
		if !out.Resolved && instruct && len(backfillCandidates) > 0 {
			b.noteTvaCouple(backfillCandidates[0], channel, 0, false, true)
		}

	default:
		tvaID, ok := b.resolver.resolve(*rate, channel)
		out.TvaID = tvaID
		out.Resolved = ok
		if instruct {
			b.noteTvaCouple(*rate, channel, tvaID, ok, false)
		}
	}

	return out
}

// noteTvaCouple enregistre un couple (taux, canal) dans la liste à résoudre.
func (b *previewBuilder) noteTvaCouple(rate float64, channel TvaChannel, tvaID int, resolved, forBackfill bool) {
	key := tvaLookupKey{rate: rateToHundredths(rate), channel: channel}

	idx, seen := b.tvaSeen[key]
	if !seen {
		b.res.TvaRates = append(b.res.TvaRates, PreviewTvaRate{
			Rate:              rate,
			Channel:           channel,
			ChannelLabel:      channel.Label(),
			TvaID:             tvaID,
			Resolved:          resolved,
			NeededForBackfill: forBackfill,
		})
		idx = len(b.res.TvaRates) - 1
		b.tvaSeen[key] = idx

		if resolved {
			b.res.Decisions.TvaMapping[TvaRateKey{Rate: rate, Channel: channel}] = tvaID
		} else {
			b.res.Summary.UnresolvedTvaRates++
			b.warn(WarningTvaRateUnresolved, fmt.Sprintf("%g:%s", rate, channel),
				fmt.Sprintf("aucun taux de TVA à %g%% n'est configuré pour le canal %s", rate, channel.Label()))
		}
	}

	b.res.TvaRates[idx].ProductCount++
	// Un couple d'abord vu en backfill peut être ensuite réellement présent
	// dans le fichier : il cesse alors d'être un couple « ajouté ».
	if !forBackfill {
		b.res.TvaRates[idx].NeededForBackfill = false
	}
}

// buildTvaRates trie la liste des couples pour une sortie stable.
func (b *previewBuilder) buildTvaRates() {
	sort.SliceStable(b.res.TvaRates, func(i, j int) bool {
		if b.res.TvaRates[i].Channel != b.res.TvaRates[j].Channel {
			return channelOrder(b.res.TvaRates[i].Channel) < channelOrder(b.res.TvaRates[j].Channel)
		}
		return b.res.TvaRates[i].Rate < b.res.TvaRates[j].Rate
	})
}

func (b *previewBuilder) buildAttributes() {
	for _, attribute := range b.imp.Attributes {
		entry := PreviewAttribute{
			ExternalID:  attribute.ExternalID,
			Name:        attribute.Name,
			Action:      ActionCreate,
			OptionCount: len(attribute.Options),
			MinOptions:  attribute.MinOptions,
			MaxOptions:  attribute.MaxOptions,
		}

		entry.MappingStale = b.live.stale(b.live.attributes, attribute.ExternalID)

		if hasStringMapping(b.lk.Imported.Attributes, attribute.ExternalID) && !entry.MappingStale {
			entry.Action = ActionAlreadyImported
			b.res.Summary.AttributesAlreadyImported++
		} else {
			b.res.Summary.AttributesToCreate++
			b.res.Summary.OptionsToCreate += len(attribute.Options)
		}

		b.res.Attributes = append(b.res.Attributes, entry)
	}
}

func (b *previewBuilder) warn(code, ref, message string) {
	b.res.Warnings = append(b.res.Warnings, PreviewWarning{Code: code, Message: message, Ref: ref})
}

// ---------------------------------------------------------------------------
// Index de l'existant
// ---------------------------------------------------------------------------

func indexCategoriesByName(categories []ExistingCategory) map[string]ExistingCategory {
	out := make(map[string]ExistingCategory, len(categories))
	for _, category := range categories {
		key := normalizeLabel(category.Name)
		// Les homonymes existent (aucune contrainte d'unicité) : le premier
		// gagne, pour que deux previews successives se rattachent au même.
		if _, ok := out[key]; !ok {
			out[key] = category
		}
	}
	return out
}

func indexTagsByName(tags []ExistingTag) map[string]ExistingTag {
	out := make(map[string]ExistingTag, len(tags))
	for _, tag := range tags {
		key := normalizeLabel(tag.Name)
		if _, ok := out[key]; !ok {
			out[key] = tag
		}
	}
	return out
}

// indexProductsByName exclut les produits qui sont eux-mêmes le résultat d'un
// import précédent du même provider : un produit ne peut pas entrer en
// collision avec sa propre version importée, c'est le mapping qui tranche.
func indexProductsByName(products []ExistingProduct, imported map[string]int) map[string]ExistingProduct {
	fromImport := make(map[int]struct{}, len(imported))
	for _, welloID := range imported {
		fromImport[welloID] = struct{}{}
	}

	out := make(map[string]ExistingProduct, len(products))
	for _, product := range products {
		if _, ok := fromImport[product.ProductID]; ok {
			continue
		}
		key := normalizeLabel(product.Name)
		if _, ok := out[key]; !ok {
			out[key] = product
		}
	}
	return out
}

func hasIntMapping(m map[string]int, key string) bool {
	_, ok := m[key]
	return ok
}

func hasStringMapping(m map[string]string, key string) bool {
	_, ok := m[key]
	return ok
}
