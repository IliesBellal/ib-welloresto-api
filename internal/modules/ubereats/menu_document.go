package ubereats

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/modules/marketplacecatalog"
)

// Document de menu Uber Eats : corps du PUT /v2/eats/stores/{store_id}/menus.
// L'envoi REMPLACE le menu du restaurant ; il est conforme au schéma de la doc
// Uber (docs/ubereats-menu-sync.md) :
//   - article = identifiant du produit WelloResto ; option = « opt_<id option> » ;
//     groupe d'options = « mg_<id attribut> » ; catégorie = « cat-<id> ». Ces
//     identifiants sont ceux que l'import des commandes retrouve dans les tables
//     de correspondance (integration_uber_eats_*_mapping), écrites AVANT l'envoi ;
//   - tax_info.vat_rate_percentage = vraie TVA de livraison du produit ;
//   - suspension_info = produit épuisé (out_of_stock), sans limite de durée ;
//   - un produit masqué (not_available) n'est pas publié ;
//   - menu unique dont les horaires sont ceux d'ouverture de l'établissement.

const (
	// PublishedMenuID : identifiant du menu publié.
	PublishedMenuID = "main"

	optionItemPrefix = "opt_"
	groupPrefix      = "mg_"
	categoryPrefix   = "cat-"

	// uberLocale : langue des textes (clé « translations »).
	uberLocale = "fr_fr"

	// suspendForever : fin de suspension d'un article épuisé « jusqu'à nouvel
	// ordre » — la valeur de l'exemple de la doc Uber (secondes, an 2243). Uber
	// n'a pas d'« indéfini » ; WelloResto remet l'article en vente lui-même.
	suspendForever = int64(8640000000)

	// maxPriceCents : au-delà, Uber Eats refuse le menu entier ; le produit est
	// alors ignoré et signalé dans le journal plutôt que de bloquer la synchro.
	maxPriceCents = 25000

	maxExternalDataRunes = 1024
)

// alcoholicItemsValue : valeur de dish_info.classifications.alcoholic_items
// d'un produit contenant de l'alcool (0 / absent = sans alcool). La doc Uber
// place ce champ à deux endroits ; à confirmer au sandbox (docs/ubereats-menu-sync.md).
const alcoholicItemsValue = 1

var (
	// ErrNoOpeningHours : aucun horaire d'ouverture exploitable. Publier sans
	// horaires ouvrirait l'établissement 24 h sur 24 sur Uber Eats.
	ErrNoOpeningHours = errors.New("ubereats menu: aucun horaire d'ouverture renseigné")
	// ErrNothingPublishable : tous les produits cochés sont masqués ou ignorés.
	ErrNothingPublishable = errors.New("ubereats menu: aucun produit à publier (tous masqués ou ignorés)")
)

// UberMenu est le corps de l'envoi.
type UberMenu struct {
	Menus          []UberMenuEntry     `json:"menus"`
	Categories     []UberCategory      `json:"categories"`
	Items          []UberItem          `json:"items"`
	ModifierGroups []UberModifierGroup `json:"modifier_groups"`
}

type UberMenuEntry struct {
	ID                  string                `json:"id"`
	Title               UberText              `json:"title"`
	CategoryIDs         []string              `json:"category_ids"`
	ServiceAvailability []ServiceAvailability `json:"service_availability"`
}

// UberText : texte multilingue.
type UberText struct {
	Translations map[string]string `json:"translations"`
}

func text(v string) UberText { return UberText{Translations: map[string]string{uberLocale: v}} }

type UberCategory struct {
	ID       string           `json:"id"`
	Title    UberText         `json:"title"`
	Entities []UberEntityLink `json:"entities"`
}

// UberEntityLink référence un article (type ITEM).
type UberEntityLink struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type UberItem struct {
	ID               string              `json:"id"`
	ExternalData     string              `json:"external_data,omitempty"`
	Title            UberText            `json:"title"`
	Description      *UberText           `json:"description,omitempty"`
	ImageURL         string              `json:"image_url,omitempty"`
	PriceInfo        UberPriceInfo       `json:"price_info"`
	TaxInfo          UberTaxInfo         `json:"tax_info"`
	SuspensionInfo   *UberSuspensionInfo `json:"suspension_info,omitempty"`
	ModifierGroupIDs *UberIDList         `json:"modifier_group_ids,omitempty"`
	DishInfo         *UberDishInfo       `json:"dish_info,omitempty"`
}

type UberPriceInfo struct {
	Price int64 `json:"price"`
}

type UberTaxInfo struct {
	// VATRatePercentage : TVA déjà comprise dans le prix (10, 5.5, 20).
	VATRatePercentage float64 `json:"vat_rate_percentage"`
}

type UberSuspensionInfo struct {
	Suspension UberSuspension `json:"suspension"`
}

type UberSuspension struct {
	SuspendUntil int64  `json:"suspend_until"`
	Reason       string `json:"reason,omitempty"`
}

type UberIDList struct {
	IDs []string `json:"ids"`
}

type UberDishInfo struct {
	Classifications *UberClassifications `json:"classifications,omitempty"`
}

type UberClassifications struct {
	AlcoholicItems int `json:"alcoholic_items"`
}

type UberModifierGroup struct {
	ID              string           `json:"id"`
	Title           UberText         `json:"title"`
	QuantityInfo    UberQuantityInfo `json:"quantity_info"`
	ModifierOptions []UberEntityLink `json:"modifier_options"`
}

type UberQuantityInfo struct {
	Quantity UberQuantity `json:"quantity"`
}

type UberQuantity struct {
	MinPermitted int `json:"min_permitted"`
	MaxPermitted int `json:"max_permitted"`
}

// BuiltMenu : le document et ce dont la publication a besoin autour.
type BuiltMenu struct {
	Menu *UberMenu
	// Fingerprint : empreinte du menu SANS les suspensions (un changement de
	// stock passe par la mise à jour d'article, pas par un nouvel envoi).
	Fingerprint string
	// ProductItemIDs : produits publiés (disponibles ou épuisés), triés.
	ProductItemIDs []string
	// States : état envoyé de chaque produit publié (available / sold_out).
	States map[string]marketplacecatalog.Availability
	// ProductNames : identifiant produit → nom (table de correspondance).
	ProductNames map[string]string
	// OptionItems : « opt_<id> » → identifiant d'option WelloResto.
	OptionItems map[string]string
	// GroupItems : « mg_<id> » → identifiant d'attribut WelloResto.
	GroupItems map[string]string
	Warnings   []string
}

// BuildMenu traduit le catalogue commun en document Uber Eats. hours vide
// refuse la publication (ErrNoOpeningHours).
func BuildMenu(cat *marketplacecatalog.Catalog, menuTitle string, hours []ServiceAvailability) (*BuiltMenu, error) {
	if len(hours) == 0 {
		return nil, ErrNoOpeningHours
	}
	if strings.TrimSpace(menuTitle) == "" {
		menuTitle = "Menu"
	}

	out := &BuiltMenu{
		Menu:         &UberMenu{Items: []UberItem{}, Categories: []UberCategory{}, ModifierGroups: []UberModifierGroup{}},
		States:       map[string]marketplacecatalog.Availability{},
		ProductNames: map[string]string{},
		OptionItems:  map[string]string{},
		GroupItems:   map[string]string{},
	}
	seenGroup := map[string]bool{}
	var categoryIDs []string

	for _, c := range cat.Categories {
		uc := UberCategory{ID: categoryPrefix + c.ID, Title: text(c.Name), Entities: []UberEntityLink{}}
		for _, pid := range c.ProductIDs {
			p := cat.Products[pid]
			if p == nil || p.Availability == marketplacecatalog.Hidden {
				continue // masqué : absent du menu publié
			}
			if p.PriceCents < 0 || p.PriceCents > maxPriceCents {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"« %s » n'est pas publié : son prix (%.2f €) dépasse la limite d'Uber Eats (250 €).", p.Name, float64(p.PriceCents)/100))
				continue
			}
			if p.TaxRate <= 0 {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"« %s » : la TVA de livraison n'est pas renseignée, elle est publiée à 0 %%.", p.Name))
			}
			if strings.TrimSpace(p.Name) == "" {
				out.Warnings = append(out.Warnings, "Un produit sans nom n'est pas publié (identifiant "+p.ID+").")
				continue
			}

			item := UberItem{
				ID:           p.ID,
				ExternalData: helpers.TruncateRunes(p.ID, maxExternalDataRunes),
				Title:        text(p.Name),
				ImageURL:     p.ImageURL,
				PriceInfo:    UberPriceInfo{Price: p.PriceCents},
				TaxInfo:      UberTaxInfo{VATRatePercentage: p.TaxRate},
			}
			if p.Description != "" {
				d := text(p.Description)
				item.Description = &d
			}
			if p.ContainsAlcohol {
				item.DishInfo = &UberDishInfo{Classifications: &UberClassifications{AlcoholicItems: alcoholicItemsValue}}
			}
			if p.Availability == marketplacecatalog.SoldOut {
				item.SuspensionInfo = &UberSuspensionInfo{Suspension: UberSuspension{SuspendUntil: suspendForever, Reason: "Épuisé"}}
			}

			for _, g := range p.OptionGroups {
				gid := groupPrefix + g.ID
				if item.ModifierGroupIDs == nil {
					item.ModifierGroupIDs = &UberIDList{}
				}
				item.ModifierGroupIDs.IDs = append(item.ModifierGroupIDs.IDs, gid)
				if seenGroup[gid] {
					continue
				}
				seenGroup[gid] = true
				out.GroupItems[gid] = g.ID

				mg := UberModifierGroup{
					ID:              gid,
					Title:           text(g.Title),
					QuantityInfo:    UberQuantityInfo{Quantity: UberQuantity{MinPermitted: g.Min, MaxPermitted: g.Max}},
					ModifierOptions: []UberEntityLink{},
				}
				for _, o := range g.Options {
					oid := optionItemPrefix + o.ID
					price := o.PriceCents
					if price < 0 {
						price = 0
					}
					out.Menu.Items = append(out.Menu.Items, UberItem{
						ID:           oid,
						ExternalData: helpers.TruncateRunes(oid, maxExternalDataRunes),
						Title:        text(o.Title),
						PriceInfo:    UberPriceInfo{Price: price},
						TaxInfo:      UberTaxInfo{VATRatePercentage: p.TaxRate},
					})
					out.OptionItems[oid] = o.ID
					mg.ModifierOptions = append(mg.ModifierOptions, UberEntityLink{ID: oid, Type: "ITEM"})
				}
				out.Menu.ModifierGroups = append(out.Menu.ModifierGroups, mg)
			}

			out.Menu.Items = append(out.Menu.Items, item)
			uc.Entities = append(uc.Entities, UberEntityLink{ID: p.ID, Type: "ITEM"})
			out.ProductItemIDs = append(out.ProductItemIDs, p.ID)
			out.States[p.ID] = stateOf(p.Availability)
			out.ProductNames[p.ID] = p.Name
		}
		if len(uc.Entities) > 0 {
			out.Menu.Categories = append(out.Menu.Categories, uc)
			categoryIDs = append(categoryIDs, uc.ID)
		}
	}

	if len(out.ProductItemIDs) == 0 {
		return nil, ErrNothingPublishable
	}
	sort.Strings(out.ProductItemIDs)

	out.Menu.Menus = []UberMenuEntry{{
		ID:                  PublishedMenuID,
		Title:               text(menuTitle),
		CategoryIDs:         categoryIDs,
		ServiceAvailability: hours,
	}}

	fp, err := fingerprintWithoutSuspensions(out.Menu)
	if err != nil {
		return nil, err
	}
	out.Fingerprint = fp
	return out, nil
}

// stateOf : état envoyé d'un produit publié.
func stateOf(a marketplacecatalog.Availability) marketplacecatalog.Availability {
	if a == marketplacecatalog.SoldOut {
		return marketplacecatalog.SoldOut
	}
	return marketplacecatalog.Available
}

// fingerprintWithoutSuspensions : empreinte du menu hors suspensions, pour ne
// pas renvoyer tout le menu à chaque rupture de stock.
func fingerprintWithoutSuspensions(menu *UberMenu) (string, error) {
	clone := *menu
	clone.Items = make([]UberItem, len(menu.Items))
	copy(clone.Items, menu.Items)
	for i := range clone.Items {
		clone.Items[i].SuspensionInfo = nil
	}
	raw, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
