package reservation

import (
	"encoding/json"
	"net/http"
	"strconv"
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
	qr := r.URL.Query().Get("qr")
	if qr == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "QR code is required"})
		return
	}

	// Appel du service
	response := h.svc.GetOpenHours(r.Context(), qr)

	// Envoi de la réponse formatée
	json.NewEncoder(w).Encode(response)
}

func (h *ReservationHandler) HandleGetAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	qr := r.URL.Query().Get("qr")
	date := r.URL.Query().Get("date")
	partySizeStr := r.URL.Query().Get("party_size")

	partySize, _ := strconv.Atoi(partySizeStr)
	if partySize <= 0 {
		partySize = 1
	}

	if qr == "" || date == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(AvailabilityResponse{Status: "error", Error: "Missing parameters"})
		return
	}

	response := h.svc.GetBookingAvailability(r.Context(), qr, date, partySize)
	json.NewEncoder(w).Encode(response)
}

func (h *ReservationHandler) HandleCreateReservation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	qr := r.URL.Query().Get("qr")
	var req BookingRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CreateBookingResponse{Status: "-4", Error: "Invalid JSON"})
		return
	}

	response := h.svc.CreateReservation(r.Context(), qr, req)
	json.NewEncoder(w).Encode(response)
}

// GET /reservation?qr=...&number=...
func (h *ReservationHandler) HandleGetReservation(w http.ResponseWriter, r *http.Request) {
	qr := r.URL.Query().Get("qr")
	number := r.URL.Query().Get("number")
	resp := h.svc.GetReservation(r.Context(), qr, number)
	json.NewEncoder(w).Encode(resp)
}

// POST /reservation/update?qr=...
func (h *ReservationHandler) HandleUpdateReservation(w http.ResponseWriter, r *http.Request) {
	qr := r.URL.Query().Get("qr")
	var req BookingRequest
	json.NewDecoder(r.Body).Decode(&req)
	resp := h.svc.UpdateReservation(r.Context(), qr, req)
	json.NewEncoder(w).Encode(resp)
}

// POST /reservation/cancel?qr=...&number=...
func (h *ReservationHandler) HandleCancelReservation(w http.ResponseWriter, r *http.Request) {
	qr := r.URL.Query().Get("qr")
	number := r.URL.Query().Get("number")
	resp := h.svc.CancelReservation(r.Context(), qr, number)
	json.NewEncoder(w).Encode(resp)
}
