package attestations

import (
	"encoding/json"
	"net/http"
	"strconv"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// Handler expose les attestations sous /accounting/attestations.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Overview GET /accounting/attestations — disponibilité, valeurs proposées
// pour le volet 2, attestations de l'établissement.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.Overview(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "attestations", "overview", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "attestations", "overview", resp)
}

// Generate POST /accounting/attestations — volet 2 complété et signé.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "attestations", "generate", models.ErrInvalidRequestBody)
		return
	}
	resp, err := h.service.Generate(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "attestations", "generate", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "attestations", "generate", resp)
}

// Download GET /accounting/attestations/{attestation_id}/download — lien
// signé d'une heure, tracé au journal d'audit.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "attestation_id"), 10, 64)
	if err != nil {
		models.SendErrorJSON(w, "attestations", "download", models.ErrMissingResourceID)
		return
	}
	resp, err := h.service.Link(r.Context(), id)
	if err != nil {
		models.SendErrorJSON(w, "attestations", "download", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "attestations", "download", resp)
}

// Email POST /accounting/attestations/{attestation_id}/email — {"email"} :
// envoi en pièce jointe, tracé au journal d'audit.
func (h *Handler) Email(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "attestation_id"), 10, 64)
	if err != nil {
		models.SendErrorJSON(w, "attestations", "email", models.ErrMissingResourceID)
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "attestations", "email", models.ErrInvalidRequestBody)
		return
	}
	if err := h.service.Email(r.Context(), id, req.Email); err != nil {
		models.SendErrorJSON(w, "attestations", "email", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "attestations", "email", map[string]string{"status": "1"})
}
