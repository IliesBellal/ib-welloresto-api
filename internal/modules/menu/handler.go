package menu

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type MenuHandler struct {
	service *MenuService
}

func NewMenuHandler(s *MenuService) *MenuHandler {
	return &MenuHandler{service: s}
}

func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	// last_menu_update
	lastMenuParam := r.URL.Query().Get("last_menu_update")
	var lastMenu *time.Time

	if lastMenuParam != "" {
		if unix, err := strconv.ParseInt(lastMenuParam, 10, 64); err == nil {
			t := time.Unix(unix, 0).UTC()
			lastMenu = &t
		} else {
			log.Warn("Invalid last_menu_update param: " + lastMenuParam)
		}
	}

	log.Info("GetMenu - lastMenu: " + lastMenuParam)

	menu, err := h.service.GetMenu(ctx, token, lastMenu)
	if err != nil {
		// LOG SERVER SIDE
		log.Error("[ERROR] GetMenu error " + err.Error())

		// RETURN CLEAN ERROR TO CLIENT
		http.Error(
			w,
			`{"status":"-2","error":"internal error"}`,
			http.StatusInternalServerError,
		)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID:   "10",
		Data: menu,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *MenuHandler) GetUnitsOfMeasures(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	updated, err := h.service.GetUnitsOfMeasures(ctx, token)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, updated, 200)
}

func (h *MenuHandler) GetAttributes(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	updated, err := h.service.GetAttributes(ctx, token)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, updated, 200)
}

func (h *MenuHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	var req CreateProductPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	product, err := h.service.CreateProduct(r.Context(), token, &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"product": product,
	})
}

func (h *MenuHandler) GetProduct(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	product_id := chi.URLParam(r, "product_id")
	if product_id == "" {
		http.Error(w, "missing order_id", http.StatusBadRequest)
		return
	}

	product, err := h.service.GetProduct(r.Context(), token, product_id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"product": product,
	})
}

func (h *MenuHandler) SetComponentAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	componentID := chi.URLParam(r, "component_id")

	var req models.AvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	updated, err := h.service.SetComponentAvailability(ctx, token, componentID, req.Status)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	}, 200)
}

func (h *MenuHandler) SetProductAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	productID := chi.URLParam(r, "product_id")

	var req models.AvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	updated, err := h.service.SetProductAvailability(ctx, token, productID, req.Status)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	}, 200)
}

func (h *MenuHandler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	// 1. Auth & Validation basique
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		http.Error(w, "missing product_id", http.StatusBadRequest)
		return
	}

	// 2. Parsing du Body
	var payload ProductUpdatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	// 3. Appel Service
	err := h.service.UpdateProduct(r.Context(), token, productID, payload)
	if err != nil {
		// Tu peux affiner les codes d'erreur selon le type d'erreur retourné
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Réponse
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "1", "message": "product updated"})
}

func (h *MenuHandler) UpdateProductAttributes(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		http.Error(w, "missing product_id", http.StatusBadRequest)
		return
	}

	var payload ProductAttributesPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	err := h.service.UpdateProductAttributes(r.Context(), token, productID, payload.Configuration)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "1", "message": "attributes updated"})
}

func (h *MenuHandler) json(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *MenuHandler) errorJSON(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "0",
		"error":  err.Error(),
	})
}
