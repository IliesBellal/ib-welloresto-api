package documents

import "time"

type EmployeeDocument struct {
	ID           string     `json:"id"`
	MerchantID   string     `json:"merchant_id"`
	EmployeeID   string     `json:"employee_id"`
	DocumentType string     `json:"document_type"`
	Name         string     `json:"name"`
	FileURL      string     `json:"file_url"`
	FileKey      string     `json:"-"`
	ContentType  string     `json:"content_type"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type EmployeeDocumentCreateRequest struct {
	DocumentType string `json:"document_type"`
	Name         string `json:"name"`
	FileKey      string `json:"file_key"`
	ContentType  string `json:"content_type"`
}

type EmployeeDocumentUploadResponse struct {
	FileKey     string `json:"file_key"`
	FileURL     string `json:"file_url"`
	ContentType string `json:"content_type"`
	FileName    string `json:"file_name"`
}

type EmployeeDocumentListFilters struct {
	Page     int
	PageSize int
}

var allowedDocumentTypes = map[string]struct{}{
	"contract": {},
	"id":       {},
	"medical":  {},
	"other":    {},
}

var allowedDocumentContentTypes = map[string]struct{}{
	"application/pdf": {},
	"image/jpeg":      {},
	"image/png":       {},
	"image/webp":      {},
}
