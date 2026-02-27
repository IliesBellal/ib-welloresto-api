package menu

import (
	"fmt"
	"welloresto-api/internal/models"
)

// ============================================================
// Uber Eats Menu DTOs
// ============================================================

type UberEatsMenu struct {
	Items          []UberEatsItem              `json:"items"`
	Categories     []UberEatsCategory          `json:"categories"`
	Menus          []UberEatsMenuEntry         `json:"menus"`
	ModifierGroups []UberEatsCustomizationList `json:"modifier_groups"`
}

type UberEatsMenuEntry struct {
	ID                  string                        `json:"id"`
	Title               UberEatsI18nValue             `json:"title"`
	CategoryIDs         []string                      `json:"category_ids"`
	ServiceAvailability []UberEatsServiceAvailability `json:"service_availability"` // CORRECTION ICI
}

type UberEatsServiceAvailability struct {
	DayOfWeek   string               `json:"day_of_week"`
	TimePeriods []UberEatsTimePeriod `json:"time_periods"`
}

type UberEatsTimePeriod struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

type UberEatsI18nValue struct {
	Translations map[string]string `json:"translations"`
}

type UberEatsCategory struct {
	ID       string                   `json:"id"`
	Title    UberEatsI18nValue        `json:"title"`
	Entities []UberEatsCategoryEntity `json:"entities"`
}

type UberEatsCategoryEntity struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "ITEM"
}

type UberEatsItem struct {
	ID               string                   `json:"id"`
	ExternalData     string                   `json:"external_data,omitempty"`
	Title            UberEatsI18nValue        `json:"title"`
	Description      UberEatsI18nValue        `json:"description"`
	PriceInfo        UberEatsPriceInfo        `json:"price_info"`
	MediaInfo        *UberEatsMediaInfo       `json:"media_info,omitempty"`
	SuspendUntil     *int64                   `json:"suspend_until,omitempty"`
	ModifierGroupIDs UberEatsModifierGroupIDs `json:"modifier_group_ids"`
}

type UberEatsModifierGroupIDs struct {
	IDs []string `json:"ids"`
}

type UberEatsPriceInfo struct {
	Price     int64 `json:"price"`
	CorePrice int64 `json:"core_price"`
}

type UberEatsMediaInfo struct {
	Photos []UberEatsPhoto `json:"photos"`
}

type UberEatsPhoto struct {
	URL string `json:"url"`
}

type UberEatsCustomizationList struct {
	ID              string                 `json:"id"`
	Title           UberEatsI18nValue      `json:"title"`
	QuantityInfo    UberEatsMGQuantityInfo `json:"quantity_info"`
	ModifierOptions []UberEatsMGOption     `json:"modifier_options"`
}

type UberEatsMGQuantityInfo struct {
	Quantity UberEatsQuantity `json:"quantity"`
}

type UberEatsQuantity struct {
	Min int `json:"min_permitted"`
	Max int `json:"max_permitted"`
}

type UberEatsMGOption struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// ============================================================
// Mapper principal
// ============================================================

func ToUberEatsFormat(internal *models.MenuResponse) (*UberEatsMenu, error) {
	if internal == nil {
		return nil, fmt.Errorf("ubereats mapper: internal menu is nil")
	}

	payload := &UberEatsMenu{
		Items:          []UberEatsItem{},
		Categories:     []UberEatsCategory{},
		Menus:          []UberEatsMenuEntry{},
		ModifierGroups: []UberEatsCustomizationList{},
	}

	addedModifierGroups := map[string]bool{}
	categoryIDsForMenu := []string{}

	for _, cat := range internal.ProductsTypes {
		catID := "cat_" + cat.Category
		uberCat := UberEatsCategory{
			ID:       catID,
			Title:    i18n(cat.Category),
			Entities: []UberEatsCategoryEntity{},
		}

		for _, product := range cat.Products {
			// 1. VÉRIFICATION : Le produit doit être marqué pour la synchro Uber Eats
			if !product.SyncUberEats {
				continue
			}

			// Calcul du prix pour la vérification
			price := product.Price
			if product.PriceDelivery != nil && *product.PriceDelivery != 0 {
				price = *product.PriceDelivery
			}

			// 2. VÉRIFICATION : Le prix ne doit pas dépasser 25000 (250.00€)
			if price > 25000 || price < 0 {
				continue
			}

			item := UberEatsItem{
				ID:           product.ProductID,
				ExternalData: product.ProductID,
				Title:        i18n(product.Name),
				PriceInfo: UberEatsPriceInfo{
					Price:     price,
					CorePrice: price,
				},
			}

			if product.Description != nil {
				item.Description = i18n(*product.Description)
			}

			if product.ImageURL != nil && *product.ImageURL != "" {
				item.MediaInfo = &UberEatsMediaInfo{
					Photos: []UberEatsPhoto{{URL: *product.ImageURL}},
				}
			}

			if product.Status == "out_of_stock" || !product.AvailableDelivery {
				far := int64(9999999999)
				item.SuspendUntil = &far
			}

			for _, attr := range product.Configuration.Attributes {
				mgID := "mg_" + attr.ID
				item.ModifierGroupIDs.IDs = append(item.ModifierGroupIDs.IDs, mgID)

				if !addedModifierGroups[mgID] {
					addedModifierGroups[mgID] = true
					mg := UberEatsCustomizationList{
						ID:    mgID,
						Title: i18n(attr.Title),
						QuantityInfo: UberEatsMGQuantityInfo{
							Quantity: UberEatsQuantity{
								Min: attr.MinOptions,
								Max: attr.MaxOptions,
							},
						},
					}

					for _, opt := range attr.Options {
						// 1. CRÉER UN ITEM POUR L'OPTION
						// Uber demande que chaque option soit aussi un "Item" dans la liste globale
						optionItemID := "opt_" + opt.ID

						optionItem := UberEatsItem{
							ID:    optionItemID,
							Title: i18n(opt.Title),
							PriceInfo: UberEatsPriceInfo{
								Price:     int64(opt.ExtraPrice),
								CorePrice: int64(opt.ExtraPrice),
							},
						}

						// Ajouter l'option à la liste globale des items
						payload.Items = append(payload.Items, optionItem)

						// 2. RÉFÉRENCER CET ITEM DANS LE GROUPE
						mg.ModifierOptions = append(mg.ModifierOptions, UberEatsMGOption{
							ID:   optionItemID,
							Type: "ITEM",
						})
					}
					payload.ModifierGroups = append(payload.ModifierGroups, mg)
				}
			}

			payload.Items = append(payload.Items, item)
			uberCat.Entities = append(uberCat.Entities, UberEatsCategoryEntity{
				ID:   product.ProductID,
				Type: "ITEM",
			})
		}

		// On n'ajoute la catégorie au menu que si elle contient au moins un produit valide
		if len(uberCat.Entities) > 0 {
			payload.Categories = append(payload.Categories, uberCat)
			categoryIDsForMenu = append(categoryIDsForMenu, catID)
		}
	}

	// Définition des horaires par défaut (indispensable pour Uber)
	days := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	availability := []UberEatsServiceAvailability{}
	for _, day := range days {
		availability = append(availability, UberEatsServiceAvailability{
			DayOfWeek: day,
			TimePeriods: []UberEatsTimePeriod{
				{StartTime: "00:00", EndTime: "23:59"},
			},
		})
	}

	payload.Menus = append(payload.Menus, UberEatsMenuEntry{
		ID:                  "default",
		Title:               i18n("Menu"),
		CategoryIDs:         categoryIDsForMenu,
		ServiceAvailability: availability,
	})

	return payload, nil
}

func i18n(value string) UberEatsI18nValue {
	return UberEatsI18nValue{
		Translations: map[string]string{"fr_fr": value},
	}
}
