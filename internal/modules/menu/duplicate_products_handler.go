package menu

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// maxBulkDuplicateProducts borne une copie groupée : chaque produit coûte une
// quinzaine de requêtes dans une seule transaction.
const maxBulkDuplicateProducts = 500

// BulkDuplicateProductsPayload — copie complète de produits vers une catégorie caisse.
type BulkDuplicateProductsPayload struct {
	ProductIDs []string `json:"product_ids"`
	CategoryID string   `json:"category_id"` // merchant_categ_id de la catégorie cible
	Status     string   `json:"status"`      // statut de vente des copies ; vide = available
}

// BulkDuplicateProducts — POST /menu/products/bulk/duplicate
// Copie les produits sélectionnés, avec tout ce qui les configure, dans une
// catégorie caisse. Répond les IDs des copies racines (pour une annulation
// par /products/bulk/delete, qui retire aussi leurs sous-produits) et le
// nombre d'images qui n'ont pas pu être dupliquées.
func (h *MenuHandler) BulkDuplicateProducts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "bulk_duplicate_products", map[string]string{"error": "missing_token"})
		return
	}

	var payload BulkDuplicateProductsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_duplicate_products", map[string]string{"error": "invalid_body"})
		return
	}
	if len(payload.ProductIDs) == 0 {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_duplicate_products", map[string]string{"error": "product_ids_required"})
		return
	}
	if len(payload.ProductIDs) > maxBulkDuplicateProducts {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_duplicate_products", map[string]string{"error": "too_many_products"})
		return
	}
	categoryID := strings.TrimSpace(payload.CategoryID)
	if categoryID == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_duplicate_products", map[string]string{"error": "missing_category_id"})
		return
	}
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if status == "" {
		status = "available"
	}
	if !bulkProductStatuses[status] {
		models.SendJSON(w, http.StatusBadRequest, "menu", "bulk_duplicate_products", map[string]string{"error": "invalid_status"})
		return
	}

	ctx := r.Context()
	log := logger.FromContext(ctx)

	copies, err := h.service.BulkDuplicateProducts(ctx, token, categoryID, status, payload.ProductIDs)
	if err != nil {
		log.Error("[ERROR] BulkDuplicateProducts error: " + err.Error())
		models.SendErrorJSON(w, "menu", "bulk_duplicate_products", err)
		return
	}

	// Les copies existent désormais : un souci d'image ne les annule pas, il
	// est seulement signalé.
	images, imagesFailed := h.duplicateProductImages(ctx, copies)
	if err := h.service.SetProductImages(ctx, token, images); err != nil {
		log.Warn("[WARN] BulkDuplicateProducts SetProductImages: " + err.Error())
		imagesFailed += len(images)
	}

	rootIDs := make([]string, 0, len(copies))
	for _, c := range copies {
		if c.Root {
			rootIDs = append(rootIDs, c.NewID)
		}
	}

	models.SendJSON(w, http.StatusOK, "menu", "bulk_duplicate_products", map[string]interface{}{
		"status":        "success",
		"message":       "products_duplicated",
		"duplicated":    len(rootIDs),
		"product_ids":   rootIDs,
		"images_failed": imagesFailed,
	})
}

// duplicateProductImages donne à chaque copie sa propre image et retourne les
// URLs à enregistrer (par ID de copie) ainsi que le nombre d'échecs.
//
// Une image hébergée sur R2 est dupliquée sous la clé de la copie. Partager
// l'objet de la source ne tiendrait pas : la clé d'une image dépend du
// produit, et UploadProductImage supprime l'ancienne image quand elle portait
// une autre clé — remplacer l'image de la copie effacerait celle de la source.
// Une URL externe, que l'API ne supprime jamais, est reprise telle quelle.
func (h *MenuHandler) duplicateProductImages(ctx context.Context, copies []DuplicatedProduct) (map[string]string, int) {
	images := make(map[string]string)
	failed := 0
	log := logger.FromContext(ctx)

	var merchantID string
	if user, err := middleware.UserFromContext(ctx); err == nil {
		merchantID = user.MerchantID
	}

	for _, c := range copies {
		if c.SourceImageURL == "" {
			continue
		}
		if h.r2Client == nil {
			failed++
			continue
		}
		srcKey := h.r2Client.GetKeyFromURL(c.SourceImageURL)
		if srcKey == "" {
			images[c.NewID] = c.SourceImageURL
			continue
		}
		dstKey := r2.GenerateProductKey(merchantID, c.NewID, path.Ext(srcKey))
		publicURL, err := h.r2Client.CopyFile(ctx, srcKey, dstKey)
		if err != nil {
			log.Warn("[WARN] BulkDuplicateProducts CopyFile (product " + c.SourceID + "): " + err.Error())
			failed++
			continue
		}
		// Même cache-buster que UploadProductImage.
		images[c.NewID] = publicURL + "?v=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	return images, failed
}
