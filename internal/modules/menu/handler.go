package menu

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	translationModule "welloresto-api/internal/modules/translation"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// validLangCode accepts only lowercase ASCII letters, 2–5 characters (e.g. "en", "fr", "zh-hans" excluded intentionally).
var validLangCode = regexp.MustCompile(`^[a-z]{2,5}$`)

type MenuHandler struct {
	service            *MenuService
	r2Client           *r2.Client
	translationRepo    *translationModule.Repository
	translationService *translationModule.Service
}

type TranslationLanguagePatchPayload struct {
	LangCode string `json:"lang_code"`
	Enabled  bool   `json:"enabled"`
}

func NewMenuHandler(s *MenuService, r2Client *r2.Client, translationRepo *translationModule.Repository, translationService *translationModule.Service) *MenuHandler {
	return &MenuHandler{
		service:            s,
		r2Client:           r2Client,
		translationRepo:    translationRepo,
		translationService: translationService,
	}
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

	// ---- TRANSLATION (optional enhancement — never blocks the response) ----
	lang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if lang != "" && validLangCode.MatchString(lang) && h.translationRepo != nil && h.translationService != nil {
		user := middleware.GetUser(r)
		if user != nil {
			enabled, checkErr := h.translationRepo.IsLanguageEnabledForMerchant(ctx, user.MerchantID, lang)
			if checkErr != nil {
				log.Warn("translation language check failed",
					zap.String("merchant_id", user.MerchantID),
					zap.String("lang", lang),
					zap.Error(checkErr),
				)
			} else if enabled {
				translated, translateErr := h.translationService.TranslateMenu(ctx, user.MerchantID, lang, *menu)
				if translateErr != nil {
					log.Warn("translation failed, serving original menu",
						zap.String("merchant_id", user.MerchantID),
						zap.String("lang", lang),
						zap.Error(translateErr),
					)
				} else {
					models.SendJSON(w, http.StatusOK, "menu", "get", &translated)
					return
				}
			}
		}
	}
	// ---- END TRANSLATION ----

	models.SendJSON(w, http.StatusOK, "menu", "get", menu)
}

func (h *MenuHandler) GetTranslationLanguages(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_translation_langs", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	if h.translationService == nil {
		models.SendJSON(w, http.StatusOK, "menu", "get_translation_langs", []translationModule.MerchantLanguage{})
		return
	}

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "menu", "get_translation_langs", models.ErrUnauthorized)
		return
	}

	langs, err := h.translationService.ListMerchantLanguages(ctx, user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "menu", "get_translation_langs", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_translation_langs", langs)
}

func (h *MenuHandler) PatchTranslationLanguages(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "patch_translation_langs", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	if h.translationService == nil {
		models.SendErrorJSON(w, "menu", "patch_translation_langs", models.ErrInternalServerError)
		return
	}

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "menu", "patch_translation_langs", models.ErrUnauthorized)
		return
	}

	var payload TranslationLanguagePatchPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "patch_translation_langs", map[string]string{"error": "invalid_body"})
		return
	}

	langCode := strings.ToLower(strings.TrimSpace(payload.LangCode))
	if !validLangCode.MatchString(langCode) {
		models.SendJSON(w, http.StatusBadRequest, "menu", "patch_translation_langs", map[string]string{"error": "invalid_lang_code"})
		return
	}

	if err := h.translationService.SetMerchantLanguage(ctx, user.MerchantID, langCode, payload.Enabled); err != nil {
		log.Error("[ERROR] PatchTranslationLanguages error: " + err.Error())
		models.SendErrorJSON(w, "menu", "patch_translation_langs", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "patch_translation_langs", map[string]interface{}{
		"status":    "success",
		"lang_code": langCode,
		"enabled":   payload.Enabled,
	})
}

func (h *MenuHandler) GetAllProducts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_all_products", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	products, err := h.service.GetAllProducts(ctx, token)
	if err != nil {
		log.Error("[ERROR] GetAllProducts error " + err.Error())
		models.SendErrorJSON(w, "menu", "get_all_products", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_all_products", products)
}

func (h *MenuHandler) GetAllComponents(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_all_components", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	components, err := h.service.GetAllComponents(ctx, token)
	if err != nil {
		log.Error("[ERROR] GetAllComponents error " + err.Error())
		models.SendErrorJSON(w, "menu", "get_all_components", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_all_components", components)
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

func (h *MenuHandler) GetAttribute(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_attribute", map[string]string{"error": "missing_token"})
		return
	}

	attributeID := chi.URLParam(r, "attribute_id")
	if attributeID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "get_attribute", map[string]string{"error": "missing_parameter"})
		return
	}

	ctx := r.Context()

	attribute, err := h.service.GetAttribute(ctx, token, attributeID)
	if err != nil {
		models.SendErrorJSON(w, "menu", "get_attribute", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_attribute", attribute)
}

func (h *MenuHandler) CreateAttribute(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "create_attribute", map[string]string{"error": "missing_token"})
		return
	}

	var payload UpdateAttributePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_attribute", map[string]string{"error": "invalid_body"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	attributeID, err := h.service.CreateAttribute(ctx, token, &payload)
	if err != nil {
		log.Error("[ERROR] CreateAttribute error: " + err.Error())
		models.SendErrorJSON(w, "menu", "create_attribute", err)
		return
	}

	// Fetch the complete attribute object to return
	attribute, err := h.service.GetAttribute(ctx, token, attributeID)
	if err != nil {
		log.Error("[ERROR] GetAttribute error after creation: " + err.Error())
		models.SendErrorJSON(w, "menu", "create_attribute", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "menu", "create_attribute", map[string]interface{}{
		"status":    "success",
		"message":   "attribute_created",
		"attribute": attribute,
	})
}

func (h *MenuHandler) UpdateAttribute(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_attribute", map[string]string{"error": "missing_token"})
		return
	}

	attributeID := chi.URLParam(r, "attribute_id")
	if attributeID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_attribute", map[string]string{"error": "missing_parameter"})
		return
	}

	var payload UpdateAttributePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_attribute", map[string]string{"error": "invalid_body"})
		return
	}

	err := h.service.UpdateAttribute(r.Context(), token, attributeID, &payload)
	if err != nil {
		models.SendErrorJSON(w, "menu", "update_attribute", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_attribute", map[string]string{"status": "success", "message": "attribute updated"})
}

func (h *MenuHandler) DeleteAttribute(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "delete_attribute", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	attributeID := chi.URLParam(r, "attribute_id")
	if attributeID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "delete_attribute", map[string]string{"error": "missing_parameter"})
		return
	}

	log := logger.FromContext(ctx)

	err := h.service.DeleteAttribute(ctx, token, attributeID)
	if err != nil {
		log.Error("[ERROR] DeleteAttribute error: " + err.Error())
		models.SendErrorJSON(w, "menu", "delete_attribute", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "delete_attribute", map[string]string{
		"status":  "success",
		"message": "attribute_disabled",
	})
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

func (h *MenuHandler) GetComponent(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_component", map[string]string{"error": "missing_token"})
		return
	}

	componentID := chi.URLParam(r, "component_id")
	if componentID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "get_component", map[string]string{"error": "missing_parameter"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	component, err := h.service.GetComponent(ctx, token, componentID)
	if err != nil {
		log.Error("[ERROR] GetComponent error: " + err.Error())
		if err.Error() == "component_not_found" {
			models.SendJSON(w, http.StatusNotFound, "menu", "get_component", map[string]string{"error": "component_not_found"})
			return
		}
		models.SendErrorJSON(w, "menu", "get_component", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_component", component)
}

func (h *MenuHandler) CreateComponent(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "create_component", map[string]string{"error": "missing_token"})
		return
	}

	var req UpdateComponentPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_component", map[string]string{"error": "invalid_body", "message": err.Error()})
		return
	}

	// Validation des champs requis
	if req.Name == nil || *req.Name == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_component", map[string]string{"error": "name_required"})
		return
	}
	if req.CategoryID == nil || *req.CategoryID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_component", map[string]string{"error": "category_id_required"})
		return
	}
	if req.UnitID == nil || *req.UnitID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_component", map[string]string{"error": "unit_id_required"})
		return
	}
	if req.Price == nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_component", map[string]string{"error": "price_required"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	componentID, err := h.service.CreateComponent(ctx, token, &req)
	if err != nil {
		log.Error("[ERROR] CreateComponent error: " + err.Error())
		models.SendErrorJSON(w, "menu", "create_component", err)
		return
	}

	// Récupérer le composant créé pour retourner l'objet complet
	component, err := h.service.GetComponent(ctx, token, componentID)
	if err != nil {
		log.Error("[ERROR] GetComponent after create error: " + err.Error())
		models.SendErrorJSON(w, "menu", "create_component", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "menu", "create_component", map[string]interface{}{
		"status":    "success",
		"message":   "component_created",
		"component": component,
	})
}

func (h *MenuHandler) CreateComponentCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "create_component_category", map[string]string{"error": "missing_token"})
		return
	}

	var req UpsertComponentCategoryPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_component_category", map[string]string{"error": "invalid_body"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	categoryID, err := h.service.CreateComponentCategory(ctx, token, &req)
	if err != nil {
		log.Error("[ERROR] CreateComponentCategory error: " + err.Error())
		models.SendErrorJSON(w, "menu", "create_component_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "create_component_category", map[string]interface{}{
		"category_id": categoryID,
		"status":      "1",
		"message":     "component_category_created",
	})
}

func (h *MenuHandler) CreateProductCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "create_product_category", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateProductCategoryPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_product_category", map[string]string{"error": "invalid_body"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	categoryID, err := h.service.CreateProductCategory(ctx, token, &req)
	if err != nil {
		log.Error("[ERROR] CreateProductCategory error: " + err.Error())
		models.SendErrorJSON(w, "menu", "create_product_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "create_product_category", map[string]interface{}{
		"category_id": categoryID,
		"status":      "success",
		"message":     "product_category_created",
	})
}

func (h *MenuHandler) GetMarketingCategories(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "get_marketing_categories", map[string]string{"error": "missing_token"})
		return
	}

	list, err := h.service.GetMarketingCategories(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "menu", "get_marketing_categories", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "get_marketing_categories", map[string]interface{}{
		"categories": list,
	})
}

func (h *MenuHandler) CreateMarketingCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "create_marketing_category", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateMarketingCategoryPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "create_marketing_category", map[string]string{"error": "invalid_body"})
		return
	}

	categoryID, err := h.service.CreateMarketingCategory(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "menu", "create_marketing_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "create_marketing_category", map[string]interface{}{
		"category_id": categoryID,
		"status":      "success",
		"message":     "marketing_category_created",
	})
}

func (h *MenuHandler) UpdateMarketingCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_marketing_category", map[string]string{"error": "missing_token"})
		return
	}

	categoryID := chi.URLParam(r, "category_id")
	if strings.TrimSpace(categoryID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_marketing_category", map[string]string{"error": "missing_category_id"})
		return
	}

	var req UpdateMarketingCategoryPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_marketing_category", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.UpdateMarketingCategory(r.Context(), token, categoryID, req); err != nil {
		models.SendErrorJSON(w, "menu", "update_marketing_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_marketing_category", map[string]interface{}{
		"status":  "success",
		"message": "marketing_category_updated",
	})
}

func (h *MenuHandler) DeleteMarketingCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "delete_marketing_category", map[string]string{"error": "missing_token"})
		return
	}

	categoryID := chi.URLParam(r, "category_id")
	if strings.TrimSpace(categoryID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "delete_marketing_category", map[string]string{"error": "missing_category_id"})
		return
	}

	if err := h.service.DeleteMarketingCategory(r.Context(), token, categoryID); err != nil {
		models.SendErrorJSON(w, "menu", "delete_marketing_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "delete_marketing_category", map[string]interface{}{
		"status":  "success",
		"message": "marketing_category_disabled",
	})
}

func (h *MenuHandler) UpdateMarketingCategoriesDisplayOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_marketing_categories_display_order", map[string]string{"error": "missing_token"})
		return
	}

	var req UpdateMarketingCategoriesDisplayOrderPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_marketing_categories_display_order", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.UpdateMarketingCategoriesDisplayOrder(r.Context(), token, req.CategoryIDs); err != nil {
		models.SendErrorJSON(w, "menu", "update_marketing_categories_display_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_marketing_categories_display_order", map[string]interface{}{
		"status":  "success",
		"message": "marketing_categories_display_order_updated",
	})
}

func (h *MenuHandler) BulkAssignProductsToMarketingCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "bulk_assign_products_to_marketing_category", map[string]string{"error": "missing_token"})
		return
	}

	categoryID := chi.URLParam(r, "category_id")
	if strings.TrimSpace(categoryID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_products_to_marketing_category", map[string]string{"error": "missing_category_id"})
		return
	}

	var body BulkAssignProductsPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_products_to_marketing_category", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.BulkAssignProductsToMarketingCategory(r.Context(), token, categoryID, body.ProductIDs); err != nil {
		models.SendErrorJSON(w, "menu", "bulk_assign_products_to_marketing_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "bulk_assign_products_to_marketing_category", map[string]interface{}{
		"status":   "success",
		"category": categoryID,
		"products": len(body.ProductIDs),
	})
}

func (h *MenuHandler) AssignProductMarketingCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "assign_product_marketing_category", map[string]string{"error": "missing_token"})
		return
	}

	productID := chi.URLParam(r, "product_id")
	if strings.TrimSpace(productID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "assign_product_marketing_category", map[string]string{"error": "missing_product_id"})
		return
	}

	var req AssignProductMarketingCategoryPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "assign_product_marketing_category", map[string]string{"error": "invalid_body"})
		return
	}

	if strings.TrimSpace(req.CategoryID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "assign_product_marketing_category", map[string]string{"error": "missing_category_id"})
		return
	}

	if err := h.service.AssignProductMarketingCategory(r.Context(), token, productID, req.CategoryID); err != nil {
		models.SendErrorJSON(w, "menu", "assign_product_marketing_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "assign_product_marketing_category", map[string]interface{}{
		"status":      "success",
		"product_id":  productID,
		"category_id": req.CategoryID,
	})
}

func (h *MenuHandler) UnassignProductMarketingCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "unassign_product_marketing_category", map[string]string{"error": "missing_token"})
		return
	}

	productID := chi.URLParam(r, "product_id")
	if strings.TrimSpace(productID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "unassign_product_marketing_category", map[string]string{"error": "missing_product_id"})
		return
	}

	if err := h.service.UnassignProductMarketingCategory(r.Context(), token, productID); err != nil {
		models.SendErrorJSON(w, "menu", "unassign_product_marketing_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "unassign_product_marketing_category", map[string]interface{}{
		"status":     "success",
		"product_id": productID,
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

func (h *MenuHandler) SetComponentStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "set_component_status", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	componentID := chi.URLParam(r, "component_id")

	var req models.StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_component_status", map[string]string{"error": "invalid_body"})
		return
	}

	_, err := h.service.SetComponentStatus(ctx, token, componentID, req.Status)
	if err != nil {
		models.SendErrorJSON(w, "menu", "set_component_status", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_component_status", models.AvailabilityResponse{
		Status: "success",
	})
}

func (h *MenuHandler) SetProductStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "set_product_status", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	productID := chi.URLParam(r, "product_id")

	var req models.StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_product_status", map[string]string{"error": "invalid_body"})
		return
	}

	_, err := h.service.SetProductStatus(ctx, token, productID, req.Status)
	if err != nil {
		models.SendErrorJSON(w, "menu", "set_product_status", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_product_status", models.AvailabilityResponse{
		Status: "success",
	})
}

func (h *MenuHandler) UpdateDisplayOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_display_order", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var payload DisplayOrderPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_display_order", map[string]string{"error": "invalid_body"})
		return
	}

	log := logger.FromContext(ctx)

	err := h.service.UpdateDisplayOrder(ctx, token, payload)
	if err != nil {
		log.Error("[ERROR] UpdateDisplayOrder error: " + err.Error())
		models.SendErrorJSON(w, "menu", "update_display_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_display_order", map[string]string{
		"status":  "success",
		"message": "display_order_updated",
	})
}

func (h *MenuHandler) UpdateProductCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_product_category", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	categoryID := chi.URLParam(r, "category_id")
	if categoryID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_product_category", map[string]string{"error": "missing_parameter"})
		return
	}

	var payload UpsertComponentCategoryPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_product_category", map[string]string{"error": "invalid_body"})
		return
	}

	log := logger.FromContext(ctx)

	err := h.service.UpdateProductCategory(ctx, token, categoryID, payload)
	if err != nil {
		log.Error("[ERROR] UpdateProductCategory error: " + err.Error())
		models.SendErrorJSON(w, "menu", "update_product_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_product_category", map[string]string{
		"status":  "success",
		"message": "product_category_updated",
	})
}

func (h *MenuHandler) DeleteProductCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "delete_product_category", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	categoryID := chi.URLParam(r, "category_id")
	if categoryID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "delete_product_category", map[string]string{"error": "missing_parameter"})
		return
	}

	log := logger.FromContext(ctx)

	err := h.service.DeleteProductCategory(ctx, token, categoryID)
	if err != nil {
		log.Error("[ERROR] DeleteProductCategory error: " + err.Error())
		models.SendErrorJSON(w, "menu", "delete_product_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "delete_product_category", map[string]string{
		"status":  "success",
		"message": "product_category_disabled",
	})
}

func (h *MenuHandler) DeleteComponentCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "delete_component_category", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	categoryID := chi.URLParam(r, "category_id")
	if categoryID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "delete_component_category", map[string]string{"error": "missing_parameter"})
		return
	}

	log := logger.FromContext(ctx)

	err := h.service.DeleteComponentCategory(ctx, token, categoryID)
	if err != nil {
		log.Error("[ERROR] DeleteComponentCategory error: " + err.Error())
		models.SendErrorJSON(w, "menu", "delete_component_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "delete_component_category", map[string]string{
		"status":  "success",
		"message": "component_category_disabled",
	})
}

func (h *MenuHandler) UpdateComponent(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "update_component", map[string]string{"error": "missing_token"})
		return
	}

	componentID := chi.URLParam(r, "component_id")
	if componentID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "update_component", map[string]string{"error": "missing_parameter"})
		return
	}

	var payload UpdateComponentPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendErrorJSON(w, "menu", "update_component", err)
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	err := h.service.UpdateComponent(ctx, token, componentID, &payload)
	if err != nil {
		log.Error("[ERROR] UpdateComponent error: " + err.Error())
		models.SendErrorJSON(w, "menu", "update_component", err)
		return
	}

	// Récupérer le composant mis à jour pour retourner l'objet complet
	component, err := h.service.GetComponent(ctx, token, componentID)
	if err != nil {
		log.Error("[ERROR] GetComponent after update error: " + err.Error())
		models.SendErrorJSON(w, "menu", "update_component", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "update_component", map[string]interface{}{
		"status":    "success",
		"message":   "component updated",
		"component": component,
	})
}

func (h *MenuHandler) DeleteComponent(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "delete_component", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	componentID := chi.URLParam(r, "component_id")
	if componentID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "delete_component", map[string]string{"error": "missing_parameter"})
		return
	}

	log := logger.FromContext(ctx)

	err := h.service.DeleteComponent(ctx, token, componentID)
	if err != nil {
		log.Error("[ERROR] DeleteComponent error: " + err.Error())
		models.SendErrorJSON(w, "menu", "delete_component", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "delete_component", map[string]string{
		"status":  "success",
		"message": "component_disabled",
	})
}

func (h *MenuHandler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "delete_product", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "delete_product", map[string]string{"error": "missing_parameter"})
		return
	}

	log := logger.FromContext(ctx)

	err := h.service.DeleteProduct(ctx, token, productID)
	if err != nil {
		log.Error("[ERROR] DeleteProduct error: " + err.Error())
		models.SendErrorJSON(w, "menu", "delete_product", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "delete_product", map[string]string{
		"status":  "success",
		"message": "product_disabled",
	})
}

func (h *MenuHandler) SetProductCategoryAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "set_category_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	categoryID := chi.URLParam(r, "category_id")
	if categoryID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_category_availability", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_category_availability", map[string]string{"error": "invalid_body"})
		return
	}

	log := logger.FromContext(ctx)

	_, err := h.service.SetProductCategoryAvailability(ctx, token, categoryID, req.Status)
	if err != nil {
		log.Error("[ERROR] SetProductCategoryAvailability error: " + err.Error())
		models.SendErrorJSON(w, "menu", "set_category_availability", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_category_availability", models.AvailabilityResponse{
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
	if productID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_product_availability", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "set_product_availability", map[string]string{"error": "invalid_body"})
		return
	}

	log := logger.FromContext(ctx)

	_, err := h.service.SetProductAvailability(ctx, token, productID, req.Status)
	if err != nil {
		log.Error("[ERROR] SetProductAvailability error: " + err.Error())
		models.SendErrorJSON(w, "menu", "set_product_availability", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "set_product_availability", models.AvailabilityResponse{
		Status: "success",
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
		models.SendErrorJSON(w, "menu", "update_product", err)
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
	models.SendJSON(w, http.StatusOK, "menu", "update_product", map[string]string{"status": "success", "message": "product updated"})
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

	models.SendJSON(w, http.StatusOK, "menu", "update_product_attributes", map[string]string{"status": "success", "message": "attributes updated"})
}

func (h *MenuHandler) UploadProductImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// 1. Auth & Validation basique
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "upload_product_image", map[string]string{"error": "missing_token"})
		return
	}

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		log.Error("[ERROR] UploadProductImage UserFromContext: " + err.Error())
		models.SendJSON(w, http.StatusUnauthorized, "menu", "upload_product_image", map[string]string{"error": "invalid_token"})
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_product_image", map[string]string{"error": "missing_parameter"})
		return
	}

	// 2. Parse multipart form (taille max 5 Mo)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		log.Error("[ERROR] UploadProductImage ParseMultipartForm: " + err.Error())
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_product_image", map[string]string{"error": "file_too_large_or_invalid"})
		return
	}

	// 3. Récupérer le fichier
	file, header, err := r.FormFile("photo")
	if err != nil {
		log.Error("[ERROR] UploadProductImage FormFile: " + err.Error())
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_product_image", map[string]string{"error": "missing_photo_field"})
		return
	}
	defer file.Close()

	// 4. Valider le type MIME
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}

	if !r2.ValidateImageType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_product_image", map[string]string{
			"error":   "invalid_image_type",
			"message": "Only JPEG, PNG, and WebP images are allowed",
		})
		return
	}

	// 6. Récupérer l'ancienne URL d'image (si elle existe)
	oldImageURL, err := h.service.GetProductImageURL(ctx, token, productID)
	if err != nil {
		// On log l'erreur mais on continue (pas bloquant)
		log.Warn("[WARN] UploadProductImage GetProductImageURL: " + err.Error())
	}

	// 7. Générer la clé R2
	ext := r2.GetExtensionFromContentType(contentType)
	key := r2.GenerateProductKey(user.MerchantID, productID, ext)

	// 8. Supprimer l'ancienne image de R2 (si elle existe)
	if oldImageURL != "" {
		// Essayer d'extraire la clé à partir de l'URL publique
		oldKey := h.r2Client.GetKeyFromURL(oldImageURL)
		if oldKey != "" {
			if err := h.r2Client.DeleteFile(ctx, oldKey); err != nil {
				// On log l'erreur mais on continue (pas bloquant)
				log.Warn("[WARN] UploadProductImage DeleteFile (old image): " + err.Error())
			}
		}
	}

	// 9. Upload le nouveau fichier vers R2
	publicURL, err := h.r2Client.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error("[ERROR] UploadProductImage UploadFile: " + err.Error())
		models.SendErrorJSON(w, "menu", "upload_product_image", fmt.Errorf("failed to upload image"))
		return
	}

	// 10. Mettre à jour la base de données
	if err := h.service.UpdateProductImage(ctx, token, productID, publicURL); err != nil {
		log.Error("[ERROR] UploadProductImage UpdateProductImage: " + err.Error())
		models.SendErrorJSON(w, "menu", "upload_product_image", err)
		return
	}

	// 9. Réponse
	models.SendJSON(w, http.StatusOK, "menu", "upload_product_image", map[string]interface{}{
		"status":    "success",
		"photo_url": publicURL,
	})
}

// maxAttributeOptionImageBytes : 2 Mo, décision actée pour les vignettes
// d'option de configuration (voir docs/KIOSK_DECISIONS.md) — plus petit que
// les 5 Mo d'une image produit plein écran.
const maxAttributeOptionImageBytes = 2 << 20

func (h *MenuHandler) UploadAttributeOptionImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// 1. Auth & Validation basique
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "upload_attribute_option_image", map[string]string{"error": "missing_token"})
		return
	}

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		log.Error("[ERROR] UploadAttributeOptionImage UserFromContext: " + err.Error())
		models.SendJSON(w, http.StatusUnauthorized, "menu", "upload_attribute_option_image", map[string]string{"error": "invalid_token"})
		return
	}

	optionID := chi.URLParam(r, "option_id")
	if optionID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_attribute_option_image", map[string]string{"error": "missing_parameter"})
		return
	}

	// 2. Parse multipart form (taille max 2 Mo)
	if err := r.ParseMultipartForm(maxAttributeOptionImageBytes); err != nil {
		log.Error("[ERROR] UploadAttributeOptionImage ParseMultipartForm: " + err.Error())
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_attribute_option_image", map[string]string{"error": "file_too_large_or_invalid"})
		return
	}

	// 3. Récupérer le fichier
	file, header, err := r.FormFile("photo")
	if err != nil {
		log.Error("[ERROR] UploadAttributeOptionImage FormFile: " + err.Error())
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_attribute_option_image", map[string]string{"error": "missing_photo_field"})
		return
	}
	defer file.Close()

	// 4. Valider le type MIME
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}

	if !r2.ValidateImageType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, "menu", "upload_attribute_option_image", map[string]string{
			"error":   "invalid_image_type",
			"message": "Only JPEG, PNG, and WebP images are allowed",
		})
		return
	}

	// 5. Récupérer l'ancienne URL d'image (si elle existe)
	oldImageURL, err := h.service.GetAttributeOptionImageURL(ctx, token, optionID)
	if err != nil {
		// On log l'erreur mais on continue (pas bloquant)
		log.Warn("[WARN] UploadAttributeOptionImage GetAttributeOptionImageURL: " + err.Error())
	}

	// 6. Générer la clé R2
	ext := r2.GetExtensionFromContentType(contentType)
	key := r2.GenerateConfigOptionKey(user.MerchantID, optionID, ext)

	// 7. Supprimer l'ancienne image de R2 (si elle existe)
	if oldImageURL != "" {
		oldKey := h.r2Client.GetKeyFromURL(oldImageURL)
		if oldKey != "" {
			if err := h.r2Client.DeleteFile(ctx, oldKey); err != nil {
				// On log l'erreur mais on continue (pas bloquant)
				log.Warn("[WARN] UploadAttributeOptionImage DeleteFile (old image): " + err.Error())
			}
		}
	}

	// 8. Upload le nouveau fichier vers R2
	publicURL, err := h.r2Client.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error("[ERROR] UploadAttributeOptionImage UploadFile: " + err.Error())
		models.SendErrorJSON(w, "menu", "upload_attribute_option_image", fmt.Errorf("failed to upload image"))
		return
	}

	// 9. Mettre à jour la base de données
	if err := h.service.UpdateAttributeOptionImageURL(ctx, token, optionID, publicURL); err != nil {
		log.Error("[ERROR] UploadAttributeOptionImage UpdateAttributeOptionImageURL: " + err.Error())
		models.SendErrorJSON(w, "menu", "upload_attribute_option_image", err)
		return
	}

	// 10. Réponse
	models.SendJSON(w, http.StatusOK, "menu", "upload_attribute_option_image", map[string]interface{}{
		"image_url": publicURL,
	})
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

	models.SendJSON(w, http.StatusOK, "menu", "sync_deliveroo_menu", map[string]string{"status": "success", "message": "menu synced to deliveroo"})
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

	models.SendJSON(w, http.StatusOK, "menu", "sync_ubereats_menu", map[string]string{"status": "success", "message": "menu synced to uber eats"})
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
		AllergenIDs []string `json:"allergen_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "sync_product_allergens", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.SyncProductAllergens(r.Context(), token, productID, body.AllergenIDs); err != nil {
		models.SendErrorJSON(w, "menu", "sync_product_allergens", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "sync_product_allergens", map[string]string{"status": "success", "message": "allergens updated"})
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
		"status":  "success",
		"message": "tag assigned",
		"updated": len(body.ProductIDs),
	})
}

// BulkAssignProductsToTag — PATCH /menu/tags/{tag_id}/bulk_assign
// Replaces all product-tag links for a given tag.
// Removes all existing links from this tag to any product, then adds new links to the provided product IDs.
func (h *MenuHandler) BulkAssignProductsToTag(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "bulk_assign_products_to_tag", map[string]string{"error": "missing_token"})
		return
	}

	tagID := chi.URLParam(r, "tag_id")
	if strings.TrimSpace(tagID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_products_to_tag", map[string]string{"error": "missing_tag_id"})
		return
	}

	var body BulkAssignProductsPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_products_to_tag", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.BulkAssignProductsToTag(r.Context(), token, tagID, body.ProductIDs); err != nil {
		models.SendErrorJSON(w, "menu", "bulk_assign_products_to_tag", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "bulk_assign_products_to_tag", map[string]interface{}{
		"status":   "success",
		"message":  "tag assignments updated",
		"tag_id":   tagID,
		"products": len(body.ProductIDs),
	})
}

// BulkAssignProductsToCategory — POST /menu/products/categories/{category_id}/bulk-assign
// Assigns multiple products (and their sub-products) to a category
func (h *MenuHandler) BulkAssignProductsToCategory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "bulk_assign_products_to_category", map[string]string{"error": "missing_token"})
		return
	}

	categoryID := chi.URLParam(r, "category_id")
	if categoryID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_products_to_category", map[string]string{"error": "missing_category_id"})
		return
	}

	var body BulkAssignProductsPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_products_to_category", map[string]string{"error": "invalid_body"})
		return
	}
	if len(body.ProductIDs) == 0 {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_assign_products_to_category", map[string]string{"error": "product_ids_required"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	if err := h.service.BulkAssignProductsToCategory(ctx, token, categoryID, body.ProductIDs); err != nil {
		log.Error("[ERROR] BulkAssignProductsToCategory error: " + err.Error())
		models.SendErrorJSON(w, "menu", "bulk_assign_products_to_category", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "bulk_assign_products_to_category", map[string]interface{}{
		"status":  "success",
		"message": "products assigned to category",
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
		"status":  "success",
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
		TagIDs []string `json:"tag_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "sync_product_tags", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.service.SyncProductTags(r.Context(), token, productID, body.TagIDs); err != nil {
		models.SendErrorJSON(w, "menu", "sync_product_tags", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "sync_product_tags", map[string]string{"status": "success", "message": "tags updated"})
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

	models.SendJSON(w, http.StatusOK, "tags", "list", map[string]interface{}{
		"status": "success",
		"tags":   tagList,
	})
}

func (h *MenuHandler) BulkUpdateProductPrices(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "bulk_update_product_prices", map[string]string{"error": "missing_token"})
		return
	}

	var payload BulkUpdateProductPricesPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendErrorJSON(w, "menu", "bulk_update_product_prices", err)
		return
	}

	if len(payload.Products) == 0 {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_update_product_prices", map[string]string{"error": "no_products_provided"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	err := h.service.BulkUpdateProductPrices(ctx, token, payload.Products)
	if err != nil {
		log.Error("[ERROR] BulkUpdateProductPrices error: " + err.Error())
		models.SendErrorJSON(w, "menu", "bulk_update_product_prices", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "bulk_update_product_prices", map[string]string{
		"status":  "success",
		"message": "products_prices_updated",
	})
}
