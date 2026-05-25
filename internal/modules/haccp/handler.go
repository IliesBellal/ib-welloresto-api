package haccp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) GetTemperatureZones(w http.ResponseWriter, r *http.Request) {
	zones, err := h.svc.ListTemperatureZones(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_temperature_zones", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_temperature_zones", map[string]interface{}{
		"status": "success",
		"zones":  zones,
	})
}

func (h *Handler) CreateTemperatureZone(w http.ResponseWriter, r *http.Request) {
	var req CreateZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "create_temperature_zone", map[string]string{"error": "invalid_request"})
		return
	}

	zone, err := h.svc.CreateTemperatureZone(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_temperature_zone", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_temperature_zone", map[string]interface{}{
		"status": "success",
		"zone":   zone,
	})
}

func (h *Handler) ReplaceTemperatureZone(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "replace_temperature_zone", map[string]string{"error": "missing_id"})
		return
	}

	var req ReplaceZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "replace_temperature_zone", map[string]string{"error": "invalid_request"})
		return
	}

	zone, err := h.svc.ReplaceTemperatureZone(r.Context(), id, req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "replace_temperature_zone", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "replace_temperature_zone", map[string]interface{}{
		"status": "success",
		"zone":   zone,
	})
}

func (h *Handler) DeleteTemperatureZone(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "delete_temperature_zone", map[string]string{"error": "missing_id"})
		return
	}

	if err := h.svc.DeleteTemperatureZone(r.Context(), id); err != nil {
		models.SendErrorJSON(w, "haccp", "delete_temperature_zone", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "delete_temperature_zone", map[string]interface{}{
		"status": "success",
	})
}

func (h *Handler) GetTemperatureReadings(w http.ResponseWriter, r *http.Request) {
	dateValue := strings.TrimSpace(r.URL.Query().Get("date"))
	zoneID := strings.TrimSpace(r.URL.Query().Get("zone_id"))

	readings, err := h.svc.ListTemperatureReadings(r.Context(), dateValue, zoneID)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_temperature_readings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_temperature_readings", map[string]interface{}{
		"status":               "success",
		"temperature_readings": readings,
	})
}

func (h *Handler) GetActivities(w http.ResponseWriter, r *http.Request) {
	params := ActivitiesListParams{
		Date:   strings.TrimSpace(r.URL.Query().Get("date")),
		Type:   strings.TrimSpace(r.URL.Query().Get("type")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
	}

	if rawPage := strings.TrimSpace(r.URL.Query().Get("page")); rawPage != "" {
		parsed, err := strconv.Atoi(rawPage)
		if err != nil {
			models.SendJSON(w, http.StatusBadRequest, "haccp", "get_activities", map[string]string{"error": "invalid_page"})
			return
		}
		params.Page = parsed
	}

	if rawPageSize := strings.TrimSpace(r.URL.Query().Get("page_size")); rawPageSize != "" {
		parsed, err := strconv.Atoi(rawPageSize)
		if err != nil {
			models.SendJSON(w, http.StatusBadRequest, "haccp", "get_activities", map[string]string{"error": "invalid_page_size"})
			return
		}
		params.PageSize = parsed
	}

	resp, err := h.svc.ListActivities(r.Context(), params)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_activities", err)
		return
	}

	filters := map[string]interface{}{
		"date": resp.Date,
	}
	if resp.Type != "" {
		filters["type"] = resp.Type
	}
	if params.Status != "" {
		filters["status"] = strings.ToLower(params.Status)
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_activities", map[string]interface{}{
		"status":     "success",
		"filters":    filters,
		"activities": resp.Activities,
		"pagination": map[string]interface{}{
			"page":        resp.Page,
			"page_size":   resp.PageSize,
			"total_items": resp.TotalItems,
			"total_pages": resp.TotalPages,
		},
	})
}

func (h *Handler) GetTemperatureSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "get_temperature_session", map[string]string{"error": "missing_id"})
		return
	}

	session, err := h.svc.GetTemperatureSession(r.Context(), id)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_temperature_session", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_temperature_session", map[string]interface{}{
		"status":              "success",
		"temperature_session": session,
	})
}

func (h *Handler) GetCleaningExecution(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "get_cleaning_execution", map[string]string{"error": "missing_id"})
		return
	}

	execution, err := h.svc.GetCleaningExecution(r.Context(), id)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_cleaning_execution", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_cleaning_execution", map[string]interface{}{
		"status":             "success",
		"cleaning_execution": execution,
	})
}

func (h *Handler) CreateTemperatureReadingsBatch(w http.ResponseWriter, r *http.Request) {
	var req BatchCreateReadingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "create_temperature_readings_batch", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.svc.CreateTemperatureReadingsBatch(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_temperature_readings_batch", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_temperature_readings_batch", map[string]interface{}{
		"status":               "success",
		"session_id":           resp.SessionID,
		"temperature_readings": resp.Readings,
	})
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.GetSettings(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_settings", map[string]interface{}{
		"status":   "success",
		"settings": settings,
	})
}

func (h *Handler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var req HACCPSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "put_settings", map[string]string{"error": "invalid_request"})
		return
	}

	settings, err := h.svc.ReplaceSettings(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "put_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "put_settings", map[string]interface{}{
		"status":   "success",
		"settings": settings,
	})
}

func (h *Handler) UploadHACCP(w http.ResponseWriter, r *http.Request) {
	const maxSize = 5 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "uploads", "haccp", map[string]string{"error": "file_too_large_or_invalid"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "uploads", "haccp", map[string]string{"error": "missing_file_field"})
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}
	if !r2.ValidateImageType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, "uploads", "haccp", map[string]string{"error": "invalid_image_type"})
		return
	}

	url, err := h.svc.UploadHACCPFile(r.Context(), contentType, file)
	if err != nil {
		models.SendErrorJSON(w, "uploads", "haccp", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "uploads", "haccp", map[string]interface{}{
		"status": "success",
		"url":    url,
	})
}

func (h *Handler) GetCleaningTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.svc.ListCleaningTasks(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_cleaning_tasks", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_cleaning_tasks", map[string]interface{}{
		"status":         "success",
		"cleaning_tasks": tasks,
	})
}

func (h *Handler) CreateCleaningTask(w http.ResponseWriter, r *http.Request) {
	var req CreateCleaningTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "create_cleaning_task", map[string]string{"error": "invalid_request"})
		return
	}

	task, err := h.svc.CreateCleaningTask(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_task", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_cleaning_task", map[string]interface{}{
		"status":        "success",
		"cleaning_task": task,
	})
}

func (h *Handler) UpdateCleaningTask(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "update_cleaning_task", map[string]string{"error": "missing_id"})
		return
	}

	var req UpdateCleaningTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "update_cleaning_task", map[string]string{"error": "invalid_request"})
		return
	}

	task, err := h.svc.UpdateCleaningTask(r.Context(), id, req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "update_cleaning_task", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "update_cleaning_task", map[string]interface{}{
		"status":        "success",
		"cleaning_task": task,
	})
}

func (h *Handler) DeleteCleaningTask(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "delete_cleaning_task", map[string]string{"error": "missing_id"})
		return
	}

	if err := h.svc.DeleteCleaningTask(r.Context(), id); err != nil {
		models.SendErrorJSON(w, "haccp", "delete_cleaning_task", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "delete_cleaning_task", map[string]interface{}{
		"status": "success",
	})
}

func (h *Handler) GetCleaningExecutions(w http.ResponseWriter, r *http.Request) {
	params := CleaningExecutionsListParams{
		TaskID: strings.TrimSpace(r.URL.Query().Get("task_id")),
	}

	if rawPage := strings.TrimSpace(r.URL.Query().Get("page")); rawPage != "" {
		parsed, err := strconv.Atoi(rawPage)
		if err != nil {
			models.SendJSON(w, http.StatusBadRequest, "haccp", "get_cleaning_executions", map[string]string{"error": "invalid_page"})
			return
		}
		params.Page = parsed
	}

	if rawPageSize := strings.TrimSpace(r.URL.Query().Get("page_size")); rawPageSize != "" {
		parsed, err := strconv.Atoi(rawPageSize)
		if err != nil {
			models.SendJSON(w, http.StatusBadRequest, "haccp", "get_cleaning_executions", map[string]string{"error": "invalid_page_size"})
			return
		}
		params.PageSize = parsed
	}

	resp, err := h.svc.ListCleaningExecutions(r.Context(), params)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_cleaning_executions", err)
		return
	}

	pagination := map[string]interface{}{
		"page":        resp.Page,
		"page_size":   resp.PageSize,
		"total_items": resp.TotalItems,
		"total_pages": resp.TotalPages,
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_cleaning_executions", map[string]interface{}{
		"status":              "success",
		"cleaning_executions": resp.CleaningExecutions,
		"pagination":          pagination,
	})
}

func (h *Handler) CreateCleaningExecution(w http.ResponseWriter, r *http.Request) {
	var req CreateCleaningExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "create_cleaning_execution", map[string]string{"error": "invalid_request"})
		return
	}

	exec, err := h.svc.CreateCleaningExecution(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_execution", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_cleaning_execution", map[string]interface{}{
		"status":             "success",
		"cleaning_execution": exec,
	})
}

func (h *Handler) CreateGoodsReceipt(w http.ResponseWriter, r *http.Request) {
	var req CreateGoodsReceiptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "haccp", "create_goods_receipt", map[string]string{"error": "invalid_request"})
		return
	}

	receipt, err := h.svc.CreateGoodsReceipt(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_goods_receipt", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_goods_receipt", map[string]interface{}{
		"status":        "success",
		"goods_receipt": receipt,
	})
}
