package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	OTPResendCooldown = 60 * time.Second
	MFAExpiration     = 1 * 24 * time.Hour // For testing purposes, we will use 24 hours  //30 * 24 * time.Hour

	PINLength      = 4
	PINMaxAttempts = 5
	PINLockoutBase = 30 * time.Second
)

var (
	ErrPINInvalidLength = errors.New("pin_invalid_length")
	ErrPINConflict      = errors.New("pin_already_used")
)

type PINAuthRequest struct {
	PIN string `json:"pin"`
}

// SetPINRequest is used by POST /auth/pin/set (self-service).
// user_id is not accepted — the caller's identity comes from the auth token.
type SetPINRequest struct {
	PIN string `json:"pin"`
}

// ResetPINRequest is used by POST /auth/pin/reset (admin).
type ResetPINRequest struct {
	UserID string `json:"user_id"`
}

// PINLockoutError carries the remaining lockout delay for the caller.
type PINLockoutError struct {
	DelaySeconds int
}

func (e *PINLockoutError) Error() string {
	return fmt.Sprintf("pin_locked: retry in %ds", e.DelaySeconds)
}

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
	NFC      string `json:"nfc"`
	PIN      string `json:"pin"`
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
	LogoURL      string `json:"logo_url"`
	Token        string `json:"token"`
}

// ==============================================================
// UserRowRights - Structure contenant tous les droits de l'utilisateur
// ==============================================================
type UserRowRights struct {

	// Accès aux modules
	AccessReception bool
	AccessDelivery  bool
	AccessWaiter    bool

	// Gestion & Rapports
	PrintMerchantCashReport bool
	OpenCashDrawer          bool
	CanManageMenu           bool
	CanManagePlannings      bool
	CanManageUsers          bool
	CanManageSettings       bool
	CanManageHACCP          bool

	// Reports & Financials
	CanViewReports      bool
	CanExportReports    bool
	CanViewFinancials   bool
	CanExportFinancials bool

	// Customers
	CanManageCustomers bool
	CanExportCustomers bool

	// Admin
	Admin bool
}

// ==============================================================
// UserLoginRow - Utilisateur authentifié avec droits
// ==============================================================
type UserLoginRow struct {
	// User Info
	UserID               string
	Password             string
	Name                 string
	FirstName            string
	LastName             string
	Email                string
	Tel                  string
	Enabled              bool
	TermsOfUseAccepted   bool
	ProfilePicture       sql.NullString
	ReceptionDeviceToken sql.NullString
	WaiterDeviceToken    sql.NullString
	DeliveryDeviceToken  sql.NullString
	EmailVerifiedAt      *string
	TelVerifiedAt        *string

	CreatedAt *string

	// Rights
	Rights        UserRowRights
	MFAType       *string
	MFAStatus     *string
	MFAVerifiedAt *string
	MFAOTPSentAt  *string

	// Token de droits
	Token            string
	MerchantID       string
	MerchantRightsID string
	LoginEnabled     bool

	// Merchant Info
	MerchantName    string
	MerchantTel     string
	MerchantLat     float64
	MerchantLng     float64
	TimeZone        string
	MerchantAddress string
	MerchantLogo    sql.NullString
	WebSite         sql.NullString

	// Merchant Parameters
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

	// Subscription / Package
	AllowWaiterAccount   bool
	AllowDeliveryAccount bool
	ScanNOrderReady      bool
	StockManagement      int
	HrManagement         bool
	PlanningEnabled      bool
	HACCPEnabled         bool
	StockEnabled         bool
	ScanNOrderEnabled    bool
	BookingsEnabled      bool

	// SNO
	SNOActivated bool

	// Integrations: Uber Eats
	UEStoreID        sql.NullString
	UEPrepTime       sql.NullString
	UEDelayUntil     *int
	UEDelayDuration  sql.NullInt64
	UEClosedUntil    *int
	UECommissionRate sql.NullFloat64

	// Uber Direct
	UDCustomerID sql.NullString

	// Deliveroo
	DrooLocationID     sql.NullString
	DrooCommissionRate sql.NullFloat64
}

// ============================================================
// PERMISSION METHODS
// Ces méthodes centralisent la logique de vérification des droits
// ============================================================

// IsAdmin vérifie si l'utilisateur est administrateur
func (u *UserLoginRow) IsAdmin() bool {
	return u.Rights.Admin
}

// HasAccessReception vérifie si l'utilisateur a accès à la réception
func (u *UserLoginRow) HasAccessReception() bool {
	return u.Rights.Admin || u.Rights.AccessReception
}

// HasAccessDelivery vérifie si l'utilisateur a accès à la livraison
func (u *UserLoginRow) HasAccessDelivery() bool {
	return u.Rights.Admin || u.Rights.AccessDelivery
}

// HasAccessWaiter vérifie si l'utilisateur a accès au module serveur
func (u *UserLoginRow) HasAccessWaiter() bool {
	return u.Rights.Admin || u.Rights.AccessWaiter
}

// CanPrintCashReport vérifie si l'utilisateur peut imprimer les rapports de caisse
func (u *UserLoginRow) CanPrintCashReport() bool {
	return u.Rights.Admin || u.Rights.PrintMerchantCashReport
}

// CanOpenCashDrawer vérifie si l'utilisateur peut ouvrir le tiroir-caisse
func (u *UserLoginRow) CanOpenCashDrawer() bool {
	return u.Rights.Admin || u.Rights.OpenCashDrawer
}

// HasMenuAccess vérifie si l'utilisateur peut gérer le menu
func (u *UserLoginRow) HasMenuAccess() bool {
	return u.Rights.Admin || u.Rights.CanManageMenu
}

// HasPlanningAccess vérifie si l'utilisateur peut gérer les plannings
func (u *UserLoginRow) HasPlanningAccess() bool {
	return u.Rights.Admin || u.Rights.CanManagePlannings
}

// HasUserManagementAccess vérifie si l'utilisateur peut gérer les utilisateurs
func (u *UserLoginRow) HasUserManagementAccess() bool {
	return u.Rights.Admin || u.Rights.CanManageUsers
}

// HasSettingsAccess vérifie si l'utilisateur peut gérer les paramètres
func (u *UserLoginRow) HasSettingsAccess() bool {
	return u.Rights.Admin || u.Rights.CanManageSettings
}

// HasHACCPAccess vérifie si l'utilisateur peut gérer le HACCP
func (u *UserLoginRow) HasHACCPAccess() bool {
	return u.Rights.Admin || u.Rights.CanManageHACCP
}

// HasReportsViewAccess vérifie si l'utilisateur peut consulter les rapports
func (u *UserLoginRow) HasReportsViewAccess() bool {
	return u.Rights.Admin || u.Rights.CanViewReports
}

// HasReportsExportAccess vérifie si l'utilisateur peut exporter les rapports
func (u *UserLoginRow) HasReportsExportAccess() bool {
	return u.Rights.Admin || u.Rights.CanExportReports
}

// HasFinancialsViewAccess vérifie si l'utilisateur peut consulter les données financières
func (u *UserLoginRow) HasFinancialsViewAccess() bool {
	return u.Rights.Admin || u.Rights.CanViewFinancials
}

// HasFinancialsExportAccess vérifie si l'utilisateur peut exporter les données financières
func (u *UserLoginRow) HasFinancialsExportAccess() bool {
	return u.Rights.Admin || u.Rights.CanExportFinancials
}

// HasCustomerManagementAccess vérifie si l'utilisateur peut gérer les clients
func (u *UserLoginRow) HasCustomerManagementAccess() bool {
	return u.Rights.Admin || u.Rights.CanManageCustomers
}

// HasCustomerExportAccess vérifie si l'utilisateur peut exporter les clients
func (u *UserLoginRow) HasCustomerExportAccess() bool {
	return u.Rights.Admin || u.Rights.CanExportCustomers
}

type VerifyOTPRequestPayload struct {
	Code string `json:"code"`
	Mode string `json:"mode"` // "email" ou "sms" ou mfa
}
