package models

import (
	"database/sql"
)

const (
	DeliverooWebhookUserID   = "WEBHOOK_DELIVEROO"
	UberEatsWebhookUserID    = "WEBHOOK_UBER_EATS"
	WelloRestoWebhookUserID  = "WELLO_RESTO"
	StripeWebhookUserID      = "WEBHOOK_STRIPE"
	ScanNOrderCashRegisterID = "SCANNORDER"

	StripeMOP      = "STRIPE"
	TicketRestoMOP = "TR"
)

// Top-level response

type UserLoginRow struct {
	// user
	UserID               string
	Password             string
	Name                 string
	FirstName            string
	LastName             string
	Email                string
	Tel                  string
	Enabled              bool
	PinCode              sql.NullString
	ProfilePicture       sql.NullString
	ReceptionDeviceToken sql.NullString
	WaiterDeviceToken    sql.NullString
	DeliveryDeviceToken  sql.NullString

	// rights
	RightsToken             string
	AccessReception         bool
	AccessDelivery          bool
	AccessWaiter            bool
	PrintMerchantCashReport bool
	OpenCashDrawer          bool
	MerchantID              string

	// merchant
	MerchantName    string
	MerchantTel     string
	MerchantLat     float64
	MerchantLng     float64
	TimeZone        string
	MerchantAddress string
	MerchantLogo    sql.NullString
	WebSite         sql.NullString

	// merchant parameters
	DeliveryFees               int
	DeliveryFeesLimit          int
	DeliveryDistanceLimit      int
	ManageOnSite               bool
	ManageTakeAway             bool
	ManageDelivery             bool
	KitchenShowOnlyPaid        bool
	ServiceRequiredForOrdering bool
	WarningNewOrderNotPaid     bool
	DisableSafetyStock         bool
	Currency                   string
	IsOpen                     bool

	// subscription / package
	AllowWaiterAccount   bool
	AllowDeliveryAccount bool
	ScanNOrderReady      bool
	StockManagement      int
	HrManagement         bool

	// SNO
	SNOActivated bool

	// integrations: Uber Eats
	UEStoreID       sql.NullString
	UEPrepTime      sql.NullString
	UEDelayUntil    sql.NullTime
	UEDelayDuration sql.NullInt64
	UEClosedUntil   sql.NullTime

	// Uber Direct
	UDCustomerID sql.NullString

	// Deliveroo
	DrooLocationID sql.NullString
}
