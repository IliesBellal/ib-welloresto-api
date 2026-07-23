//go:build postgres_integration

package documents

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestDocumentsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999903"
	cleanup := func() { _, _ = db.ExecContext(ctx, `DELETE FROM employee_documents WHERE merchant_id = $1`, merchantID) }
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	doc, err := repo.CreateEmployeeDocument(ctx, merchantID, "emp-doc-1", EmployeeDocumentCreateRequest{
		DocumentType: "contract", Name: "contrat.pdf", FileKey: "k/itest.pdf", ContentType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("CreateEmployeeDocument: %v", err)
	}
	list, total, err := repo.ListEmployeeDocuments(ctx, merchantID, "emp-doc-1", EmployeeDocumentListFilters{Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListEmployeeDocuments = (%d/%d, %v)", len(list), total, err)
	}
	if got, err := repo.GetEmployeeDocumentByID(ctx, merchantID, doc.ID); err != nil || got.Name != "contrat.pdf" {
		t.Fatalf("GetEmployeeDocumentByID = (%+v, %v)", got, err)
	}
	if _, err := repo.DeleteEmployeeDocument(ctx, merchantID, doc.ID); err != nil {
		t.Fatalf("DeleteEmployeeDocument: %v", err)
	}
	if _, err := repo.GetEmployeeDocumentByID(ctx, merchantID, doc.ID); err == nil {
		t.Fatalf("document devrait être soft-deleted")
	}
}
