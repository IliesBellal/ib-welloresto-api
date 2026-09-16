package presets

import (
	"errors"
	"net/http"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// GetSuggestedModules handles
// GET /v1/public/presets/{code}/suggested-modules — chantier 2's gap for the
// signup tunnel's module-selection screen (LOT B chantier 4b). Public, no
// auth: like /v1/public/signup-context, this runs before any account
// exists. Only suggested_modules is exposed — the rest of PresetConfig
// configures merchant_parameters/entities post-signup and is never meant to
// reach a not-yet-a-merchant visitor (see PresetConfig's doc comment).
func (h *Handler) GetSuggestedModules(w http.ResponseWriter, r *http.Request) {
	const fnName = "get_suggested_modules"
	code := chi.URLParam(r, "code")

	preset, err := h.repo.GetActivePresetByCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, ErrPresetNotFound) {
			models.SendErrorJSON(w, "presets", fnName, models.ErrInvalidPresetCode)
			return
		}
		models.SendErrorJSON(w, "presets", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "presets", fnName, map[string]interface{}{
		"status":            "success",
		"suggested_modules": preset.Config.SuggestedModules,
	})
}
