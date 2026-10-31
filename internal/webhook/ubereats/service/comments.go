package service

import (
	"strings"

	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

// orderComment rassemble ce que le client a écrit pour la commande entière, tel
// que la caisse et la production l'affichent (order_comments sans ligne).
//
// Uber place les consignes de préparation dans cart.special_instructions (champ
// documenté, que l'établissement peut désactiver) ; store_instructions, lu avant,
// est conservé au cas où. Les consignes de livraison du client
// (eater.delivery.notes) n'existent que pour les commandes livrées par
// l'établissement. Les couverts ne sont signalés que lorsque le client en veut.
func orderComment(order *ueModels.UberOrder) *string {
	var parts []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, p := range parts {
			if p == s {
				return
			}
		}
		parts = append(parts, s)
	}

	if order.StoreInstructions != nil {
		add(*order.StoreInstructions)
	}
	if order.Cart.SpecialInstructions != nil {
		add(*order.Cart.SpecialInstructions)
	}
	if notes := strings.TrimSpace(order.Eater.Delivery.Notes); notes != "" {
		add("Livraison : " + notes)
	}
	if want := order.Packaging.DisposableItems.ShouldInclude; want != nil && *want {
		add("Couverts demandés")
	}

	if len(parts) == 0 {
		return nil
	}
	comment := strings.Join(parts, "\n")
	return &comment
}

// itemComment rassemble ce que le client a écrit pour un article : allergies
// d'abord (sécurité alimentaire), puis consignes de préparation.
func itemComment(item ueModels.UberCartItem) string {
	var parts []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, p := range parts {
			if p == s {
				return
			}
		}
		parts = append(parts, s)
	}

	for _, sr := range item.SpecialRequests {
		if sr.Allergy != nil {
			add(allergyText(*sr.Allergy))
		}
	}
	if cr := item.CustomerRequest; cr != nil {
		if cr.Allergy != nil && cr.Allergy.Instructions != nil {
			add(*cr.Allergy.Instructions)
		}
	}
	if item.SpecialInstructions != nil {
		add(*item.SpecialInstructions)
	}
	if cr := item.CustomerRequest; cr != nil && cr.SpecialInstructions != nil {
		add(*cr.SpecialInstructions)
	}

	return strings.Join(parts, " | ")
}

var allergenLabels = map[string]string{
	"DAIRY":     "lait",
	"EGGS":      "œufs",
	"FISH":      "poisson",
	"SHELLFISH": "crustacés",
	"TREENUTS":  "fruits à coque",
	"PEANUTS":   "arachides",
	"GLUTEN":    "gluten",
	"SOY":       "soja",
}

// allergyText : « Allergie : gluten, arachides — pas de traces ».
func allergyText(a ueModels.UberAllergyRequest) string {
	var allergens []string
	for _, al := range a.AllergensToExclude {
		label, known := allergenLabels[al.Type]
		switch {
		case known:
		case strings.TrimSpace(al.FreeformText) != "":
			label = strings.TrimSpace(al.FreeformText)
		case al.Type != "" && al.Type != "OTHER":
			label = strings.ToLower(al.Type)
		default:
			continue
		}
		allergens = append(allergens, label)
	}
	text := ""
	if len(allergens) > 0 {
		text = "Allergie : " + strings.Join(allergens, ", ")
	}
	if instr := strings.TrimSpace(a.AllergyInstructions); instr != "" {
		if text != "" {
			text += " — "
		} else {
			text = "Allergie : "
		}
		text += instr
	}
	return text
}
