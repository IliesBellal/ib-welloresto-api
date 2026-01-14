package auth

import "database/sql"

type SaveDeviceTokenRequest struct {
	DeviceToken string `json:"device_token"`
	DeviceID    string `json:"device_id"`
	App         string `json:"app"`
}

type CheckAppVersionRequest struct {
	Version string `json:"version"`
	App     string `json:"app"`
}

type LoginRequestPayload struct {
	App      string `json:"app"`
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	// Use only Username or Email, one field will be removed as we will get rid of username and use only email for login
	// Or keep Username if it is better for evolving
}

type MerchantRow struct {
	MerchantID   string `json:"merchant_id"`
	FullName     string
	BusinessName string `json:"business_name"`
	Lat          float64
	Lng          float64
	Address      string
	City         string
	Country      string
	ZipCode      string
	Token        string `json:"token"`
}

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
	TermsOfUseAccepted   bool
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
	Admin                   bool
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
	DeliveryFees                    int
	DeliveryFeesLimit               int
	DeliveryDistanceLimit           int
	ManageOnSite                    bool
	ManageTakeAway                  bool
	ManageDelivery                  bool
	KitchenShowOnlyPaid             bool
	KitchenDistributionMode         string
	ProductionDisplayMode           string
	PagerNumberRequired             bool
	ServiceRequiredForOrdering      bool
	WarningNewOrderNotPaid          bool
	CashRegisterRequiredForOrdering bool
	DisableSafetyStock              bool
	Currency                        string
	IsOpen                          bool

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
	UEDelayUntil    *int
	UEDelayDuration sql.NullInt64
	UEClosedUntil   *int

	// Uber Direct
	UDCustomerID sql.NullString

	// Deliveroo
	DrooLocationID sql.NullString
}
