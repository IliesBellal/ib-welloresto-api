package documents

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListEmployeeDocuments(ctx context.Context, merchantID, employeeID string) ([]EmployeeDocument, error) {
	db := dbutils.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, employee_id, document_type, name, file_key, content_type, created_at, updated_at, deleted_at
		FROM employee_documents
		WHERE merchant_id = ? AND employee_id = ? AND enabled = 1
		ORDER BY created_at DESC
	`, merchantID, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]EmployeeDocument, 0)
	for rows.Next() {
		doc, err := scanEmployeeDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *doc)
	}
	return docs, rows.Err()
}

func (r *Repository) GetEmployeeDocumentByID(ctx context.Context, merchantID, documentID string) (*EmployeeDocument, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, employee_id, document_type, name, file_key, content_type, created_at, updated_at, deleted_at
		FROM employee_documents
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`, merchantID, documentID)
	return scanEmployeeDocumentRow(row)
}

func (r *Repository) CreateEmployeeDocument(ctx context.Context, merchantID, employeeID string, req EmployeeDocumentCreateRequest) (*EmployeeDocument, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	doc := EmployeeDocument{
		ID:           helpers.GeneratePrefixedID(helpers.PlanningEmployeeDocumentIDPrefix),
		MerchantID:   merchantID,
		EmployeeID:   employeeID,
		DocumentType: req.DocumentType,
		Name:         req.Name,
		FileKey:      req.FileKey,
		ContentType:  req.ContentType,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO employee_documents (
			id, merchant_id, employee_id, document_type, name, file_key, content_type, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, doc.ID, doc.MerchantID, doc.EmployeeID, doc.DocumentType, doc.Name, doc.FileKey, doc.ContentType, doc.CreatedAt, doc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *Repository) DeleteEmployeeDocument(ctx context.Context, merchantID, documentID string) (*EmployeeDocument, error) {
	db := dbutils.GetDB(ctx, r.db)
	doc, err := r.GetEmployeeDocumentByID(ctx, merchantID, documentID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE employee_documents
		SET enabled = 0, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, now, now, merchantID, documentID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return doc, nil
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}

func scanEmployeeDocumentRow(row scannable) (*EmployeeDocument, error) {
	doc := &EmployeeDocument{}
	var deletedAt sql.NullTime
	if err := row.Scan(&doc.ID, &doc.MerchantID, &doc.EmployeeID, &doc.DocumentType, &doc.Name, &doc.FileKey, &doc.ContentType, &doc.CreatedAt, &doc.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		doc.DeletedAt = &t
	}
	return doc, nil
}

func scanEmployeeDocument(rows scannableRows) (*EmployeeDocument, error) {
	return scanEmployeeDocumentRow(rows)
}
