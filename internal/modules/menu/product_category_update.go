package menu

import (
	"fmt"
	"regexp"
	"strings"

	"welloresto-api/internal/models"
)

// categoryBgColorPattern : #RRGGBB uniquement (ce qu'envoie le sélecteur de
// couleur du back-office). Pas d'alpha : un fond de catégorie doit rester
// opaque et les clients lisent 8 chiffres de façon différente
// (RRGGBBAA en CSS, AARRGGBB dans l'helper Flutter des produits).
var categoryBgColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// normalizeProductCategoryUpdate valide le payload de modification d'une
// catégorie caisse et renvoie les valeurs à écrire : nom nettoyé (nil = ne pas
// toucher) et couleur en minuscules (nil = ne pas toucher). Rejette un payload
// vide pour ne pas répondre « succès » à une requête qui ne fait rien.
func normalizeProductCategoryUpdate(p UpdateProductCategoryPayload) (name, bgColor *string, err error) {
	if p.Name == nil && p.BgColor == nil {
		return nil, nil, fmt.Errorf("%w: name or bg_color required", models.ErrInvalidInput)
	}

	if p.Name != nil {
		trimmed := strings.TrimSpace(*p.Name)
		if trimmed == "" {
			return nil, nil, fmt.Errorf("%w: name must not be empty", models.ErrInvalidInput)
		}
		name = &trimmed
	}

	if p.BgColor != nil {
		color := strings.TrimSpace(*p.BgColor)
		if !categoryBgColorPattern.MatchString(color) {
			return nil, nil, fmt.Errorf("%w: bg_color must be #RRGGBB", models.ErrInvalidInput)
		}
		color = strings.ToLower(color)
		bgColor = &color
	}

	return name, bgColor, nil
}
