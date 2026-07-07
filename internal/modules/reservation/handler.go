package reservation

import (
	"encoding/json"
	"net/http"
	"strconv"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type ReservationHandler struct {
	svc ReservationService
}

func NewReservationHandler(svc ReservationService) *ReservationHandler {
	return &ReservationHandler{svc: svc}
}

// HandleGetOpenHours intercepte la requête web
func (h *ReservationHandler) HandleGetOpenHours(w http.ResponseWriter, r *http.Request) {
	// On s'assure de renvoyer du JSON
	w.Header().Set("Content-Type", "application/json")

	// Récupération du paramètre QR (ex: /open-hours?qr=ABC)
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		models.SendErrorJSON(w, "rsv", "open-hours", models.ErrInvalidInput)
		return
	}

	// Appel du service
	response := h.svc.GetOpenHours(r.Context(), slug)

	// Envoi de la réponse formatée
	models.SendJSON(w, 200, "rsv", "update.booking", response)
}

func (h *ReservationHandler) HandleGetAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	slug := chi.URLParam(r, "slug")
	dateUnixStr := r.URL.Query().Get("date") // Ex: "1773784295"
	partySizeStr := r.URL.Query().Get("party_size")

	partySize, _ := strconv.Atoi(partySizeStr)
	if partySize <= 0 {
		partySize = 1
	}

	// Conversion de la string Unix en entier 64 bits
	dateUnix, err := strconv.ParseInt(dateUnixStr, 10, 64)
	if slug == "" || dateUnixStr == "" || err != nil {
		models.SendErrorJSON(w, "rsv", "booking-availability", models.ErrInvalidInput)
		return
	}
	response := h.svc.GetBookingAvailability(r.Context(), slug, dateUnix, partySize)

	models.SendJSON(w, http.StatusOK, "rsv", "booking-availability", response)
}

func (h *ReservationHandler) HandleCreateReservation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	slug := chi.URLParam(r, "slug")
	var req BookingRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "rsv", "booking-create", models.ErrInvalidInput)
		return
	}

	response := h.svc.CreateReservation(r.Context(), slug, req)
	models.SendJSON(w, http.StatusOK, "rsv", "booking-create", response)
}

// GET /reservation?slug=...&number=...
func (h *ReservationHandler) HandleGetReservation(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	number := chi.URLParam(r, "booking_id")
	if number == "" {
		number = r.URL.Query().Get("number")
	}
	resp := h.svc.GetReservation(r.Context(), slug, number)
	models.SendJSON(w, 200, "rsv", "get.booking", resp)
}

// POST /reservation/update?slug=...
func (h *ReservationHandler) HandleUpdateReservation(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var req BookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "rsv", "update.booking", models.ErrInvalidInput)
		return
	}
	if req.Booking != nil && req.Booking.BookingNumber == "" {
		req.Booking.BookingNumber = chi.URLParam(r, "booking_id")
	}
	resp := h.svc.UpdateReservation(r.Context(), slug, req)
	models.SendJSON(w, 200, "rsv", "update.booking", resp)
}

// POST /reservation/cancel?slug=...&number=...
func (h *ReservationHandler) HandleCancelReservation(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	number := chi.URLParam(r, "booking_id")
	if number == "" {
		number = r.URL.Query().Get("number")
	}
	resp := h.svc.CancelReservation(r.Context(), slug, number)
	models.SendJSON(w, 200, "rsv", "cancel.booking", resp)
}
