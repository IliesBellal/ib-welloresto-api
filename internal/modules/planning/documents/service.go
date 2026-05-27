package documents

import (
	"context"
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	employeespkg "welloresto-api/internal/modules/planning/employees"
)

const EmployeeDocumentMaxSize = 10 << 20

type EmployeeReader interface {
	GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error)
}

type Service struct {
	repo         *Repository
	employeeRepo EmployeeReader
	privateR2    *r2.Client
}

func NewService(repo *Repository, employeeRepo EmployeeReader, privateR2 *r2.Client) *Service {
	return &Service{repo: repo, employeeRepo: employeeRepo, privateR2: privateR2}
}

func (s *Service) UploadEmployeeDocument(ctx context.Context, fileHeader *multipart.FileHeader, file multipart.File) (*EmployeeDocumentUploadResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if fileHeader == nil {
		return nil, models.ErrPlanningEmployeeDocumentFileRequired
	}
	contentType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(fileHeader.Filename)
	}
	if _, ok := allowedDocumentContentTypes[contentType]; !ok {
		return nil, models.ErrInvalidImageType
	}

	key := generateEmployeeDocumentKey(user.MerchantID, fileHeader.Filename)
	url, err := s.privateR2.UploadPrivateFile(ctx, key, file, contentType)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrPlanningEmployeeDocumentUploadFailed, err)
	}

	return &EmployeeDocumentUploadResponse{
		FileKey:     key,
		FileURL:     url,
		ContentType: contentType,
		FileName:    fileHeader.Filename,
	}, nil
}

func (s *Service) ListEmployeeDocuments(ctx context.Context, employeeID string) ([]EmployeeDocument, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	docs, err := s.repo.ListEmployeeDocuments(ctx, user.MerchantID, employeeID)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		if docs[i].FileKey != "" {
			signed, signErr := s.privateR2.GenerateSignedURL(ctx, docs[i].FileKey, time.Hour)
			if signErr != nil {
				return nil, fmt.Errorf("%w: %v", models.ErrPlanningEmployeeDocumentUrlFailed, signErr)
			}
			docs[i].FileURL = signed
		}
	}
	return docs, nil
}

func (s *Service) CreateEmployeeDocument(ctx context.Context, employeeID string, req EmployeeDocumentCreateRequest) (*EmployeeDocument, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" {
		return nil, models.ErrMissingResourceID
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, models.ErrPlanningEmployeeDocumentNameRequired
	}
	if strings.TrimSpace(req.FileKey) == "" {
		return nil, models.ErrPlanningEmployeeDocumentFileRequired
	}
	if _, ok := allowedDocumentTypes[strings.ToLower(strings.TrimSpace(req.DocumentType))]; !ok {
		return nil, models.ErrPlanningEmployeeDocumentTypeInvalid
	}
	if _, err := s.employeeRepo.GetEmployeeByID(ctx, user.MerchantID, employeeID); err != nil {
		return nil, models.ErrPlanningEmployeeNotFound
	}
	req.DocumentType = strings.ToLower(strings.TrimSpace(req.DocumentType))
	req.Name = strings.TrimSpace(req.Name)
	req.FileKey = strings.TrimSpace(req.FileKey)
	req.ContentType = strings.TrimSpace(req.ContentType)
	return s.repo.CreateEmployeeDocument(ctx, user.MerchantID, employeeID, req)
}

func (s *Service) GetEmployeeDocumentDownloadURL(ctx context.Context, employeeID, documentID string) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" || strings.TrimSpace(documentID) == "" {
		return "", models.ErrMissingResourceID
	}
	doc, err := s.repo.GetEmployeeDocumentByID(ctx, user.MerchantID, documentID)
	if err != nil {
		return "", models.ErrPlanningEmployeeDocumentNotFound
	}
	if strings.TrimSpace(doc.EmployeeID) != strings.TrimSpace(employeeID) {
		return "", models.ErrPlanningEmployeeDocumentNotFound
	}
	url, signErr := s.privateR2.GenerateSignedURL(ctx, doc.FileKey, time.Hour)
	if signErr != nil {
		return "", fmt.Errorf("%w: %v", models.ErrPlanningEmployeeDocumentUrlFailed, signErr)
	}
	return url, nil
}

func (s *Service) DeleteEmployeeDocument(ctx context.Context, employeeID, documentID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}
	if strings.TrimSpace(employeeID) == "" || strings.TrimSpace(documentID) == "" {
		return models.ErrMissingResourceID
	}
	doc, err := s.repo.GetEmployeeDocumentByID(ctx, user.MerchantID, documentID)
	if err != nil {
		return models.ErrPlanningEmployeeDocumentNotFound
	}
	if strings.TrimSpace(doc.EmployeeID) != strings.TrimSpace(employeeID) {
		return models.ErrPlanningEmployeeDocumentNotFound
	}
	doc, err = s.repo.DeleteEmployeeDocument(ctx, user.MerchantID, documentID)
	if err != nil {
		return models.ErrPlanningEmployeeDocumentNotFound
	}
	if doc != nil && doc.FileKey != "" {
		_ = s.privateR2.DeleteFile(ctx, doc.FileKey)
	}
	return nil
}

func generateEmployeeDocumentKey(merchantID, filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".bin"
	}
	return fmt.Sprintf("wello_resto_private_storage/merchants/%s/planning-documents/%s%s", merchantID, helpers.GeneratePrefixedID(helpers.PlanningEmployeeDocumentIDPrefix), ext)
}
