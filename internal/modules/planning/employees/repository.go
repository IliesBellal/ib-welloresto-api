package employees

import (
	"context"
	"database/sql"
	"strings"
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

func (r *Repository) ListEmployees(ctx context.Context, merchantID string, filters EmployeeListFilters) ([]Employee, error) {
	db := dbutils.GetDB(ctx, r.db)
	query := `
		SELECT id, merchant_id, user_id, first_name, last_name, position, job_title, email, phone, role,
			contract_type_code, contract_start_date, contract_end_date, probation_end_date, last_medical_checkup_date,
			contract_hours, max_weekly_hours, required_rest_days, sunday_premium, night_premium, time_tracking_mode_code,
			hourly_rate, gross_monthly_salary, employer_charges_pct, transport_cost, birth_date, gender, nationality,
			address, hr_comment, active, created_at, updated_at, deleted_at
		FROM employees
		WHERE merchant_id = ? AND enabled = 1
	`
	args := []interface{}{merchantID}
	if strings.TrimSpace(filters.Search) != "" {
		query += ` AND (first_name LIKE ? OR last_name LIKE ? OR email LIKE ? OR phone LIKE ?)`
		search := "%" + strings.TrimSpace(filters.Search) + "%"
		args = append(args, search, search, search, search)
	}
	if filters.Active != nil {
		query += ` AND active = ?`
		args = append(args, *filters.Active)
	}
	if strings.TrimSpace(filters.Position) != "" {
		query += ` AND position = ?`
		args = append(args, strings.TrimSpace(filters.Position))
	}
	if strings.TrimSpace(filters.ContractType) != "" {
		query += ` AND contract_type_code = ?`
		args = append(args, strings.TrimSpace(filters.ContractType))
	}
	query += ` ORDER BY last_name ASC, first_name ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Employee, 0)
	for rows.Next() {
		item, err := scanEmployee(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*Employee, error) {
	db := dbutils.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, user_id, first_name, last_name, position, job_title, email, phone, role,
			contract_type_code, contract_start_date, contract_end_date, probation_end_date, last_medical_checkup_date,
			contract_hours, max_weekly_hours, required_rest_days, sunday_premium, night_premium, time_tracking_mode_code,
			hourly_rate, gross_monthly_salary, employer_charges_pct, transport_cost, birth_date, gender, nationality,
			address, hr_comment, active, created_at, updated_at, deleted_at
		FROM employees
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, merchantID, employeeID)
	item, err := scanEmployeeRow(row)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) CreateEmployee(ctx context.Context, merchantID string, req EmployeeCreateRequest) (*Employee, error) {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	id := helpers.GeneratePrefixedID(helpers.PlanningEmployeeIDPrefix)
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	role := "employee"
	if req.Role != nil && strings.TrimSpace(*req.Role) != "" {
		role = strings.ToLower(strings.TrimSpace(*req.Role))
	}
	employee := Employee{
		ID:                     id,
		MerchantID:             merchantID,
		UserID:                 req.UserID,
		FirstName:              strings.TrimSpace(req.FirstName),
		LastName:               strings.TrimSpace(req.LastName),
		Position:               strings.TrimSpace(req.Position),
		JobTitle:               req.JobTitle,
		Email:                  req.Email,
		Phone:                  req.Phone,
		Role:                   role,
		ContractTypeCode:       strings.TrimSpace(req.ContractTypeCode),
		ContractStartDate:      req.ContractStartDate,
		ContractEndDate:        req.ContractEndDate,
		ProbationEndDate:       req.ProbationEndDate,
		LastMedicalCheckupDate: req.LastMedicalCheckupDate,
		ContractHours:          35,
		MaxWeeklyHours:         35,
		RequiredRestDays:       2,
		SundayPremium:          false,
		NightPremium:           false,
		TimeTrackingModeCode:   strings.TrimSpace(req.TimeTrackingModeCode),
		HourlyRate:             0,
		GrossMonthlySalary:     0,
		EmployerChargesPct:     45,
		TransportCost:          0,
		BirthDate:              req.BirthDate,
		Gender:                 req.Gender,
		Nationality:            req.Nationality,
		Address:                req.Address,
		HrComment:              req.HrComment,
		Active:                 active,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if req.ContractHours != nil {
		employee.ContractHours = *req.ContractHours
	}
	if req.MaxWeeklyHours != nil {
		employee.MaxWeeklyHours = *req.MaxWeeklyHours
	}
	if req.RequiredRestDays != nil {
		employee.RequiredRestDays = *req.RequiredRestDays
	}
	if req.SundayPremium != nil {
		employee.SundayPremium = *req.SundayPremium
	}
	if req.NightPremium != nil {
		employee.NightPremium = *req.NightPremium
	}
	if req.HourlyRate != nil {
		employee.HourlyRate = *req.HourlyRate
	}
	if req.GrossMonthlySalary != nil {
		employee.GrossMonthlySalary = *req.GrossMonthlySalary
	}
	if req.EmployerChargesPct != nil {
		employee.EmployerChargesPct = *req.EmployerChargesPct
	}
	if req.TransportCost != nil {
		employee.TransportCost = *req.TransportCost
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO employees (
			id, merchant_id, user_id, first_name, last_name, position, job_title, email, phone, role,
			contract_type_code, contract_start_date, contract_end_date, probation_end_date, last_medical_checkup_date,
			contract_hours, max_weekly_hours, required_rest_days, sunday_premium, night_premium, time_tracking_mode_code,
			hourly_rate, gross_monthly_salary, employer_charges_pct, transport_cost, birth_date, gender, nationality,
			address, hr_comment, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, employee.ID, employee.MerchantID, employee.UserID, employee.FirstName, employee.LastName, employee.Position, employee.JobTitle, employee.Email, employee.Phone, employee.Role, employee.ContractTypeCode, employee.ContractStartDate, employee.ContractEndDate, employee.ProbationEndDate, employee.LastMedicalCheckupDate, employee.ContractHours, employee.MaxWeeklyHours, employee.RequiredRestDays, employee.SundayPremium, employee.NightPremium, employee.TimeTrackingModeCode, employee.HourlyRate, employee.GrossMonthlySalary, employee.EmployerChargesPct, employee.TransportCost, employee.BirthDate, employee.Gender, employee.Nationality, employee.Address, employee.HrComment, employee.Active, employee.CreatedAt, employee.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &employee, nil
}

func (r *Repository) UpdateEmployee(ctx context.Context, merchantID, employeeID string, employee Employee) (*Employee, error) {
	db := dbutils.GetDB(ctx, r.db)
	employee.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE employees
		SET user_id = ?, first_name = ?, last_name = ?, position = ?, job_title = ?, email = ?, phone = ?, role = ?,
			contract_type_code = ?, contract_start_date = ?, contract_end_date = ?, probation_end_date = ?, last_medical_checkup_date = ?,
			contract_hours = ?, max_weekly_hours = ?, required_rest_days = ?, sunday_premium = ?, night_premium = ?, time_tracking_mode_code = ?,
			hourly_rate = ?, gross_monthly_salary = ?, employer_charges_pct = ?, transport_cost = ?, birth_date = ?, gender = ?, nationality = ?,
			address = ?, hr_comment = ?, active = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, employee.UserID, employee.FirstName, employee.LastName, employee.Position, employee.JobTitle, employee.Email, employee.Phone, employee.Role, employee.ContractTypeCode, employee.ContractStartDate, employee.ContractEndDate, employee.ProbationEndDate, employee.LastMedicalCheckupDate, employee.ContractHours, employee.MaxWeeklyHours, employee.RequiredRestDays, employee.SundayPremium, employee.NightPremium, employee.TimeTrackingModeCode, employee.HourlyRate, employee.GrossMonthlySalary, employee.EmployerChargesPct, employee.TransportCost, employee.BirthDate, employee.Gender, employee.Nationality, employee.Address, employee.HrComment, employee.Active, employee.UpdatedAt, merchantID, employeeID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	employee.ID = employeeID
	employee.MerchantID = merchantID
	return &employee, nil
}

func (r *Repository) SoftDeleteEmployee(ctx context.Context, merchantID, employeeID string) error {
	db := dbutils.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE employees
		SET active = 0, enabled = 0, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`, now, now, merchantID, employeeID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

type scannableRows interface {
	Scan(dest ...any) error
}

func scanEmployeeRow(row scannable) (*Employee, error) {
	item := &Employee{}
	var userID, jobTitle, email, phone, gender, nationality, address, hrComment sql.NullString
	var contractStartDate, contractEndDate, probationEndDate, medicalDate, birthDate sql.NullTime
	var contractHours, maxWeeklyHours, employerChargesPct sql.NullFloat64
	var hourlyRate, grossMonthlySalary, transportCost sql.NullInt64
	var requiredRestDays sql.NullInt64
	var sundayPremium, nightPremium, active sql.NullBool
	var deletedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.MerchantID, &userID, &item.FirstName, &item.LastName, &item.Position, &jobTitle, &email, &phone, &item.Role,
		&item.ContractTypeCode, &contractStartDate, &contractEndDate, &probationEndDate, &medicalDate,
		&contractHours, &maxWeeklyHours, &requiredRestDays, &sundayPremium, &nightPremium, &item.TimeTrackingModeCode,
		&hourlyRate, &grossMonthlySalary, &employerChargesPct, &transportCost, &birthDate, &gender, &nationality,
		&address, &hrComment, &active, &item.CreatedAt, &item.UpdatedAt, &deletedAt,
	); err != nil {
		return nil, err
	}
	if userID.Valid {
		item.UserID = &userID.String
	}
	if jobTitle.Valid {
		item.JobTitle = &jobTitle.String
	}
	if email.Valid {
		item.Email = &email.String
	}
	if phone.Valid {
		item.Phone = &phone.String
	}
	if contractStartDate.Valid {
		t := contractStartDate.Time
		item.ContractStartDate = &t
	}
	if contractEndDate.Valid {
		t := contractEndDate.Time
		item.ContractEndDate = &t
	}
	if probationEndDate.Valid {
		t := probationEndDate.Time
		item.ProbationEndDate = &t
	}
	if medicalDate.Valid {
		t := medicalDate.Time
		item.LastMedicalCheckupDate = &t
	}
	if birthDate.Valid {
		t := birthDate.Time
		item.BirthDate = &t
	}
	if gender.Valid {
		item.Gender = &gender.String
	}
	if nationality.Valid {
		item.Nationality = &nationality.String
	}
	if address.Valid {
		item.Address = &address.String
	}
	if hrComment.Valid {
		item.HrComment = &hrComment.String
	}
	item.ContractHours = contractHours.Float64
	item.MaxWeeklyHours = maxWeeklyHours.Float64
	item.RequiredRestDays = int(requiredRestDays.Int64)
	item.SundayPremium = sundayPremium.Bool
	item.NightPremium = nightPremium.Bool
	item.HourlyRate = hourlyRate.Int64
	item.GrossMonthlySalary = grossMonthlySalary.Int64
	item.EmployerChargesPct = employerChargesPct.Float64
	item.TransportCost = transportCost.Int64
	item.Active = active.Bool
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}

func scanEmployee(rows scannableRows) (*Employee, error) {
	item := &Employee{}
	var userID, jobTitle, email, phone, gender, nationality, address, hrComment sql.NullString
	var contractStartDate, contractEndDate, probationEndDate, medicalDate, birthDate sql.NullTime
	var contractHours, maxWeeklyHours, employerChargesPct sql.NullFloat64
	var hourlyRate, grossMonthlySalary, transportCost sql.NullInt64
	var requiredRestDays sql.NullInt64
	var sundayPremium, nightPremium, active sql.NullBool
	var deletedAt sql.NullTime
	if err := rows.Scan(
		&item.ID, &item.MerchantID, &userID, &item.FirstName, &item.LastName, &item.Position, &jobTitle, &email, &phone, &item.Role,
		&item.ContractTypeCode, &contractStartDate, &contractEndDate, &probationEndDate, &medicalDate,
		&contractHours, &maxWeeklyHours, &requiredRestDays, &sundayPremium, &nightPremium, &item.TimeTrackingModeCode,
		&hourlyRate, &grossMonthlySalary, &employerChargesPct, &transportCost, &birthDate, &gender, &nationality,
		&address, &hrComment, &active, &item.CreatedAt, &item.UpdatedAt, &deletedAt,
	); err != nil {
		return nil, err
	}
	if userID.Valid {
		item.UserID = &userID.String
	}
	if jobTitle.Valid {
		item.JobTitle = &jobTitle.String
	}
	if email.Valid {
		item.Email = &email.String
	}
	if phone.Valid {
		item.Phone = &phone.String
	}
	if contractStartDate.Valid {
		t := contractStartDate.Time
		item.ContractStartDate = &t
	}
	if contractEndDate.Valid {
		t := contractEndDate.Time
		item.ContractEndDate = &t
	}
	if probationEndDate.Valid {
		t := probationEndDate.Time
		item.ProbationEndDate = &t
	}
	if medicalDate.Valid {
		t := medicalDate.Time
		item.LastMedicalCheckupDate = &t
	}
	if birthDate.Valid {
		t := birthDate.Time
		item.BirthDate = &t
	}
	if gender.Valid {
		item.Gender = &gender.String
	}
	if nationality.Valid {
		item.Nationality = &nationality.String
	}
	if address.Valid {
		item.Address = &address.String
	}
	if hrComment.Valid {
		item.HrComment = &hrComment.String
	}
	item.ContractHours = contractHours.Float64
	item.MaxWeeklyHours = maxWeeklyHours.Float64
	item.RequiredRestDays = int(requiredRestDays.Int64)
	item.SundayPremium = sundayPremium.Bool
	item.NightPremium = nightPremium.Bool
	item.HourlyRate = hourlyRate.Int64
	item.GrossMonthlySalary = grossMonthlySalary.Int64
	item.EmployerChargesPct = employerChargesPct.Float64
	item.TransportCost = transportCost.Int64
	item.Active = active.Bool
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}
