//go:build postgres_integration

package employees

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérification réelle de planning/employees (+ positions) contre le Postgres de dev.
func TestEmployeesRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999902"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM employees WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_positions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	// positions
	pos, err := repo.CreateEmployeePosition(ctx, merchantID, EmployeePositionCreateRequest{Label: "Serveur itest", Color: "#123456"})
	if err != nil || !pos.Active {
		t.Fatalf("CreateEmployeePosition = (%+v, %v)", pos, err)
	}
	if next, err := repo.NextEmployeePositionSortOrder(ctx, merchantID); err != nil || next <= pos.SortOrder {
		t.Fatalf("NextEmployeePositionSortOrder = (%d, %v)", next, err)
	}
	if got, err := repo.GetEmployeePositionByLabel(ctx, merchantID, "serveur itest", ""); err != nil || got == nil {
		t.Fatalf("GetEmployeePositionByLabel = (%+v, %v)", got, err)
	}

	// employés
	emp, err := repo.CreateEmployee(ctx, merchantID, EmployeeCreateRequest{
		FirstName: "Jean", LastName: "Zulu", PositionID: pos.ID, ContractTypeCode: "CDI",
	})
	if err != nil || emp.Role != "employee" || !emp.Active {
		t.Fatalf("CreateEmployee = (%+v, %v)", emp, err)
	}
	emp2, err := repo.CreateEmployee(ctx, merchantID, EmployeeCreateRequest{
		FirstName: "Alice", LastName: "Alpha", PositionID: pos.ID, ContractTypeCode: "CDI",
	})
	if err != nil || emp2.Role != "employee" || !emp2.Active {
		t.Fatalf("CreateEmployee(2) = (%+v, %v)", emp2, err)
	}
	if n, err := repo.CountEmployeesByPositionID(ctx, merchantID, pos.ID); err != nil || n != 2 {
		t.Fatalf("CountEmployeesByPositionID = (%d, %v)", n, err)
	}
	list, total, err := repo.ListEmployees(ctx, merchantID, EmployeeListFilters{Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(list) != 2 || list[0].ID != emp.ID || list[1].ID != emp2.ID {
		t.Fatalf("ListEmployees = (%d/%d, %v) %+v", len(list), total, err, list)
	}
	if err := repo.UpdateEmployeesDisplayOrder(ctx, merchantID, []string{emp2.ID, emp.ID}); err != nil {
		t.Fatalf("UpdateEmployeesDisplayOrder = %v", err)
	}
	list, total, err = repo.ListEmployees(ctx, merchantID, EmployeeListFilters{Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(list) != 2 || list[0].ID != emp2.ID || list[1].ID != emp.ID {
		t.Fatalf("ListEmployees(after reorder) = (%d/%d, %v) %+v", len(list), total, err, list)
	}

	// lien user
	userID := "itest-emp-user"
	if _, err := db.ExecContext(ctx, `INSERT INTO users_rights (user_id, merchant_id, token, enabled) VALUES ($1, $2, 'tok-emp', true)`, userID, merchantID); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users (user_id, merchant_id, name, first_name, last_name, password, email, token) VALUES ($1, $2, 'Jean ITest', 'Jean', 'ITest', 'x', 'itest-emp@example.com', 'utok-emp')`, userID, merchantID); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if linked, err := repo.IsMerchantUserLinked(ctx, merchantID, userID); err != nil || !linked {
		t.Fatalf("IsMerchantUserLinked = (%v, %v)", linked, err)
	}
	if _, err := repo.UpdateEmployeeUserLink(ctx, merchantID, emp.ID, &userID); err != nil {
		t.Fatalf("UpdateEmployeeUserLink: %v", err)
	}
	if got, err := repo.GetActiveEmployeeByUserID(ctx, merchantID, userID); err != nil || got.ID != emp.ID {
		t.Fatalf("GetActiveEmployeeByUserID = (%+v, %v)", got, err)
	}
	if id, err := repo.GetEmployeeIDByMemberID(ctx, merchantID, userID); err != nil || id != emp.ID {
		t.Fatalf("GetEmployeeIDByMemberID = (%q, %v)", id, err)
	}

	// update + soft delete
	emp.FirstName = "Paul"
	if updated, err := repo.UpdateEmployee(ctx, merchantID, emp.ID, *emp); err != nil || updated.FirstName != "Paul" {
		t.Fatalf("UpdateEmployee = (%+v, %v)", updated, err)
	}
	if err := repo.SoftDeleteEmployee(ctx, merchantID, emp.ID); err != nil {
		t.Fatalf("SoftDeleteEmployee: %v", err)
	}
	if _, err := repo.GetEmployeeByID(ctx, merchantID, emp.ID); err == nil {
		t.Fatalf("GetEmployeeByID après delete devrait échouer")
	}
	if err := repo.SoftDeleteEmployeePosition(ctx, merchantID, pos.ID); err != nil {
		t.Fatalf("SoftDeleteEmployeePosition: %v", err)
	}
}
