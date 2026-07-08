package bookings

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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
	var req DenyBookingRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			models.SendJSON(w, http.StatusBadRequest, "bookings", "deny", map[string]string{"error": "invalid_request"})
			return
		}
	}

	result, err := h.svc.DenyBooking(ctx, token, bookingID, &req)

	if err != nil {
		models.SendErrorJSON(w, "bookings", "deny", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "deny", result)
}

func (h *BookingsHandler) CancelBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "cancel", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")
	var req CancelBookingRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			models.SendJSON(w, http.StatusBadRequest, "bookings", "cancel", map[string]string{"error": "invalid_request"})
			return
		}
	}

	result, err := h.svc.CancelBooking(ctx, token, bookingID, &req)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "cancel", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "cancel", result)
}

func (h *BookingsHandler) RescheduleBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "reschedule", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")
	var req RescheduleBookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "reschedule", map[string]string{"error": "invalid_request"})
		return
	}

	result, err := h.svc.RescheduleBooking(ctx, token, bookingID, &req)
	if err != nil {
		var conflictErr *TableConflictError
		if errors.As(err, &conflictErr) {
			models.SendJSON(w, http.StatusConflict, "bookings", "reschedule", map[string]interface{}{
				"error":     "table_conflict",
				"conflicts": conflictErr.Conflicts,
			})
			return
		}

		models.SendErrorJSON(w, "bookings", "reschedule", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "reschedule", result)
}

func (h *BookingsHandler) SeatBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "seat", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.SeatBooking(ctx, token, bookingID)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "seat", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "seat", result)
}

func (h *BookingsHandler) CompleteBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "complete", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.CompleteBooking(ctx, token, bookingID)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "complete", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "complete", result)
}

func (h *BookingsHandler) NoShowBooking(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "no_show", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	bookingID := chi.URLParam(r, "booking_id")

	result, err := h.svc.NoShowBooking(ctx, token, bookingID)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "no_show", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "no_show", result)
}

func (h *BookingsHandler) AssignBookingLocations(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "assign_locations", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	bookingID := chi.URLParam(r, "booking_id")

	var req AssignBookingLocationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "assign_locations", map[string]string{"error": "invalid_request"})
		return
	}

	booking, err := h.svc.AssignBookingLocations(ctx, token, bookingID, &req)
	if err != nil {
		var conflictErr *TableConflictError
		if errors.As(err, &conflictErr) {
			models.SendJSON(w, http.StatusConflict, "bookings", "assign_locations", map[string]interface{}{
				"error":     "table_conflict",
				"conflicts": conflictErr.Conflicts,
			})
			return
		}

		models.SendErrorJSON(w, "bookings", "assign_locations", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "assign_locations", map[string]interface{}{
		"status":  "1",
		"booking": booking,
	})
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

	models.SendJSON(w, http.StatusOK, "bookings", "get_availability", avail)
}

func (h *BookingsHandler) GetBookingSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "get_settings", map[string]string{"error": "missing_token"})
		return
	}

	settings, err := h.svc.GetBookingSettings(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "get_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "get_settings", settings)
}

func (h *BookingsHandler) PutBookingSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "put_settings", map[string]string{"error": "missing_token"})
		return
	}

	var req PutBookingSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "put_settings", map[string]string{"error": "invalid_request"})
		return
	}

	settings, err := h.svc.PutBookingSettings(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "put_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "put_settings", settings)
}

func (h *BookingsHandler) ListBookingDurationRules(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "list_duration_rules", map[string]string{"error": "missing_token"})
		return
	}

	rules, err := h.svc.ListBookingDurationRules(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "list_duration_rules", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "list_duration_rules", rules)
}

func (h *BookingsHandler) CreateBookingDurationRule(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "create_duration_rule", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateDurationRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "create_duration_rule", map[string]string{"error": "invalid_request"})
		return
	}

	rule, err := h.svc.CreateBookingDurationRule(r.Context(), token, req)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "create_duration_rule", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "bookings", "create_duration_rule", rule)
}

func (h *BookingsHandler) PatchBookingDurationRule(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "patch_duration_rule", map[string]string{"error": "missing_token"})
		return
	}

	ruleID := strings.TrimSpace(chi.URLParam(r, "rule_id"))
	if ruleID == "" {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "patch_duration_rule", map[string]string{"error": "missing_rule_id"})
		return
	}

	var req PatchDurationRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "patch_duration_rule", map[string]string{"error": "invalid_request"})
		return
	}

	rule, err := h.svc.UpdateBookingDurationRule(r.Context(), token, ruleID, req)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "patch_duration_rule", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "patch_duration_rule", rule)
}

func (h *BookingsHandler) DeleteBookingDurationRule(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "delete_duration_rule", map[string]string{"error": "missing_token"})
		return
	}

	ruleID := strings.TrimSpace(chi.URLParam(r, "rule_id"))
	if ruleID == "" {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "delete_duration_rule", map[string]string{"error": "missing_rule_id"})
		return
	}

	if err := h.svc.DeleteBookingDurationRule(r.Context(), token, ruleID); err != nil {
		models.SendErrorJSON(w, "bookings", "delete_duration_rule", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "delete_duration_rule", map[string]string{"status": "1"})
}

func (h *BookingsHandler) GetBookingSettingsHours(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "get_settings_hours", map[string]string{"error": "missing_token"})
		return
	}

	hours, err := h.svc.GetBookingHours(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "get_settings_hours", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "get_settings_hours", map[string]interface{}{
		"status": "1",
		"hours":  hours,
	})
}

func (h *BookingsHandler) PutBookingSettingsHours(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "put_settings_hours", map[string]string{"error": "missing_token"})
		return
	}

	var req PutBookingSettingsHoursRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "bookings", "put_settings_hours", map[string]string{"error": "invalid_request"})
		return
	}

	hours, err := h.svc.PutBookingHours(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "put_settings_hours", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "put_settings_hours", map[string]interface{}{
		"status": "1",
		"hours":  hours,
	})
}

func (h *BookingsHandler) ListBookingsBackOffice(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "bookings", "list", map[string]string{"error": "missing_token"})
		return
	}

	query := r.URL.Query()
	filters := BookingListFilters{
		Statuses: query["status"],
		SortBy:   strings.TrimSpace(query.Get("sort_by")),
		SortDir:  strings.TrimSpace(query.Get("sort_dir")),
	}

	if raw := strings.TrimSpace(query.Get("date_from")); raw != "" {
		filters.DateFrom = &raw
	}
	if raw := strings.TrimSpace(query.Get("date_to")); raw != "" {
		filters.DateTo = &raw
	}
	if raw := strings.TrimSpace(query.Get("search")); raw != "" {
		filters.Search = &raw
	}
	if raw := strings.TrimSpace(query.Get("source")); raw != "" {
		filters.Source = &raw
	}
	if raw := strings.TrimSpace(query.Get("party_size")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			models.SendErrorJSON(w, "bookings", "list", models.ErrInvalidInput)
			return
		}
		filters.PartySize = &value
	}

	page := helpers.StringToInt(query.Get("page"))
	if page <= 0 {
		page = 1
	}
	limit := helpers.StringToInt(query.Get("page_size"))
	if limit <= 0 {
		limit = 20
	}
	filters.Page = page
	filters.Limit = limit

	resp, err := h.svc.ListBookingsBackOffice(r.Context(), token, filters)
	if err != nil {
		models.SendErrorJSON(w, "bookings", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "bookings", "list", map[string]interface{}{
		"status":   "1",
		"metadata": resp.Metadata,
		"bookings": resp.Bookings,
	})
}
