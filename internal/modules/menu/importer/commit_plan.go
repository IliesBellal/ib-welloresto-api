package importer

import (
	"fmt"
	"sort"
	"strings"

	"welloresto-api/internal/importutil"
)

// Codes de blocage. Le commit est refusé tant qu'il en reste un : il vaut mieux
// renvoyer l'utilisateur au wizard que matérialiser un catalogue incomplet.
const (
	BlockerProductNeedsCategory    = "product_needs_category"
	BlockerTvaRateUnresolved       = "tva_rate_unresolved"
	BlockerNameCollisionUnresolved = "product_name_collision_unresolved"
	BlockerInvalidTvaMapping       = "invalid_tva_mapping"
	BlockerInvalidCategoryDecision = "invalid_category_decision"
)

// CommitBlocker est une raison de refuser le lot, rattachée à l'entité fautive.
type CommitBlocker struct {
	Code    string `json:"code"`
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message"`
}

// CommitPlan est le lot entièrement résolu, prêt à être écrit. Il ne contient
// plus aucune décision à prendre ni aucune valeur à résoudre : la transaction
// n'a plus qu'à l'exécuter.
type CommitPlan struct {
	Provider string

	Categories []PlannedCategory
	Tags       []PlannedTag
	Attributes []PlannedAttribute
	Products   []PlannedProduct
}

// PlannedCategory est une catégorie caisse à matérialiser.
type PlannedCategory struct {
	ExternalID string
	Name       string

	// ReuseCategID / ReuseMerchantCategID sont renseignés quand on se rattache
	// à une catégorie existante — soit qu'elle porte déjà ce nom, soit qu'un
	// import précédent l'ait créée. Les deux servent : la PK pour le mapping,
	// le merchant_categ_id pour la référence portée par les produits.
	ReuseCategID         int
	ReuseMerchantCategID string

	// AlreadyImported : un import précédent du même provider l'a déjà créée.
	// Elle est sautée, et le mapping n'est pas réécrit.
	AlreadyImported bool

	// RemapExisting : une correspondance existe déjà pour cet identifiant
	// externe, mais elle désigne une entité disparue. On recrée, et le mapping
	// est mis à jour au lieu d'être inséré — l'index unique l'exige.
	RemapExisting bool
}

// Usable dit si la catégorie sera référençable par un produit : soit elle
// existe déjà, soit elle va être créée — y compris quand elle est recréée
// après la disparition de celle qu'un import précédent avait posée.
func (c PlannedCategory) Usable() bool {
	return !c.AlreadyImported || c.ReuseMerchantCategID != ""
}

type PlannedTag struct {
	ExternalID string
	Name       string

	ReuseTagID      string
	AlreadyImported bool
	RemapExisting   bool
}

type PlannedAttribute struct {
	ExternalID string
	Name       string
	Type       string
	MinOptions int
	MaxOptions int
	Options    []PlannedOption

	AlreadyImported bool
	RemapExisting   bool
}

type PlannedOption struct {
	ExternalID string
	Title      string
	ExtraPrice int
}

// PlannedProduct porte des valeurs finales : plus de taux, que des tva_id.
type PlannedProduct struct {
	ExternalID  string
	Name        string
	Description string
	Status      string

	// CategoryExternalID désigne une entrée de Categories ou de Tags — c'est la
	// transaction qui le traduira en merchant_categ_id, la catégorie pouvant
	// n'exister qu'après son insertion.
	CategoryExternalID string
	TagExternalIDs     []string

	PriceIn       int
	PriceTakeAway int
	PriceDelivery int

	TvaInID       int
	TvaTakeAwayID int
	TvaDeliveryID int

	AvailableIn       bool
	AvailableTakeAway bool
	AvailableDelivery bool

	AlreadyImported bool

	// RemapExisting : le produit est recréé alors qu'une correspondance existe
	// déjà — soit qu'elle désigne un produit supprimé, soit que l'utilisateur
	// ait demandé une nouvelle création. Le mapping est réaffecté.
	RemapExisting bool

	// SkippedByCollision : un produit du marchand porte déjà ce nom et
	// l'utilisateur a tranché « ignorer ».
	SkippedByCollision bool
}

// Materializable dit si l'entité doit réellement être écrite.
func (p PlannedProduct) Materializable() bool {
	return !p.AlreadyImported && !p.SkippedByCollision
}

// BuildCommitPlan résout un snapshot et les décisions du wizard en un plan
// exécutable, ou rend la liste des raisons de refuser.
//
// Fonction pure : lookups injectés, aucun accès base. Ils sont volontairement
// rechargés au moment du commit plutôt que repris de la preview — entre les
// deux, une catégorie a pu être créée, un produit renommé, un import concurrent
// passer. Le plan doit refléter l'état au moment où il sera écrit.
//
// Rien de ce que le client renvoie n'est cru sur parole : chaque tva_id est
// revérifié contre son canal, chaque catégorie citée contre sa classification,
// chaque collision contre la liste réellement détectée.
func BuildCommitPlan(imp *IntermediateImport, decisions ImportDecisions, lk PreviewLookups) (*CommitPlan, []CommitBlocker) {
	if imp == nil {
		return nil, []CommitBlocker{{Code: BlockerInvalidCategoryDecision, Message: ErrNilImport.Error()}}
	}

	b := &commitPlanner{
		imp:       imp,
		decisions: decisions,
		lk:        lk,
		live:      newLiveImportedEntities(lk),
		resolver:  newTvaResolver(lk.TvaRates),
		plan:      &CommitPlan{Provider: imp.Provider},
	}

	b.validateTvaDecisions()
	b.resolveClassification()
	b.buildCategories()
	b.buildTags()
	b.buildAttributes()
	b.buildProducts()

	if len(b.blockers) > 0 {
		return nil, b.blockers
	}
	return b.plan, nil
}

type commitPlanner struct {
	imp       *IntermediateImport
	decisions ImportDecisions
	lk        PreviewLookups
	live      liveImportedEntities
	resolver  *tvaResolver

	plan     *CommitPlan
	blockers []CommitBlocker

	labelClass map[string]TagClass
	// categoryLabels recense les identifiants utilisables comme catégorie :
	// catégories explicites de la source + libellés classés catégorie.
	categoryLabels map[string]struct{}

	// Index sur les entrées du plan, pour que la résolution des produits
	// puisse vérifier qu'une catégorie citée sera bien référençable.
	plannedCategories map[string]*PlannedCategory
	plannedTags       map[string]*PlannedTag
}

func (b *commitPlanner) block(code, ref, message string) {
	b.blockers = append(b.blockers, CommitBlocker{Code: code, Ref: ref, Message: message})
}

// validateTvaDecisions vérifie que chaque tva_id renvoyé par le wizard existe,
// est actif, et appartient au canal annoncé.
//
// Le taux, lui, n'a pas à correspondre : c'est le sens même de l'écran de
// vérification. Quand un taux du fichier n'existe pas chez le marchand — 5,5 %
// sur place, par exemple, alors que sa caisse n'a que 10 % et 20 % — celui-ci
// désigne le taux à utiliser à la place. Exiger la correspondance exacte
// rendrait ce choix systématiquement refusé, donc la fonctionnalité
// inutilisable.
//
// Ce qui reste vérifié est ce qui compte : products.tva_*_id doit pointer sur
// une ligne du bon delivery_type, faute de quoi la TVA appliquée ne
// correspondrait à rien.
func (b *commitPlanner) validateTvaDecisions() {
	for key, tvaID := range b.decisions.TvaMapping {
		channel, _, ok := b.resolver.describeID(tvaID)
		if !ok {
			b.block(BlockerInvalidTvaMapping, tvaKeyRef(key),
				fmt.Sprintf("le taux de TVA n° %d n'existe pas ou est désactivé", tvaID))
			continue
		}
		if channel != key.Channel {
			b.block(BlockerInvalidTvaMapping, tvaKeyRef(key),
				fmt.Sprintf("le taux de TVA n° %d est configuré pour le canal %s, pas %s",
					tvaID, channel.Label(), key.Channel.Label()))
		}
	}
}

// resolveClassification retient la classification du wizard, en la complétant
// par le défaut de la preview pour tout libellé qu'il n'aurait pas tranché.
func (b *commitPlanner) resolveClassification() {
	b.labelClass = make(map[string]TagClass, len(b.imp.Tags))
	b.categoryLabels = make(map[string]struct{})

	opensAProduct := make(map[string]bool)
	for _, p := range b.imp.Products {
		if len(p.TagExternalIDs) > 0 && p.CategoryExternalID == "" {
			opensAProduct[p.TagExternalIDs[0]] = true
		}
	}

	for _, tag := range b.imp.Tags {
		class, given := b.decisions.TagClassification[tag.ExternalID]
		switch {
		case given && (class == TagClassCategory || class == TagClassTag):
			// décision explicite du wizard
		case opensAProduct[tag.ExternalID]:
			class = TagClassCategory
		default:
			class = TagClassTag
		}

		b.labelClass[tag.ExternalID] = class
		if class == TagClassCategory {
			b.categoryLabels[tag.ExternalID] = struct{}{}
		}
	}

	for _, category := range b.imp.Categories {
		b.categoryLabels[category.ExternalID] = struct{}{}
	}
}

func (b *commitPlanner) buildCategories() {
	byName := indexCategoriesByName(b.lk.ExistingCategories)
	byID := make(map[int]ExistingCategory, len(b.lk.ExistingCategories))
	for _, category := range b.lk.ExistingCategories {
		byID[category.CategID] = category
	}

	b.plannedCategories = make(map[string]*PlannedCategory)

	// Les catégories explicites de la source et les libellés promus en
	// catégorie rejoignent la même liste : rien ne les distingue une fois la
	// classification tranchée.
	type candidate struct{ externalID, name string }
	candidates := make([]candidate, 0, len(b.imp.Categories)+len(b.imp.Tags))
	for _, category := range b.imp.Categories {
		candidates = append(candidates, candidate{category.ExternalID, category.Name})
	}
	for _, tag := range b.imp.Tags {
		if b.labelClass[tag.ExternalID] == TagClassCategory {
			candidates = append(candidates, candidate{tag.ExternalID, tag.Name})
		}
	}

	for _, c := range candidates {
		entry := PlannedCategory{ExternalID: c.externalID, Name: c.name}

		if categID, imported := b.lk.Imported.Categories[c.externalID]; imported {
			if match, alive := byID[categID]; alive {
				entry.AlreadyImported = true
				entry.ReuseCategID = match.CategID
				entry.ReuseMerchantCategID = match.MerchantCategID
			} else if match, ok := byName[importutil.NormalizeLabel(c.name)]; ok {
				// Recréée à la main entre-temps sous le même nom : on s'y
				// rattache et on réaffecte la correspondance.
				entry.ReuseCategID = match.CategID
				entry.ReuseMerchantCategID = match.MerchantCategID
				entry.RemapExisting = true
			} else {
				// La catégorie a disparu. La laisser « déjà importée » la
				// rendrait définitivement irrécupérable : on la recrée et on
				// réaffecte la correspondance.
				entry.RemapExisting = true
			}
		} else if match, ok := byName[importutil.NormalizeLabel(c.name)]; ok {
			entry.ReuseCategID = match.CategID
			entry.ReuseMerchantCategID = match.MerchantCategID
		}

		b.plan.Categories = append(b.plan.Categories, entry)
	}

	for i := range b.plan.Categories {
		b.plannedCategories[b.plan.Categories[i].ExternalID] = &b.plan.Categories[i]
	}
}

func (b *commitPlanner) buildTags() {
	byName := indexTagsByName(b.lk.ExistingTags)
	alive := make(map[string]struct{}, len(b.lk.ExistingTags))
	for _, tag := range b.lk.ExistingTags {
		alive[tag.TagID] = struct{}{}
	}

	b.plannedTags = make(map[string]*PlannedTag)

	for _, tag := range b.imp.Tags {
		if b.labelClass[tag.ExternalID] != TagClassTag {
			continue
		}

		entry := PlannedTag{ExternalID: tag.ExternalID, Name: tag.Name}

		if tagID, imported := b.lk.Imported.Tags[tag.ExternalID]; imported {
			if _, ok := alive[tagID]; ok {
				entry.AlreadyImported = true
				entry.ReuseTagID = tagID
			} else if match, ok := byName[importutil.NormalizeLabel(tag.Name)]; ok {
				entry.ReuseTagID = match.TagID
				entry.RemapExisting = true
			} else {
				// tags n'a pas de suppression logique : un tag disparu l'est
				// physiquement. On le recrée et on réaffecte, sans quoi les
				// produits recréés perdraient leurs étiquettes.
				entry.RemapExisting = true
			}
		} else if match, ok := byName[importutil.NormalizeLabel(tag.Name)]; ok {
			entry.ReuseTagID = match.TagID
		}

		b.plan.Tags = append(b.plan.Tags, entry)
	}

	for i := range b.plan.Tags {
		b.plannedTags[b.plan.Tags[i].ExternalID] = &b.plan.Tags[i]
	}
}

func (b *commitPlanner) buildAttributes() {
	for _, attribute := range b.imp.Attributes {
		entry := PlannedAttribute{
			ExternalID: attribute.ExternalID,
			Name:       attribute.Name,
			Type:       attribute.Type,
			MinOptions: attribute.MinOptions,
			MaxOptions: attribute.MaxOptions,
		}

		if hasStringMapping(b.lk.Imported.Attributes, attribute.ExternalID) {
			if b.live.stale(b.live.attributes, attribute.ExternalID) {
				// Le groupe d'options a été supprimé depuis : on le recrée et
				// on réaffecte la correspondance.
				entry.RemapExisting = true
			} else {
				entry.AlreadyImported = true
			}
		}

		for _, option := range attribute.Options {
			entry.Options = append(entry.Options, PlannedOption{
				ExternalID: option.ExternalID,
				Title:      option.Title,
				ExtraPrice: option.ExtraPrice,
			})
		}

		b.plan.Attributes = append(b.plan.Attributes, entry)
	}
}

func (b *commitPlanner) buildProducts() {
	collisions := indexProductsByName(b.lk.ExistingProducts, b.lk.Imported.Products)

	for i := range b.imp.Products {
		p := &b.imp.Products[i]

		entry := PlannedProduct{
			ExternalID:    p.ExternalID,
			Name:          p.Name,
			Description:   p.Description,
			Status:        ProductStatusAvailable,
			PriceIn:       p.PriceIn,
			PriceTakeAway: p.PriceTakeAway,
			PriceDelivery: p.PriceDelivery,
		}
		if p.AllPricesZero {
			entry.Status = ProductStatusRemovedFromMenu
		}

		// Déjà importé : ignoré par défaut, donc rien à lui réclamer — le
		// contrôler ferait échouer un lot pour un produit qu'on écarte.
		// L'utilisateur peut demander sa recréation, et il redevient alors un
		// produit à instruire comme les autres : c'est ce qui permet de
		// réimporter un menu supprimé.
		if _, imported := b.lk.Imported.Products[p.ExternalID]; imported {
			if b.decisions.AlreadyImported[p.ExternalID] != ReimportRecreate {
				entry.AlreadyImported = true
				b.plan.Products = append(b.plan.Products, entry)
				continue
			}
			entry.RemapExisting = true
		}

		if match, collides := collisions[importutil.NormalizeLabel(p.Name)]; collides {
			switch b.decisions.NameCollisions[p.ExternalID] {
			case CollisionSkip:
				entry.SkippedByCollision = true
			case CollisionImportAnyway:
				// création assumée du doublon
			default:
				b.block(BlockerNameCollisionUnresolved, p.ExternalID,
					fmt.Sprintf("le produit %q existe déjà (product_id %d) : choisir de l'ignorer ou de l'importer quand même",
						p.Name, match.ProductID))
				b.plan.Products = append(b.plan.Products, entry)
				continue
			}
		}

		if entry.SkippedByCollision {
			b.plan.Products = append(b.plan.Products, entry)
			continue
		}

		b.assignCategory(p, &entry)
		b.checkCategoryUsable(p, &entry)
		b.assignTags(p, &entry)
		b.assignChannels(p, &entry)

		b.plan.Products = append(b.plan.Products, entry)
	}
}

// assignCategory retient la catégorie du produit : décision explicite du
// wizard, sinon catégorie de la source, sinon premier libellé classé catégorie.
func (b *commitPlanner) assignCategory(p *CanonicalProduct, entry *PlannedProduct) {
	if forced, ok := b.decisions.CategoryPerProduct[p.ExternalID]; ok && forced != "" {
		if _, valid := b.categoryLabels[forced]; !valid {
			b.block(BlockerInvalidCategoryDecision, p.ExternalID,
				fmt.Sprintf("la catégorie %q imposée à %q n'est pas classée comme catégorie", forced, p.Name))
			return
		}
		entry.CategoryExternalID = forced
		return
	}

	if p.CategoryExternalID != "" {
		entry.CategoryExternalID = p.CategoryExternalID
		return
	}

	for _, id := range p.TagExternalIDs {
		if _, ok := b.categoryLabels[id]; ok {
			entry.CategoryExternalID = id
			return
		}
	}

	b.block(BlockerProductNeedsCategory, p.ExternalID,
		fmt.Sprintf("%q n'a aucune catégorie : la catégorie est obligatoire", p.Name))
}

// checkCategoryUsable est le pendant en mémoire de la validation 2 de
// validateProductForCreate (« la catégorie existe et est activée ») : elle
// couvre le cas d'une catégorie créée par un import précédent puis désactivée,
// que le mapping empêche de recréer et qui laisserait products.category
// pointer dans le vide.
func (b *commitPlanner) checkCategoryUsable(p *CanonicalProduct, entry *PlannedProduct) {
	if entry.CategoryExternalID == "" {
		return // déjà signalé par assignCategory
	}

	planned, known := b.plannedCategories[entry.CategoryExternalID]
	if !known {
		b.block(BlockerInvalidCategoryDecision, p.ExternalID,
			fmt.Sprintf("la catégorie %q de %q ne fait pas partie de l'import", entry.CategoryExternalID, p.Name))
		return
	}
	if !planned.Usable() {
		b.block(BlockerProductNeedsCategory, p.ExternalID,
			fmt.Sprintf("la catégorie %q de %q a été importée puis désactivée : en choisir une autre", planned.Name, p.Name))
	}
}

// assignTags ne garde que les libellés restés tags — et jamais celui retenu
// comme catégorie.
//
// Un tag déjà mappé mais physiquement supprimé depuis est écarté : il n'est
// plus rattachable, et SyncProductTags refuserait le produit entier pour un
// tag facultatif.
func (b *commitPlanner) assignTags(p *CanonicalProduct, entry *PlannedProduct) {
	for _, id := range p.TagExternalIDs {
		if b.labelClass[id] != TagClassTag || id == entry.CategoryExternalID {
			continue
		}
		if planned, known := b.plannedTags[id]; known && planned.AlreadyImported && planned.ReuseTagID == "" {
			continue
		}
		entry.TagExternalIDs = append(entry.TagExternalIDs, id)
	}
}

// assignChannels transforme les taux bruts en tva_id définitifs.
//
// Miroir en mémoire de validateProductForCreate (validations 1 à 4) : champs de
// TVA obligatoires, tva_id numériques, catégorie existante et activée, et
// existence de chacun des trois taux. La différence est le coût — l'original
// émet quatre requêtes par produit et revalide 141 fois les mêmes valeurs,
// alors que le pool est plafonné à une connexion. Toute évolution de
// validateProductForCreate doit être répercutée ici.
func (b *commitPlanner) assignChannels(p *CanonicalProduct, entry *PlannedProduct) {
	maxPrice := p.PriceIn
	if p.PriceTakeAway > maxPrice {
		maxPrice = p.PriceTakeAway
	}
	if p.PriceDelivery > maxPrice {
		maxPrice = p.PriceDelivery
	}

	// Taux candidats au repli, du plus bas au plus haut : un taux à 0 signifie
	// « utiliser le seul taux de TVA défini », ou le plus bas s'il y en a
	// plusieurs — pas « canal indisponible ».
	var backfillCandidates []float64
	for _, rate := range []*float64{p.TvaRateIn, p.TvaRateTakeAway, p.TvaRateDelivery} {
		if rate != nil && *rate > 0 {
			backfillCandidates = append(backfillCandidates, *rate)
		}
	}
	sort.Sort(sort.Float64Slice(backfillCandidates))

	channels := []struct {
		channel   TvaChannel
		rate      *float64
		price     *int
		tvaID     *int
		available *bool
	}{
		{TvaChannelIn, p.TvaRateIn, &entry.PriceIn, &entry.TvaInID, &entry.AvailableIn},
		{TvaChannelTakeAway, p.TvaRateTakeAway, &entry.PriceTakeAway, &entry.TvaTakeAwayID, &entry.AvailableTakeAway},
		{TvaChannelDelivery, p.TvaRateDelivery, &entry.PriceDelivery, &entry.TvaDeliveryID, &entry.AvailableDelivery},
	}

	for _, ch := range channels {
		*ch.available = true

		switch {
		case ch.rate == nil:
			b.block(BlockerTvaRateUnresolved, p.ExternalID,
				fmt.Sprintf("%q n'a pas de taux de TVA sur le canal %s", p.Name, ch.channel.Label()))

		case *ch.rate == 0:
			// Un taux à 0 ne signifie pas « canal indisponible », mais « aucun
			// taux spécifique pour ce canal » : on résout le canal avec le
			// taux le plus bas défini par ailleurs sur le produit (ou l'unique
			// taux défini s'il n'y en a qu'un). price_* reste NOT NULL : on le
			// remplit avec le prix le plus haut défini si le canal n'a pas de
			// prix propre.
			if *ch.price == 0 && maxPrice > 0 {
				*ch.price = maxPrice
			}

			resolved := false
			for _, candidate := range backfillCandidates {
				if tvaID, ok := b.lookupTva(candidate, ch.channel); ok {
					*ch.tvaID = tvaID
					resolved = true
					break
				}
			}
			if !resolved {
				b.block(BlockerTvaRateUnresolved, p.ExternalID,
					fmt.Sprintf("le canal %s de %q a un taux de TVA à 0 mais aucun taux de repli n'est configuré pour ce canal",
						ch.channel.Label(), p.Name))
			}

		default:
			tvaID, ok := b.lookupTva(*ch.rate, ch.channel)
			if !ok {
				b.block(BlockerTvaRateUnresolved, p.ExternalID,
					fmt.Sprintf("aucun taux de TVA à %g%% n'est configuré pour le canal %s (produit %q)",
						*ch.rate, ch.channel.Label(), p.Name))
				continue
			}
			*ch.tvaID = tvaID
		}
	}
}

// lookupTva privilégie la décision du wizard quand elle existe et qu'elle a
// passé validateTvaDecisions, et retombe sinon sur la résolution directe.
//
// La décision l'emporte même si le taux du fichier existe par ailleurs : c'est
// un choix explicite de l'utilisateur, pas une valeur par défaut à corriger.
func (b *commitPlanner) lookupTva(rate float64, channel TvaChannel) (int, bool) {
	if tvaID, ok := b.decisions.TvaMapping[TvaRateKey{Rate: rate, Channel: channel}]; ok {
		if decided, _, exists := b.resolver.describeID(tvaID); exists && decided == channel {
			return tvaID, true
		}
	}
	return b.resolver.resolve(rate, channel)
}

func tvaKeyRef(key TvaRateKey) string {
	encoded, err := key.MarshalText()
	if err != nil {
		return ""
	}
	return string(encoded)
}

// BlockersMessage résume des blocages pour un log sur une ligne.
func BlockersMessage(blockers []CommitBlocker) string {
	parts := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		parts = append(parts, blocker.Code+"("+blocker.Ref+")")
	}
	return strings.Join(parts, ", ")
}
