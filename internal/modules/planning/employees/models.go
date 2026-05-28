package employees

import "time"

type EmployeePosition struct {
	ID            string     `json:"id"`
	MerchantID    string     `json:"merchant_id"`
	Label         string     `json:"label"`
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
}

type EmployeeCreateRequest struct {
	UserID                 *string    `json:"user_id,omitempty"`
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

type EmployeePositionListFilters struct {
	Search string
	Active *bool
}

type EmployeePositionCreateRequest struct {
	Label     string `json:"label"`
	SortOrder *int   `json:"sort_order,omitempty"`
	Active    *bool  `json:"active,omitempty"`
}

type EmployeePositionUpdateRequest struct {
	Label     *string `json:"label,omitempty"`
	SortOrder *int    `json:"sort_order,omitempty"`
	Active    *bool   `json:"active,omitempty"`
}
