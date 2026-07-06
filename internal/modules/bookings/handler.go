package bookings

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type BookingsHandler struct {
	svc *BookingsService
}

func NewBookingsHandler(s *BookingsService) *BookingsHandler {
	return &BookingsHandler{svc: s}
}

func (h *BookingsHandler) SearchBookings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "search", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req BookingObjectRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "search", map[string]string{"error": "invalid_request"})
		return
	}

	bookings, err := h.svc.GetBookings(ctx, token, &req)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "search", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "search", map[string]interface{}{
		"status":   "1",
		"bookings": bookings,
	})
}

func (h *BookingsHandler) GetBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "get", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	id := chi.URLParam(r, "booking_id")

	booking, err := h.svc.GetBookingByID(ctx, token, id)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "get", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "get", map[string]interface{}{
		"status":  "1",
		"booking": booking,
	})
}

func (h *BookingsHandler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req BookingObjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "create", map[string]string{"error": "invalid_request"})
		return
	}

	booking, err := h.svc.CreateBooking(ctx, &req)
	if err != nil {
		// 409 enrichi : les bookings/tables en collision sont renvoyés au client
		// pour que le POS puisse afficher le conflit.
		var conflictErr *TableConflictError
		if errors.As(err, &conflictErr) {
			models.SendJSON(w, http.StatusConflict, "bookings", "create", map[string]interface{}{
				"error":     "table_conflict",
				"conflicts": conflictErr.Conflicts,
			})
			return
		}

		models.SendErrorJSON(w, "bookings", "create", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "create", map[string]interface{}{
		"status":  "1",
		"booking": booking,
	})
}

func (h *BookingsHandler) AcceptBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "accept", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.AcceptBooking(ctx, token, bookingID)

	if err != nil {
		models.SendErrorJSON(w, "bookings", "accept", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "accept", result)
}

func (h *BookingsHandler) DenyBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "deny", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.DenyBooking(ctx, token, bookingID)

	if err != nil {
		models.SendErrorJSON(w, "bookings", "deny", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "deny", result)
}

func (h *BookingsHandler) GetBookingAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "get_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	date := chi.URLParam(r, "date")

	avail, err := h.svc.GetBookingAvailability(ctx, token, date)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "get_availability", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "get_availability", map[string]interface{}{
		"status": "1",
		"data":   avail,
	})
}
