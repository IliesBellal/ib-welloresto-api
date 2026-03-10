package tags

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{service: svc}
}

// GET /pos/tags
func (h *Handler) ListTags(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "tags", "list", map[string]string{"error": "missing_token"})
		return
	}

	tagList, err := h.service.ListTags(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "tags", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "tags", "list", tagList)
}

// POST /merchants/tags
func (h *Handler) CreateTag(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "tags", "create", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "tags", "create", map[string]string{"error": "invalid_request"})
		return
	}

	tag, err := h.service.CreateTag(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "tags", "create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "tags", "create", tag)
}

// DELETE /merchants/tags/:tag_id
func (h *Handler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "tags", "delete", map[string]string{"error": "missing_token"})
		return
	}

	tagID := chi.URLParam(r, "tag_id")
	if strings.TrimSpace(tagID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "tags", "delete", map[string]string{"error": "missing_id"})
		return
	}

	err := h.service.DeleteTag(r.Context(), token, tagID)
	if err != nil {
		models.SendErrorJSON(w, "tags", "delete", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
