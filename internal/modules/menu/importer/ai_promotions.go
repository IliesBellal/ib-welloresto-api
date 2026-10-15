package importer

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"welloresto-api/internal/importutil"
)

// Promotions lues sur la carte (porte IA).
//
// Le modèle ne décrit pas une promotion en texte libre : il remplit une
// structure fermée (AIPromotion) que le code valide et traduit en promotion de
// caisse (table discounts). Une promotion que la structure ne sait pas exprimer
// exactement, ou dont un élément est illisible, n'est pas créée : elle donne un
// avertissement qui reprend le texte de la carte et la raison.
//
// Toute promotion lue est créée INACTIVE (discounts.available = false, donc
// jamais appliquée par le calcul de prix) : une erreur de lecture sur une
// remise coûte de l'argent sans se voir, à la différence d'un prix.
//
// Le moteur de promotions n'est pas modifié ici. Ses limites actuelles
// (docs/promotions-moteur-a-fiabiliser.md) sont signalées sur la promotion
// concernée plutôt que contournées.

// Codes des signalements des promotions (SourceWarning → PreviewWarning).
const (
	WarningAIPromotionNotCreated = "ai_promotion_not_created"
	WarningAIPromotionDuplicate  = "ai_promotion_duplicate_merged"
)

const (
	aiPromotionPrefix   = "ai-promo"
	aiPromotionNameMax  = 50  // discounts.discount_name varchar(50)
	aiPromotionDescMax  = 100 // discounts.discount_desc varchar(100)
	aiPromotionMaxBuy   = 20
	aiPromotionAllUnits = 99 // « toutes les unités » : le moteur remise discounted_quantity unités

	// Valeurs de discounts.discount_unit / min_order_unit.
	discountUnitPercentage = "PERCENTAGE"
	discountUnitCurrency   = "CURRENCY"
	discountUnitNewPrice   = "NEWPRICE"
	minOrderUnitQuantity   = "QUANTITY"
	minOrderUnitCurrency   = "CURRENCY"

	// Modes de commande, tels que le moteur de prix les compare.
	orderTypeIn       = "IN"
	orderTypeTakeAway = "TAKE_AWAY"
	orderTypeDelivery = "DELIVERY"

	promotionEngineNthFreeNote = "Limite actuelle du moteur de promotions : la promotion s'applique une seule fois par commande, " +
		"sur le premier article éligible du panier (pas forcément le moins cher)."
)

// PromotionKind est la nature d'une promotion lue.
type PromotionKind string

const (
	PromotionNewPrice   PromotionKind = "new_price"   // prix fixe sur les produits ciblés
	PromotionPercentOff PromotionKind = "percent_off" // x % de remise
	PromotionAmountOff  PromotionKind = "amount_off"  // x € de remise
	PromotionNthFree    PromotionKind = "nth_free"    // N achetés, le suivant offert
)

// AIPromotion est une promotion telle que le modèle la restitue. Le JSON est
// contraint par AIMenuPageSchema, volontairement sans champ facultatif
// (anyOf avec null) : la grammaire compilée par l'API a une taille limite que
// la première version, à champs facultatifs, dépassait. Une valeur absente est
// donc 0 ou une chaîne vide ; les bornes et références sont revalidées ici
// (aiMerger.promotion).
type AIPromotion struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Value dépend de Kind : nouveau prix en centimes (new_price), pourcentage
	// (percent_off), montant en centimes (amount_off), nombre d'articles
	// achetés avant l'offert (nth_free : 3 → « le 4ᵉ offert »).
	Value int `json:"value"`
	// MaxUnits : nombre d'articles remisés par commande ; 0 = tous les
	// articles concernés. Toujours 0 pour nth_free.
	MaxUnits int `json:"max_units"`
	// OrderTypes : IN, TAKE_AWAY, DELIVERY ; vide = tous les modes.
	OrderTypes []string `json:"order_types"`
	// Days : 1 = lundi … 7 = dimanche ; vide = tous les jours.
	Days []int `json:"days"`
	// FromTime, ToTime : « HH:MM » ; vides = toute la journée.
	FromTime string `json:"from_time"`
	ToTime   string `json:"to_time"`
	// ValidUntil : « AAAA-MM-JJ », date de fin ; vide = sans limite.
	ValidUntil string `json:"valid_until"`
	// MinOrderCents : montant minimum de la commande, en centimes ; 0 = aucun.
	MinOrderCents int `json:"min_order_cents"`
	// ProductRefs : produits concernés (refs de products). AllProducts : la
	// promotion vise toute la carte.
	ProductRefs []string `json:"product_refs"`
	AllProducts bool     `json:"all_products"`
	SourceText  string   `json:"source_text"`
}

// AIPromotionNotCreated est une promotion que le modèle a vue sans pouvoir la
// restituer exactement dans la structure.
type AIPromotionNotCreated struct {
	SourceText string `json:"source_text"`
	Reason     string `json:"reason"`
}

// CanonicalPromotion est une promotion validée, prête pour la preview et le
// commit.
type CanonicalPromotion struct {
	ExternalID  string
	Name        string
	Description string
	Kind        PromotionKind

	NewPriceCents int // new_price
	Percent       int // percent_off
	AmountCents   int // amount_off
	BuyQuantity   int // nth_free
	MaxUnits      int // 0 = tous les articles concernés

	OrderTypes []string // IN, TAKE_AWAY, DELIVERY ; vide = tous
	Days       []int    // 1..7 triés ; vide = tous les jours
	FromTime   string   // « HH:MM » ou vide
	ToTime     string
	ValidUntil string // « AAAA-MM-JJ » ou vide ; la validité commence à la date de l'import

	MinOrderCents int

	ProductExternalIDs []string
	AllProducts        bool

	SourceText  string
	SourcePhoto int
	// Notes : limites à montrer au restaurateur avant d'activer.
	Notes []string
}

// promotion valide une promotion lue sur une photo et l'ajoute à l'import, ou
// signale pourquoi elle n'est pas créée.
func (m *aiMerger) promotion(p AIPromotion, productIDByRef map[string]string, photo int) {
	promo, reason := buildCanonicalPromotion(p, productIDByRef, photo)
	if reason != "" {
		m.promotionNotCreated(promoLabel(p), p.SourceText, reason, photo)
		return
	}

	key := importutil.NormalizeLabel(promo.Name)
	if idx, dup := m.promotionByKey[key]; dup {
		m.warn(WarningAIPromotionDuplicate, promo.ExternalID, fmt.Sprintf(
			"promotion « %s » présente sur les photos %d et %d : gardée une seule fois, telle que lue sur la photo %d",
			promo.Name, m.promotions[idx].SourcePhoto, photo, m.promotions[idx].SourcePhoto))
		return
	}
	m.promotionByKey[key] = len(m.promotions)
	m.promotions = append(m.promotions, promo)
}

// promotionNotCreated donne au restaurateur ce qu'il faut pour la créer lui-même.
func (m *aiMerger) promotionNotCreated(name, sourceText, reason string, photo int) {
	message := fmt.Sprintf("promotion « %s » (photo %d) non créée : %s.", name, photo, reason)
	if text := strings.TrimSpace(sourceText); text != "" {
		message += fmt.Sprintf(" Texte de la carte : « %s ». À configurer manuellement dans les promotions.", text)
	}
	m.warn(WarningAIPromotionNotCreated, fmt.Sprintf("photo %d", photo), message)
}

func promoLabel(p AIPromotion) string {
	if name := strings.TrimSpace(p.Name); name != "" {
		return name
	}
	if text := strings.TrimSpace(p.SourceText); text != "" {
		return truncateRunes(text, 40)
	}
	return "sans nom"
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

// buildCanonicalPromotion valide une promotion. Une raison non vide, rédigée
// pour le restaurateur, signifie qu'elle ne sera pas créée : « comprise
// parfaitement » ou rien.
func buildCanonicalPromotion(p AIPromotion, productIDByRef map[string]string, photo int) (CanonicalPromotion, string) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return CanonicalPromotion{}, "nom de la promotion illisible"
	}
	promo := CanonicalPromotion{
		Name:        truncateRunes(name, aiPromotionNameMax),
		SourceText:  strings.TrimSpace(p.SourceText),
		SourcePhoto: photo,
	}
	// Le texte de la carte sert de description (varchar(100) côté caisse).
	promo.Description = promo.SourceText
	if promo.Description == "" {
		promo.Description = promo.Name
	}
	promo.Description = truncateRunes(promo.Description, aiPromotionDescMax)
	promo.ExternalID = importutil.GeneratedExternalID(aiPromotionPrefix, promo.Name)

	// Effet.
	promo.Kind = PromotionKind(strings.TrimSpace(p.Kind))
	switch promo.Kind {
	case PromotionNewPrice:
		if p.Value <= 0 || p.Value > aiMaxPriceCents {
			return promo, "prix promotionnel absent ou illisible"
		}
		promo.NewPriceCents = p.Value
	case PromotionPercentOff:
		if p.Value < 1 || p.Value > 100 {
			return promo, "pourcentage de remise absent ou illisible"
		}
		promo.Percent = p.Value
	case PromotionAmountOff:
		if p.Value <= 0 || p.Value > aiMaxPriceCents {
			return promo, "montant de la remise absent ou illisible"
		}
		promo.AmountCents = p.Value
	case PromotionNthFree:
		if p.Value < 1 || p.Value > aiPromotionMaxBuy {
			return promo, "nombre d'articles achetés absent ou illisible"
		}
		promo.BuyQuantity = p.Value
		promo.Notes = append(promo.Notes, promotionEngineNthFreeNote)
	default:
		return promo, fmt.Sprintf("type de promotion %q non pris en charge", p.Kind)
	}

	if p.MaxUnits != 0 {
		if promo.Kind == PromotionNthFree {
			return promo, "un nombre d'articles remisés n'a pas de sens pour un article offert"
		}
		if p.MaxUnits < 1 || p.MaxUnits > aiPromotionAllUnits {
			return promo, "nombre d'articles remisés par commande illisible"
		}
		promo.MaxUnits = p.MaxUnits
	}

	if p.MinOrderCents != 0 {
		if promo.Kind == PromotionNthFree {
			return promo, "un montant minimum de commande ne se combine pas avec un article offert"
		}
		if p.MinOrderCents < 0 || p.MinOrderCents > aiMaxPriceCents {
			return promo, "montant minimum de commande illisible"
		}
		promo.MinOrderCents = p.MinOrderCents
	}

	// Modes de commande.
	seenOrderType := make(map[string]struct{}, 3)
	for _, raw := range p.OrderTypes {
		orderType := strings.ToUpper(strings.TrimSpace(raw))
		switch orderType {
		case orderTypeIn, orderTypeTakeAway, orderTypeDelivery:
		default:
			return promo, fmt.Sprintf("mode de commande %q non reconnu", raw)
		}
		if _, dup := seenOrderType[orderType]; !dup {
			seenOrderType[orderType] = struct{}{}
			promo.OrderTypes = append(promo.OrderTypes, orderType)
		}
	}
	if len(promo.OrderTypes) == 3 {
		promo.OrderTypes = nil
	}

	// Jours et heures.
	seenDay := make(map[int]struct{}, 7)
	for _, day := range p.Days {
		if day < 1 || day > 7 {
			return promo, fmt.Sprintf("jour %d hors de 1 (lundi) à 7 (dimanche)", day)
		}
		if _, dup := seenDay[day]; !dup {
			seenDay[day] = struct{}{}
			promo.Days = append(promo.Days, day)
		}
	}
	sort.Ints(promo.Days)

	from, to := strings.TrimSpace(p.FromTime), strings.TrimSpace(p.ToTime)
	if (from == "") != (to == "") {
		return promo, "plage horaire incomplète (une seule heure lue)"
	}
	if from != "" {
		start, err := time.Parse("15:04", from)
		if err != nil {
			return promo, fmt.Sprintf("heure de début %q illisible", from)
		}
		end, err := time.Parse("15:04", to)
		if err != nil {
			return promo, fmt.Sprintf("heure de fin %q illisible", to)
		}
		if !start.Before(end) && to != "00:00" {
			return promo, fmt.Sprintf("plage horaire %s–%s à l'envers", from, to)
		}
		promo.FromTime, promo.ToTime = from, to
	}
	if len(promo.Days) == 7 && promo.FromTime == "" {
		promo.Days = nil
	}

	// Date de fin : la validité commence à la date de l'import.
	if until := strings.TrimSpace(p.ValidUntil); until != "" {
		t, err := time.Parse("2006-01-02", until)
		if err != nil {
			return promo, fmt.Sprintf("date de fin %q illisible", until)
		}
		if t.Before(time.Now().UTC().Truncate(24 * time.Hour)) {
			return promo, fmt.Sprintf("période déjà terminée (jusqu'au %s)", until)
		}
		promo.ValidUntil = until
	}

	// Produits concernés : tous reconnus, sinon on ne devine pas.
	if p.AllProducts && len(p.ProductRefs) > 0 {
		return promo, "à la fois toute la carte et des produits précis"
	}
	if p.AllProducts {
		if promo.Kind == PromotionNewPrice {
			return promo, "un prix fixe sur toute la carte n'a pas de sens : produits concernés non identifiés"
		}
		promo.AllProducts = true
		return promo, ""
	}
	if len(p.ProductRefs) == 0 {
		return promo, "produits concernés non identifiés"
	}
	seenProduct := make(map[string]struct{}, len(p.ProductRefs))
	for _, ref := range p.ProductRefs {
		id, ok := productIDByRef[ref]
		if !ok || id == "" {
			return promo, fmt.Sprintf("produit concerné %q introuvable parmi les produits lus sur cette photo", ref)
		}
		if _, dup := seenProduct[id]; !dup {
			seenProduct[id] = struct{}{}
			promo.ProductExternalIDs = append(promo.ProductExternalIDs, id)
		}
	}
	return promo, ""
}

// ---------------------------------------------------------------------------
// Description lisible (preview)
// ---------------------------------------------------------------------------

var promotionDayNames = [...]string{"", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche"}

// DescribePromotion écrit la promotion en une phrase pour la relecture : ce
// que le restaurateur lit est ce qui sera réellement créé, pas la phrase de la
// carte. names donne le nom des produits ciblés, par identifiant externe.
func DescribePromotion(p CanonicalPromotion, names map[string]string) string {
	var effect string
	switch p.Kind {
	case PromotionNewPrice:
		effect = fmt.Sprintf("prix à %s €", formatCents(p.NewPriceCents))
	case PromotionPercentOff:
		effect = fmt.Sprintf("−%d %%", p.Percent)
	case PromotionAmountOff:
		effect = fmt.Sprintf("−%s €", formatCents(p.AmountCents))
	case PromotionNthFree:
		effect = fmt.Sprintf("%d acheté(s), le suivant offert", p.BuyQuantity)
	}

	parts := []string{effect}
	if p.Kind != PromotionNthFree {
		if p.MaxUnits == 0 {
			parts = append(parts, "sur chaque article concerné")
		} else {
			parts = append(parts, fmt.Sprintf("sur %d article(s) par commande", p.MaxUnits))
		}
	}

	if len(p.OrderTypes) == 0 {
		parts = append(parts, "tous les modes de commande")
	} else {
		labels := make([]string, 0, len(p.OrderTypes))
		for _, orderType := range p.OrderTypes {
			switch orderType {
			case orderTypeIn:
				labels = append(labels, "sur place")
			case orderTypeTakeAway:
				labels = append(labels, "à emporter")
			case orderTypeDelivery:
				labels = append(labels, "en livraison")
			}
		}
		parts = append(parts, strings.Join(labels, ", "))
	}

	if len(p.Days) == 0 {
		parts = append(parts, "tous les jours")
	} else {
		days := make([]string, 0, len(p.Days))
		for _, day := range p.Days {
			days = append(days, promotionDayNames[day])
		}
		parts = append(parts, strings.Join(days, ", "))
	}
	if p.FromTime != "" {
		parts = append(parts, fmt.Sprintf("de %s à %s", p.FromTime, p.ToTime))
	}
	if p.ValidUntil != "" {
		parts = append(parts, fmt.Sprintf("jusqu'au %s", p.ValidUntil))
	}
	if p.MinOrderCents > 0 {
		parts = append(parts, fmt.Sprintf("dès %s € d'achat", formatCents(p.MinOrderCents)))
	}

	if p.AllProducts {
		parts = append(parts, "toute la carte")
	} else {
		labels := make([]string, 0, len(p.ProductExternalIDs))
		for _, id := range p.ProductExternalIDs {
			if name := names[id]; name != "" {
				labels = append(labels, name)
			}
		}
		parts = append(parts, "produits : "+strings.Join(labels, ", "))
	}
	return strings.Join(parts, " · ")
}

// newPriceNotLower dit si un prix fixe ne baisserait pas le prix du produit
// sur l'un des canaux de la promotion : le moteur appliquerait quand même ce
// prix, donc le client paierait plus cher (docs/promotions-moteur-a-fiabiliser.md, P2).
func newPriceNotLower(newPrice int, orderTypes []string, in, takeAway, delivery int) bool {
	if len(orderTypes) == 0 {
		orderTypes = []string{orderTypeIn, orderTypeTakeAway, orderTypeDelivery}
	}
	for _, orderType := range orderTypes {
		price := in
		switch orderType {
		case orderTypeTakeAway:
			price = takeAway
		case orderTypeDelivery:
			price = delivery
		}
		if newPrice >= price {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

// PreviewPromotion est une promotion proposée à la relecture.
type PreviewPromotion struct {
	ExternalID  string        `json:"external_id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Kind        PromotionKind `json:"kind"`
	// Summary : la promotion telle qu'elle sera créée, en une phrase.
	Summary string `json:"summary"`
	// Action : create | already_exists (une promotion du même nom existe déjà).
	Action             string   `json:"action"`
	AllProducts        bool     `json:"all_products,omitempty"`
	ProductExternalIDs []string `json:"product_external_ids,omitempty"`
	// Notes : limites à connaître avant d'activer la promotion.
	Notes       []string `json:"notes,omitempty"`
	SourceText  string   `json:"source_text,omitempty"`
	SourcePhoto int      `json:"source_photo,omitempty"`
	Excluded    bool     `json:"excluded,omitempty"`
}

// ExistingPromotion est une promotion active du marchand ; son nom sert à ne
// pas recréer celle qu'un import précédent (ou le marchand) a déjà posée.
type ExistingPromotion struct {
	Name string
}

func promotionNameKey(name string) string { return importutil.NormalizeLabel(name) }

func (b *previewBuilder) buildPromotions() {
	if len(b.imp.Promotions) == 0 {
		return
	}

	existing := make(map[string]struct{}, len(b.lk.ExistingPromotions))
	for _, e := range b.lk.ExistingPromotions {
		existing[promotionNameKey(e.Name)] = struct{}{}
	}
	names := make(map[string]string, len(b.imp.Products))
	products := make(map[string]CanonicalProduct, len(b.imp.Products))
	for _, p := range b.imp.Products {
		names[p.ExternalID] = p.Name
		products[p.ExternalID] = p
	}

	for _, promo := range b.imp.Promotions {
		entry := PreviewPromotion{
			ExternalID:         promo.ExternalID,
			Name:               promo.Name,
			Description:        promo.Description,
			Kind:               promo.Kind,
			Summary:            DescribePromotion(promo, names),
			Action:             ActionCreate,
			AllProducts:        promo.AllProducts,
			ProductExternalIDs: promo.ProductExternalIDs,
			Notes:              append([]string(nil), promo.Notes...),
			SourceText:         promo.SourceText,
			SourcePhoto:        promo.SourcePhoto,
			Excluded:           b.res.Decisions.ExcludedPromotions[promo.ExternalID],
		}
		if _, found := existing[promotionNameKey(promo.Name)]; found {
			entry.Action = ActionAlreadyExists
			entry.Notes = append(entry.Notes, "Une promotion de ce nom existe déjà : elle ne sera pas recréée.")
		} else {
			b.res.Summary.PromotionsToCreate++
		}
		if promo.Kind == PromotionNewPrice {
			for _, id := range promo.ProductExternalIDs {
				p := products[id]
				if newPriceNotLower(promo.NewPriceCents, promo.OrderTypes, p.PriceIn, p.PriceTakeAway, p.PriceDelivery) {
					entry.Notes = append(entry.Notes, fmt.Sprintf(
						"Le prix promotionnel n'est pas inférieur au prix de « %s » : ce produit sera retiré de la promotion.", p.Name))
				}
			}
		}
		b.res.Promotions = append(b.res.Promotions, entry)
	}
}

// ---------------------------------------------------------------------------
// Plan de commit
// ---------------------------------------------------------------------------

// PlannedPromotionSchedule est un créneau hebdomadaire de la promotion.
type PlannedPromotionSchedule struct {
	DayOfWeek int // 1 = lundi … 7 = dimanche
	From, To  string
}

// PlannedPromotion est une promotion entièrement résolue, prête à devenir une
// ligne de discounts. Elle est toujours créée inactive.
type PlannedPromotion struct {
	ExternalID  string
	Name        string
	Description string

	DiscountUnit  string // PERCENTAGE | CURRENCY | NEWPRICE
	DiscountValue int    // % ou centimes ; 0 pour NEWPRICE
	// NewPriceCents est le prix fixe de chaque produit ciblé (NEWPRICE).
	NewPriceCents int

	OrderTypes         []string
	DiscountedQuantity int
	MinOrderValue      int
	MinOrderUnit       string // "" = aucun minimum
	ValidUntil         string
	Schedules          []PlannedPromotionSchedule

	AllProducts        bool
	ProductExternalIDs []string

	// SkipReason non vide : la promotion n'est pas créée (écartée en
	// relecture, déjà existante, plus aucun produit ciblé…).
	SkipReason string
	// Notes : produits retirés de la promotion, limites du moteur.
	Notes []string
}

// Materializable dit si la promotion doit être écrite.
func (p PlannedPromotion) Materializable() bool { return p.SkipReason == "" }

// buildPromotions planifie les promotions de la porte IA, après les produits :
// les produits ciblés qui ne seront pas créés (écartés, déjà importés,
// homonymes ignorés) sortent de la promotion, et une promotion sans produit
// n'est pas créée.
func (b *commitPlanner) buildPromotions() {
	existing := make(map[string]struct{}, len(b.lk.ExistingPromotions))
	for _, e := range b.lk.ExistingPromotions {
		existing[promotionNameKey(e.Name)] = struct{}{}
	}
	products := make(map[string]PlannedProduct, len(b.plan.Products))
	for _, p := range b.plan.Products {
		products[p.ExternalID] = p
	}

	for _, promo := range b.imp.Promotions {
		planned := PlannedPromotion{
			ExternalID:         promo.ExternalID,
			Name:               promo.Name,
			Description:        promo.Description,
			NewPriceCents:      promo.NewPriceCents,
			OrderTypes:         promo.OrderTypes,
			ValidUntil:         promo.ValidUntil,
			AllProducts:        promo.AllProducts,
			Notes:              append([]string(nil), promo.Notes...),
			DiscountedQuantity: promo.MaxUnits,
		}
		if planned.DiscountedQuantity == 0 {
			planned.DiscountedQuantity = aiPromotionAllUnits
		}
		if promo.MinOrderCents > 0 {
			planned.MinOrderValue, planned.MinOrderUnit = promo.MinOrderCents, minOrderUnitCurrency
		}

		switch promo.Kind {
		case PromotionNewPrice:
			planned.DiscountUnit = discountUnitNewPrice
		case PromotionPercentOff:
			planned.DiscountUnit, planned.DiscountValue = discountUnitPercentage, promo.Percent
		case PromotionAmountOff:
			planned.DiscountUnit, planned.DiscountValue = discountUnitCurrency, promo.AmountCents
		case PromotionNthFree:
			// N achetés, le suivant offert : une unité à 100 % dès N+1 articles.
			planned.DiscountUnit, planned.DiscountValue = discountUnitPercentage, 100
			planned.DiscountedQuantity = 1
			planned.MinOrderValue, planned.MinOrderUnit = promo.BuyQuantity+1, minOrderUnitQuantity
		}

		planned.Schedules = promotionSchedules(promo)

		switch {
		case b.decisions.ExcludedPromotions[promo.ExternalID]:
			planned.SkipReason = "écartée à la relecture"
		default:
			if _, found := existing[promotionNameKey(promo.Name)]; found {
				planned.SkipReason = "une promotion du même nom existe déjà"
			}
		}

		if planned.SkipReason == "" && !promo.AllProducts {
			for _, id := range promo.ProductExternalIDs {
				p, ok := products[id]
				if !ok || !p.Materializable() || p.IsGroup {
					continue
				}
				if promo.Kind == PromotionNewPrice &&
					newPriceNotLower(promo.NewPriceCents, promo.OrderTypes, p.PriceIn, p.PriceTakeAway, p.PriceDelivery) {
					planned.Notes = append(planned.Notes, fmt.Sprintf(
						"« %s » retiré de la promotion : le prix promotionnel n'est pas inférieur à son prix.", p.Name))
					continue
				}
				planned.ProductExternalIDs = append(planned.ProductExternalIDs, id)
			}
			if len(planned.ProductExternalIDs) == 0 {
				planned.SkipReason = "aucun des produits ciblés ne sera créé par cet import"
			}
		}

		b.plan.Promotions = append(b.plan.Promotions, planned)
	}
}

// promotionSchedules traduit jours et heures en créneaux hebdomadaires. Des
// heures sans jour valent tous les jours ; des jours sans heures, la journée
// entière (00:00 → 00:00, « jusqu'à minuit »).
func promotionSchedules(p CanonicalPromotion) []PlannedPromotionSchedule {
	if len(p.Days) == 0 && p.FromTime == "" {
		return nil
	}
	days := p.Days
	if len(days) == 0 {
		days = []int{1, 2, 3, 4, 5, 6, 7}
	}
	from, to := p.FromTime, p.ToTime
	if from == "" {
		from, to = "00:00", "00:00"
	}
	out := make([]PlannedPromotionSchedule, 0, len(days))
	for _, day := range days {
		out = append(out, PlannedPromotionSchedule{DayOfWeek: day, From: from, To: to})
	}
	return out
}
