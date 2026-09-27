package availabilities

import (
	"encoding/json"
	"time"
)

// Availability agrège les métadonnées, la liste des produits et les créneaux horaires
type Availability struct {
	AvailabilityID     string                 `json:"availability_id"`
	MerchantID         string                 `json:"merchant_id"`
	Name               string                 `json:"name"`
	UnavailableMessage *string                `json:"unavailable_message,omitempty"`
	Available          bool                   `json:"available"`
	CreatedAt          time.Time              `json:"creation_date"`
	UpdatedAt          time.Time              `json:"update_date"`
	ProductIDs         []string               `json:"product_ids"`
	Schedules          []AvailabilitySchedule `json:"schedules"`
}

// AvailabilitySchedule représente un créneau horaire
type AvailabilitySchedule struct {
	ScheduleID     string    `json:"schedule_id"`
	AvailabilityID string    `json:"availability_id"`
	DayOfWeek      int       `json:"day_of_week"` // 1 = lundi, ..., 7 = dimanche
	StartTime      string    `json:"start_time"`  // Format: "HH:MM:SS"
	EndTime        string    `json:"end_time"`    // Format: "HH:MM:SS"
	CreatedAt      time.Time `json:"creation_date"`
	UpdatedAt      time.Time `json:"update_date"`
}

// CreateAvailabilityRequest DTOs pour la création
type CreateAvailabilityRequest struct {
	Name               string                          `json:"name"`
	UnavailableMessage *string                         `json:"unavailable_message,omitempty"`
	ProductIDs         []string                        `json:"product_ids"`
	Schedules          []CreateAvailabilityScheduleReq `json:"schedules"`
}

// CreateAvailabilityScheduleReq représente un créneau dans la requête de création
type CreateAvailabilityScheduleReq struct {
	ScheduleID string `json:"schedule_id"`
	DayOfWeek  int    `json:"day_of_week"` // 1 = lundi, ..., 7 = dimanche
	StartTime  string `json:"start_time"`  // Format: "HH:MM:SS" ou "HH:MM"
	EndTime    string `json:"end_time"`    // Format: "HH:MM:SS" ou "HH:MM"
}

// UpdateAvailabilityRequest pour la mise à jour (tous les champs sont optionnels).
// ProductIDs / Schedules : nil = inchangé ; non nil, même vide = remplacé
// (liste vide = tous retirés). Voir UnmarshalJSON pour la lecture de null.
type UpdateAvailabilityRequest struct {
	Name               *string                         `json:"name,omitempty"`
	UnavailableMessage *string                         `json:"unavailable_message,omitempty"`
	ProductIDs         []string                        `json:"product_ids,omitempty"`
	Schedules          []CreateAvailabilityScheduleReq `json:"schedules,omitempty"`
	Available          *bool                           `json:"available,omitempty"`
}

// UnmarshalJSON distingue une clé absente (inchangé) d'une clé à null ou []
// (liste vidée) pour product_ids et schedules : encoding/json décode null et
// une clé absente tous deux en nil. Le back-office envoyait product_ids: null
// pour retirer le dernier produit — ignoré, la disponibilité gardait ses
// produits (docs/AVAILABILITIES_EMPTY_LISTS.md).
func (r *UpdateAvailabilityRequest) UnmarshalJSON(data []byte) error {
	type alias UpdateAvailabilityRequest
	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, present := raw["product_ids"]; present && aux.ProductIDs == nil {
		aux.ProductIDs = []string{}
	}
	if _, present := raw["schedules"]; present && aux.Schedules == nil {
		aux.Schedules = []CreateAvailabilityScheduleReq{}
	}
	*r = UpdateAvailabilityRequest(aux)
	return nil
}

// AvailabilityResponse pour les réponses API
type AvailabilityResponse struct {
	AvailabilityID     string                 `json:"availability_id"`
	Name               string                 `json:"name"`
	UnavailableMessage *string                `json:"unavailable_message,omitempty"`
	Available          bool                   `json:"available"`
	CreatedAt          time.Time              `json:"creation_date"`
	UpdatedAt          time.Time              `json:"update_date"`
	ProductIDs         []string               `json:"product_ids"`
	Schedules          []AvailabilitySchedule `json:"schedules"`
}

// ProductScheduleRow est une ligne (produit, créneau) d'une disponibilité
// active (a.enabled, a.available, ap.enabled). HasSchedule est faux quand la
// disponibilité n'a aucun créneau actif : le produit est alors restreint sans
// jamais être ouvert. Jour/heures sont en heure locale du merchant, comme
// stockés en base.
type ProductScheduleRow struct {
	ProductID   string
	ProductName string
	HasSchedule bool
	DayOfWeek   int    // 1 = lundi, ..., 7 = dimanche
	StartTime   string // HH:MM:SS, heure locale
	EndTime     string // HH:MM:SS, heure locale (> StartTime)
}

// ProductAvailabilityInfo utilisé pour le contrôle de disponibilité
type ProductAvailabilityInfo struct {
	IsAvailable bool     `json:"is_available"`
	Reasons     []string `json:"reasons,omitempty"`
}
