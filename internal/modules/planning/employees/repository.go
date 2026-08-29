package employees

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListEmployees(ctx context.Context, merchantID string, filters EmployeeListFilters) ([]Employee, int, error) {
	db := dbx.GetDB(ctx, r.db)
	baseQuery := `
		FROM employees e
		LEFT JOIN planning_positions p ON p.id = e.position_id AND p.merchant_id = e.merchant_id AND p.enabled = TRUE
		WHERE e.merchant_id = ? AND e.enabled = TRUE
	`
	args := []interface{}{merchantID}
	if strings.TrimSpace(filters.Search) != "" {
		baseQuery += ` AND (e.first_name LIKE ? OR e.last_name LIKE ? OR e.email LIKE ? OR e.phone LIKE ? OR p.label LIKE ?)`
		search := "%" + strings.TrimSpace(filters.Search) + "%"
		args = append(args, search, search, search, search, search)
	}
	if filters.Active != nil {
		baseQuery += ` AND e.active = ?`
		args = append(args, *filters.Active)
	}
	if strings.TrimSpace(filters.PositionID) != "" {
		baseQuery += ` AND e.position_id = ?`
		args = append(args, strings.TrimSpace(filters.PositionID))
	}
	if strings.TrimSpace(filters.ContractType) != "" {
		baseQuery += ` AND e.contract_type_code = ?`
		args = append(args, strings.TrimSpace(filters.ContractType))
	}
	if strings.TrimSpace(filters.UserID) != "" {
		baseQuery += ` AND e.user_id = ?`
		args = append(args, strings.TrimSpace(filters.UserID))
	}
	if filters.Unlinked != nil {
		if *filters.Unlinked {
			baseQuery += ` AND e.user_id IS NULL`
		} else {
			baseQuery += ` AND e.user_id IS NOT NULL`
		}
	}

	countQuery := `SELECT COUNT(1) ` + baseQuery
	var totalItems int
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&totalItems); err != nil {
		return nil, 0, err
	}

	selectQuery := `
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone,
			e.contract_type_code, e.contract_start_date, e.contract_end_date, e.probation_end_date, e.last_medical_checkup_date,
			e.contract_hours, e.max_weekly_hours, e.required_rest_days, e.sunday_premium, e.night_premium,
			e.hourly_rate, e.gross_monthly_salary, e.employer_charges_pct, e.transport_cost, e.birth_date, e.gender, e.nationality,
			e.address, e.hr_comment, e.active, e.created_at, e.updated_at, e.deleted_at
	`
	query := selectQuery + baseQuery
	query += ` ORDER BY e.display_order ASC, e.last_name ASC, e.first_name ASC LIMIT ? OFFSET ?`
	dataArgs := append([]interface{}{}, args...)
	dataArgs = append(dataArgs, filters.PageSize, (filters.Page-1)*filters.PageSize)

	rows, err := db.QueryContext(ctx, query, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Employee, 0)
	for rows.Next() {
		item, err := scanEmployee(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, totalItems, rows.Err()
}

func (r *Repository) NextEmployeeDisplayOrder(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.db)
	var next int
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(display_order), 0) + 1
		FROM employees
		WHERE merchant_id = ? AND enabled = TRUE
	`, merchantID).Scan(&next); err != nil {
		return 0, err
	}
	return next, nil
}

func (r *Repository) UpdateEmployeesDisplayOrder(ctx context.Context, merchantID string, employeeIDs []string) error {
	return dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.db)

		normalizedIDs := make([]string, 0, len(employeeIDs))
		seen := make(map[string]struct{}, len(employeeIDs))
		for _, rawID := range employeeIDs {
			employeeID := strings.TrimSpace(rawID)
			if employeeID == "" {
				return models.ErrInvalidInput
			}
			if _, exists := seen[employeeID]; exists {
				return models.ErrInvalidInput
			}
			seen[employeeID] = struct{}{}
			normalizedIDs = append(normalizedIDs, employeeID)
		}

		inClause := sqlInPlaceholders(len(normalizedIDs))
		countArgs := make([]interface{}, 0, len(normalizedIDs)+1)
		countArgs = append(countArgs, merchantID)
		for _, employeeID := range normalizedIDs {
			countArgs = append(countArgs, employeeID)
		}

		var count int
		countQuery := `SELECT COUNT(1) FROM employees WHERE merchant_id = ? AND enabled = TRUE AND id IN (` + inClause + `)`
		if err := db.QueryRowContext(txCtx, countQuery, countArgs...).Scan(&count); err != nil {
			return err
		}
		if count != len(normalizedIDs) {
			return models.ErrPlanningEmployeeNotFound
		}

		caseParts := make([]string, 0, len(normalizedIDs))
		updateArgs := make([]interface{}, 0, len(normalizedIDs)*2+2+len(normalizedIDs))
		for index, employeeID := range normalizedIDs {
			caseParts = append(caseParts, "WHEN ? THEN ?")
			updateArgs = append(updateArgs, employeeID, index+1)
		}

		now := time.Now().UTC()
		updateArgs = append(updateArgs, now, merchantID)
		for _, employeeID := range normalizedIDs {
			updateArgs = append(updateArgs, employeeID)
		}

		updateQuery := `
			UPDATE employees
			SET display_order = CASE id ` + strings.Join(caseParts, " ") + ` ELSE display_order END,
				updated_at = ?
			WHERE merchant_id = ? AND enabled = TRUE AND id IN (` + inClause + `)
		`
		_, err := db.ExecContext(txCtx, updateQuery, updateArgs...)
		return err
	})
}

func (r *Repository) GetEmployeeByUserID(ctx context.Context, merchantID, userID string) (*Employee, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone,
			e.contract_type_code, e.contract_start_date, e.contract_end_date, e.probation_end_date, e.last_medical_checkup_date,
			e.contract_hours, e.max_weekly_hours, e.required_rest_days, e.sunday_premium, e.night_premium,
			e.hourly_rate, e.gross_monthly_salary, e.employer_charges_pct, e.transport_cost, e.birth_date, e.gender, e.nationality,
			e.address, e.hr_comment, e.active, e.created_at, e.updated_at, e.deleted_at
		FROM employees e
		LEFT JOIN planning_positions p ON p.id = e.position_id AND p.merchant_id = e.merchant_id AND p.enabled = TRUE
		WHERE e.merchant_id = ? AND e.user_id = ? AND e.enabled = TRUE
		LIMIT 1
	`, merchantID, strings.TrimSpace(userID))
	return scanEmployeeRow(row)
}

func (r *Repository) GetActiveEmployeeByUserID(ctx context.Context, merchantID, userID string) (*Employee, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone,
			e.contract_type_code, e.contract_start_date, e.contract_end_date, e.probation_end_date, e.last_medical_checkup_date,
			e.contract_hours, e.max_weekly_hours, e.required_rest_days, e.sunday_premium, e.night_premium,
			e.hourly_rate, e.gross_monthly_salary, e.employer_charges_pct, e.transport_cost, e.birth_date, e.gender, e.nationality,
			e.address, e.hr_comment, e.active, e.created_at, e.updated_at, e.deleted_at
		FROM employees e
		LEFT JOIN planning_positions p ON p.id = e.position_id AND p.merchant_id = e.merchant_id AND p.enabled = TRUE
		WHERE e.merchant_id = ? AND e.user_id = ? AND e.enabled = TRUE AND e.active = TRUE
		LIMIT 1
	`, merchantID, strings.TrimSpace(userID))
	return scanEmployeeRow(row)
}

func (r *Repository) IsMerchantUserLinked(ctx context.Context, merchantID, userID string) (bool, error) {
	db := dbx.GetDB(ctx, r.db)
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM users_rights ur
		INNER JOIN users u ON u.user_id = ur.user_id
		WHERE ur.merchant_id = ? AND ur.user_id = ? AND ur.enabled = TRUE AND u.enabled = TRUE
	`, merchantID, strings.TrimSpace(userID)).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) GetEmployeeIDByMemberID(ctx context.Context, merchantID, userID string) (string, error) {
	db := dbx.GetDB(ctx, r.db)
	var employeeID string
	err := db.QueryRowContext(ctx, `
		SELECT emp.id
		FROM employees emp
		INNER JOIN users_rights ur ON ur.user_id = emp.user_id AND ur.merchant_id = emp.merchant_id AND ur.enabled = TRUE
		WHERE emp.merchant_id = ? AND emp.user_id = ? AND emp.enabled = TRUE AND ur.enabled = TRUE
		LIMIT 1
	`, merchantID, userID).Scan(&employeeID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(employeeID), nil
}

func (r *Repository) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*Employee, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone,
			e.contract_type_code, e.contract_start_date, e.contract_end_date, e.probation_end_date, e.last_medical_checkup_date,
			e.contract_hours, e.max_weekly_hours, e.required_rest_days, e.sunday_premium, e.night_premium,
			e.hourly_rate, e.gross_monthly_salary, e.employer_charges_pct, e.transport_cost, e.birth_date, e.gender, e.nationality,
			e.address, e.hr_comment, e.active, e.created_at, e.updated_at, e.deleted_at
		FROM employees e
		LEFT JOIN planning_positions p ON p.id = e.position_id AND p.merchant_id = e.merchant_id AND p.enabled = TRUE
		WHERE e.merchant_id = ? AND e.id = ? AND e.enabled = TRUE
	`, merchantID, employeeID)
	item, err := scanEmployeeRow(row)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) CreateEmployee(ctx context.Context, merchantID string, req EmployeeCreateRequest) (*Employee, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	id := helpers.GeneratePrefixedID(helpers.PlanningEmployeeIDPrefix)
	displayOrder, err := r.NextEmployeeDisplayOrder(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	employee := Employee{
		ID:                     id,
		MerchantID:             merchantID,
		UserID:                 req.UserID,
		FirstName:              strings.TrimSpace(req.FirstName),
		LastName:               strings.TrimSpace(req.LastName),
		PositionID:             strings.TrimSpace(req.PositionID),
		PositionNote:           req.PositionNote,
		Email:                  req.Email,
		Phone:                  req.Phone,
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
	if position, err := r.GetEmployeePositionByID(ctx, merchantID, employee.PositionID); err == nil && position != nil {
		employee.Position = position.Label
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO employees (
			id, merchant_id, user_id, first_name, last_name, position_id, display_order, position_note, email, phone,
			contract_type_code, contract_start_date, contract_end_date, probation_end_date, last_medical_checkup_date,
			contract_hours, max_weekly_hours, required_rest_days, sunday_premium, night_premium,
			hourly_rate, gross_monthly_salary, employer_charges_pct, transport_cost, birth_date, gender, nationality,
			address, hr_comment, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, employee.ID, employee.MerchantID, employee.UserID, employee.FirstName, employee.LastName, employee.PositionID, displayOrder, employee.PositionNote, employee.Email, employee.Phone, employee.ContractTypeCode, employee.ContractStartDate, employee.ContractEndDate, employee.ProbationEndDate, employee.LastMedicalCheckupDate, employee.ContractHours, employee.MaxWeeklyHours, employee.RequiredRestDays, employee.SundayPremium, employee.NightPremium, employee.HourlyRate, employee.GrossMonthlySalary, employee.EmployerChargesPct, employee.TransportCost, employee.BirthDate, employee.Gender, employee.Nationality, employee.Address, employee.HrComment, employee.Active, employee.CreatedAt, employee.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &employee, nil
}

func (r *Repository) UpdateEmployee(ctx context.Context, merchantID, employeeID string, employee Employee) (*Employee, error) {
	db := dbx.GetDB(ctx, r.db)
	employee.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE employees
		SET user_id = ?, first_name = ?, last_name = ?, position_id = ?, position_note = ?, email = ?, phone = ?,
			contract_type_code = ?, contract_start_date = ?, contract_end_date = ?, probation_end_date = ?, last_medical_checkup_date = ?,
			contract_hours = ?, max_weekly_hours = ?, required_rest_days = ?, sunday_premium = ?, night_premium = ?,
			hourly_rate = ?, gross_monthly_salary = ?, employer_charges_pct = ?, transport_cost = ?, birth_date = ?, gender = ?, nationality = ?,
			address = ?, hr_comment = ?, active = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, employee.UserID, employee.FirstName, employee.LastName, employee.PositionID, employee.PositionNote, employee.Email, employee.Phone, employee.ContractTypeCode, employee.ContractStartDate, employee.ContractEndDate, employee.ProbationEndDate, employee.LastMedicalCheckupDate, employee.ContractHours, employee.MaxWeeklyHours, employee.RequiredRestDays, employee.SundayPremium, employee.NightPremium, employee.HourlyRate, employee.GrossMonthlySalary, employee.EmployerChargesPct, employee.TransportCost, employee.BirthDate, employee.Gender, employee.Nationality, employee.Address, employee.HrComment, employee.Active, employee.UpdatedAt, merchantID, employeeID)
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

func (r *Repository) UpdateEmployeeUserLink(ctx context.Context, merchantID, employeeID string, userID *string) (*Employee, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE employees
		SET user_id = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`, userID, now, merchantID, employeeID)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetEmployeeByID(ctx, merchantID, employeeID)
}

func (r *Repository) SoftDeleteEmployee(ctx context.Context, merchantID, employeeID string) error {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE employees
		SET active = FALSE, enabled = FALSE, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
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

func sqlInPlaceholders(size int) string {
	if size <= 0 {
		return ""
	}
	parts := make([]string, size)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

type scannableRows interface {
	Scan(dest ...any) error
}

func scanEmployeeRow(row scannable) (*Employee, error) {
	item := &Employee{}
	var userID, positionLabel, positionNote, email, phone, gender, nationality, address, hrComment sql.NullString
	var contractStartDate, contractEndDate, probationEndDate, medicalDate, birthDate sql.NullTime
	var contractHours, maxWeeklyHours, employerChargesPct sql.NullFloat64
	var hourlyRate, grossMonthlySalary, transportCost sql.NullInt64
	var requiredRestDays sql.NullInt64
	var sundayPremium, nightPremium, active sql.NullBool
	var deletedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.MerchantID, &userID, &item.FirstName, &item.LastName, &item.PositionID, &positionLabel, &positionNote, &email, &phone,
		&item.ContractTypeCode, &contractStartDate, &contractEndDate, &probationEndDate, &medicalDate,
		&contractHours, &maxWeeklyHours, &requiredRestDays, &sundayPremium, &nightPremium,
		&hourlyRate, &grossMonthlySalary, &employerChargesPct, &transportCost, &birthDate, &gender, &nationality,
		&address, &hrComment, &active, &item.CreatedAt, &item.UpdatedAt, &deletedAt,
	); err != nil {
		return nil, err
	}
	if userID.Valid {
		item.UserID = &userID.String
	}
	if positionLabel.Valid {
		item.Position = positionLabel.String
	}
	if positionNote.Valid {
		item.PositionNote = &positionNote.String
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
	item.MemberID = &item.ID
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}

func scanEmployee(rows scannableRows) (*Employee, error) {
	item := &Employee{}
	var userID, positionLabel, positionNote, email, phone, gender, nationality, address, hrComment sql.NullString
	var contractStartDate, contractEndDate, probationEndDate, medicalDate, birthDate sql.NullTime
	var contractHours, maxWeeklyHours, employerChargesPct sql.NullFloat64
	var hourlyRate, grossMonthlySalary, transportCost sql.NullInt64
	var requiredRestDays sql.NullInt64
	var sundayPremium, nightPremium, active sql.NullBool
	var deletedAt sql.NullTime
	if err := rows.Scan(
		&item.ID, &item.MerchantID, &userID, &item.FirstName, &item.LastName, &item.PositionID, &positionLabel, &positionNote, &email, &phone,
		&item.ContractTypeCode, &contractStartDate, &contractEndDate, &probationEndDate, &medicalDate,
		&contractHours, &maxWeeklyHours, &requiredRestDays, &sundayPremium, &nightPremium,
		&hourlyRate, &grossMonthlySalary, &employerChargesPct, &transportCost, &birthDate, &gender, &nationality,
		&address, &hrComment, &active, &item.CreatedAt, &item.UpdatedAt, &deletedAt,
	); err != nil {
		return nil, err
	}
	if userID.Valid {
		item.UserID = &userID.String
	}
	if positionLabel.Valid {
		item.Position = positionLabel.String
	}
	if positionNote.Valid {
		item.PositionNote = &positionNote.String
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
