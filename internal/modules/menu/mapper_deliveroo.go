package menu

import (
	"fmt"
	"strings"
	"welloresto-api/internal/models"
)

// ============================================================
// Deliveroo Menu DTOs
// Ref: https://api.developers.deliveroo.com/docs/menu
// ============================================================

// DeliverooMenu is the top-level payload sent to PUT /menu/v1/brands/{brandID}/menus
type DeliverooMenu struct {
	Menus []DeliverooMenuEntry `json:"menus"`
}

type DeliverooMenuEntry struct {
	// A stable identifier for the menu (e.g. "main" or merchant's internal ID)
	ID               string                   `json:"id"`
	Name             string                   `json:"name"`
	FulfillmentModes []string                 `json:"fulfillment_modes"` // "DELIVERY", "PICKUP"
	Categories       []DeliverooCategory      `json:"categories"`
	Items            []DeliverooItem          `json:"items"`
	ModifierGroups   []DeliverooModifierGroup `json:"modifier_groups,omitempty"`
}

type DeliverooCategory struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	ItemIDs []string `json:"item_ids"`
}

type DeliverooItem struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	PriceMoney  DeliverooPriceMoney `json:"price_money"`
	ImageURLs   []string            `json:"image_urls,omitempty"`
	// "AVAILABLE" or "UNAVAILABLE"
	Availability     string   `json:"availability"`
	ModifierGroupIDs []string `json:"modifier_group_ids,omitempty"`
}

type DeliverooPriceMoney struct {
	// Amount in the smallest currency unit (centimes)
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type DeliverooModifierGroup struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	MinPermitted int                 `json:"min_permitted"`
	MaxPermitted int                 `json:"max_permitted"`
	Modifiers    []DeliverooModifier `json:"modifiers"`
}

type DeliverooModifier struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	PriceMoney DeliverooPriceMoney `json:"price_money"`
}

// ============================================================
// ToDeliverooFormat converts the internal MenuResponse to the
// Deliveroo menu push format.
//
// Price mapping: internal prices are already in centimes, so no
// conversion is needed. Deliveroo expects amounts in the smallest
// currency unit (centimes for EUR).
// ============================================================
func ToDeliverooFormat(internal *models.MenuResponse) (*DeliverooMenu, error) {
	if internal == nil {
		return nil, fmt.Errorf("deliveroo mapper: internal menu is nil")
	}
	if len(internal.ProductsTypes) == 0 {
		return nil, fmt.Errorf("deliveroo mapper: menu has no product categories — cannot sync an empty menu")
	}

	var categories []DeliverooCategory
	var items []DeliverooItem
	var modifierGroups []DeliverooModifierGroup

	// Track modifier groups already added to avoid duplicates across products
	addedModifierGroups := map[string]bool{}

	for _, cat := range internal.ProductsTypes {
		catID := modelCategoryID(cat)
		if catID == "" {
			continue
		}

		var itemIDs []string

		for _, product := range cat.Products {
			if product.ProductID == "" {
				continue
			}
			if product.Name == "" {
				return nil, fmt.Errorf("deliveroo mapper: product %q has no name", product.ProductID)
			}

			// Use delivery price when set, fall back to base price.
			// Internal prices are stored in centimes — no conversion needed.
			price := product.Price
			if product.PriceDelivery != nil && *product.PriceDelivery != 0 {
				price = *product.PriceDelivery
			}

			availability := "AVAILABLE"
			if product.Status == "out_of_stock" || (product.AvailableDelivery != nil && !*product.AvailableDelivery) {
				availability = "UNAVAILABLE"
			}

			item := DeliverooItem{
				ID:           product.ProductID,
				Name:         product.Name,
				Description:  derefString(product.Description),
				PriceMoney:   DeliverooPriceMoney{Amount: price, Currency: "EUR"},
				Availability: availability,
			}

			if product.ImageURL != nil && *product.ImageURL != "" {
				item.ImageURLs = []string{*product.ImageURL}
			}

			// Map configurable attributes → modifier groups
			for _, attr := range product.Configuration.Attributes {
				mgID := "mg_" + attr.ID
				item.ModifierGroupIDs = append(item.ModifierGroupIDs, mgID)

				if !addedModifierGroups[mgID] {
					addedModifierGroups[mgID] = true
					mg := DeliverooModifierGroup{
						ID:           mgID,
						Name:         attr.Title,
						MinPermitted: attr.MinOptions,
						MaxPermitted: attr.MaxOptions,
					}
					for _, opt := range attr.Options {
						mg.Modifiers = append(mg.Modifiers, DeliverooModifier{
							ID:   opt.ID,
							Name: opt.Title,
							PriceMoney: DeliverooPriceMoney{
								Amount:   int64(opt.ExtraPrice),
								Currency: "EUR",
							},
						})
					}
					modifierGroups = append(modifierGroups, mg)
				}
			}

			items = append(items, item)
			itemIDs = append(itemIDs, product.ProductID)
		}

		if len(itemIDs) > 0 {
			categories = append(categories, DeliverooCategory{
				ID:      catID,
				Name:    cat.Category,
				ItemIDs: itemIDs,
			})
		}
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("deliveroo mapper: no valid products found to sync")
	}

	entry := DeliverooMenuEntry{
		ID:               "main",
		Name:             "Menu",
		FulfillmentModes: []string{"DELIVERY", "PICKUP"},
		Categories:       categories,
		Items:            items,
		ModifierGroups:   modifierGroups,
	}

	return &DeliverooMenu{Menus: []DeliverooMenuEntry{entry}}, nil
}

// modelCategoryID returns a stable ID for a models.ProductCategory.
// Prefers the explicit CategoryID; falls back to a slug of the category name.
func modelCategoryID(cat models.ProductCategory) string {
	if cat.CategoryID != nil && *cat.CategoryID != "" {
		return *cat.CategoryID
	}
	slug := strings.ToLower(strings.ReplaceAll(cat.Category, " ", "_"))
	return slug
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

