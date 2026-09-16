package subscriptions

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// --- B1d — internal admin endpoints (RequirePlatformAdmin, see routes.go) --

type createOverrideRequest struct {
	Kind        string  `json:"kind"`
	Target      string  `json:"target"`
	Reason      string  `json:"reason"`
	Note        *string `json:"note,omitempty"`
	ExpiresAt   *string `json:"expires_at,omitempty"`   // RFC3339, optional
	TrialEndsAt *string `json:"trial_ends_at,omitempty"` // RFC3339, optional — B2c-1
}

// CreateOverride handles POST /v1/admin/merchants/{id}/overrides.
func (h *Handler) CreateOverride(w http.ResponseWriter, r *http.Request) {
	const fnName = "create_override"
	merchantID := chi.URLParam(r, "id")

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "subscriptions", fnName, models.ErrUnauthorized)
		return
	}

	var req createOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, models.ErrInvalidInput)
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			models.SendErrorJSON(w, "subscriptions", fnName, models.ErrInvalidInput)
			return
		}
		expiresAt = &t
	}
	var trialEndsAt *time.Time
	if req.TrialEndsAt != nil && strings.TrimSpace(*req.TrialEndsAt) != "" {
		t, err := time.Parse(time.RFC3339, *req.TrialEndsAt)
		if err != nil {
			models.SendErrorJSON(w, "subscriptions", fnName, models.ErrInvalidInput)
			return
		}
		trialEndsAt = &t
	}

	override, err := h.svc.CreateOverride(r.Context(), merchantID, req.Kind, req.Target, req.Reason, req.Note, user.UserID, expiresAt, trialEndsAt)
	if err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "subscriptions", fnName, map[string]interface{}{"status": "success", "override": override})
}

// ListOverrides handles GET /v1/admin/overrides.
func (h *Handler) ListOverrides(w http.ResponseWriter, r *http.Request) {
	const fnName = "list_overrides"
	overrides, err := h.svc.ListActiveOverrides(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "subscriptions", fnName, map[string]interface{}{"status": "success", "overrides": overrides})
}

// RevokeOverride handles DELETE /v1/admin/overrides/{id}.
func (h *Handler) RevokeOverride(w http.ResponseWriter, r *http.Request) {
	const fnName = "revoke_override"
	id := chi.URLParam(r, "id")
	if err := h.svc.RevokeOverride(r.Context(), id); err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "subscriptions", fnName, map[string]interface{}{"status": "success"})
}

// --- B1e — client-facing endpoints (RequirePermission(settings.manage)) ----

func splitCodes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	codes := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			codes = append(codes, p)
		}
	}
	return codes
}

// GetCurrent handles GET /v1/subscriptions/current — chantier 2's gap:
// distinct from PreviewChange (always a hypothetical add/remove diff), this
// reads the merchant's actually-active subscription_items with no
// what-if computation, so a screen can render "what you have" before any
// change is proposed.
func (h *Handler) GetCurrent(w http.ResponseWriter, r *http.Request) {
	const fnName = "get_current"
	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "subscriptions", fnName, models.ErrUnauthorized)
		return
	}

	amount, err := h.svc.ComputeSubscriptionAmount(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "subscriptions", fnName, map[string]interface{}{"status": "success", "current": amount})
}

// PreviewChange handles GET /v1/subscriptions/preview?add=haccp&remove=reservation.
func (h *Handler) PreviewChange(w http.ResponseWriter, r *http.Request) {
	const fnName = "preview_change"
	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "subscriptions", fnName, models.ErrUnauthorized)
		return
	}

	add := splitCodes(r.URL.Query().Get("add"))
	remove := splitCodes(r.URL.Query().Get("remove"))

	preview, err := h.svc.PreviewChange(r.Context(), user.MerchantID, add, remove)
	if err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "subscriptions", fnName, map[string]interface{}{"status": "success", "preview": preview})
}

type applyItemsRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// ApplyItems handles POST /v1/subscriptions/items.
func (h *Handler) ApplyItems(w http.ResponseWriter, r *http.Request) {
	const fnName = "apply_items"
	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "subscriptions", fnName, models.ErrUnauthorized)
		return
	}

	var req applyItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, models.ErrInvalidInput)
		return
	}

	amount, err := h.svc.ApplyItemChanges(r.Context(), user.MerchantID, req.Add, req.Remove)
	if err != nil {
		models.SendErrorJSON(w, "subscriptions", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "subscriptions", fnName, map[string]interface{}{"status": "success", "amount": amount})
}
