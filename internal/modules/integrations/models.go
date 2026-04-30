package integrations

import "time"

// IntegrationKPIs holds revenue/order metrics for a given platform.
// All monetary values are in centimes (smallest currency unit).
type IntegrationKPIs struct {
	Revenue   int `json:"revenue"`
	Orders    int `json:"orders"`
	AvgBasket int `json:"avg_basket"`
}

// UberEatsIntegration is the response payload for GET /integrations/uber-eats.
type UberEatsIntegration struct {
	Platform               string          `json:"platform"`
	Active                 bool            `json:"active"`
	CommissionRate         int             `json:"commission_rate"`
	AutoAcceptOrders       bool            `json:"auto_accept_orders"`
	PreparationTimeMinutes int             `json:"preparation_time_minutes"`
	LastSync               *time.Time      `json:"last_sync"`
	SyncedItems            int             `json:"synced_items"`
	KPIs                   IntegrationKPIs `json:"kpis"`
}

// DeliverooIntegration is the response payload for GET /integrations/deliveroo.
type DeliverooIntegration struct {
	Platform               string          `json:"platform"`
	Active                 bool            `json:"active"`
	CommissionRate         int             `json:"commission_rate"`
	AutoAcceptOrders       bool            `json:"auto_accept_orders"`
	PreparationTimeMinutes int             `json:"preparation_time_minutes"`
	LastSync               *time.Time      `json:"last_sync"`
	SyncedItems            int             `json:"synced_items"`
	KPIs                   IntegrationKPIs `json:"kpis"`
}

// ScanNOrderIntegration is the response payload for GET /integrations/scannorder.
// It embeds the standard integration fields plus ScanNOrder-specific settings.
type ScanNOrderIntegration struct {
	Platform         string          `json:"platform"`
	Active           bool            `json:"active"`
	CommissionRate   int             `json:"commission_rate"`
	AutoAcceptOrders bool            `json:"auto_accept_orders"`
	LastSync         *time.Time      `json:"last_sync"`
	SyncedItems      int             `json:"synced_items"`
	KPIs             IntegrationKPIs `json:"kpis"`

	// Visual / branding
	LogoURL   *string `json:"logo_url"`
	BannerURL *string `json:"banner_url"`

	// Storefront copy
	PrimaryColor     string  `json:"primary_color"`
	HeaderTitle      *string `json:"header_title"`
	HeaderText       *string `json:"header_text"`
	CGVLink          *string `json:"cgv_link"`
	ReturnPolicyLink *string `json:"return_policy_link"`
	LegalNoticesLink *string `json:"legal_notices_link"`

	// Order-type settings
	TakeawayEnabled       bool `json:"takeaway_enabled"`
	TakeawayAutoAccept    bool `json:"takeaway_auto_accept"`
	DeliveryEnabled       bool `json:"delivery_enabled"`
	DeliveryAutoAccept    bool `json:"delivery_auto_accept"`
	DeliveryDistanceLimit int  `json:"delivery_distance_limit"`
}

// IntegrationData wraps a single integration inside the standard envelope.
type IntegrationData struct {
	Integration interface{} `json:"integration"`
}

// UpdateIntegrationRequest is the body for PATCH /integrations/{platform}.
type UpdateIntegrationRequest struct {
	CommissionRate         *int  `json:"commission_rate,omitempty"`
	AutoAcceptOrders       *bool `json:"auto_accept_orders,omitempty"`
	PreparationTimeMinutes *int  `json:"preparation_time_minutes,omitempty"`
}

// CloseTemporaryIntegrationsRequest is the body for PATCH /integrations/global/close-temporary.
type CloseTemporaryIntegrationsRequest struct {
	DurationMinutes      int      `json:"duration_minutes"`
	AffectedIntegrations []string `json:"affected_integrations"`
}

// CloseTemporaryIntegrationsResponse is the response for temporary global closure.
type CloseTemporaryIntegrationsResponse struct {
	Status               string    `json:"status"`
	ClosedUntil          time.Time `json:"closed_until"`
	AffectedIntegrations []string  `json:"affected_integrations"`
}

// StripeBrandingResult is the response for POST /integrations/stripe/branding.
type StripeBrandingResult struct {
	LogoFileID   string `json:"logo_file_id"`
	PrimaryColor string `json:"primary_color"`
}

// UpdateScanNOrderRequest is the body for PATCH /integrations/scannorder.
type UpdateScanNOrderRequest struct {
	Active                *bool   `json:"active,omitempty"`
	PrimaryColor          *string `json:"primary_color,omitempty"`
	HeaderTitle           *string `json:"header_title,omitempty"`
	HeaderText            *string `json:"header_text,omitempty"`
	CGVLink               *string `json:"cgv_link,omitempty"`
	ReturnPolicyLink      *string `json:"return_policy_link,omitempty"`
	LegalNoticesLink      *string `json:"legal_notices_link,omitempty"`
	TakeawayEnabled       *bool   `json:"takeaway_enabled,omitempty"`
	TakeawayAutoAccept    *bool   `json:"takeaway_auto_accept,omitempty"`
	DeliveryEnabled       *bool   `json:"delivery_enabled,omitempty"`
	DeliveryAutoAccept    *bool   `json:"delivery_auto_accept,omitempty"`
	DeliveryDistanceLimit *int    `json:"delivery_distance_limit,omitempty"`
}
