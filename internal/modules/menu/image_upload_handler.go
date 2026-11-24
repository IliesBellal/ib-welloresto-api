package menu

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/imagepick"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// Endpoints d'image du menu (produit, catégorie, catégorie marketing, option).
//
// Chacun accepte deux corps (docs/banque-images.md) :
//   - multipart, champ "photo" : fichier de l'appareil, allégé avant envoi ;
//   - JSON {"image_url": "…"} : image de la banque (URL reprise telle quelle)
//     ou image déjà utilisée par l'établissement (fichier copié).
//
// Ordre commun : placer la nouvelle image, l'enregistrer, puis seulement
// supprimer l'ancienne si elle portait une autre clé — un échec ne laisse
// jamais la cible pointer vers un fichier effacé. Les fichiers de la banque ne
// sont jamais supprimés (r2.Client.DeleteFile).

// SetImagePicker branche le placement d'images. Sans lui, les endpoints
// d'image répondent une erreur.
func (h *MenuHandler) SetImagePicker(p *imagepick.Picker) { h.picker = p }

// menuImageTarget décrit une cible d'image du menu.
type menuImageTarget struct {
	fnName    string
	idParam   string
	memory    int64
	keyFor    func(merchantID, id, ext string) string
	getOld    func(ctx context.Context, token, id string) (string, error)
	save      func(ctx context.Context, token, id, url string) error
	canDelete func(oldKey, newKey, merchantID, id string) bool
	respKey   string
}

func (h *MenuHandler) uploadMenuImage(w http.ResponseWriter, r *http.Request, t menuImageTarget) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", t.fnName, map[string]string{"error": "missing_token"})
		return
	}
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		log.Error("[ERROR] " + t.fnName + " UserFromContext: " + err.Error())
		models.SendJSON(w, http.StatusUnauthorized, "menu", t.fnName, map[string]string{"error": "invalid_token"})
		return
	}
	id := chi.URLParam(r, t.idParam)
	if strings.TrimSpace(id) == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", t.fnName, map[string]string{"error": "missing_parameter"})
		return
	}

	placed, err := h.picker.Place(w, r, imagepick.Request{
		Field:           "photo",
		MultipartMemory: t.memory,
		OwnPrefixes:     []string{r2.MerchantKeyPrefix(user.MerchantID)},
		KeyFor:          func(ext string) string { return t.keyFor(user.MerchantID, id, ext) },
	})
	if err != nil {
		log.Error("[ERROR] " + t.fnName + " Place: " + err.Error())
		imagepick.Send(w, "menu", t.fnName, err)
		return
	}

	oldImageURL, err := t.getOld(ctx, token, id)
	if err != nil {
		log.Warn("[WARN] " + t.fnName + " get old image: " + err.Error())
	}

	if err := t.save(ctx, token, id, placed.URL); err != nil {
		log.Error("[ERROR] " + t.fnName + " save: " + err.Error())
		models.SendErrorJSON(w, "menu", t.fnName, err)
		return
	}

	if oldImageURL != "" {
		oldKey := h.r2Client.GetKeyFromURL(oldImageURL)
		if t.canDelete(oldKey, placed.Key, user.MerchantID, id) {
			if err := h.r2Client.DeleteFile(ctx, oldKey); err != nil {
				log.Warn("[WARN] " + t.fnName + " DeleteFile (old image): " + err.Error())
			}
		}
	}

	models.SendJSON(w, http.StatusOK, "menu", t.fnName, map[string]interface{}{
		"status":  "success",
		t.respKey: placed.URL,
	})
}

// differentKey : règle par défaut de suppression de l'ancienne image.
func differentKey(oldKey, newKey, _, _ string) bool { return oldKey != "" && oldKey != newKey }

func (h *MenuHandler) UploadProductImage(w http.ResponseWriter, r *http.Request) {
	h.uploadMenuImage(w, r, menuImageTarget{
		fnName:    "upload_product_image",
		idParam:   "product_id",
		memory:    5 << 20,
		keyFor:    r2.GenerateProductKey,
		getOld:    h.service.GetProductImageURL,
		save:      h.service.UpdateProductImage,
		canDelete: differentKey,
		respKey:   "photo_url",
	})
}

func (h *MenuHandler) UploadProductCategoryImage(w http.ResponseWriter, r *http.Request) {
	h.uploadMenuImage(w, r, menuImageTarget{
		fnName:    "upload_product_category_image",
		idParam:   "category_id",
		memory:    5 << 20,
		keyFor:    r2.GenerateProductCategoryKey,
		getOld:    h.service.GetProductCategoryImageURL,
		save:      h.service.UpdateProductCategoryImageURL,
		canDelete: differentKey,
		respKey:   "image_url",
	})
}

func (h *MenuHandler) UploadMarketingCategoryImage(w http.ResponseWriter, r *http.Request) {
	h.uploadMenuImage(w, r, menuImageTarget{
		fnName:    "upload_marketing_category_image",
		idParam:   "category_id",
		memory:    5 << 20,
		keyFor:    r2.GenerateMarketingCategoryKey,
		getOld:    h.service.GetMarketingCategoryImageURL,
		save:      h.service.UpdateMarketingCategoryImageURL,
		canDelete: differentKey,
		respKey:   "image_url",
	})
}

// UploadAttributeOptionImage : l'ancienne image n'est supprimée que si c'était
// un fichier de cette option (voir shouldDeleteOldOptionImage).
func (h *MenuHandler) UploadAttributeOptionImage(w http.ResponseWriter, r *http.Request) {
	h.uploadMenuImage(w, r, menuImageTarget{
		fnName:    "upload_attribute_option_image",
		idParam:   "option_id",
		memory:    maxAttributeOptionImageBytes,
		keyFor:    r2.GenerateConfigOptionKey,
		getOld:    h.service.GetAttributeOptionImageURL,
		save:      h.service.UpdateAttributeOptionImageURL,
		canDelete: shouldDeleteOldOptionImage,
		respKey:   "image_url",
	})
}
