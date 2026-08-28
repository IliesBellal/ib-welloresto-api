package users

import (
	"database/sql"
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
	ScanNOrderReady bool
	StockManagement int
	HrManagement    bool

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

type MerchantRow struct {
	ID       int64
	FullName string
	Lat      float64
	Lng      float64
	Address  string
	City     string
	Country  string
	ZipCode  string
}

type UserNotification struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	ActionLabel string `json:"actionLabel"`
}

type UserNotificationsData struct {
	Notifications []UserNotification `json:"notifications"`
}

type UserVerificationStatus struct {
	Email         string
	Phone         string
	EmailVerified bool
	PhoneVerified bool
}
