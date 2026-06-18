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
	ID                string                    `json:"id"`
	SessionID         string                    `json:"session_id,omitempty"`
	MerchantID        string                    `json:"merchant_id"`
	ZoneID            string                    `json:"zone_id"`
	ZoneName          *string                   `json:"zone_name,omitempty"`
	Value             float64                   `json:"value"`
	Status            string                    `json:"status"`
	PhotoURL          *string                   `json:"photo_url,omitempty"`
	Signature         *string                   `json:"signature,omitempty"`
	Comment           *string                   `json:"comment,omitempty"`
	CorrectiveActions []ReadingCorrectiveAction `json:"corrective_actions,omitempty"`
	CreatedBy         string                    `json:"created_by"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
}

type CorrectiveAction struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Label         string `json:"label"`
	Description   string `json:"description,omitempty"`
	SeverityScope string `json:"severity_scope,omitempty"`
	Active        bool   `json:"active"`
}

type ReadingCorrectiveAction struct {
	ID            string   `json:"id"`
	ActionID      string   `json:"action_id"`
	Code          string   `json:"code"`
	Label         string   `json:"label"`
	Note          *string  `json:"note,omitempty"`
	PhotoURL      *string  `json:"photo_url,omitempty"`
	FollowUpValue *float64 `json:"follow_up_value,omitempty"`
}

type TemperatureSession struct {
	ID         string    `json:"id"`
	MerchantID string    `json:"merchant_id"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ActivityPerformedBy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TemperatureSessionDetail struct {
	ID          string              `json:"id"`
	MerchantID  string              `json:"merchant_id"`
	Status      string              `json:"status"`
	PerformedAt time.Time           `json:"performed_at"`
	PerformedBy ActivityPerformedBy `json:"performed_by"`
	Readings    []Reading           `json:"readings"`
}

type TemperatureSessionSummary struct {
	ID          string    `json:"id"`
	PerformedAt time.Time `json:"performed_at"`
	Status      string    `json:"status"`
}

type HubTemperatures struct {
	Enabled     bool                       `json:"enabled"`
	LastSession *TemperatureSessionSummary `json:"last_session"`
	Due         bool                       `json:"due"`
	Overdue     bool                       `json:"overdue"`
}

type HubCleaning struct {
	Enabled        bool `json:"enabled"`
	CompletedCount int  `json:"completed_count"`
	DueCount       int  `json:"due_count"`
	TotalCount     int  `json:"total_count"`
	OverdueCount   int  `json:"overdue_count"`
}

type HubPlaceholder struct {
	Enabled bool `json:"enabled"`
}

type HubData struct {
	GlobalStatus        string          `json:"global_status"`
	Temperatures        HubTemperatures `json:"temperatures"`
	Cleaning            HubCleaning     `json:"cleaning"`
	Reception           HubPlaceholder  `json:"reception"`
	IngredientsLabeling HubPlaceholder  `json:"ingredients_labeling"`
}

type HubResponse struct {
	Date        string    `json:"date"`
	GeneratedAt time.Time `json:"generated_at"`
	Hub         HubData   `json:"hub"`
}

type HACCPSettings struct {
	MerchantID                 string `json:"merchant_id"`
	TempEntryRequired          bool   `json:"temp_entry_required"`
	TempCorrectiveActions      bool   `json:"temp_corrective_actions"`
	TempFailurePhotoRequired   bool   `json:"temp_failure_photo_required"`
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
	ZoneID                   string   `json:"zone_id"`
	Value                    float64  `json:"value"`
	PhotoURL                 *string  `json:"photo_url"`
	Signature                *string  `json:"signature"`
	Comment                  *string  `json:"comment,omitempty"`
	CorrectiveActionIDs      []string `json:"corrective_action_ids,omitempty"`
	CorrectiveActionNote     *string  `json:"corrective_action_note,omitempty"`
	CorrectiveActionPhotoURL *string  `json:"corrective_action_photo_url,omitempty"`
	CorrectiveFollowUpValue  *float64 `json:"corrective_follow_up_value,omitempty"`
}

type BatchCreateReadingsResponse struct {
	SessionID string    `json:"session_id"`
	Readings  []Reading `json:"readings"`
}

type CleaningZone struct {
	ID         string     `json:"id"`
	MerchantID string     `json:"merchant_id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

type CleaningZoneWithSurfaces struct {
	ID         string                        `json:"id"`
	MerchantID string                        `json:"merchant_id"`
	Name       string                        `json:"name"`
	Enabled    bool                          `json:"enabled"`
	CreatedAt  time.Time                     `json:"created_at"`
	UpdatedAt  time.Time                     `json:"updated_at"`
	DeletedAt  *time.Time                    `json:"deleted_at,omitempty"`
	Surfaces   []CleaningSurfaceWithComputed `json:"surfaces"`
}

type CreateCleaningZoneRequest struct {
	Name string `json:"name"`
}

type UpdateCleaningZoneRequest struct {
	Name string `json:"name"`
}

type CleaningSurface struct {
	ID             string     `json:"id"`
	MerchantID     string     `json:"merchant_id"`
	ZoneID         string     `json:"zone_id"`
	ZoneName       string     `json:"zone_name"`
	Name           string     `json:"name"`
	FrequencyUnit  string     `json:"frequency_unit"`
	FrequencyCount int        `json:"frequency_count"`
	Active         bool       `json:"active"`
	Enabled        bool       `json:"enabled"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

type CleaningSurfaceWithComputed struct {
	ID             string `json:"id"`
	ZoneID         string `json:"zone_id"`
	ZoneName       string `json:"zone_name"`
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

type CreateCleaningSurfaceRequest struct {
	ZoneID         string `json:"zone_id"`
	Name           string `json:"name"`
	FrequencyUnit  string `json:"frequency_unit"`
	FrequencyCount int    `json:"frequency_count"`
	Active         *bool  `json:"active,omitempty"`
}

type UpdateCleaningSurfaceRequest struct {
	ZoneID         string `json:"zone_id"`
	Name           string `json:"name"`
	FrequencyUnit  string `json:"frequency_unit"`
	FrequencyCount int    `json:"frequency_count"`
	Active         *bool  `json:"active,omitempty"`
}

type CleaningExecution struct {
	ID          string              `json:"id"`
	SessionID   string              `json:"session_id,omitempty"`
	SurfaceID   string              `json:"surface_id"`
	SurfaceName string              `json:"surface_name"`
	ZoneID      string              `json:"zone_id"`
	ZoneName    string              `json:"zone_name"`
	MerchantID  string              `json:"merchant_id"`
	Comment     *string             `json:"comment,omitempty"`
	PhotoURL    *string             `json:"photo_url,omitempty"`
	Status      string              `json:"status"`
	CreatedBy   string              `json:"created_by"`
	PerformedBy ActivityPerformedBy `json:"performed_by,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type CleaningSession struct {
	ID         string    `json:"id"`
	MerchantID string    `json:"merchant_id"`
	Status     string    `json:"status"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CleaningSessionListItem struct {
	ID              string              `json:"id"`
	Status          string              `json:"status"`
	PerformedAt     time.Time           `json:"performed_at"`
	PerformedBy     ActivityPerformedBy `json:"performed_by"`
	ExecutionsCount int                 `json:"executions_count"`
}

type CleaningSessionDetail struct {
	ID          string              `json:"id"`
	MerchantID  string              `json:"merchant_id"`
	Status      string              `json:"status"`
	PerformedAt time.Time           `json:"performed_at"`
	PerformedBy ActivityPerformedBy `json:"performed_by"`
	Executions  []CleaningExecution `json:"executions"`
}

type CreateCleaningSessionRequest struct {
	Executions []CleaningSessionExecutionInput `json:"executions"`
}

type CleaningSessionExecutionInput struct {
	SurfaceID string  `json:"surface_id"`
	Comment   *string `json:"comment,omitempty"`
	PhotoURL  *string `json:"photo_url,omitempty"`
}

type BatchCreateCleaningSessionResponse struct {
	SessionID  string              `json:"session_id"`
	Executions []CleaningExecution `json:"executions"`
}

type CleaningSessionsListParams struct {
	Date   string `json:"date"`
	ZoneID string `json:"zone_id"`
}

type ActivitiesListParams struct {
	Date     string `json:"date"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
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

type HaccpComponent struct {
	ComponentID      string   `json:"component_id"`
	Name             string   `json:"name"`
	Category         string   `json:"category"`
	UnitOfMeasure    string   `json:"unit_of_measure"`
	ConservationDays *int     `json:"conservation_days"`
	ConservationType string   `json:"conservation_type"`
	StorageTempMin   *float64 `json:"storage_temp_min"`
	StorageTempMax   *float64 `json:"storage_temp_max"`
	Status           string   `json:"status"`
}

type HaccpComponentCategory struct {
	CategoryName string           `json:"category"`
	CategoryID   string           `json:"category_id"`
	Order        int              `json:"order"`
	Components   []HaccpComponent `json:"components"`
}
