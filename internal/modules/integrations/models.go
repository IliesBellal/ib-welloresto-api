package integrations

import "time"

// IntegrationKPIs holds revenue/order metrics for a given platform.
// All monetary values are in centimes (smallest currency unit).
type IntegrationKPIs struct {
	Revenue   int `json:"revenue"`
	Orders    int `json:"orders"`
	AvgBasket int `json:"avg_basket"`
}

// AccountSummary is one entry of the account selector for a multi-account
// platform (GET /integrations/uber-eats/accounts, /integrations/deliveroo/accounts).
// AccountID is the store_id (Uber Eats) or location_id (Deliveroo).
type AccountSummary struct {
	AccountID string `json:"account_id"`
	Enabled   bool   `json:"enabled"`
	IsPrimary bool   `json:"is_primary"`
}

// UberEatsIntegration is the response payload for GET /integrations/uber-eats.
type UberEatsIntegration struct {
	Platform string `json:"platform"`
	// StoreID est le compte Uber Eats effectivement retourné (le "principal"
	// si aucun store_id n'était demandé, sinon celui demandé).
	StoreID                string          `json:"store_id,omitempty"`
	Active                 bool            `json:"active"`
	CommissionRate         int             `json:"commission_rate"`
	AutoAcceptOrders       bool            `json:"auto_accept_orders"`
	PreparationTimeMinutes int             `json:"preparation_time_minutes"`
	ClosedUntil            *time.Time      `json:"closed_until"`
	LastSync               *time.Time      `json:"last_sync"`
	SyncedItems            int             `json:"synced_items"`
	KPIs                   IntegrationKPIs `json:"kpis"`
}

// DeliverooIntegration is the response payload for GET /integrations/deliveroo.
type DeliverooIntegration struct {
	Platform string `json:"platform"`
	// LocationID est le compte Deliveroo effectivement retourné (le
	// "principal" si aucun location_id n'était demandé, sinon celui demandé).
	LocationID             string          `json:"location_id,omitempty"`
	Active                 bool            `json:"active"`
	CommissionRate         int             `json:"commission_rate"`
	AutoAcceptOrders       bool            `json:"auto_accept_orders"`
	PreparationTimeMinutes int             `json:"preparation_time_minutes"`
	ClosedUntil            *time.Time      `json:"closed_until"`
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
	ClosedUntil      *time.Time      `json:"closed_until"`
	LastSync         *time.Time      `json:"last_sync"`
	SyncedItems      int             `json:"synced_items"`
	KPIs             IntegrationKPIs `json:"kpis"`

	// Temps d'attente supplémentaire temporaire. Renvoyés bruts (sans filtrage
	// par l'échéance) : c'est l'écran de réglages, il doit montrer ce qui est
	// enregistré. Le filtrage temporel n'a lieu que sur le chemin client
	// (scannorder.snoActiveExtraPrepMinutes).
	ExtraPrepMinutes *int       `json:"extra_prep_minutes"`
	ExtraPrepUntil   *time.Time `json:"extra_prep_until"`

	// Visual / branding
	LogoURL   *string `json:"logo_url"`
	BannerURL *string `json:"banner_url"`

	// AccessURL is the public storefront URL of the merchant
	// (SCANNORDER_BASE_URL/restaurant/{slug}). nil when the merchant has no
	// main QR code or when SCANNORDER_BASE_URL is not configured.
	AccessURL *string `json:"access_url"`

	// slug is the merchant main QR code (qrcodes.code), read by the repository
	// and consumed by the service to build AccessURL. Unexported: never
	// serialized, package-internal plumbing only.
	slug string

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

// AffectedAccount identifies one specific provider account (store_id for
// uber_eats, location_id for deliveroo) within a multi-account action. See
// CloseTemporaryIntegrationsRequest / SetWaitTimeRequest.
type AffectedAccount struct {
	Platform  string `json:"platform"`
	AccountID string `json:"account_id"`
}

// CloseTemporaryIntegrationsRequest is the body for PATCH /integrations/global/close-temporary.
//
// AffectedAccounts est l'extension multi-comptes : pour une plateforme donnée,
// l'entrée AffectedAccounts correspondante (par Platform) prévaut sur
// AffectedIntegrations et cible ce compte précis. Une plateforme listée dans
// AffectedIntegrations mais absente de AffectedAccounts cible son compte
// "principal" (comportement historique, identique pour un marchand
// mono-compte) - c'est ce qui garde le POS Flutter actuel compatible sans
// modification tant qu'un marchand n'a qu'un compte par plateforme.
type CloseTemporaryIntegrationsRequest struct {
	DurationMinutes      int               `json:"duration_minutes"`
	AffectedIntegrations []string          `json:"affected_integrations"`
	AffectedAccounts     []AffectedAccount `json:"affected_accounts,omitempty"`
}

// CloseTemporaryIntegrationsResponse is the response for temporary global closure.
type CloseTemporaryIntegrationsResponse struct {
	Status               string    `json:"status"`
	ClosedUntil          time.Time `json:"closed_until"`
	AffectedIntegrations []string  `json:"affected_integrations"`
}

// SetWaitTimeRequest is the body for PATCH /integrations/global/wait-time.
//
// Le nom de route et des champs reprennent à l'identique ce que le POS Flutter
// envoie déjà (data/api/integration_api.dart) : l'action rapide « Temps
// d'attente » existe et est déployée, seul l'endpoint manquait côté API.
type SetWaitTimeRequest struct {
	WaitTimeMinutes      int      `json:"wait_time_minutes"`
	AffectedIntegrations []string `json:"affected_integrations"`

	// AffectedAccounts : voir CloseTemporaryIntegrationsRequest.AffectedAccounts,
	// même sémantique (compte précis par plateforme, sinon compte "principal").
	AffectedAccounts []AffectedAccount `json:"affected_accounts,omitempty"`

	// DurationMinutes : durée pendant laquelle le supplément s'applique avant
	// de s'effacer seul. Optionnel — le POS n'envoie que le supplément, d'où le
	// défaut defaultWaitTimeWindowMinutes.
	DurationMinutes *int `json:"duration_minutes,omitempty"`
}

// SetWaitTimeResponse is the response for a temporary extra wait time.
type SetWaitTimeResponse struct {
	Status               string    `json:"status"`
	WaitTimeMinutes      int       `json:"wait_time_minutes"`
	AppliedUntil         time.Time `json:"applied_until"`
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
