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

func (h *Handler) GetCorrectiveActions(w http.ResponseWriter, r *http.Request) {
	actions, err := h.svc.ListCorrectiveActions(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_corrective_actions", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_corrective_actions", map[string]interface{}{
		"status":             "success",
		"corrective_actions": actions,
	})
}

func (h *Handler) CreateTemperatureZone(w http.ResponseWriter, r *http.Request) {
	var req CreateZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "haccp", "create_temperature_zone", models.ErrInvalidRequestBody)
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
		models.SendErrorJSON(w, "haccp", "replace_temperature_zone", models.ErrMissingResourceID)
		return
	}

	var req ReplaceZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "haccp", "replace_temperature_zone", models.ErrInvalidRequestBody)
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
		models.SendErrorJSON(w, "haccp", "delete_temperature_zone", models.ErrMissingResourceID)
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
			models.SendErrorJSON(w, "haccp", "get_activities", models.ErrInvalidPage)
			return
		}
		params.Page = parsed
	}

	if rawPageSize := strings.TrimSpace(r.URL.Query().Get("page_size")); rawPageSize != "" {
		parsed, err := strconv.Atoi(rawPageSize)
		if err != nil {
			models.SendErrorJSON(w, "haccp", "get_activities", models.ErrInvalidPageSize)
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

func (h *Handler) GetHub(w http.ResponseWriter, r *http.Request) {
	dateValue := strings.TrimSpace(r.URL.Query().Get("date"))

	resp, err := h.svc.GetHub(r.Context(), dateValue)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_hub", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_hub", map[string]interface{}{
		"status":       "success",
		"date":         resp.Date,
		"generated_at": resp.GeneratedAt,
		"hub":          resp.Hub,
	})
}

func (h *Handler) GetTemperatureSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendErrorJSON(w, "haccp", "get_temperature_session", models.ErrMissingResourceID)
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

func (h *Handler) GetCleaningSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendErrorJSON(w, "haccp", "get_cleaning_session", models.ErrMissingResourceID)
		return
	}

	session, err := h.svc.GetCleaningSession(r.Context(), id)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_cleaning_session", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_cleaning_session", map[string]interface{}{
		"status":           "success",
		"cleaning_session": session,
	})
}

func (h *Handler) CreateTemperatureReadingsBatch(w http.ResponseWriter, r *http.Request) {
	var req BatchCreateReadingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "haccp", "create_temperature_readings_batch", models.ErrInvalidRequestBody)
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
		models.SendErrorJSON(w, "haccp", "put_settings", models.ErrInvalidRequestBody)
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
		models.SendErrorJSON(w, "uploads", "haccp", models.ErrUploadFileTooLargeOrInvalid)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		models.SendErrorJSON(w, "uploads", "haccp", models.ErrUploadFileMissing)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}
	if !r2.ValidateImageType(contentType) {
		models.SendErrorJSON(w, "uploads", "haccp", models.ErrInvalidImageType)
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

func (h *Handler) GetCleaningZones(w http.ResponseWriter, r *http.Request) {
	zones, err := h.svc.ListCleaningZones(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_cleaning_zones", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_cleaning_zones", map[string]interface{}{
		"status":         "success",
		"cleaning_zones": zones,
	})
}

func (h *Handler) CreateCleaningZone(w http.ResponseWriter, r *http.Request) {
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_zone", models.ErrInvalidRequestBody)
		return
	}

	req := CreateCleaningZoneRequest{Name: extractCleaningZoneName(raw)}

	zone, err := h.svc.CreateCleaningZone(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_zone", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_cleaning_zone", map[string]interface{}{
		"status":        "success",
		"cleaning_zone": zone,
	})
}

func (h *Handler) UpdateCleaningZone(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendErrorJSON(w, "haccp", "update_cleaning_zone", models.ErrMissingResourceID)
		return
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		models.SendErrorJSON(w, "haccp", "update_cleaning_zone", models.ErrInvalidRequestBody)
		return
	}

	req := UpdateCleaningZoneRequest{Name: extractCleaningZoneName(raw)}

	zone, err := h.svc.UpdateCleaningZone(r.Context(), id, req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "update_cleaning_zone", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "update_cleaning_zone", map[string]interface{}{
		"status":        "success",
		"cleaning_zone": zone,
	})
}

func (h *Handler) DeleteCleaningZone(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendErrorJSON(w, "haccp", "delete_cleaning_zone", models.ErrMissingResourceID)
		return
	}

	if err := h.svc.DeleteCleaningZone(r.Context(), id); err != nil {
		models.SendErrorJSON(w, "haccp", "delete_cleaning_zone", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "delete_cleaning_zone", map[string]interface{}{
		"status": "success",
	})
}

func (h *Handler) GetCleaningSurfaces(w http.ResponseWriter, r *http.Request) {
	zoneID := strings.TrimSpace(r.URL.Query().Get("zone_id"))
	surfaces, err := h.svc.ListCleaningSurfaces(r.Context(), zoneID)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_cleaning_surfaces", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_cleaning_surfaces", map[string]interface{}{
		"status":            "success",
		"cleaning_surfaces": surfaces,
	})
}

func (h *Handler) CreateCleaningSurface(w http.ResponseWriter, r *http.Request) {
	var req CreateCleaningSurfaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_surface", models.ErrInvalidRequestBody)
		return
	}

	surface, err := h.svc.CreateCleaningSurface(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_surface", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_cleaning_surface", map[string]interface{}{
		"status":           "success",
		"cleaning_surface": surface,
	})
}

func (h *Handler) UpdateCleaningSurface(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendErrorJSON(w, "haccp", "update_cleaning_surface", models.ErrMissingResourceID)
		return
	}

	var req UpdateCleaningSurfaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "haccp", "update_cleaning_surface", models.ErrInvalidRequestBody)
		return
	}

	surface, err := h.svc.UpdateCleaningSurface(r.Context(), id, req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "update_cleaning_surface", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "update_cleaning_surface", map[string]interface{}{
		"status":           "success",
		"cleaning_surface": surface,
	})
}

func (h *Handler) DeleteCleaningSurface(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		models.SendErrorJSON(w, "haccp", "delete_cleaning_surface", models.ErrMissingResourceID)
		return
	}

	if err := h.svc.DeleteCleaningSurface(r.Context(), id); err != nil {
		models.SendErrorJSON(w, "haccp", "delete_cleaning_surface", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "delete_cleaning_surface", map[string]interface{}{
		"status": "success",
	})
}

func (h *Handler) GetCleaningSessions(w http.ResponseWriter, r *http.Request) {
	params := CleaningSessionsListParams{
		Date:   strings.TrimSpace(r.URL.Query().Get("date")),
		ZoneID: strings.TrimSpace(r.URL.Query().Get("zone_id")),
	}

	sessions, err := h.svc.ListCleaningSessions(r.Context(), params)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_cleaning_sessions", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_cleaning_sessions", map[string]interface{}{
		"status":            "success",
		"cleaning_sessions": sessions,
	})
}

func (h *Handler) CreateCleaningSession(w http.ResponseWriter, r *http.Request) {
	var req CreateCleaningSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_session", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.svc.CreateCleaningSession(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "haccp", "create_cleaning_session", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "haccp", "create_cleaning_session", map[string]interface{}{
		"status":              "success",
		"session_id":          resp.SessionID,
		"cleaning_executions": resp.Executions,
	})
}

func (h *Handler) GetHaccpComponents(w http.ResponseWriter, r *http.Request) {
	categories, err := h.svc.GetHaccpComponents(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "haccp", "get_haccp_components", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "haccp", "get_haccp_components", map[string]interface{}{
		"status":     "success",
		"categories": categories,
	})
}

func (h *Handler) CreateGoodsReceipt(w http.ResponseWriter, r *http.Request) {
	var req CreateGoodsReceiptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "haccp", "create_goods_receipt", models.ErrInvalidRequestBody)
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

func extractCleaningZoneName(raw map[string]interface{}) string {
	keys := []string{"name", "zone_name", "zone", "label", "title"}
	for _, key := range keys {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					return trimmed
				}
			}
		}
	}

	if zone, ok := raw["zone"].(map[string]interface{}); ok {
		if v, ok := zone["name"].(string); ok {
			return strings.TrimSpace(v)
		}
	}

	return ""
}
