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
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get", map[string]string{"error": "missing_token"})
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

	menu, err := h.service.GetMenu(ctx, token, lastMenu)
	if err != nil {
		// LOG SERVER SIDE
		log.Error("[ERROR] GetMenu error " + err.Error())

		// RETURN CLEAN ERROR TO CLIENT
		models.SendJSON(w, http.StatusInternalServerError, "menu", "get", map[string]string{"error": "internal error"})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get", menu)
}

func (h *MenuHandler) GetUnitsOfMeasures(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_units_of_measures", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	updated, err := h.service.GetUnitsOfMeasures(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "menu", "get_units_of_measures", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_units_of_measures", updated)
}

func (h *MenuHandler) GetAttributes(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_attributes", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	updated, err := h.service.GetAttributes(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "menu", "get_attributes", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_attributes", updated)
}

func (h *MenuHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "create_product", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateProductPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_product", map[string]string{"error": "invalid_body"})
		return
	}

	product, err := h.service.CreateProduct(r.Context(), token, &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "menu", "create_product", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "create_product", map[string]interface{}{
		"product": product,
	})
}

func (h *MenuHandler) GetProduct(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_product", map[string]string{"error": "missing_token"})
		return
	}

	product_id := chi.URLParam(r, "product_id")
	if product_id == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "get_product", map[string]string{"error": "missing_parameter"})
		return
	}

	product, err := h.service.GetProduct(r.Context(), token, product_id)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "menu", "get_product", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_product", map[string]interface{}{
		"product": product,
	})
}

func (h *MenuHandler) SetComponentAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "set_component_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	componentID := chi.URLParam(r, "component_id")

	var req models.StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_component_availability", map[string]string{"error": "invalid_body"})
		return
	}

	updated, err := h.service.SetComponentAvailability(ctx, token, componentID, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "menu", "set_component_availability", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_component_availability", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *MenuHandler) SetProductAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "set_product_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	productID := chi.URLParam(r, "product_id")

	var req models.StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_product_availability", map[string]string{"error": "invalid_body"})
		return
	}

	updated, err := h.service.SetProductAvailability(ctx, token, productID, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "menu", "set_product_availability", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_product_availability", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *MenuHandler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	// 1. Auth & Validation basique
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_product", map[string]string{"error": "missing_token"})
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_product", map[string]string{"error": "missing_parameter"})
		return
	}

	// 2. Parsing du Body
	var payload ProductUpdatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_product", map[string]string{"error": "invalid_body"})
		return
	}

	// 3. Appel Service
	err := h.service.UpdateProduct(r.Context(), token, productID, payload)
	if err != nil {
		// Tu peux affiner les codes d'erreur selon le type d'erreur retourné
		models.SendJSON(w, http.StatusInternalServerError, "menu", "update_product", map[string]string{"error": err.Error()})
		return
	}

	// 4. Réponse
	models.SendJSON(w, http.StatusOK, "menu", "update_product", map[string]string{"status": "1", "message": "product updated"})
}

func (h *MenuHandler) UpdateProductAttributes(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_product_attributes", map[string]string{"error": "missing_token"})
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_product_attributes", map[string]string{"error": "missing_parameter"})
		return
	}

	var payload ProductAttributesPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_product_attributes", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.service.UpdateProductAttributes(r.Context(), token, productID, payload.Configuration)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "menu", "update_product_attributes", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_product_attributes", map[string]string{"status": "1", "message": "attributes updated"})
}
