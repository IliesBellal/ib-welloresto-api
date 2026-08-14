package customers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers/importer"
)

// CustomerImportHandler expose la preview d'import de clients.
type CustomerImportHandler struct {
	service *CustomerImportService
}

func NewCustomerImportHandler(s *CustomerImportService) *CustomerImportHandler {
	return &CustomerImportHandler{service: s}
}

// errCustomerImportResponseWritten signale au point d'entrée qu'une réponse
// d'erreur a déjà été envoyée, et qu'il n'a plus rien à écrire.
var errCustomerImportResponseWritten = errors.New("customer import: réponse déjà écrite")

// PreviewImport calcule un dry-run d'import de clients et renvoie un token.
//
// Deux modes sur la même route, distingués par le Content-Type :
//   - multipart/form-data : champs "provider" et "file", pour un export
//     Zelty ou le template Wello ;
//   - application/json : clients saisis directement, pour la saisie manuelle
//     du back-office (provider forcé à "manual").
//
// Aucune écriture : ni en base, ni ailleurs. Le seul effet de bord est le
// dépôt du snapshot en cache, sous un token à durée de vie limitée.
func (h *CustomerImportHandler) PreviewImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "preview_import", map[string]string{"error": "missing_token"})
		return
	}

	var (
		result *importer.PreviewResult
		err    error
	)
	if isMultipartCustomerImportRequest(r) {
		result, err = h.previewFromMultipart(w, r)
	} else {
		result, err = h.previewFromJSON(w, r)
	}

	switch {
	case errors.Is(err, errCustomerImportResponseWritten):
		// La réponse a déjà été écrite au plus près de la cause.
		return
	case err != nil:
		log.Error("[ERROR] CustomerImportHandler.PreviewImport: " + err.Error())
		h.sendImportError(w, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "preview_import", result)
}

func isMultipartCustomerImportRequest(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
}

func (h *CustomerImportHandler) previewFromMultipart(w http.ResponseWriter, r *http.Request) (*importer.PreviewResult, error) {
	if err := r.ParseMultipartForm(maxCustomerImportFileSize); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{"error": "file_too_large_or_invalid"})
		return nil, errCustomerImportResponseWritten
	}

	provider := strings.TrimSpace(r.FormValue(customerImportFormProviderField))
	if provider == "" {
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{"error": "missing_provider"})
		return nil, errCustomerImportResponseWritten
	}

	file, _, err := r.FormFile(customerImportFormFileField)
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{"error": "missing_file"})
		return nil, errCustomerImportResponseWritten
	}
	defer file.Close()

	return h.service.PreviewImport(r.Context(), provider, PreviewInput{File: file})
}

func (h *CustomerImportHandler) previewFromJSON(w http.ResponseWriter, r *http.Request) (*importer.PreviewResult, error) {
	var req ImportPreviewJSONRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{"error": "invalid_body"})
		return nil, errCustomerImportResponseWritten
	}

	return h.service.PreviewImport(r.Context(), importer.ManualSlug, PreviewInput{Inputs: req.toManualCustomerInputs()})
}

// sendImportError distingue ce que l'utilisateur peut corriger (un fichier
// illisible, un provider inconnu) de ce qui relève du serveur. Un fichier mal
// rempli est une erreur 400 avec le message du parser, qui porte la ligne et
// la colonne fautives.
func (h *CustomerImportHandler) sendImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, importer.ErrUnknownProvider):
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{
			"error":   "unknown_provider",
			"message": err.Error(),
		})
	case errors.Is(err, ErrCustomerImportProviderRequired):
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{"error": "missing_provider"})
	case errors.Is(err, ErrCustomerImportFileRequired):
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{"error": "missing_file"})
	case errors.Is(err, ErrCustomerImportNoCustomers), errors.Is(err, importer.ErrNoCustomers):
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{
			"error":   "no_customers",
			"message": "aucun client n'a été trouvé — vérifiez le fichier et le provider sélectionné",
		})
	case errors.Is(err, importer.ErrMissingColumn),
		errors.Is(err, importer.ErrEmptyWorkbook),
		errors.Is(err, importer.ErrInvalidWorkbook),
		errors.Is(err, importer.ErrInvalidCSV):
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{
			"error":   "invalid_file",
			"message": err.Error(),
		})
	case isCustomerImportRowError(err):
		models.SendJSON(w, http.StatusBadRequest, "customers", "preview_import", map[string]string{
			"error":   "invalid_file_content",
			"message": err.Error(),
		})
	default:
		models.SendErrorJSON(w, "customers", "preview_import", err)
	}
}

// isCustomerImportRowError reconnaît une erreur situant une ligne et une
// colonne du fichier — la seule catégorie d'erreur que l'utilisateur peut
// corriger lui-même.
func isCustomerImportRowError(err error) bool {
	var rowErr *importer.RowError
	return errors.As(err, &rowErr)
}

// CommitImport matérialise un lot précédemment prévisualisé.
//
// Seul endpoint du chemin d'import clients qui écrit. Il refuse plutôt que
// d'écrire à moitié : un lot dont une ligne ne peut pas être résolue contre
// l'état frais de la base repart en 422 avec la liste des blocages, sans
// qu'une seule ligne ait été insérée ou modifiée.
func (h *CustomerImportHandler) CommitImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "commit_import", map[string]string{"error": "missing_token"})
		return
	}

	var req CommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "commit_import", map[string]string{
			"error":   "invalid_body",
			"message": err.Error(),
		})
		return
	}

	summary, blockers, err := h.service.CommitImport(ctx, req)
	if err != nil {
		h.sendCommitError(w, log, err)
		return
	}
	if len(blockers) > 0 {
		models.SendJSON(w, http.StatusUnprocessableEntity, "customers", "commit_import", map[string]interface{}{
			"error":    "import_not_committable",
			"message":  "des decisions manquent avant de pouvoir valider l'import",
			"blockers": blockers,
		})
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "commit_import", summary)
}

func (h *CustomerImportHandler) sendCommitError(w http.ResponseWriter, log *zap.Logger, err error) {
	switch {
	case errors.Is(err, ErrCustomerImportTokenRequired):
		models.SendJSON(w, http.StatusBadRequest, "customers", "commit_import", map[string]string{"error": "missing_preview_token"})

	case errors.Is(err, ErrCustomerImportPreviewNotFound):
		// 410 plutôt que 404 : la preview a existé ou n'existera plus, dans
		// les deux cas le client doit en relancer une, pas réessayer celle-ci.
		models.SendJSON(w, http.StatusGone, "customers", "commit_import", map[string]string{
			"error":   "preview_expired",
			"message": "cette prévisualisation a expiré ou a déjà été validée — relancez un import",
		})

	default:
		log.Error("[ERROR] CustomerImportHandler.CommitImport: " + err.Error())
		models.SendErrorJSON(w, "customers", "commit_import", err)
	}
}

// mimeXLSX est le type des classeurs OpenXML.
const mimeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// DownloadImportTemplate renvoie le classeur vierge d'un provider.
//
// En attachment : c'est un fichier à remplir, pas un document à consulter.
//
// Le paramètre provider est exigé, sans valeur par défaut : un défaut
// masquerait un appel fautif du back-office le jour où un second provider
// proposera un modèle.
func (h *CustomerImportHandler) DownloadImportTemplate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "import_template", map[string]string{"error": "missing_token"})
		return
	}

	provider := strings.TrimSpace(r.URL.Query().Get(customerImportFormProviderField))

	data, filename, err := h.service.ImportTemplate(provider)
	if err != nil {
		switch {
		case errors.Is(err, ErrCustomerImportProviderRequired):
			models.SendJSON(w, http.StatusBadRequest, "customers", "import_template", map[string]string{
				"error":   "missing_provider",
				"message": "le paramètre provider est requis",
			})
		case errors.Is(err, importer.ErrUnknownProvider):
			models.SendJSON(w, http.StatusBadRequest, "customers", "import_template", map[string]string{
				"error":   "unknown_provider",
				"message": err.Error(),
			})
		case errors.Is(err, ErrCustomerImportTemplateUnavailable):
			models.SendJSON(w, http.StatusBadRequest, "customers", "import_template", map[string]string{
				"error":   "template_not_available",
				"message": "ce provider est un export tiers ou une saisie manuelle : il n'y a pas de modèle Wello à remplir",
			})
		default:
			log.Error("[ERROR] CustomerImportHandler.DownloadImportTemplate: " + err.Error())
			models.SendErrorJSON(w, "customers", "import_template", err)
		}
		return
	}

	w.Header().Set("Content-Type", mimeXLSX)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
