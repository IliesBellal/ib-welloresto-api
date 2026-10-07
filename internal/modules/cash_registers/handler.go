package cash_registers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// LocationsHandler handles orders endpoints
type CashRegisterHandler struct {
	cashRegisterService *CashRegisterService
}

func NewCashRegisterHandler(cashRegisterService *CashRegisterService) *CashRegisterHandler {
	return &CashRegisterHandler{
		cashRegisterService: cashRegisterService,
	}
}

func (h *CashRegisterHandler) OpenCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "open", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.OpenCashRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "open", map[string]string{"error": "invalid_body"})
		return
	}

	open_call, err := h.cashRegisterService.OpenCashRegister(ctx, token, &req)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "open", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "open", open_call)
}

func (h *CashRegisterHandler) CloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "close", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "close", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.CloseCashRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.Header.Get("X-App-Source") != "backoffice" {
		models.SendErrorJSON(w, "cash_register", "close", err)
		return
	}

	result, err := h.cashRegisterService.CloseCashRegister(ctx, token, cashRegisterID, &req)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "close", err)
		return
	}

	// closing_mode / enclosed : en AUTO, le registre est déjà validé — l'app
	// passe directement à la suite, sans écran de relevé de caisse.
	if result.AlreadyClosed {
		models.SendJSON(w, http.StatusOK, "cash_register", "close", map[string]interface{}{
			"status":       "success",
			"message":      "Le registre de caisse est deja ferme.",
			"closing_mode": result.ClosingMode,
			"enclosed":     result.Enclosed,
		})
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "close", map[string]interface{}{
		"status":       "cash_register_closed",
		"closing_mode": result.ClosingMode,
		"enclosed":     result.Enclosed,
	})
}

// closingModeErrorResponse traduit une erreur de mode de clôture en réponse
// explicite pour l'équipe WelloResto (endpoints /admin) ; ok=false si l'erreur
// n'en est pas une.
func closingModeErrorResponse(err error) (status int, code string, message string, ok bool) {
	switch {
	case errors.Is(err, ErrClosingModeInvalidMode):
		return http.StatusBadRequest, "closing_mode_invalid_mode", "Mode inconnu : MANUAL ou AUTO attendu.", true
	case errors.Is(err, ErrClosingModeInvalidDate):
		return http.StatusBadRequest, "closing_mode_invalid_date", "Date invalide : format YYYY-MM-DD attendu.", true
	case errors.Is(err, ErrClosingModeNotMonthStart):
		return http.StatusBadRequest, "closing_mode_not_month_start", "Un changement de mode prend effet le 1er d'un mois.", true
	case errors.Is(err, ErrClosingModeRetroactive):
		return http.StatusBadRequest, "closing_mode_retroactive", "Pas de changement rétroactif : au plus tôt le 1er du mois prochain (calendrier de l'établissement) ; un changement déjà en vigueur ne peut pas être annulé.", true
	case errors.Is(err, ErrClosingModeAlreadyInEffect):
		return http.StatusConflict, "closing_mode_already_in_effect", "Ce mode est déjà celui en vigueur à cette date.", true
	case errors.Is(err, ErrClosingModeAlreadyPlanned):
		return http.StatusConflict, "closing_mode_already_planned", "Un changement est déjà planifié à cette date : l'annuler d'abord.", true
	case errors.Is(err, ErrClosingModeNotFound):
		return http.StatusNotFound, "closing_mode_not_found", "Aucun changement planifié à cette date.", true
	case errors.Is(err, ErrClosingModeMerchantUnknown):
		return http.StatusNotFound, "merchant_not_found", "Établissement inconnu.", true
	}
	return 0, "", "", false
}

func (h *CashRegisterHandler) sendClosingModeResult(w http.ResponseWriter, fnName string, overview *ClosingModesOverview, err error) {
	if err != nil {
		if status, code, message, ok := closingModeErrorResponse(err); ok {
			models.SendJSON(w, status, "cash_register", fnName, map[string]string{"status": code, "message": message, "error": message})
			return
		}
		models.SendErrorJSON(w, "cash_register", fnName, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "cash_register", fnName, overview)
}

// GetClosingModes GET /admin/merchants/{id}/cash-register-closing-modes —
// mode de clôture en vigueur et historique (équipe WelloResto uniquement).
func (h *CashRegisterHandler) GetClosingModes(w http.ResponseWriter, r *http.Request) {
	overview, err := h.cashRegisterService.GetClosingModes(r.Context(), chi.URLParam(r, "id"))
	h.sendClosingModeResult(w, "get_closing_modes", overview, err)
}

// ScheduleClosingMode POST /admin/merchants/{id}/cash-register-closing-modes
// {"mode": "MANUAL"|"AUTO", "effective_from": "YYYY-MM-01"} — planifie un
// changement au 1er d'un mois futur (équipe WelloResto uniquement).
func (h *CashRegisterHandler) ScheduleClosingMode(w http.ResponseWriter, r *http.Request) {
	var req ScheduleClosingModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "cash_register", "schedule_closing_mode", models.ErrInvalidRequestBody)
		return
	}
	overview, err := h.cashRegisterService.ScheduleClosingMode(r.Context(), chi.URLParam(r, "id"), req)
	h.sendClosingModeResult(w, "schedule_closing_mode", overview, err)
}

// CancelClosingMode DELETE /admin/merchants/{id}/cash-register-closing-modes/{effective_from}
// — annule un changement planifié pas encore en vigueur (équipe WelloResto
// uniquement).
func (h *CashRegisterHandler) CancelClosingMode(w http.ResponseWriter, r *http.Request) {
	overview, err := h.cashRegisterService.CancelClosingMode(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "effective_from"))
	h.sendClosingModeResult(w, "cancel_closing_mode", overview, err)
}

func (h *CashRegisterHandler) GetCashRegisterSummary(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "get_summary", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "get_summary", map[string]string{"error": "missing_parameter"})
		return
	}

	summary, err := h.cashRegisterService.GetCashRegisterSummary(ctx, token, cashRegisterID)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "get_summary", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "get_summary", summary)
}

func (h *CashRegisterHandler) GetCashRegisterTVADetails(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "get_tva_details", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "get_tva_details", map[string]string{"error": "missing_parameter"})
		return
	}

	resp, err := h.cashRegisterService.GetCashRegisterTVADetails(ctx, token, cashRegisterID)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "get_tva_details", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "get_tva_details", resp)
}

func (h *CashRegisterHandler) AddCustomItem(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "add_custom_item", map[string]string{"error": "missing_token"})
		return
	}

	id := chi.URLParam(r, "cash_register_id")

	var req models.AddCustomItemRequest
	json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.cashRegisterService.AddCustomItem(r.Context(), token, id, &req)

	if err != nil {
		models.SendErrorJSON(w, "cash_register", "add_custom_item", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "add_custom_item", resp)
}

func (h *CashRegisterHandler) DeleteCustomItem(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "delete_custom_item", map[string]string{"error": "missing_token"})
		return
	}

	id := chi.URLParam(r, "cash_register_id")
	itemID := chi.URLParam(r, "item_id")

	resp, err := h.cashRegisterService.DeleteCustomItem(r.Context(), token, id, itemID)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "delete_custom_item", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "delete_custom_item", resp)
}

func (h *CashRegisterHandler) EncloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "enclose", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "cash_register_id")

	var req models.EncloseCashRegisterRequest
	json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.cashRegisterService.EncloseCashRegister(ctx, id, token, req.Comment)

	if err != nil {
		models.SendErrorJSON(w, "cash_register", "enclose", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "enclose", resp)
}

func (h *CashRegisterHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "get_history", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req CashRegisterHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "get_history", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.cashRegisterService.GetCashRegisterHistory(ctx, req)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "get_history", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "get_history", CashRegisterHistoryResponse{
		Status:        "success",
		Metadata:      &result.Metadata,
		CashRegisters: result.CashRegisters,
	})
}

func (h *CashRegisterHandler) GetCashRegisterHistoryByID(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "get_cash_register", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "get_cash_register", map[string]string{"error": "missing_parameter"})
		return
	}

	// Construire la requête avec le filtre sur l'ID
	req := CashRegisterHistoryRequest{
		CashRegisterID: &cashRegisterID,
	}

	result, err := h.cashRegisterService.GetCashRegisterHistory(ctx, req)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "get_cash_register", err)
		return
	}

	if len(result.CashRegisters) == 0 {
		models.SendErrorJSON(w, "cash_register", "get_cash_register", models.ErrNotFound)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "get_cash_register", map[string]interface{}{
		"status":        "success",
		"cash_register": result.CashRegisters[0],
	})
}

func (h *CashRegisterHandler) json(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *CashRegisterHandler) OpenCashDrawer(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_drawer", "open", map[string]string{"error": "missing_token"})
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_drawer", "open", map[string]string{"status": "success"})
}

func (h *CashRegisterHandler) HandleLinkDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req DeviceLinkRequest
	json.NewDecoder(r.Body).Decode(&req)

	err := h.cashRegisterService.LinkDevice(r.Context(), req)
	if err != nil {
		logger.FromContext(ctx).Error("LinkDevice Error " + err.Error())
		models.SendErrorJSON(w, "cash_register", "link_device", err)
		return
	}

	// 4. Succès
	models.SendJSON(w, http.StatusOK, "cash_register", "link_device", map[string]string{"status": "success"})
}

func (h *CashRegisterHandler) HandleUnlinkDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req DeviceUnlinkRequest
	json.NewDecoder(r.Body).Decode(&req)

	err := h.cashRegisterService.UnlinkDevice(ctx, req.DeviceID)
	if err != nil {
		if err == models.ErrNotFound {
			models.SendJSON(w, http.StatusNotFound, "cash_register", "unlink_device", map[string]string{"error": "Aucune liaison trouvée pour cet appareil"})
			return
		}
		logger.FromContext(ctx).Error("UnlinkDevice Error " + err.Error())
		models.SendErrorJSON(w, "cash_register", "unlink_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "unlink_device", map[string]string{"message": "Liaison supprimée"})
}
