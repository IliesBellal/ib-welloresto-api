package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

type BookingsHandler struct {
	svc *services.BookingsService
}

func NewBookingsHandler(s *services.BookingsService) *BookingsHandler {
	return &BookingsHandler{svc: s}
}

func (h *BookingsHandler) SearchBookings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	var req models.BookingObjectRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	bookings, err := h.svc.GetBookings(ctx, token, &req)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, map[string]interface{}{
		"status":   "1",
		"bookings": bookings,
	}, 200)
}

func (h *BookingsHandler) GetBooking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	id := chi.URLParam(r, "booking_id")

	booking, err := h.svc.GetBookingByID(ctx, token, id)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, 200)
}

func (h *BookingsHandler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	var req models.BookingObjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	booking, err := h.svc.CreateBooking(ctx, token, &req)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, map[string]interface{}{
		"status":  "1",
		"booking": booking,
	}, 200)
}

func (h *BookingsHandler) AcceptBooking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.AcceptBooking(ctx, token, bookingID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(result)
}

func (h *BookingsHandler) DenyBooking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.DenyBooking(ctx, token, bookingID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(result)
}

func (h *BookingsHandler) GetBookingAvailability(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
	date := chi.URLParam(r, "date")

	avail, err := h.svc.GetBookingAvailability(ctx, token, date)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, map[string]interface{}{
		"status": "1",
		"data":   avail,
	}, http.StatusOK)
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
