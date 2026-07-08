package reservation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/bookings"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Modèles publics liste d'attente
// ---------------------------------------------------------------------------

// WaitlistJoinRequest est le corps d'une inscription publique.
type WaitlistJoinRequest struct {
	PartySize     int     `json:"party_size"`
	CustomerName  string  `json:"customer_name"`
	CustomerPhone string  `json:"customer_phone"`
	Notes         *string `json:"notes"`
}

// WaitlistPublicEntry expose l'état d'une entrée sans les données internes.
type WaitlistPublicEntry struct {
	Status       string  `json:"status"`
	PartySize    int     `json:"party_size"`
	CustomerName string  `json:"customer_name"`
	CreatedAt    string  `json:"created_at"`
	NotifiedAt   *string `json:"notified_at,omitempty"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
}

// WaitlistPublicResponse porte le token d'accès sans authentification.
type WaitlistPublicResponse struct {
	Status        string               `json:"status"`
	Error         string               `json:"error,omitempty"`
	WaitlistID    string               `json:"waitlist_id,omitempty"`
	WaitlistToken string               `json:"waitlist_token,omitempty"`
	Entry         *WaitlistPublicEntry `json:"entry,omitempty"`
}

func toWaitlistPublicEntry(e *bookings.WaitlistEntry) *WaitlistPublicEntry {
	if e == nil {
		return nil
	}
	return &WaitlistPublicEntry{
		Status:       e.Status,
		PartySize:    e.PartySize,
		CustomerName: e.CustomerName,
		CreatedAt:    e.CreatedAt,
		NotifiedAt:   e.NotifiedAt,
		ExpiresAt:    e.ExpiresAt,
	}
}

// mapWaitlistError traduit une erreur métier bookings en statut public.
func mapWaitlistError(err error) WaitlistPublicResponse {
	switch {
	case errors.Is(err, models.ErrWaitlistDisabled):
		return WaitlistPublicResponse{Status: "waitlist_disabled", Error: "Waitlist not enabled"}
	case errors.Is(err, models.ErrWaitlistFull):
		return WaitlistPublicResponse{Status: "waitlist_full", Error: "Waitlist full"}
	case errors.Is(err, models.ErrInvalidInput):
		return WaitlistPublicResponse{Status: "-4", Error: "Invalid waitlist payload"}
	case errors.Is(err, models.ErrNotFound):
		return WaitlistPublicResponse{Status: "0", Error: "Waitlist entry not found"}
	default:
		return WaitlistPublicResponse{Status: "-2", Error: err.Error()}
	}
}

// ---------------------------------------------------------------------------
// Service (public — merchant résolu par QR, token = id d'entrée)
// ---------------------------------------------------------------------------

func (s *reservationService) JoinWaitlist(ctx context.Context, qr string, req WaitlistJoinRequest) WaitlistPublicResponse {
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return WaitlistPublicResponse{Status: "-1", Error: "QR Code expired"}
	}

	entry, err := s.bookingSvc.CreateWaitlistEntryPublic(ctx, merchant.MerchantID, bookings.CreateWaitlistRequest{
		PartySize:     req.PartySize,
		CustomerName:  req.CustomerName,
		CustomerPhone: req.CustomerPhone,
		Notes:         req.Notes,
	})
	if err != nil {
		return mapWaitlistError(err)
	}

	return WaitlistPublicResponse{
		Status:        "1",
		WaitlistID:    entry.ID,
		WaitlistToken: entry.ID,
		Entry:         toWaitlistPublicEntry(entry),
	}
}

func (s *reservationService) GetWaitlistStatus(ctx context.Context, qr string, token string) WaitlistPublicResponse {
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return WaitlistPublicResponse{Status: "-1", Error: "QR Code expired"}
	}

	entry, err := s.bookingSvc.GetWaitlistEntryPublic(ctx, merchant.MerchantID, token)
	if err != nil {
		return mapWaitlistError(err)
	}

	return WaitlistPublicResponse{
		Status:        "1",
		WaitlistID:    entry.ID,
		WaitlistToken: entry.ID,
		Entry:         toWaitlistPublicEntry(entry),
	}
}

func (s *reservationService) LeaveWaitlist(ctx context.Context, qr string, token string) GenericResponse {
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil || merchant == nil {
		return GenericResponse{Status: "-1", Error: "QR Code expired"}
	}

	if _, err := s.bookingSvc.CancelWaitlistEntryPublic(ctx, merchant.MerchantID, token); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return GenericResponse{Status: "0", Error: "Waitlist entry not found"}
		}
		if errors.Is(err, models.ErrInvalidInput) {
			return GenericResponse{Status: "already_closed"}
		}
		return GenericResponse{Status: "-2", Error: err.Error()}
	}

	return GenericResponse{Status: "1"}
}

// ---------------------------------------------------------------------------
// Handlers publics
// ---------------------------------------------------------------------------

func (h *ReservationHandler) HandleJoinWaitlist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	slug := chi.URLParam(r, "slug")

	var req WaitlistJoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "rsv", "waitlist-join", models.ErrInvalidInput)
		return
	}

	resp := h.svc.JoinWaitlist(r.Context(), slug, req)
	models.SendJSON(w, http.StatusOK, "rsv", "waitlist-join", resp)
}

func (h *ReservationHandler) HandleGetWaitlistStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	slug := chi.URLParam(r, "slug")
	token := chi.URLParam(r, "waitlist_token")

	resp := h.svc.GetWaitlistStatus(r.Context(), slug, token)
	models.SendJSON(w, http.StatusOK, "rsv", "waitlist-status", resp)
}

func (h *ReservationHandler) HandleLeaveWaitlist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	slug := chi.URLParam(r, "slug")
	token := chi.URLParam(r, "waitlist_token")

	resp := h.svc.LeaveWaitlist(r.Context(), slug, token)
	models.SendJSON(w, http.StatusOK, "rsv", "waitlist-leave", resp)
}
