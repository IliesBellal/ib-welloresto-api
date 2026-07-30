package employees

import (
	"encoding/json"
	"time"

	"welloresto-api/internal/models"
)

type EmployeePosition struct {
	ID            string     `json:"id"`
	MerchantID    string     `json:"merchant_id"`
	Label         string     `json:"label"`
	Color         string     `json:"color"`
	SortOrder     int        `json:"sort_order"`
	Active        bool       `json:"active"`
	EmployeeCount int        `json:"employee_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

type Employee struct {
	ID                     string     `json:"id"`
	MerchantID             string     `json:"merchant_id"`
	UserID                 *string    `json:"user_id,omitempty"`
	MemberID               *string    `json:"-"`
	FirstName              string     `json:"first_name"`
	LastName               string     `json:"last_name"`
	PositionID             string     `json:"position_id"`
	Position               string     `json:"position"`
	PositionNote           *string    `json:"position_note,omitempty"`
	JobTitle               *string    `json:"job_title,omitempty"`
	Email                  *string    `json:"email,omitempty"`
	Phone                  *string    `json:"phone,omitempty"`
	Role                   string     `json:"role"`
	ContractTypeCode       string     `json:"contract_type_code"`
	ContractStartDate      *time.Time `json:"contract_start_date,omitempty"`
	ContractEndDate        *time.Time `json:"contract_end_date,omitempty"`
	ProbationEndDate       *time.Time `json:"probation_end_date,omitempty"`
	LastMedicalCheckupDate *time.Time `json:"last_medical_checkup_date,omitempty"`
	ContractHours          float64    `json:"contract_hours"`
	MaxWeeklyHours         float64    `json:"max_weekly_hours"`
	RequiredRestDays       int        `json:"required_rest_days"`
	SundayPremium          bool       `json:"sunday_premium"`
	NightPremium           bool       `json:"night_premium"`
	HourlyRate             int64      `json:"hourly_rate"`
	GrossMonthlySalary     int64      `json:"gross_monthly_salary"`
	EmployerChargesPct     float64    `json:"employer_charges_pct"`
	TransportCost          int64      `json:"transport_cost"`
	BirthDate              *time.Time `json:"birth_date,omitempty"`
	Gender                 *string    `json:"gender,omitempty"`
	Nationality            *string    `json:"nationality,omitempty"`
	Address                *string    `json:"address,omitempty"`
	HrComment              *string    `json:"hr_comment,omitempty"`
	Active                 bool       `json:"active"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	DeletedAt              *time.Time `json:"deleted_at,omitempty"`
}

type EmployeeListFilters struct {
	Search       string
	Active       *bool
	PositionID   string
	ContractType string
	UserID       string
	Unlinked     *bool
	Page         int
	PageSize     int
}

type EmployeeUserLinkRequest struct {
	UserID string `json:"user_id"`
}

type EmployeeDisplayOrderUpdateRequest struct {
	EmployeeIDs []string `json:"employee_ids"`
}

type EmployeeCreateRequest struct {
	UserID                 *string    `json:"user_id,omitempty"`
	MemberID               *string    `json:"-"`
	FirstName              string     `json:"first_name"`
	LastName               string     `json:"last_name"`
	PositionID             string     `json:"position_id"`
	PositionNote           *string    `json:"position_note,omitempty"`
	JobTitle               *string    `json:"job_title,omitempty"`
	Email                  *string    `json:"email,omitempty"`
	Phone                  *string    `json:"phone,omitempty"`
	Role                   *string    `json:"role,omitempty"`
	ContractTypeCode       string     `json:"contract_type_code"`
	ContractStartDate      *time.Time `json:"contract_start_date,omitempty"`
	ContractEndDate        *time.Time `json:"contract_end_date,omitempty"`
	ProbationEndDate       *time.Time `json:"probation_end_date,omitempty"`
	LastMedicalCheckupDate *time.Time `json:"last_medical_checkup_date,omitempty"`
	ContractHours          *float64   `json:"contract_hours,omitempty"`
	MaxWeeklyHours         *float64   `json:"max_weekly_hours,omitempty"`
	RequiredRestDays       *int       `json:"required_rest_days,omitempty"`
	SundayPremium          *bool      `json:"sunday_premium,omitempty"`
	NightPremium           *bool      `json:"night_premium,omitempty"`
	HourlyRate             *int64     `json:"hourly_rate,omitempty"`
	GrossMonthlySalary     *int64     `json:"gross_monthly_salary,omitempty"`
	EmployerChargesPct     *float64   `json:"employer_charges_pct,omitempty"`
	TransportCost          *int64     `json:"transport_cost,omitempty"`
	BirthDate              *time.Time `json:"birth_date,omitempty"`
	Gender                 *string    `json:"gender,omitempty"`
	Nationality            *string    `json:"nationality,omitempty"`
	Address                *string    `json:"address,omitempty"`
	HrComment              *string    `json:"hr_comment,omitempty"`
	Active                 *bool      `json:"active,omitempty"`
}

type EmployeeUpdateRequest struct {
	UserID                 *string    `json:"user_id,omitempty"`
	FirstName              *string    `json:"first_name,omitempty"`
	LastName               *string    `json:"last_name,omitempty"`
	PositionID             *string    `json:"position_id,omitempty"`
	PositionNote           *string    `json:"position_note,omitempty"`
	JobTitle               *string    `json:"job_title,omitempty"`
	Email                  *string    `json:"email,omitempty"`
	Phone                  *string    `json:"phone,omitempty"`
	Role                   *string    `json:"role,omitempty"`
	ContractTypeCode       *string    `json:"contract_type_code,omitempty"`
	ContractStartDate      *time.Time `json:"contract_start_date,omitempty"`
	ContractEndDate        *time.Time `json:"contract_end_date,omitempty"`
	ProbationEndDate       *time.Time `json:"probation_end_date,omitempty"`
	LastMedicalCheckupDate *time.Time `json:"last_medical_checkup_date,omitempty"`
	ContractHours          *float64   `json:"contract_hours,omitempty"`
	MaxWeeklyHours         *float64   `json:"max_weekly_hours,omitempty"`
	RequiredRestDays       *int       `json:"required_rest_days,omitempty"`
	SundayPremium          *bool      `json:"sunday_premium,omitempty"`
	NightPremium           *bool      `json:"night_premium,omitempty"`
	HourlyRate             *int64     `json:"hourly_rate,omitempty"`
	GrossMonthlySalary     *int64     `json:"gross_monthly_salary,omitempty"`
	EmployerChargesPct     *float64   `json:"employer_charges_pct,omitempty"`
	TransportCost          *int64     `json:"transport_cost,omitempty"`
	BirthDate              *time.Time `json:"birth_date,omitempty"`
	Gender                 *string    `json:"gender,omitempty"`
	Nationality            *string    `json:"nationality,omitempty"`
	Address                *string    `json:"address,omitempty"`
	HrComment              *string    `json:"hr_comment,omitempty"`
	Active                 *bool      `json:"active,omitempty"`
}

// UnmarshalJSON accepts date-only ("2006-01-02") strings for the contract/HR
// date fields, matching what HTML date inputs (and the rest of this API,
// see models.DateOnly) send — the default time.Time decoding requires a full
// RFC3339 timestamp and rejects plain dates.
func (r *EmployeeCreateRequest) UnmarshalJSON(data []byte) error {
	type alias EmployeeCreateRequest
	aux := &struct {
		ContractStartDate      *models.DateOnly `json:"contract_start_date,omitempty"`
		ContractEndDate        *models.DateOnly `json:"contract_end_date,omitempty"`
		ProbationEndDate       *models.DateOnly `json:"probation_end_date,omitempty"`
		LastMedicalCheckupDate *models.DateOnly `json:"last_medical_checkup_date,omitempty"`
		BirthDate              *models.DateOnly `json:"birth_date,omitempty"`
		*alias
	}{alias: (*alias)(r)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.ContractStartDate = dateOnlyToTimePtr(aux.ContractStartDate)
	r.ContractEndDate = dateOnlyToTimePtr(aux.ContractEndDate)
	r.ProbationEndDate = dateOnlyToTimePtr(aux.ProbationEndDate)
	r.LastMedicalCheckupDate = dateOnlyToTimePtr(aux.LastMedicalCheckupDate)
	r.BirthDate = dateOnlyToTimePtr(aux.BirthDate)
	return nil
}

// UnmarshalJSON accepts date-only ("2006-01-02") strings — see
// EmployeeCreateRequest.UnmarshalJSON for why this is needed.
func (r *EmployeeUpdateRequest) UnmarshalJSON(data []byte) error {
	type alias EmployeeUpdateRequest
	aux := &struct {
		ContractStartDate      *models.DateOnly `json:"contract_start_date,omitempty"`
		ContractEndDate        *models.DateOnly `json:"contract_end_date,omitempty"`
		ProbationEndDate       *models.DateOnly `json:"probation_end_date,omitempty"`
		LastMedicalCheckupDate *models.DateOnly `json:"last_medical_checkup_date,omitempty"`
		BirthDate              *models.DateOnly `json:"birth_date,omitempty"`
		*alias
	}{alias: (*alias)(r)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.ContractStartDate = dateOnlyToTimePtr(aux.ContractStartDate)
	r.ContractEndDate = dateOnlyToTimePtr(aux.ContractEndDate)
	r.ProbationEndDate = dateOnlyToTimePtr(aux.ProbationEndDate)
	r.LastMedicalCheckupDate = dateOnlyToTimePtr(aux.LastMedicalCheckupDate)
	r.BirthDate = dateOnlyToTimePtr(aux.BirthDate)
	return nil
}

func dateOnlyToTimePtr(d *models.DateOnly) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time()
	return &t
}

type EmployeePositionListFilters struct {
	Search string
	Active *bool
}

type EmployeePositionCreateRequest struct {
	Label     string `json:"label"`
	Color     string `json:"color"`
	SortOrder *int   `json:"sort_order,omitempty"`
	Active    *bool  `json:"active,omitempty"`
}

type EmployeePositionUpdateRequest struct {
	Label     *string `json:"label,omitempty"`
	Color     *string `json:"color,omitempty"`
	SortOrder *int    `json:"sort_order,omitempty"`
	Active    *bool   `json:"active,omitempty"`
}
