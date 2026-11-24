package imagebank

import (
	"net/http"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

const module = "image_bank"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List handles GET /image-bank?format=square|banner&category=…&q=…
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := ListFilter{
		Format:   strings.TrimSpace(q.Get("format")),
		Category: strings.TrimSpace(q.Get("category")),
		Query:    strings.TrimSpace(q.Get("q")),
	}
	if f.Format != "" && !IsFormat(f.Format) {
		models.SendJSON(w, http.StatusBadRequest, module, "list", map[string]string{"error": "invalid_format"})
		return
	}
	if f.Category != "" && !IsCategory(f.Category) {
		models.SendJSON(w, http.StatusBadRequest, module, "list", map[string]string{"error": "invalid_category"})
		return
	}
	if len(f.Query) > 100 {
		f.Query = f.Query[:100]
	}
	resp, err := h.svc.List(r.Context(), f)
	if err != nil {
		models.SendErrorJSON(w, module, "list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "list", resp)
}

// Categories handles GET /image-bank/categories
func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.svc.Categories(r.Context())
	if err != nil {
		models.SendErrorJSON(w, module, "categories", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "categories", map[string]any{"categories": cats})
}

// Mine handles GET /image-bank/mine : images déjà utilisées par
// l'établissement courant.
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, module, "mine", models.ErrUnauthorized)
		return
	}
	resp, err := h.svc.Mine(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, module, "mine", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "mine", resp)
}
