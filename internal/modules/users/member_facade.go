package users

import (
	"context"
	"database/sql"

	planningemployees "welloresto-api/internal/modules/planning/employees"
)

type planningMemberEmployeeFacade struct {
	repo    *planningemployees.Repository
	service *planningemployees.Service
}

func newMemberEmployeeFacade(db *sql.DB) memberEmployeeFacade {
	repo := planningemployees.NewRepository(db)
	return &planningMemberEmployeeFacade{
		repo:    repo,
		service: planningemployees.NewService(repo),
	}
}

func (f *planningMemberEmployeeFacade) GetActiveEmployeeByUserID(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
	return f.repo.GetActiveEmployeeByUserID(ctx, merchantID, userID)
}

func (f *planningMemberEmployeeFacade) CreateEmployee(ctx context.Context, req planningemployees.EmployeeCreateRequest) (*planningemployees.Employee, error) {
	return f.service.CreateEmployee(ctx, req)
}

func (f *planningMemberEmployeeFacade) LinkEmployeeUser(ctx context.Context, employeeID string, req planningemployees.EmployeeUserLinkRequest) (*planningemployees.Employee, error) {
	return f.service.LinkEmployeeUser(ctx, employeeID, req)
}

func (f *planningMemberEmployeeFacade) UpdateEmployee(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error) {
	return f.service.UpdateEmployee(ctx, employeeID, req)
}
