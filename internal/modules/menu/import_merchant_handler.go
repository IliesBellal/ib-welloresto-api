package menu

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu/importer"
)

// PreviewImportFromMerchant calcule un dry-run d'import depuis un autre
// établissement et renvoie un token — même contrat de réponse que PreviewImport
// (PreviewResult), même endpoint de commit (POST /menu/import/commit,
// inchangé : il ne s'intéresse jamais à la façon dont un snapshot a été
// produit, seulement à ce qu'il contient).
//
// Toujours en JSON, jamais en multipart : cette porte n'a pas de fichier.
func (h *ImportHandler) PreviewImportFromMerchant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "preview_import_merchant", map[string]string{"error": "missing_token"})
		return
	}

	var req ImportPreviewMerchantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import_merchant", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.service.PreviewImportFromMerchant(ctx, req.SourceMerchantID)
	if err != nil {
		switch {
		case errors.Is(err, ErrSourceMerchantNotFound):
			// 404 générique, jamais 403 : ne pas confirmer à l'appelant
			// l'existence d'un marchand sur lequel il n'a pas de droits (même
			// parti pris que le refus de snapshot d'un autre marchand dans
			// CommitImport).
			models.SendJSON(w, http.StatusNotFound, "menu", "preview_import_merchant", map[string]string{
				"error": "source_merchant_not_found",
			})
		case errors.Is(err, importer.ErrNoProducts):
			models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import_merchant", map[string]string{
				"error":   "no_products",
				"message": "cet établissement n'a aucun produit à importer",
			})
		default:
			log.Error("[ERROR] PreviewImportFromMerchant: " + err.Error())
			models.SendErrorJSON(w, "menu", "preview_import_merchant", err)
		}
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "preview_import_merchant", result)
}
