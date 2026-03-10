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
		models.SendErrorJSON(w, "menu", "get", err)
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
		models.SendErrorJSON(w, "menu", "get_units_of_measures", err)
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
		models.SendErrorJSON(w, "menu", "get_attributes", err)
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
		models.SendErrorJSON(w, "menu", "create_product", err)
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
		models.SendErrorJSON(w, "menu", "get_product", err)
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

	_, err := h.service.SetComponentAvailability(ctx, token, componentID, req.Status)
	if err != nil {
		models.SendErrorJSON(w, "menu", "set_component_availability", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_component_availability", models.AvailabilityResponse{
		Status: "success",
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

	_, err := h.service.SetProductAvailability(ctx, token, productID, req.Status)
	if err != nil {
		models.SendErrorJSON(w, "menu", "set_product_availability", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_product_availability", models.AvailabilityResponse{
		Status: "1",
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
		models.SendErrorJSON(w, "menu", "update_product", err)
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
		models.SendErrorJSON(w, "menu", "update_product_attributes", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_product_attributes", map[string]string{"status": "1", "message": "attributes updated"})
}

// GetDeliverooMenu récupère le menu depuis l'API Deliveroo
func (h *MenuHandler) GetDeliverooMenu(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_deliveroo_menu", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	menu, err := h.service.GetDeliverooMenu(ctx, token)
	if err != nil {
		log.Error("[ERROR] GetDeliverooMenu error " + err.Error())
		models.SendErrorJSON(w, "menu", "get_deliveroo_menu", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_deliveroo_menu", menu)
}

// SyncDeliverooMenu synchronise le menu interne vers l'API Deliveroo
func (h *MenuHandler) SyncDeliverooMenu(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "sync_deliveroo_menu", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	if err := h.service.SyncDeliverooMenu(ctx, token); err != nil {
		log.Error("[ERROR] SyncDeliverooMenu error " + err.Error())
		models.SendErrorJSON(w, "menu", "sync_deliveroo_menu", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "sync_deliveroo_menu", map[string]string{"status": "1", "message": "menu synced to deliveroo"})
}

// GetUberEatsMenu récupère le menu depuis l'API Uber Eats
func (h *MenuHandler) GetUberEatsMenu(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_ubereats_menu", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	menu, err := h.service.GetUberEatsMenu(ctx, token)
	if err != nil {
		log.Error("[ERROR] GetUberEatsMenu error " + err.Error())
		models.SendErrorJSON(w, "menu", "get_ubereats_menu", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_ubereats_menu", menu)
}

// SyncUberEatsMenu synchronise le menu interne vers l'API Uber Eats
func (h *MenuHandler) SyncUberEatsMenu(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "sync_ubereats_menu", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	if err := h.service.SyncUberEatsMenu(ctx, token); err != nil {
		log.Error("[ERROR] SyncUberEatsMenu error " + err.Error())
		models.SendErrorJSON(w, "menu", "sync_ubereats_menu", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "sync_ubereats_menu", map[string]string{"status": "1", "message": "menu synced to uber eats"})
}

// SyncProductAllergens — PUT /menu/product/:product_id/allergens
// Full-sync: replaces all allergen associations for the given product.
func (h *MenuHandler) SyncProductAllergens(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "sync_product_allergens", map[string]string{"error": "missing_token"})
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "sync_product_allergens", map[string]string{"error": "missing_parameter"})
		return
	}

	var body struct {
		AllergenIDs []int `json:"allergen_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "sync_product_allergens", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.SyncProductAllergens(r.Context(), token, productID, body.AllergenIDs); err != nil {
		models.SendErrorJSON(w, "menu", "sync_product_allergens", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "sync_product_allergens", map[string]string{"status": "1", "message": "allergens updated"})
}

// BulkAssignTag — POST /menu/bulk/tags/assign
// Assigns a tag to multiple products (additive, does not remove existing tags).
func (h *MenuHandler) BulkAssignTag(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "bulk_assign_tag", map[string]string{"error": "missing_token"})
		return
	}

	var body struct {
		TagID      string   `json:"tag_id"`
		ProductIDs []string `json:"product_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_tag", map[string]string{"error": "invalid_body"})
		return
	}
	if len(body.ProductIDs) == 0 {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_tag", map[string]string{"error": "product_ids_required"})
		return
	}

	if err := h.service.BulkAssignTag(r.Context(), token, body.TagID, body.ProductIDs); err != nil {
		models.SendErrorJSON(w, "menu", "bulk_assign_tag", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "bulk_assign_tag", map[string]interface{}{
		"status":  "1",
		"message": "tag assigned",
		"updated": len(body.ProductIDs),
	})
}

// BulkAssignAllergen — POST /menu/bulk/allergens/assign
// Assigns an allergen to multiple products (additive, does not remove existing allergens).
func (h *MenuHandler) BulkAssignAllergen(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "bulk_assign_allergen", map[string]string{"error": "missing_token"})
		return
	}

	var body struct {
		AllergenID string   `json:"allergen_id"`
		ProductIDs []string `json:"product_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_allergen", map[string]string{"error": "invalid_body"})
		return
	}
	if len(body.ProductIDs) == 0 {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_allergen", map[string]string{"error": "product_ids_required"})
		return
	}

	if err := h.service.BulkAssignAllergen(r.Context(), token, body.AllergenID, body.ProductIDs); err != nil {
		models.SendErrorJSON(w, "menu", "bulk_assign_allergen", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "bulk_assign_allergen", map[string]interface{}{
		"status":  "1",
		"message": "allergen assigned",
		"updated": len(body.ProductIDs),
	})
}

// SyncProductTags — PUT /menu/product/:product_id/tags
// Full-sync: replaces all tag associations for the given product.
func (h *MenuHandler) SyncProductTags(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "sync_product_tags", map[string]string{"error": "missing_token"})
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "sync_product_tags", map[string]string{"error": "missing_parameter"})
		return
	}

	var body struct {
		TagIDs []int `json:"tag_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "sync_product_tags", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.SyncProductTags(r.Context(), token, productID, body.TagIDs); err != nil {
		models.SendErrorJSON(w, "menu", "sync_product_tags", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "sync_product_tags", map[string]string{"status": "1", "message": "tags updated"})
}

// GET /pos/tags
func (h *MenuHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "tags", "list", map[string]string{"error": "missing_token"})
		return
	}

	tagList, err := h.service.ListTags(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "tags", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "tags", "list", tagList)
}
