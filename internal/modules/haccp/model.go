package haccp

import "time"

const (
	ActivityTypeTemperatures = "temperatures"
	ActivityTypeCleanings    = "cleanings"
)

type Zone struct {
	ID            string     `json:"id"`
	MerchantID    string     `json:"merchant_id"`
	Name          string     `json:"name"`
	TargetTempMin float64    `json:"target_temp_min"`
	TargetTempMax float64    `json:"target_temp_max"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Enabled       bool       `json:"enabled"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

type Reading struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id,omitempty"`
	MerchantID string    `json:"merchant_id"`
	ZoneID     string    `json:"zone_id"`
	ZoneName   *string   `json:"zone_name,omitempty"`
	Value      float64   `json:"value"`
	Status     string    `json:"status"`
	PhotoURL   *string   `json:"photo_url,omitempty"`
	Signature  *string   `json:"signature,omitempty"`
	Comment    *string   `json:"comment,omitempty"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TemperatureSession struct {
	ID         string    `json:"id"`
	MerchantID string    `json:"merchant_id"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TemperatureSessionDetail struct {
	ID          string              `json:"id"`
	MerchantID  string              `json:"merchant_id"`
	Status      string              `json:"status"`
	PerformedAt time.Time           `json:"performed_at"`
	PerformedBy ActivityPerformedBy `json:"performed_by"`
	Readings    []Reading           `json:"readings"`
}

type HACCPSettings struct {
	MerchantID                 string `json:"merchant_id"`
	TempEntryRequired          bool   `json:"temp_entry_required"`
	TempCorrectiveActions      bool   `json:"temp_corrective_actions"`
	TempBlockPastDates         bool   `json:"temp_block_past_dates"`
	TraceabilityProductName    bool   `json:"traceability_product_name"`
	TraceabilityBlockPastDates bool   `json:"traceability_block_past_dates"`
	CleaningPhoto              bool   `json:"cleaning_photo"`
	CleaningBlockPastDates     bool   `json:"cleaning_block_past_dates"`
	ReceptionOtherProducts     bool   `json:"reception_other_products"`
	ReceptionControlSample     bool   `json:"reception_control_sample"`
	ReceptionBlockPastDates    bool   `json:"reception_block_past_dates"`
	ReceptionPhoto             bool   `json:"reception_photo"`
	ReceptionNonConformities   bool   `json:"reception_non_conformities"`
	OilsBlockPastDates         bool   `json:"oils_block_past_dates"`
	OilsPolarCompoundRate      bool   `json:"oils_polar_compound_rate"`
	OilsPhoto                  bool   `json:"oils_photo"`
	ProductionBlockPastDates   bool   `json:"production_block_past_dates"`
	ProductionTraceability     bool   `json:"production_traceability"`
	CoolingBlockPastDates      bool   `json:"cooling_block_past_dates"`
	FreezingBlockPastDates     bool   `json:"freezing_block_past_dates"`
	ReheatingBlockPastDates    bool   `json:"reheating_block_past_dates"`
	HoldingBlockPastDates      bool   `json:"holding_block_past_dates"`
	HoldingCorrectiveActions   bool   `json:"holding_corrective_actions"`
	NotifAuthorization         bool   `json:"notif_authorization"`
	NotifSecurity              bool   `json:"notif_security"`
}

type CreateZoneRequest struct {
	Name          string  `json:"name"`
	TargetTempMin float64 `json:"target_temp_min"`
	TargetTempMax float64 `json:"target_temp_max"`
}

type ReplaceZoneRequest struct {
	Name          string  `json:"name"`
	TargetTempMin float64 `json:"target_temp_min"`
	TargetTempMax float64 `json:"target_temp_max"`
}

type BatchCreateReadingsRequest struct {
	Readings []BatchReadingInput `json:"readings"`
}

type BatchReadingInput struct {
	ZoneID    string  `json:"zone_id"`
	Value     float64 `json:"value"`
	PhotoURL  *string `json:"photo_url"`
	Signature *string `json:"signature"`
	Comment   *string `json:"comment,omitempty"`
}

type BatchCreateReadingsResponse struct {
	SessionID string    `json:"session_id"`
	Readings  []Reading `json:"readings"`
}

type CleaningTask struct {
	ID             string     `json:"id"`
	MerchantID     string     `json:"merchant_id"`
	Zone           string     `json:"zone"`
	Name           string     `json:"name"`
	FrequencyUnit  string     `json:"frequency_unit"`
	FrequencyCount int        `json:"frequency_count"`
	Active         bool       `json:"active"`
	Enabled        bool       `json:"enabled"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

type CleaningTaskWithComputed struct {
	ID             string `json:"id"`
	Zone           string `json:"zone"`
	Name           string `json:"name"`
	FrequencyUnit  string `json:"frequency_unit"`
	FrequencyCount int    `json:"frequency_count"`
	Active         bool   `json:"active"`
	Computed       struct {
		DueToday        bool       `json:"due_today"`
		Overdue         bool       `json:"overdue"`
		LastExecutionAt *time.Time `json:"last_execution_at"`
	} `json:"computed"`
}

type CleaningExecution struct {
	ID          string              `json:"id"`
	TaskID      string              `json:"task_id"`
	MerchantID  string              `json:"merchant_id"`
	Comment     *string             `json:"comment,omitempty"`
	PhotoURL    *string             `json:"photo_url,omitempty"`
	Status      string              `json:"status"`
	CreatedBy   string              `json:"created_by"`
	PerformedBy ActivityPerformedBy `json:"performed_by,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type CleaningTaskSummary struct {
	ID   string `json:"id"`
	Zone string `json:"zone"`
	Name string `json:"name"`
}

type CleaningExecutionDetail struct {
	ID          string              `json:"id"`
	TaskID      string              `json:"task_id"`
	Task        CleaningTaskSummary `json:"task"`
	MerchantID  string              `json:"merchant_id"`
	Comment     *string             `json:"comment,omitempty"`
	PhotoURL    *string             `json:"photo_url,omitempty"`
	Status      string              `json:"status"`
	PerformedAt time.Time           `json:"performed_at"`
	PerformedBy ActivityPerformedBy `json:"performed_by"`
}

type CreateCleaningExecutionRequest struct {
	TaskID   string  `json:"task_id"`
	Comment  *string `json:"comment"`
	PhotoURL *string `json:"photo_url"`
}

type CreateCleaningTaskRequest struct {
	Zone           string `json:"zone"`
	Name           string `json:"name"`
	FrequencyUnit  string `json:"frequency_unit"`
	FrequencyCount int    `json:"frequency_count"`
	Active         *bool  `json:"active,omitempty"`
}

type UpdateCleaningTaskRequest struct {
	Zone           string `json:"zone"`
	Name           string `json:"name"`
	FrequencyUnit  string `json:"frequency_unit"`
	FrequencyCount int    `json:"frequency_count"`
	Active         *bool  `json:"active,omitempty"`
}

type CleaningExecutionsListParams struct {
	TaskID   string `json:"task_id"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

type CleaningExecutionsListResponse struct {
	CleaningExecutions []CleaningExecution `json:"cleaning_executions"`
	Page               int                 `json:"page"`
	PageSize           int                 `json:"page_size"`
	TotalItems         int                 `json:"total_items"`
	TotalPages         int                 `json:"total_pages"`
}

type ActivitiesListParams struct {
	Date     string `json:"date"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

type ActivityPerformedBy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ActivityItem struct {
	ID          string              `json:"id"`
	Type        string              `json:"type"`
	Status      *string             `json:"status,omitempty"`
	PerformedAt time.Time           `json:"performed_at"`
	PerformedBy ActivityPerformedBy `json:"performed_by"`
	Title       string              `json:"title"`
	Subtitle    string              `json:"subtitle"`
	Metadata    map[string]any      `json:"metadata,omitempty"`
}

type ActivitiesListResponse struct {
	Activities []ActivityItem `json:"activities"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalItems int            `json:"total_items"`
	TotalPages int            `json:"total_pages"`
	Date       string         `json:"date"`
	Type       string         `json:"type,omitempty"`
}

type GoodsReceipt struct {
	ID                 string    `json:"id"`
	MerchantID         string    `json:"merchant_id"`
	Supplier           string    `json:"supplier"`
	ProductType        string    `json:"product_type"`
	BatchNumber        string    `json:"batch_number"`
	ProductTemp        float64   `json:"product_temp"`
	ControlSample      *string   `json:"control_sample,omitempty"`
	QuantitiesVerified bool      `json:"quantities_verified"`
	NonConformities    []string  `json:"non_conformities"`
	Comment            *string   `json:"comment,omitempty"`
	InvoiceURL         *string   `json:"invoice_url,omitempty"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CreateGoodsReceiptRequest struct {
	Supplier           string   `json:"supplier"`
	ProductType        string   `json:"product_type"`
	BatchNumber        string   `json:"batch_number"`
	ProductTemp        float64  `json:"product_temp"`
	ControlSample      *string  `json:"control_sample"`
	QuantitiesVerified bool     `json:"quantities_verified"`
	NonConformities    []string `json:"non_conformities"`
	Comment            *string  `json:"comment"`
	InvoiceURL         *string  `json:"invoice_url"`
}
