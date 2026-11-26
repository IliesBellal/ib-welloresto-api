package pos

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/imagepick"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// maxMerchantLogoBytes plafonne l'upload du logo général de l'établissement,
// même limite que le logo Kiosk (voir kiosk.AdminHandler). Le fichier est
// ensuite allégé par imageopt avant d'aller sur R2.
const (
	maxMerchantLogoBytes   = 5 << 20
	multipartOverheadBytes = 1 << 20
)

type POSHandler struct {
	service  *POSService
	r2Client *r2.Client
	picker   *imagepick.Picker
}

// SetImagePicker branche le placement d'images (logo) : allègement des
// fichiers envoyés, banque d'images et « Mes images » (docs/banque-images.md).
func (h *POSHandler) SetImagePicker(p *imagepick.Picker) { h.picker = p }

func NewPOSHandler(s *POSService, r2Client *r2.Client) *POSHandler {
	return &POSHandler{service: s, r2Client: r2Client}
}

func (h *POSHandler) GetPOSStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_pos_status", map[string]string{"error": "missing_token"})
		return
	}

	resp, err := h.service.GetPOSStatus(r.Context(), token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_status", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_status", map[string]interface{}{"pos_status": resp})
}

func (h *POSHandler) UpdatePOSStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "update_pos_status", map[string]string{"error": "missing_token"})
		return
	}

	var body struct {
		Status bool `json:"status"`
	}

	json.NewDecoder(r.Body).Decode(&body)

	resp, err := h.service.UpdatePOSStatus(r.Context(), token, body.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "update_status", map[string]string{"error": err.Error()})
		return
	}

	// Return same as GET
	models.SendJSON(w, http.StatusOK, "pos", "update_status", map[string]interface{}{"pos_status": resp})
}

func (h *POSHandler) GetDeletionReasons(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_deletion_reasons", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	obj := chi.URLParam(r, "object")

	reasons, err := h.service.GetDeletionReasons(ctx, obj)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_deletion_reasons", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_deletion_reasons", models.DeletionReasonResponse{
		Status:          "1",
		DeletionReasons: reasons,
	})
}

func (h *POSHandler) ToggleScanNOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "toggle_scannorder", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "toggle_scannorder", map[string]string{"error": "invalid_request"})
		return
	}

	updated, err := h.service.ToggleScanNOrder(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "toggle_scannorder", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "toggle_scannorder", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) ToggleProductionPaidOnly(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "toggle_production_paid_only", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "toggle_production_paid_only", map[string]string{"error": "invalid_request"})
		return
	}

	updated, err := h.service.ToggleProductionPaidOnly(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "toggle_production_paid_only", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "toggle_production_paid_only", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) GetTVARates(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_tva_rates", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	updated, err := h.service.GetTVARates(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_tva_rates", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_tva_rates", updated)
}

func (h *POSHandler) ToggleSafetyStockActive(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "toggle_safety_stock", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "toggle_safety_stock", map[string]string{"error": "invalid_request"})
		return
	}

	updated, err := h.service.ToggleSafetyStock(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "toggle_safety_stock", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "toggle_safety_stock", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) GetDeliveryMen(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_users", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	users, err := h.service.GetDeliveryMen(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_users", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_users", models.DeliveryMenResponse{
		Users: users,
	})
}

func (h *POSHandler) CheckTR(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "check_tr", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	code := chi.URLParam(r, "code")

	resp, err := h.service.CheckTR(ctx, token, code)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "check_tr", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "check_tr", resp)
}

func (h *POSHandler) UpdateMerchantSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "update_merchant_settings", map[string]string{"error": "missing_token"})
		return
	}

	var req models.UpdateMerchantSettingsRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "update_merchant_settings", map[string]string{"error": "invalid_request"})
		return
	}

	result, err := h.service.UpdateMerchantSettings(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "pos", "update_merchant_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "update_merchant_settings", result)
}

func (h *POSHandler) ListPlanningHolidays(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "list_holidays", map[string]string{"error": "missing_token"})
		return
	}

	items, err := h.service.ListPlanningHolidays(r.Context(), token, PlanningHolidayListFilters{
		StartDate: strings.TrimSpace(r.URL.Query().Get("start_date")),
		EndDate:   strings.TrimSpace(r.URL.Query().Get("end_date")),
	})
	if err != nil {
		models.SendErrorJSON(w, "pos", "list_holidays", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "list_holidays", map[string]interface{}{"status": "success", "holidays": items})
}

func (h *POSHandler) PatchPlanningHolidayOverride(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "patch_holiday", map[string]string{"error": "missing_token"})
		return
	}

	holidayDate := strings.TrimSpace(chi.URLParam(r, "date"))
	var req PlanningHolidayOverridePatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "pos", "patch_holiday", models.ErrInvalidRequestBody)
		return
	}

	item, err := h.service.PatchPlanningHolidayOverride(r.Context(), token, holidayDate, req)
	if err != nil {
		models.SendErrorJSON(w, "pos", "patch_holiday", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "patch_holiday", map[string]interface{}{"status": "success", "holiday": item})
}

func (h *POSHandler) DeletePlanningHolidayOverride(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "delete_holiday", map[string]string{"error": "missing_token"})
		return
	}

	holidayDate := strings.TrimSpace(chi.URLParam(r, "date"))
	if err := h.service.DeletePlanningHolidayOverride(r.Context(), token, holidayDate); err != nil {
		models.SendErrorJSON(w, "pos", "delete_holiday", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "delete_holiday", map[string]interface{}{"status": "success"})
}

func (h *POSHandler) ListPlanningVacationPeriods(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "list_vacation_periods", map[string]string{"error": "missing_token"})
		return
	}

	items, err := h.service.ListPlanningVacationPeriods(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "pos", "list_vacation_periods", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "list_vacation_periods", map[string]interface{}{"status": "success", "vacation_periods": items})
}

func (h *POSHandler) CreatePlanningVacationPeriod(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "create_vacation_period", map[string]string{"error": "missing_token"})
		return
	}

	var req PlanningVacationPeriodCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "pos", "create_vacation_period", models.ErrInvalidRequestBody)
		return
	}

	item, err := h.service.CreatePlanningVacationPeriod(r.Context(), token, req)
	if err != nil {
		models.SendErrorJSON(w, "pos", "create_vacation_period", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "pos", "create_vacation_period", map[string]interface{}{"status": "success", "vacation_period": item})
}

func (h *POSHandler) UpdatePlanningVacationPeriod(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "update_vacation_period", map[string]string{"error": "missing_token"})
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var req PlanningVacationPeriodUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "pos", "update_vacation_period", models.ErrInvalidRequestBody)
		return
	}

	item, err := h.service.UpdatePlanningVacationPeriod(r.Context(), token, id, req)
	if err != nil {
		models.SendErrorJSON(w, "pos", "update_vacation_period", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "update_vacation_period", map[string]interface{}{"status": "success", "vacation_period": item})
}

func (h *POSHandler) DeletePlanningVacationPeriod(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "delete_vacation_period", map[string]string{"error": "missing_token"})
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.service.DeletePlanningVacationPeriod(r.Context(), token, id); err != nil {
		models.SendErrorJSON(w, "pos", "delete_vacation_period", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "delete_vacation_period", map[string]interface{}{"status": "success"})
}

func (h *POSHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_settings", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	resp, err := h.service.GetMerchantSettings(ctx, token)
	if err != nil {
		models.SendErrorJSON(w, "pos", "get_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_settings", resp)
}

func (h *POSHandler) CreateHourOfOperation(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "create_hour_of_operation", map[string]string{"error": "missing_token"})
		return
	}

	var req models.POSHoursOfOperationPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "create_hour_of_operation", map[string]string{"error": "invalid_request"})
		return
	}

	created, err := h.service.CreateHourOfOperation(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "pos", "create_hour_of_operation", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "pos", "create_hour_of_operation", map[string]interface{}{
		"hour_of_operation": created,
	})
}

func (h *POSHandler) UpdateHourOfOperation(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "update_hour_of_operation", map[string]string{"error": "missing_token"})
		return
	}

	hourID := strings.TrimSpace(chi.URLParam(r, "hour_id"))
	if hourID == "" {
		models.SendJSON(w, http.StatusBadRequest, "pos", "update_hour_of_operation", map[string]string{"error": "missing_hour_id"})
		return
	}

	var req models.POSHoursOfOperationPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "update_hour_of_operation", map[string]string{"error": "invalid_request"})
		return
	}

	updated, err := h.service.UpdateHourOfOperation(r.Context(), token, hourID, &req)
	if err != nil {
		models.SendErrorJSON(w, "pos", "update_hour_of_operation", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "update_hour_of_operation", map[string]interface{}{
		"hour_of_operation": updated,
	})
}

func (h *POSHandler) DeleteHourOfOperation(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "delete_hour_of_operation", map[string]string{"error": "missing_token"})
		return
	}

	hourID := strings.TrimSpace(chi.URLParam(r, "hour_id"))
	if hourID == "" {
		models.SendJSON(w, http.StatusBadRequest, "pos", "delete_hour_of_operation", map[string]string{"error": "missing_hour_id"})
		return
	}

	if err := h.service.DeleteHourOfOperation(r.Context(), token, hourID); err != nil {
		models.SendErrorJSON(w, "pos", "delete_hour_of_operation", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "delete_hour_of_operation", map[string]string{
		"status": "1",
	})
}

// UploadMerchantLogo handles POST /pos/settings/logo — upload du logo général
// de l'établissement (identité merchant, distinct du logo Kiosk/ScanNOrder).
// Fichier de l'appareil (champ "file", allégé par imageopt) ou JSON
// {"image_url"} (banque d'images / « Mes images »), comme le logo Kiosk.
func (h *POSHandler) UploadMerchantLogo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fnName := "upload_merchant_logo"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "pos", fnName, models.ErrUnauthorized)
		return
	}

	settings, err := h.service.GetMerchantSettings(ctx, "")
	if err != nil {
		models.SendErrorJSON(w, "pos", fnName, err)
		return
	}
	oldURL := settings.Info.LogoURL

	r.Body = http.MaxBytesReader(w, r.Body, maxMerchantLogoBytes+multipartOverheadBytes)
	placed, err := h.picker.Place(w, r, imagepick.Request{
		Field:           "file",
		MultipartMemory: maxMerchantLogoBytes,
		OwnPrefixes:     []string{r2.MerchantKeyPrefix(user.MerchantID)},
		KeyFor:          func(ext string) string { return r2.GenerateMerchantLogoKey(user.MerchantID, ext) },
	})
	if err != nil {
		imagepick.Send(w, "pos", fnName, err)
		return
	}

	updated, err := h.service.SetLogoURL(ctx, user.MerchantID, placed.URL)
	if err != nil {
		models.SendErrorJSON(w, "pos", fnName, err)
		return
	}

	// Ancien logo supprimé une fois le nouveau enregistré (l'extension peut
	// changer quand l'allègement convertit le fichier).
	if oldURL != "" && h.r2Client != nil {
		if oldKey := h.r2Client.GetKeyFromURL(oldURL); oldKey != "" && oldKey != placed.Key {
			h.r2Client.DeleteFile(ctx, oldKey)
		}
	}

	models.SendJSON(w, http.StatusOK, "pos", fnName, map[string]string{"logo_url": updated.Info.LogoURL})
}
