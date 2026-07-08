package bookings

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

func (h *BookingsHandler) ListWaitlist(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "waitlist_list", map[string]string{"error": "missing_token"})
		return
	}

	entries, err := h.svc.ListWaitlist(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "waitlist_list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "waitlist_list", map[string]interface{}{
		"status":   "1",
		"waitlist": entries,
	})
}

func (h *BookingsHandler) CreateWaitlistEntry(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "waitlist_create", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateWaitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "waitlist_create", map[string]string{"error": "invalid_request"})
		return
	}

	entry, err := h.svc.CreateWaitlistManual(r.Context(), token, req)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "waitlist_create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "bookings", "waitlist_create", map[string]interface{}{
		"status": "1",
		"entry":  entry,
	})
}

func (h *BookingsHandler) SeatWaitlistEntry(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "waitlist_seat", map[string]string{"error": "missing_token"})
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	entry, err := h.svc.SeatWaitlistEntry(r.Context(), token, id)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "waitlist_seat", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "waitlist_seat", map[string]interface{}{
		"status": "1",
		"entry":  entry,
	})
}

func (h *BookingsHandler) CancelWaitlistEntry(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "waitlist_cancel", map[string]string{"error": "missing_token"})
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	entry, err := h.svc.CancelWaitlistEntry(r.Context(), token, id)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "waitlist_cancel", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "waitlist_cancel", map[string]interface{}{
		"status": "1",
		"entry":  entry,
	})
}

func (h *BookingsHandler) DeleteWaitlistEntry(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "waitlist_delete", map[string]string{"error": "missing_token"})
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.svc.DeleteWaitlistEntry(r.Context(), token, id); err != nil {
		models.SendErrorJSON(w, "bookings", "waitlist_delete", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "waitlist_delete", map[string]interface{}{
		"status": "1",
	})
}
