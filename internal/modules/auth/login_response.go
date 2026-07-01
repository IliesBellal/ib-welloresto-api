package auth

import "encoding/json"

type LoginResponse struct {
	Status         string                       `json:"status"`
	DeviceCashDesk *LoginDeviceCashDeskResponse `json:"device_cash_desk"`
	Enabled        string                       `json:"enabled"`

	Session      *LoginSessionResponse      `json:"session,omitempty"`
	User         *LoginUserResponse         `json:"user,omitempty"`
	Merchant     *LoginMerchantResponse     `json:"merchant,omitempty"`
	Access       *LoginAccessResponse       `json:"access,omitempty"`
	Capabilities *LoginCapabilitiesResponse `json:"capabilities,omitempty"`
	Integrations *LoginIntegrationsResponse `json:"integrations,omitempty"`
	SNOSettings  *LoginSNOSettingsResponse  `json:"SNOSettings,omitempty"`

	Legacy *LoginLegacyFields `json:"-"`
}

type loginResponseJSON LoginResponse

type loginResponseWithLegacy struct {
	*loginResponseJSON
	*LoginLegacyFields
}

func (r LoginResponse) MarshalJSON() ([]byte, error) {
	alias := loginResponseJSON(r)
	return json.Marshal(loginResponseWithLegacy{
		loginResponseJSON: &alias,
		LoginLegacyFields: r.Legacy,
	})
}

type LoginDeviceCashDeskResponse struct{}

type LoginSessionResponse struct {
	Enabled    bool          `json:"enabled"`
	MerchantID string        `json:"merchant_id"`
	Token      string        `json:"token"`
	MFAStatus  *string       `json:"mfa_status"`
	MFAType    *string       `json:"mfa_type"`
	Merchants  []MerchantRow `json:"merchants"`
}

type LoginUserResponse struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	FirstName          string `json:"first_name"`
	LastName           string `json:"last_name"`
	Email              string `json:"email"`
	Tel                string `json:"tel"`
	TermsOfUseAccepted bool   `json:"terms_of_use_accepted"`
	ProfilePicture     string `json:"profile_picture"`
}

type LoginMerchantResponse struct {
	ID           string                        `json:"id"`
	Name         string                        `json:"name"`
	BusinessName string                        `json:"business_name"`
	Tel          string                        `json:"tel"`
	Address      string                        `json:"address"`
	Lat          float64                       `json:"lat"`
	Lng          float64                       `json:"lng"`
	TimeZone     string                        `json:"timezone"`
	WebSite      string                        `json:"web_site"`
	Currency     string                        `json:"currency"`
	IsOpen       bool                          `json:"is_open"`
	Settings     LoginMerchantSettingsResponse `json:"settings"`
}

type LoginMerchantSettingsResponse struct {
	DeliveryFees                    int              `json:"delivery_fees"`
	DeliveryFeesLimit               int              `json:"delivery_fees_limit"`
	DeliveryDistanceLimit           int              `json:"delivery_distance_limit"`
	ManageOnSite                    bool             `json:"manage_on_site"`
	ManageTakeAway                  bool             `json:"manage_take_away"`
	ManageDelivery                  bool             `json:"manage_delivery"`
	KitchenShowOnlyPaid             bool             `json:"kitchen_show_only_paid"`
	KitchenDistributionMode         string           `json:"kitchen_distribution_mode"`
	ProductionDisplayMode           string           `json:"production_display_mode"`
	PagerNumberRequired             bool             `json:"pager_number_required"`
	POSAutoLockEnabled              bool             `json:"pos_auto_lock_enabled"`
	POSAutoLockDelayMinutes         int              `json:"pos_auto_lock_delay_minutes"`
	ServiceRequiredForOrdering      bool             `json:"service_required_for_ordering"`
	CashRegisterRequiredForOrdering bool             `json:"cash_register_required_for_ordering"`
	WarningNewOrderNotPaid          bool             `json:"warning_new_order_not_paid"`
	POSUpsellEnabled                bool             `json:"pos_upsell_enabled"`
	DisableSafetyStock              bool             `json:"disable_safety_stock"`
	CustomerFormRequirements        *json.RawMessage `json:"customer_form_requirements,omitempty"`
}

type LoginAccessResponse struct {
	Admin       bool                           `json:"admin"`
	Apps        LoginAccessAppsResponse        `json:"apps"`
	Permissions LoginAccessPermissionsResponse `json:"permissions"`
}

type LoginAccessAppsResponse struct {
	Reception bool `json:"reception"`
	Delivery  bool `json:"delivery"`
	Waiter    bool `json:"waiter"`
}

type LoginAccessPermissionsResponse struct {
	PrintMerchantCashReport bool `json:"print_merchant_cash_report"`
	OpenCashDrawer          bool `json:"open_cash_drawer"`
	ManageMenu              bool `json:"manage_menu"`
	ManagePlannings         bool `json:"manage_plannings"`
	ManageUsers             bool `json:"manage_users"`
	ManageSettings          bool `json:"manage_settings"`
	ManageHACCP             bool `json:"manage_haccp"`
	ViewReports             bool `json:"view_reports"`
	ExportReports           bool `json:"export_reports"`
	ViewFinancials          bool `json:"view_financials"`
	ExportFinancials        bool `json:"export_financials"`
	ManageCustomers         bool `json:"manage_customers"`
	ExportCustomers         bool `json:"export_customers"`
}

type LoginCapabilitiesResponse struct {
	Apps         LoginCapabilityAppsResponse         `json:"apps"`
	Modules      LoginCapabilityModulesResponse      `json:"modules"`
	OrderTypes   LoginCapabilityOrderTypesResponse   `json:"order_types"`
	Actions      LoginCapabilityActionsResponse      `json:"actions"`
	Integrations LoginCapabilityIntegrationsResponse `json:"integrations"`
}

type LoginCapabilityAppsResponse struct {
	Reception bool `json:"reception"`
	Delivery  bool `json:"delivery"`
	Waiter    bool `json:"waiter"`
}

type LoginCapabilityModulesResponse struct {
	Menu       bool `json:"menu"`
	Planning   bool `json:"planning"`
	Users      bool `json:"users"`
	Settings   bool `json:"settings"`
	HACCP      bool `json:"haccp"`
	Bookings   bool `json:"bookings"`
	Reports    bool `json:"reports"`
	Financials bool `json:"financials"`
	Customers  bool `json:"customers"`
	Stock      bool `json:"stock"`
	HR         bool `json:"hr"`
	ScanNOrder bool `json:"scannorder"`
}

type LoginCapabilityOrderTypesResponse struct {
	OnSite   bool `json:"on_site"`
	TakeAway bool `json:"take_away"`
	Delivery bool `json:"delivery"`
}

type LoginCapabilityActionsResponse struct {
	OpenCashDrawer          bool `json:"open_cash_drawer"`
	PrintMerchantCashReport bool `json:"print_merchant_cash_report"`
	ManageMenu              bool `json:"manage_menu"`
	ManagePlannings         bool `json:"manage_plannings"`
	ManageUsers             bool `json:"manage_users"`
	ManageSettings          bool `json:"manage_settings"`
	ManageHACCP             bool `json:"manage_haccp"`
	ViewReports             bool `json:"view_reports"`
	ExportReports           bool `json:"export_reports"`
	ViewFinancials          bool `json:"view_financials"`
	ExportFinancials        bool `json:"export_financials"`
	ManageCustomers         bool `json:"manage_customers"`
	ExportCustomers         bool `json:"export_customers"`
}

type LoginCapabilityIntegrationsResponse struct {
	UberEats   bool `json:"uber_eats"`
	UberDirect bool `json:"uber_direct"`
	Deliveroo  bool `json:"deliveroo"`
	ScanNOrder bool `json:"scannorder"`
}

type LoginIntegrationsResponse struct {
	UberEats   LoginUberEatsIntegrationResponse   `json:"uber_eats"`
	UberDirect LoginUberDirectIntegrationResponse `json:"uber_direct"`
	Deliveroo  LoginDeliverooIntegrationResponse  `json:"deliveroo"`
}

type LoginUberEatsIntegrationResponse struct {
	StoreID                  string  `json:"store_id"`
	EstimatedPreparationTime string  `json:"estimated_preparation_time"`
	DelayUntil               *int    `json:"delay_until"`
	DelayDuration            int64   `json:"delay_duration"`
	ClosedUntil              *int    `json:"closed_until"`
	CommissionRate           float64 `json:"commission_rate"`
}

type LoginUberDirectIntegrationResponse struct {
	CustomerID string `json:"customer_id"`
}

type LoginDeliverooIntegrationResponse struct {
	LocationID     string  `json:"location_id"`
	CommissionRate float64 `json:"commission_rate"`
}

type LoginSNOSettingsResponse struct {
	Activated bool `json:"activated"`
}

// LoginLegacyFields are deprecated compatibility fields for existing clients.
// Do not use them in new code. Remove them after back-office and Flutter migration.
type LoginLegacyFields struct {
	Name                            string                             `json:"name"`
	FirstName                       string                             `json:"first_name"`
	LastName                        string                             `json:"last_name"`
	UserID                          string                             `json:"userId"`
	UserMail                        string                             `json:"user_mail"`
	UserTel                         string                             `json:"user_tel"`
	OpenCashDrawer                  bool                               `json:"open_cash_drawer"`
	TermsOfUseAccepted              bool                               `json:"terms_of_use_accepted"`
	Admin                           bool                               `json:"admin"`
	MerchantID                      string                             `json:"merchantId"`
	MerchantIDLegacy                string                             `json:"merchant_id"`
	MerchantName                    string                             `json:"merchantName"`
	BusinessName                    string                             `json:"business_name"`
	MerchantTel                     string                             `json:"merchantTel"`
	MerchantTelLegacy               string                             `json:"merchant_tel"`
	DeliveryFees                    int                                `json:"delivery_fees"`
	DeliveryFeesLimit               int                                `json:"delivery_fees_limit"`
	KitchenShowOnlyPaid             bool                               `json:"kitchen_show_only_paid"`
	AllowWaiterAccount              bool                               `json:"allow_waiter_account"`
	PrintCashReport                 bool                               `json:"print_merchant_cash_report"`
	MerchantAd                      string                             `json:"merchantAd"`
	MerchantAddress                 string                             `json:"merchant_address"`
	MerchantLat                     float64                            `json:"merchant_lat"`
	DeliveryDistanceLimit           int                                `json:"delivery_distance_limit"`
	KitchenDistributionMode         string                             `json:"kitchen_distribution_mode"`
	ProductionDisplayMode           string                             `json:"production_display_mode"`
	PagerNumberRequired             bool                               `json:"pager_number_required"`
	CashRegisterRequiredForOrdering bool                               `json:"cash_register_required_for_ordering"`
	MerchantLng                     float64                            `json:"merchant_lng"`
	TimeZone                        string                             `json:"timezone"`
	MFAStatus                       *string                            `json:"mfa_status"`
	MFAType                         *string                            `json:"mfa_type"`
	IntegrationUberEats             LoginUberEatsIntegrationResponse   `json:"integration_uber_eats"`
	IntegrationUberDirect           LoginUberDirectIntegrationResponse `json:"integration_uber_direct"`
	IntegrationDeliveroo            LoginDeliverooIntegrationResponse  `json:"integration_deliveroo"`
	ScanNOrderReady                 bool                               `json:"scannorder_ready"`
	ManageOnSite                    bool                               `json:"manage_on_site"`
	ManageTakeAway                  bool                               `json:"manage_take_away"`
	ManageDelivery                  bool                               `json:"manage_delivery"`
	StockManagement                 int                                `json:"stock_management"`
	HRManagement                    bool                               `json:"hr_management"`
	ServiceRequiredForOrdering      bool                               `json:"service_required_for_ordering"`
	SafetyStockActive               bool                               `json:"safety_stock_active"`
	WarningNewOrderNotPaid          bool                               `json:"warning_new_order_not_paid"`
	Currency                        string                             `json:"currency"`
	IsOpen                          bool                               `json:"is_open"`
	MerchantWebSite                 string                             `json:"merchant_web_site"`
	Token                           string                             `json:"token"`
	ProfilePicture                  string                             `json:"profile_picture"`
	Merchants                       []MerchantRow                      `json:"merchants"`
	SNOSettings                     LoginSNOSettingsResponse           `json:"SNOSettings"`
}
