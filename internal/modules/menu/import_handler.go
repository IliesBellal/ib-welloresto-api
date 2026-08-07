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

// ImportHandler expose la preview d'import de produits.
type ImportHandler struct {
	service *ImportService
}

func NewImportHandler(s *ImportService) *ImportHandler {
	return &ImportHandler{service: s}
}

// PreviewImport calcule un dry-run d'import et renvoie un token.
//
// Deux modes sur la même route, distingués par le Content-Type :
//   - multipart/form-data : champs "provider" et "file", pour un export d'un
//     éditeur tiers ou le template Wello ;
//   - application/json : produits saisis directement, pour le formulaire de
//     masse du back-office.
//
// Aucune écriture : ni en base, ni sur le menu. Le seul effet de bord est le
// dépôt du snapshot en cache, sous un token à durée de vie limitée.
func (h *ImportHandler) PreviewImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "menu", "preview_import", map[string]string{"error": "missing_token"})
		return
	}

	var (
		result *importer.PreviewResult
		err    error
	)
	if isMultipartRequest(r) {
		result, err = h.previewFromMultipart(w, r)
	} else {
		result, err = h.previewFromJSON(w, r)
	}

	switch {
	case errors.Is(err, errImportResponseWritten):
		// La réponse a déjà été écrite au plus près de la cause.
		return
	case err != nil:
		log.Error("[ERROR] PreviewImport: " + err.Error())
		h.sendImportError(w, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "menu", "preview_import", result)
}

// errImportResponseWritten signale au point d'entrée qu'une réponse d'erreur a
// déjà été envoyée, et qu'il n'a plus rien à écrire.
var errImportResponseWritten = errors.New("import: réponse déjà écrite")

func isMultipartRequest(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
}

func (h *ImportHandler) previewFromMultipart(w http.ResponseWriter, r *http.Request) (*importer.PreviewResult, error) {
	if err := r.ParseMultipartForm(maxImportFileSize); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{"error": "file_too_large_or_invalid"})
		return nil, errImportResponseWritten
	}

	provider := strings.TrimSpace(r.FormValue(importFormProviderField))
	if provider == "" {
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{"error": "missing_provider"})
		return nil, errImportResponseWritten
	}

	file, _, err := r.FormFile(importFormFileField)
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{"error": "missing_file"})
		return nil, errImportResponseWritten
	}
	defer file.Close()

	return h.service.PreviewImportFile(r.Context(), provider, file)
}

func (h *ImportHandler) previewFromJSON(w http.ResponseWriter, r *http.Request) (*importer.PreviewResult, error) {
	var req ImportPreviewJSONRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{"error": "invalid_body"})
		return nil, errImportResponseWritten
	}

	return h.service.PreviewImportManual(r.Context(), &req)
}

// sendImportError distingue ce que l'utilisateur peut corriger (un fichier
// illisible, un provider inconnu) de ce qui relève du serveur. Un fichier mal
// rempli est une erreur 400 avec le message du parser, qui porte la ligne et
// la colonne fautives.
func (h *ImportHandler) sendImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, importer.ErrUnknownProvider):
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{
			"error":   "unknown_provider",
			"message": err.Error(),
		})
	case errors.Is(err, ErrImportProviderRequired):
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{"error": "missing_provider"})
	case errors.Is(err, ErrImportFileRequired):
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{"error": "missing_file"})
	case errors.Is(err, ErrImportNoProducts), errors.Is(err, importer.ErrNoProducts):
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{
			"error":   "no_products",
			"message": "aucun produit n'a été trouvé — vérifiez le fichier et le provider sélectionné",
		})
	case errors.Is(err, importer.ErrMissingColumn),
		errors.Is(err, importer.ErrEmptyWorkbook),
		errors.Is(err, importer.ErrInvalidWorkbook):
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{
			"error":   "invalid_file",
			"message": err.Error(),
		})
	case isImportRowError(err):
		models.SendJSON(w, http.StatusBadRequest, "menu", "preview_import", map[string]string{
			"error":   "invalid_file_content",
			"message": err.Error(),
		})
	default:
		models.SendErrorJSON(w, "menu", "preview_import", err)
	}
}

// isImportRowError reconnaît une erreur situant une ligne et une colonne du
// fichier — la seule catégorie d'erreur que le restaurateur peut corriger
// lui-même.
func isImportRowError(err error) bool {
	var rowErr *importer.RowError
	return errors.As(err, &rowErr)
}
