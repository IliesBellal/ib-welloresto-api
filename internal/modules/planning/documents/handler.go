package documents

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) UploadEmployeeDocument(w http.ResponseWriter, r *http.Request) {
	const maxSize = EmployeeDocumentMaxSize
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	if err := r.ParseMultipartForm(maxSize); err != nil {
		models.SendErrorJSON(w, "planning", "upload_employee_document", models.ErrPlanningEmployeeDocumentFileRequired)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		models.SendErrorJSON(w, "planning", "upload_employee_document", models.ErrPlanningEmployeeDocumentFileRequired)
		return
	}
	defer file.Close()

	resp, err := h.svc.UploadEmployeeDocument(r.Context(), header, file)
	if err != nil {
		models.SendErrorJSON(w, "planning", "upload_employee_document", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "upload_employee_document", map[string]interface{}{"status": "success", "upload": resp})
}

func (h *Handler) ListEmployeeDocuments(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	docs, err := h.svc.ListEmployeeDocuments(r.Context(), employeeID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_employee_documents", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_employee_documents", map[string]interface{}{"status": "success", "documents": docs})
}

func (h *Handler) CreateEmployeeDocument(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req EmployeeDocumentCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_employee_document", models.ErrInvalidRequestBody)
		return
	}
	doc, err := h.svc.CreateEmployeeDocument(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_employee_document", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_employee_document", map[string]interface{}{"status": "success", "document": doc})
}

func (h *Handler) GetEmployeeDocumentDownloadURL(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	documentID := strings.TrimSpace(chi.URLParam(r, "document_id"))
	url, err := h.svc.GetEmployeeDocumentDownloadURL(r.Context(), employeeID, documentID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_employee_document_download_url", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "get_employee_document_download_url", map[string]interface{}{"status": "success", "url": url})
}

func (h *Handler) DeleteEmployeeDocument(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	documentID := strings.TrimSpace(chi.URLParam(r, "document_id"))
	if err := h.svc.DeleteEmployeeDocument(r.Context(), employeeID, documentID); err != nil {
		models.SendErrorJSON(w, "planning", "delete_employee_document", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "delete_employee_document", map[string]interface{}{"status": "success"})
}
