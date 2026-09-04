package ubereats

import (
	"time"
)

// ConfigUberEats structure pour l'injection des variables d'environnement

type ConfigUberEats struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	TokenType    string
	AuthURL      string
	TokenURL     string
}

// Store représente les données du magasin (table integration_uber_eats + merchant)
type Store struct {
	MerchantID                   string `db:"merchant_id"`
	StoreID                      string `db:"store_id"`
	Timezone                     string `db:"timezone"`
	RefreshToken                 string `db:"refresh_token"`
	EstimatedPreparationTime     int    `db:"estimated_preparation_time"`
	LastEstimatedPreparationTime int    `db:"last_estimated_preparation_time"`
	BearerToken                  string // Rempli dynamiquement
	AutoAcceptOrders             bool   `db:"auto_accept_orders"`
}

// UberToken représente le token stocké en base
type UberToken struct {
	AccessToken string    `db:"access_token"`
	ExpiresAt   time.Time `db:"expires_at"` // Attention au parsing SQL
}

// UberOrderMetadata stocke les IDs liés à la commande
type UberOrderMetadata struct {
	BrandOrderID string    `db:"brand_order_id"`
	CreationDate time.Time `db:"creation_date"`
	// StoreID identifie le compte Uber Eats exact d'où vient la commande
	// (orders.brand_store_id, migration 111). Vide pour une commande
	// antérieure à la migration sans brand_store_id résolu.
	StoreID string `db:"brand_store_id"`
}

// Structures pour les payloads JSON de l'API Uber
type UberAuthResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type UberAcceptRequest struct {
	ReadyForPickupTime string `json:"ready_for_pickup_time"`
	ExternalID         string `json:"external_id"` // Notre order_id
	AcceptedBy         string `json:"accepted_by"` // Merchant ID casté en string
}

type UberDenyRequest struct {
	DenyReason DenyReasonDetails `json:"deny_reason"`
}

type DenyReasonDetails struct {
	Info       string `json:"info"`
	Type       string `json:"type"`
	ClientCode string `json:"client_code"`
}

// UberCancelRequest pour le refus et l'annulation
type UberCancelRequest struct {
	DenyReason DenyReasonDetails `json:"deny_reason"`
}

// UberOrderDetails structure pour mapper la réponse GET /v1/delivery/order/{id}
type UberOrderDetails struct {
	Order struct {
		State       string `json:"state"` // ACCEPTED, HANDED_OFF, FAILED, SUCCEEDED
		FailureInfo struct {
			Reason string `json:"reason"` // ACCEPT_TIMED_OUT, DELIVERY_FAILED, etc.
		} `json:"failure_info"`
	} `json:"order"`
}

// Constantes pour les statuts
const (
	StatusAccepted       = "ACCEPTED"
	StatusDenied         = "DENIED"
	StatusCanceled       = "CANCELED"
	StatusCompleted      = "COMPLETED"
	StatusDeliveryFailed = "DELIVERY_FAILED"
	StatusEnRoute        = "EN_ROUTE_TO_DROPOFF"
	StatusReady          = "READY_FOR_HANDOFF"

	StateOpen   = "OPEN"
	StateClosed = "CLOSED"

	// BYOC delivery status values accepted by POST
	// /v1/eats/orders/{order_id}/restaurantdelivery/status (self-delivery orders
	// only — see developer.uber.com/docs/eats/references/api/v1/post-eats-orders-orderid-restaurantdelivery-status).
	StatusBYOCStarted   = "started"
	StatusBYOCArriving  = "arriving"
	StatusBYOCDelivered = "delivered"
)

// BYOCStatusRequest pour mettre à jour le statut de livraison (BYOC)
type BYOCStatusRequest struct {
	Status string `json:"status"`
}

// BYOCLocationRequest is the payload for POST
// /v1/eats/byoc/restaurants/orders/event/location (Ingest Courier Live Location).
// order_workflow_uuid/restaurant_uuid are populated from brand_order_id/store_id -
// see IngestLiveLocation/ShareDriverLocation for the documented working hypothesis.
type BYOCLocationRequest struct {
	LocationRequest BYOCLocationRequestBody `json:"location_request"`
}

type BYOCLocationRequestBody struct {
	OrderWorkflowUUID string              `json:"order_workflow_uuid"`
	RestaurantUUID    string              `json:"restaurant_uuid"`
	LocationEvents    []BYOCLocationEvent `json:"location_events"`
}

type BYOCLocationEvent struct {
	PositionEvent BYOCPositionEvent `json:"position_event"`
}

type BYOCPositionEvent struct {
	Point BYOCPoint     `json:"point"`
	Time  BYOCEventTime `json:"time"`
}

type BYOCPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type BYOCEventTime struct {
	EpochMillis int64 `json:"epochMillis"`
}

// UberPrepTimeRequest structure flexible pour update-store-prep-time
// On utilise des pointeurs pour omettre les champs vides (omitempty)
type UberPrepTimeRequest struct {
	DefaultPrepTime *int         `json:"default_prep_time,omitempty"`
	DelayConfig     *DelayConfig `json:"delay_config,omitempty"`
}

type DelayConfig struct {
	DelayUntil    string `json:"delay_until"`
	DelayDuration int    `json:"delay_duration"` // En secondes
}

// UberStoreStatusRequest pour mettre le magasin hors ligne
type UberStoreStatusRequest struct {
	IsOfflineUntil string `json:"is_offline_until"`
	Status         string `json:"status"` // "OFFLINE"
	Reason         string `json:"reason"`
}

// UberItemSuspensionRequest pour toggleItemAvailability
type UberItemSuspensionRequest struct {
	SuspensionInfo SuspensionInfo `json:"suspension_info"`
}

type SuspensionInfo struct {
	Suspension SuspensionDetail  `json:"suspension"`
	Overrides  []OverrideContext `json:"overrides"`
}

type OverrideContext struct {
	ContextType  string           `json:"context_type"`  // "MODIFIER_GROUP"
	ContextValue string           `json:"context_value"` // Item ID
	Suspension   SuspensionDetail `json:"suspension"`
}

type SuspensionDetail struct {
	SuspendUntil *int64 `json:"suspend_until"` // Timestamp ou null
	Reason       string `json:"reason"`
}

// AuthURLResponse renvoyée au front pour la redirection
type AuthURLResponse struct {
	URL string `json:"url"`
}

// TokenRequest envoyé à Uber pour échanger le code contre un token
type TokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// MerchantInfoResponse réponse de l'API Uber pour identifier le restaurant connecté
type MerchantInfoResponse struct {
	Stores []struct {
		StoreID string `json:"store_id"`
		Name    string `json:"name"`
	} `json:"stores"`
}
