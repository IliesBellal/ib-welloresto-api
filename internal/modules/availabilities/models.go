package availabilities

import "time"

// Availability agrège les métadonnées, la liste des produits et les créneaux horaires
type Availability struct {
	AvailabilityID string                 `json:"availability_id"`
	MerchantID     string                 `json:"merchant_id"`
	Name           string                 `json:"name"`
	Description    *string                `json:"description,omitempty"`
	Enabled        int                    `json:"enabled"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	ProductIDs     []string               `json:"product_ids"`
	Schedules      []AvailabilitySchedule `json:"schedules"`
}

// AvailabilitySchedule représente un créneau horaire
type AvailabilitySchedule struct {
	ScheduleID     string    `json:"schedule_id"`
	AvailabilityID string    `json:"availability_id"`
	DayOfWeek      int       `json:"day_of_week"` // 1 = dimanche, 2 = lundi, ..., 7 = samedi
	StartTime      string    `json:"start_time"`  // Format: "HH:MM:SS"
	EndTime        string    `json:"end_time"`    // Format: "HH:MM:SS"
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreateAvailabilityRequest DTOs pour la création
type CreateAvailabilityRequest struct {
	Name        string                          `json:"name"`
	Description *string                         `json:"description,omitempty"`
	ProductIDs  []string                        `json:"product_ids"`
	Schedules   []CreateAvailabilityScheduleReq `json:"schedules"`
}

// CreateAvailabilityScheduleReq représente un créneau dans la requête de création
type CreateAvailabilityScheduleReq struct {
	DayOfWeek int    `json:"day_of_week"`
	StartTime string `json:"start_time"` // Format: "HH:MM:SS" ou "HH:MM"
	EndTime   string `json:"end_time"`   // Format: "HH:MM:SS" ou "HH:MM"
}

// UpdateAvailabilityRequest pour la mise à jour
type UpdateAvailabilityRequest struct {
	Name        string                          `json:"name"`
	Description *string                         `json:"description,omitempty"`
	ProductIDs  []string                        `json:"product_ids"`
	Schedules   []CreateAvailabilityScheduleReq `json:"schedules"`
}

// AvailabilityResponse pour les réponses API
type AvailabilityResponse struct {
	AvailabilityID string                 `json:"availability_id"`
	Name           string                 `json:"name"`
	Description    *string                `json:"description,omitempty"`
	Enabled        int                    `json:"enabled"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	ProductIDs     []string               `json:"product_ids"`
	Schedules      []AvailabilitySchedule `json:"schedules"`
}

// ProductAvailabilityInfo utilisé pour le contrôle de disponibilité
type ProductAvailabilityInfo struct {
	IsAvailable bool     `json:"is_available"`
	Reasons     []string `json:"reasons,omitempty"`
}
