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
	Platform         string          `json:"platform"`
	Active           bool            `json:"active"`
	CommissionRate   int             `json:"commission_rate"`
	AutoAcceptOrders bool            `json:"auto_accept_orders"`
	LastSync         *time.Time      `json:"last_sync"`
	SyncedItems      int             `json:"synced_items"`
	KPIs             IntegrationKPIs `json:"kpis"`
}

// DeliverooIntegration is the response payload for GET /integrations/deliveroo.
type DeliverooIntegration struct {
	Platform         string          `json:"platform"`
	Active           bool            `json:"active"`
	CommissionRate   int             `json:"commission_rate"`
	AutoAcceptOrders bool            `json:"auto_accept_orders"`
	LastSync         *time.Time      `json:"last_sync"`
	SyncedItems      int             `json:"synced_items"`
	KPIs             IntegrationKPIs `json:"kpis"`
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
