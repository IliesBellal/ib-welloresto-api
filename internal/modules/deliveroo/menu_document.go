package deliveroo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/modules/marketplacecatalog"
)

// Document de menu Deliveroo : corps du PUT /menu/v1/brands/{brand}/menus/{id}
// et contenu du fichier déposé sur l'URL présignée de la V3 (même schéma, voir
// docs/deliveroo-menu-sync.md).
//
// Identifiants :
//   - article = identifiant du produit WelloResto, PLU identique. C'est ce
//     qu'utilisent l'alignement des disponibilités (availability.go) et l'import des
//     commandes (pos_item_id, via integration_deliveroo_products_mapping) ;
//   - option = "opt-<id option>", PLU identique (namespace distinct des
//     produits : les deux identifiants sont des entiers) ;
//   - groupe d'options = "mod-<id attribut>", catégorie = "cat-<id>".

// PublishedMenuID est l'identifiant du menu publié pour chaque site.
const PublishedMenuID = "main"

const (
	mealtimeID          = "main"
	optionItemPrefix    = "opt-"
	modifierPrefix      = "mod-"
	categoryPrefix      = "cat-"
	maxItemNameRunes    = 120
	maxDescriptionRunes = 500
	maxCategories       = 100
)

// menuLocales : langues des textes traduits. "en" est la seule clé déjà
// acceptée par le sandbox (scénarios 4 et suivants) ; voir la doc pour "fr".
var menuLocales = []string{"en"}

// MenuDocument est le document envoyé à Deliveroo.
type MenuDocument struct {
	Name    string      `json:"name"`
	SiteIDs []string    `json:"site_ids"`
	Menu    MenuContent `json:"menu"`
}

type MenuContent struct {
	Mealtimes  []Mealtime     `json:"mealtimes"`
	Categories []MenuCategory `json:"categories"`
	Items      []MenuItem     `json:"items"`
	Modifiers  []MenuModifier `json:"modifiers,omitempty"`
}

type Mealtime struct {
	ID          string            `json:"id"`
	Name        map[string]string `json:"name"`
	Description map[string]string `json:"description"`
	Image       MenuImage         `json:"image"`
	CategoryIDs []string          `json:"category_ids"`
	Schedule    []ScheduleEntry   `json:"schedule"`
}

type MenuImage struct {
	URL string `json:"url"`
}

type ScheduleEntry struct {
	DayOfWeek   int          `json:"day_of_week"`
	TimePeriods []TimePeriod `json:"time_periods"`
}

type TimePeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type MenuCategory struct {
	ID      string            `json:"id"`
	Name    map[string]string `json:"name"`
	ItemIDs []string          `json:"item_ids"`
}

type MenuItem struct {
	ID              string            `json:"id"`
	Name            map[string]string `json:"name"`
	OperationalName string            `json:"operational_name"`
	Description     map[string]string `json:"description,omitempty"`
	PLU             string            `json:"plu"`
	Type            string            `json:"type"`
	PriceInfo       PriceInfo         `json:"price_info"`
	TaxRate         string            `json:"tax_rate"`
	ContainsAlcohol bool              `json:"contains_alcohol"`
	Image           *MenuImage        `json:"image,omitempty"`
	ModifierIDs     []string          `json:"modifier_ids,omitempty"`
}

type PriceInfo struct {
	Price int64 `json:"price"`
}

type MenuModifier struct {
	ID           string            `json:"id"`
	Name         map[string]string `json:"name"`
	MinSelection int               `json:"min_selection"`
	MaxSelection int               `json:"max_selection"`
	Repeatable   bool              `json:"repeatable"`
	ItemIDs      []string          `json:"item_ids"`
}

// DocumentOptions complète le catalogue.
type DocumentOptions struct {
	SiteID        string
	MenuName      string // texte du mealtime (nom de l'établissement)
	CoverImageURL string // couverture 1920x1080 du mealtime
	// Revision, si non vide, est ajoutée à la description du mealtime. Elle ne
	// sert qu'aux scénarios de certification : elle garantit un document
	// différent du précédent, donc un webhook de résultat (un menu identique
	// renvoie MATCH_EXISTING_MENU, sans webhook).
	Revision string
}

// BuiltDocument est le document et ce qu'il faut en garder.
type BuiltDocument struct {
	Document *MenuDocument
	// ProductItemIDs : identifiants des articles produits publiés, dans
	// l'ordre d'affichage (journal, alignement des disponibilités).
	ProductItemIDs []string
	// OptionItems : identifiant d'article Deliveroo -> identifiant d'option
	// WelloResto (table integration_deliveroo_options_mapping).
	OptionItems map[string]string
	// ProductNames : identifiant produit -> nom publié
	// (integration_deliveroo_products_mapping.item_name).
	ProductNames map[string]string
	Fingerprint  string
	Warnings     []string
}

// BuildMenuDocument traduit un catalogue en document Deliveroo.
func BuildMenuDocument(cat *marketplacecatalog.Catalog, opts DocumentOptions) (*BuiltDocument, error) {
	if cat == nil || len(cat.Products) == 0 {
		return nil, marketplacecatalog.ErrNothingToPublish
	}
	if strings.TrimSpace(opts.SiteID) == "" {
		return nil, fmt.Errorf("deliveroo menu: site_id manquant")
	}
	if strings.TrimSpace(opts.CoverImageURL) == "" {
		return nil, fmt.Errorf("deliveroo menu: image de couverture manquante")
	}

	out := &BuiltDocument{OptionItems: map[string]string{}, ProductNames: map[string]string{}}
	content := MenuContent{}
	modifiers := map[string]bool{}
	optionItems := map[string]bool{}

	for _, c := range cat.Categories {
		mc := MenuCategory{ID: categoryPrefix + c.ID, Name: tr(c.Name)}
		for _, pid := range c.ProductIDs {
			p := cat.Products[pid]
			if p == nil {
				continue
			}
			name := helpers.TruncateRunes(p.Name, maxItemNameRunes)
			if len([]rune(name)) < 2 {
				out.Warnings = append(out.Warnings, fmt.Sprintf("produit %s ignoré : nom trop court (2 caractères minimum)", p.ID))
				continue
			}
			taxRate := formatTaxRate(p.TaxRate)
			item := MenuItem{
				ID:              p.ID,
				Name:            tr(name),
				OperationalName: name,
				PLU:             p.ID,
				Type:            "ITEM",
				PriceInfo:       PriceInfo{Price: p.PriceCents},
				TaxRate:         taxRate,
				ContainsAlcohol: p.ContainsAlcohol,
			}
			if d := helpers.TruncateRunes(p.Description, maxDescriptionRunes); d != "" {
				item.Description = tr(d)
			}
			if p.ImageURL != "" {
				item.Image = &MenuImage{URL: p.ImageURL}
			}

			for _, g := range p.OptionGroups {
				modID := modifierPrefix + g.ID
				item.ModifierIDs = append(item.ModifierIDs, modID)
				if modifiers[modID] {
					continue
				}
				modifiers[modID] = true
				mod := MenuModifier{
					ID:           modID,
					Name:         tr(nonEmpty(g.Title, "Options")),
					MinSelection: g.Min,
					MaxSelection: g.Max,
					Repeatable:   g.Repeatable,
				}
				for _, o := range g.Options {
					oid := optionItemPrefix + o.ID
					mod.ItemIDs = append(mod.ItemIDs, oid)
					if optionItems[oid] {
						continue
					}
					optionItems[oid] = true
					out.OptionItems[oid] = o.ID
					optName := helpers.TruncateRunes(nonEmpty(o.Title, "Option"), maxItemNameRunes)
					if len([]rune(optName)) < 2 {
						optName = optName + "."
					}
					// Une option partagée entre plusieurs produits prend la TVA
					// du premier produit qui l'utilise.
					content.Items = append(content.Items, MenuItem{
						ID:              oid,
						Name:            tr(optName),
						OperationalName: optName,
						PLU:             oid,
						Type:            "CHOICE",
						PriceInfo:       PriceInfo{Price: o.PriceCents},
						TaxRate:         taxRate,
						ContainsAlcohol: false,
					})
				}
				content.Modifiers = append(content.Modifiers, mod)
			}

			content.Items = append(content.Items, item)
			mc.ItemIDs = append(mc.ItemIDs, p.ID)
			out.ProductItemIDs = append(out.ProductItemIDs, p.ID)
			out.ProductNames[p.ID] = name
		}
		if len(mc.ItemIDs) > 0 {
			content.Categories = append(content.Categories, mc)
		}
	}

	if len(content.Categories) == 0 {
		return nil, marketplacecatalog.ErrNothingToPublish
	}
	if len(content.Categories) > maxCategories {
		return nil, fmt.Errorf("deliveroo menu: %d catégories, Deliveroo en accepte %d au maximum", len(content.Categories), maxCategories)
	}

	categoryIDs := make([]string, 0, len(content.Categories))
	for _, c := range content.Categories {
		categoryIDs = append(categoryIDs, c.ID)
	}
	description := nonEmpty(opts.MenuName, "Menu")
	if opts.Revision != "" {
		description += " · " + opts.Revision
	}
	content.Mealtimes = []Mealtime{{
		ID:          mealtimeID,
		Name:        tr(nonEmpty(opts.MenuName, "Menu")),
		Description: tr(description),
		Image:       MenuImage{URL: opts.CoverImageURL},
		CategoryIDs: categoryIDs,
		Schedule:    allWeekSchedule(),
	}}

	out.Document = &MenuDocument{
		Name:    nonEmpty(opts.MenuName, "Menu"),
		SiteIDs: []string{opts.SiteID},
		Menu:    content,
	}
	fp, err := fingerprint(out.Document)
	if err != nil {
		return nil, err
	}
	out.Fingerprint = fp
	return out, nil
}

// allWeekSchedule : un seul mealtime ouvert 7 jours sur 7. Les heures
// d'ouverture réelles se règlent sur le site Deliveroo. Format HH:MM:SS, celui
// accepté par le sandbox (la doc mentionne aussi HH:MM).
func allWeekSchedule() []ScheduleEntry {
	out := make([]ScheduleEntry, 0, 7)
	for d := 0; d <= 6; d++ {
		out = append(out, ScheduleEntry{DayOfWeek: d, TimePeriods: []TimePeriod{{Start: "00:00:00", End: "23:59:59"}}})
	}
	return out
}

// formatTaxRate : 10 -> "10", 5.5 -> "5.5".
func formatTaxRate(rate float64) string {
	return strconv.FormatFloat(rate, 'f', -1, 64)
}

func tr(s string) map[string]string {
	m := make(map[string]string, len(menuLocales))
	for _, l := range menuLocales {
		m[l] = s
	}
	return m
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}

// fingerprint : empreinte du document, pour ne pas republier un menu
// identique au dernier publié avec succès.
func fingerprint(doc *MenuDocument) (string, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("deliveroo menu: empreinte: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// UnavailabilityStatus traduit un état commun en statut Deliveroo
// (POST item_unavailabilities).
func UnavailabilityStatus(a marketplacecatalog.Availability) string {
	switch a {
	case marketplacecatalog.Available:
		return "available"
	case marketplacecatalog.SoldOut:
		return "unavailable"
	default:
		return "hidden"
	}
}
