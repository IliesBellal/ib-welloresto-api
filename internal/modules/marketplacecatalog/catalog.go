// Package marketplacecatalog calcule, une seule fois et pour toutes les
// plateformes de livraison (Deliveroo aujourd'hui, Uber Eats ensuite), ce
// qu'un établissement publie : quels produits, dans quelles catégories, à quel
// prix, avec quelle TVA, quelles options et dans quel état de disponibilité.
//
// Chaque plateforme n'a ensuite qu'à traduire ce catalogue dans son propre
// format (voir internal/modules/deliveroo/menu_document.go). Le package ne
// dépend que de internal/models : aucune requête, aucun appel réseau, tout est
// testable sur des données en mémoire.
//
// Documentation : docs/deliveroo-menu-sync.md.
package marketplacecatalog

import (
	"errors"
	"strings"

	"welloresto-api/internal/models"
)

// Platform identifie une plateforme de livraison. La valeur est aussi celle de
// la colonne marketplace_menu_syncs.platform.
type Platform string

const (
	Deliveroo Platform = "deliveroo"
	UberEats  Platform = "uber_eats"
)

// Availability est l'état d'un produit publié, commun à toutes les plateformes.
type Availability string

const (
	// Available : commandable.
	Available Availability = "available"
	// SoldOut : affiché mais épuisé (products.status = out_of_stock).
	SoldOut Availability = "sold_out"
	// Hidden : au menu de la plateforme mais invisible pour le client
	// (products.status = not_available ou valeur héritée '0').
	Hidden Availability = "hidden"
)

// ErrNothingToPublish : aucun produit n'est coché pour la plateforme.
var ErrNothingToPublish = errors.New("marketplacecatalog: aucun produit coché pour cette plateforme")

// Catalog est le contenu publié sur une plateforme.
type Catalog struct {
	Categories []Category
	// Products est indexé par identifiant produit ; l'ordre d'affichage est
	// celui de Category.ProductIDs.
	Products map[string]*Product
}

// Category est une catégorie publiée (catégorie marketing ou de caisse).
type Category struct {
	ID         string
	Name       string
	ProductIDs []string
}

// Product est un produit publié.
type Product struct {
	ID          string
	Name        string
	Description string
	ImageURL    string
	// PriceCents : prix de la plateforme s'il est renseigné, sinon prix de
	// livraison, sinon prix de base.
	PriceCents int64
	// TaxRate : taux de TVA livraison du produit, en pourcentage (10, 5.5, 20).
	TaxRate         float64
	ContainsAlcohol bool
	Availability    Availability
	OptionGroups    []OptionGroup
}

// OptionGroup est un groupe d'options (configurable_attributes).
type OptionGroup struct {
	ID         string
	Title      string
	Min        int
	Max        int
	Repeatable bool
	Options    []Option
}

// Option est un choix d'un groupe (configurable_attribute_options).
//
// TODO(options) : pas d'état de disponibilité (rupture d'une option ou de son
// composant) — voir le TODO(options) de internal/modules/deliveroo/availability.go.
type Option struct {
	ID         string
	Title      string
	PriceCents int64
}

// ProductExtras porte les colonnes produit que le chargement du menu commun ne
// lit pas (et qu'il ne doit pas lire : une migration non appliquée casserait
// alors les menus du POS, de ScanNOrder et de la borne).
type ProductExtras struct {
	// PlatformPriceCents : price_deliveroo ou price_uber_eats ; nil ou 0 = non renseigné.
	PlatformPriceCents *int64
	ContainsAlcohol    bool
}

// Build construit le catalogue d'une plateforme à partir du menu de
// l'établissement. extras est indexé par identifiant produit et peut être vide.
//
// Règles :
//   - seul un produit coché pour la plateforme (sync_deliveroo / sync_uber_eats)
//     est publié ; le menu chargé exclut déjà removed_from_menu et les
//     produits supprimés (enabled = false) ;
//   - un produit groupe n'est jamais publié, ses sous-produits le sont à sa
//     place (même règle que la caisse, ScanNOrder et la borne) ;
//   - available_delivery n'est pas lu : la livraison propre et chaque
//     plateforme ont leur propre case.
func Build(menu *models.MenuResponse, platform Platform, extras map[string]ProductExtras) (*Catalog, error) {
	if menu == nil {
		return nil, errors.New("marketplacecatalog: menu absent")
	}

	cat := &Catalog{Products: map[string]*Product{}}

	for _, pc := range menu.ProductsTypes {
		catID := categoryID(pc)
		if catID == "" {
			continue
		}
		category := Category{ID: catID, Name: categoryName(pc)}

		for _, p := range flatten(pc.Products) {
			if p.ProductID == "" || !syncedTo(p, platform) {
				continue
			}
			if _, seen := cat.Products[p.ProductID]; seen {
				// Un produit n'apparaît qu'une fois : première catégorie rencontrée.
				continue
			}
			ex := extras[p.ProductID]
			product := &Product{
				ID:              p.ProductID,
				Name:            strings.TrimSpace(p.Name),
				Description:     strings.TrimSpace(deref(p.Description)),
				ImageURL:        strings.TrimSpace(deref(p.ImageURL)),
				PriceCents:      price(p, ex),
				TaxRate:         derefFloat(p.TVADelivery),
				ContainsAlcohol: ex.ContainsAlcohol,
				Availability:    AvailabilityFromStatus(p.Status),
				OptionGroups:    optionGroups(p.Configuration),
			}
			cat.Products[p.ProductID] = product
			category.ProductIDs = append(category.ProductIDs, p.ProductID)
		}

		if len(category.ProductIDs) > 0 {
			cat.Categories = append(cat.Categories, category)
		}
	}

	if len(cat.Products) == 0 {
		return nil, ErrNothingToPublish
	}
	return cat, nil
}

// AvailabilityFromStatus traduit products.status (texte libre, valeurs
// héritées comprises).
//
// Une valeur inconnue ou vide masque le produit : la caisse et ScanNOrder la
// considèrent déjà comme non commandable. La disponibilité de la catégorie de
// caisse (productcateg.available) n'est volontairement pas lue : le chargement
// du menu commun ne la renseigne pas (ProductCategory.Available vaut false
// pour toutes les catégories de caisse).
func AvailabilityFromStatus(status string) Availability {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "available", "1", "true":
		return Available
	case "out_of_stock":
		return SoldOut
	default: // not_available, '0', 'false', inconnu
		return Hidden
	}
}

// Availabilities renvoie l'état voulu de chaque produit publié, limité aux
// identifiants de onlyIDs quand il est non vide (produits réellement présents
// dans le menu en ligne : un identifiant inconnu fait échouer tout l'envoi
// chez Deliveroo).
func (c *Catalog) Availabilities(onlyIDs []string) map[string]Availability {
	out := make(map[string]Availability, len(c.Products))
	if len(onlyIDs) == 0 {
		for id, p := range c.Products {
			out[id] = p.Availability
		}
		return out
	}
	for _, id := range onlyIDs {
		if p, ok := c.Products[id]; ok {
			out[id] = p.Availability
		}
	}
	return out
}

func syncedTo(p models.ProductEntry, platform Platform) bool {
	switch platform {
	case Deliveroo:
		return p.SyncDeliveroo != nil && *p.SyncDeliveroo
	case UberEats:
		return p.SyncUberEats != nil && *p.SyncUberEats
	}
	return false
}

// flatten : un produit groupe n'est jamais publié, ses sous-produits prennent
// sa place (ordre d'affichage conservé).
func flatten(products []models.ProductEntry) []models.ProductEntry {
	out := make([]models.ProductEntry, 0, len(products))
	for _, p := range products {
		if p.IsProductGroup == nil || !*p.IsProductGroup {
			out = append(out, p)
		}
		out = append(out, p.SubProducts...)
	}
	return out
}

func price(p models.ProductEntry, ex ProductExtras) int64 {
	if ex.PlatformPriceCents != nil && *ex.PlatformPriceCents > 0 {
		return *ex.PlatformPriceCents
	}
	if p.PriceDelivery != nil && *p.PriceDelivery > 0 {
		return *p.PriceDelivery
	}
	return p.Price
}

func optionGroups(conf models.ConfigurableResponse) []OptionGroup {
	var groups []OptionGroup
	for _, attr := range conf.Attributes {
		if attr.ID == "" || len(attr.Options) == 0 {
			continue
		}
		g := OptionGroup{ID: attr.ID, Title: strings.TrimSpace(attr.Title), Min: attr.MinOptions, Max: attr.MaxOptions}
		for _, o := range attr.Options {
			if o.ID == "" {
				continue
			}
			g.Options = append(g.Options, Option{ID: o.ID, Title: strings.TrimSpace(o.Title), PriceCents: int64(o.ExtraPrice)})
			if o.MaxQuantity > 1 {
				g.Repeatable = true
			}
		}
		if len(g.Options) == 0 {
			continue
		}
		if g.Min < 0 {
			g.Min = 0
		}
		// max_options à 0 (ou incohérent) : aucune limite réelle côté caisse.
		if g.Max <= 0 || g.Max < g.Min {
			g.Max = len(g.Options)
			if g.Repeatable && g.Max < 99 {
				g.Max = 99
			}
		}
		groups = append(groups, g)
	}
	return groups
}

func categoryID(pc models.ProductCategory) string {
	if pc.CategoryID != nil && strings.TrimSpace(*pc.CategoryID) != "" {
		return strings.TrimSpace(*pc.CategoryID)
	}
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(pc.Category), " ", "_"))
}

func categoryName(pc models.ProductCategory) string {
	if n := strings.TrimSpace(pc.CategoryName); n != "" {
		return n
	}
	return strings.TrimSpace(pc.Category)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
