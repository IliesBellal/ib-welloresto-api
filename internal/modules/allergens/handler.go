package allergens

import (
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type Handler struct {
	service *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{service: svc}
}

// GET /allergens
func (h *Handler) ListAllergens(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "allergens", "list", map[string]string{"error": "missing_token"})
		return
	}

	allergens, err := h.service.ListAllergens(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "allergens", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "allergens", "list", allergens)
}
