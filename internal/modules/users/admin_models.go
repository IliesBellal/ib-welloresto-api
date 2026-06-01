package users

import "time"

type MerchantUserPermissions struct {
	AccessReception         bool `json:"access_reception"`
	AccessDelivery          bool `json:"access_delivery"`
	AccessWaiter            bool `json:"access_waiter"`
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

type MerchantUserRights struct {
	MerchantRightsID int64                   `json:"merchant_rights_id"`
	MerchantID       string                  `json:"merchant_id"`
	UserID           string                  `json:"user_id"`
	Admin            bool                    `json:"admin"`
	Permissions      MerchantUserPermissions `json:"permissions"`
	LoginEnabled     bool                    `json:"login_enabled"`
}

type MerchantUserRightsUpsertRequest struct {
	Admin       bool                    `json:"admin"`
	Permissions MerchantUserPermissions `json:"permissions"`
}

type MerchantUserListFilters struct {
	Search         string
	Active         *bool
	LinkedEmployee *bool
	Admin          *bool
	Page           int
	PageSize       int
}

type LinkableUserSearchFilters struct {
	Search   string
	Page     int
	PageSize int
}

type MerchantUserListItem struct {
	UserID           string                  `json:"user_id"`
	FirstName        string                  `json:"first_name"`
	LastName         string                  `json:"last_name"`
	Email            *string                 `json:"email,omitempty"`
	Tel              *string                 `json:"tel,omitempty"`
	ProfilePicture   *string                 `json:"profile_picture,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
	LastLoginAt      *time.Time              `json:"last_login_at,omitempty"`
	Enabled          bool                    `json:"enabled"`
	LoginEnabled     bool                    `json:"login_enabled"`
	Status           string                  `json:"status"`
	MerchantRightsID int64                   `json:"merchant_rights_id"`
	Admin            bool                    `json:"admin"`
	Permissions      MerchantUserPermissions `json:"permissions"`
	EmployeeID       *string                 `json:"employee_id,omitempty"`
	EmployeeName     *string                 `json:"employee_name,omitempty"`
}

type MerchantUserDetail struct {
	MerchantUserListItem
}

type LinkableUser struct {
	UserID         string     `json:"user_id"`
	FirstName      string     `json:"first_name"`
	LastName       string     `json:"last_name"`
	Email          *string    `json:"email,omitempty"`
	Tel            *string    `json:"tel,omitempty"`
	ProfilePicture *string    `json:"profile_picture,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastLoginAt    *time.Time `json:"last_login_at,omitempty"`
	Enabled        bool       `json:"enabled"`
	Status         string     `json:"status"`
}

type MerchantUserLinkRequest struct {
	Rights *MerchantUserRightsUpsertRequest `json:"rights,omitempty"`
	Admin  bool                             `json:"admin"`
}

type ForceResetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

type MerchantUserUnlinkResult struct {
	Unlinked             bool `json:"unlinked"`
	EmployeeLinksCleared int  `json:"employee_links_cleared"`
}

func defaultMerchantUserRights(admin bool) MerchantUserRightsUpsertRequest {
	return MerchantUserRightsUpsertRequest{Admin: admin}
}

func (req MerchantUserRightsUpsertRequest) Normalize(defaults MerchantUserRightsUpsertRequest) MerchantUserRightsUpsertRequest {
	return MerchantUserRightsUpsertRequest{
		Admin: req.Admin || defaults.Admin,
		Permissions: MerchantUserPermissions{
			AccessReception:         req.Permissions.AccessReception,
			AccessDelivery:          req.Permissions.AccessDelivery,
			AccessWaiter:            req.Permissions.AccessWaiter,
			PrintMerchantCashReport: req.Permissions.PrintMerchantCashReport,
			OpenCashDrawer:          req.Permissions.OpenCashDrawer,
			ManageMenu:              req.Permissions.ManageMenu,
			ManagePlannings:         req.Permissions.ManagePlannings,
			ManageUsers:             req.Permissions.ManageUsers,
			ManageSettings:          req.Permissions.ManageSettings,
			ManageHACCP:             req.Permissions.ManageHACCP,
			ViewReports:             req.Permissions.ViewReports,
			ExportReports:           req.Permissions.ExportReports,
			ViewFinancials:          req.Permissions.ViewFinancials,
			ExportFinancials:        req.Permissions.ExportFinancials,
			ManageCustomers:         req.Permissions.ManageCustomers,
			ExportCustomers:         req.Permissions.ExportCustomers,
		},
	}
}
