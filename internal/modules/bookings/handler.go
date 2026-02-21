package bookings

import (
	"encoding/json"
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
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	var req BookingObjectRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	bookings, err := h.svc.GetBookings(ctx, token, &req)
	if err != nil {
		models.SendJSON(w, "bookings", "search_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "bookings", "search", map[string]interface{}{
		"status":   "1",
		"bookings": bookings,
	})
}

func (h *BookingsHandler) GetBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}
	ctx := r.Context()

	id := chi.URLParam(r, "booking_id")

	booking, err := h.svc.GetBookingByID(ctx, token, id)
	if err != nil {
		models.SendJSON(w, "bookings", "get_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "bookings", "get", map[string]interface{}{
		"status":  "1",
		"booking": booking,
	})
}

func (h *BookingsHandler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}
	ctx := r.Context()

	var req BookingObjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	booking, err := h.svc.CreateBooking(ctx, token, &req)
	if err != nil {
		models.SendJSON(w, "bookings", "create_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "bookings", "create", map[string]interface{}{
		"status":  "1",
		"booking": booking,
	})
}

func (h *BookingsHandler) AcceptBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.AcceptBooking(ctx, token, bookingID)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "bookings", "accept_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "bookings", "accept", result)
}

func (h *BookingsHandler) DenyBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.DenyBooking(ctx, token, bookingID)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "bookings", "deny_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "bookings", "deny", result)
}

func (h *BookingsHandler) GetBookingAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()
	date := chi.URLParam(r, "date")

	avail, err := h.svc.GetBookingAvailability(ctx, token, date)
	if err != nil {
		models.SendJSON(w, "bookings", "get_availability_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "bookings", "get_availability", map[string]interface{}{
		"status": "1",
		"data":   avail,
	})
}

func (h *BookingsHandler) json(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *BookingsHandler) errorJSON(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "0",
		"error":  err.Error(),
	})
}
