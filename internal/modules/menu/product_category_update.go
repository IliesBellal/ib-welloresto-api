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

// maxCategoryCourseStep : borne de productcateg.course_step (migration 212).
const maxCategoryCourseStep = 20

// productCategoryUpdate : valeurs à écrire, nil = ne pas toucher.
// CourseStep pointe sur 0 pour retirer l'étape.
type productCategoryUpdate struct {
	Name       *string
	BgColor    *string
	CourseStep *int
}

// normalizeProductCategoryUpdate valide le payload de modification d'une
// catégorie caisse et renvoie les valeurs à écrire : nom nettoyé, couleur en
// minuscules, étape de service. Rejette un payload vide pour ne pas répondre
// « succès » à une requête qui ne fait rien.
func normalizeProductCategoryUpdate(p UpdateProductCategoryPayload) (productCategoryUpdate, error) {
	var u productCategoryUpdate
	if p.Name == nil && p.BgColor == nil && p.CourseStep == nil {
		return u, fmt.Errorf("%w: name, bg_color or course_step required", models.ErrInvalidInput)
	}

	if p.Name != nil {
		trimmed := strings.TrimSpace(*p.Name)
		if trimmed == "" {
			return u, fmt.Errorf("%w: name must not be empty", models.ErrInvalidInput)
		}
		u.Name = &trimmed
	}

	if p.BgColor != nil {
		color := strings.TrimSpace(*p.BgColor)
		if !categoryBgColorPattern.MatchString(color) {
			return u, fmt.Errorf("%w: bg_color must be #RRGGBB", models.ErrInvalidInput)
		}
		color = strings.ToLower(color)
		u.BgColor = &color
	}

	if p.CourseStep != nil {
		step := *p.CourseStep
		if step < 0 || step > maxCategoryCourseStep {
			return u, fmt.Errorf("%w: course_step must be between 0 and %d", models.ErrInvalidInput, maxCategoryCourseStep)
		}
		u.CourseStep = &step
	}

	return u, nil
}
