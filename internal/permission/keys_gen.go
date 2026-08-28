// Package permission declares the fixed catalog of RBAC permission keys as
// typed Go constants. The catalog itself lives in the database (table
// `permissions`, seeded by migrations/done/095_roles_permissions_catalog.up.sql,
// extended by migrations/done/097_permission_pos_status_manage.up.sql,
// reduced by migrations/done/100_deprecate_pos_access_and_discount_apply.up.sql,
// and extended again by migrations/todo/103_permission_catalog_lot10.up.sql)
// — this file is a typed mirror of every migration's net INSERT/DELETE
// effect, kept honest by keys_gen_test.go, which fails the build the moment
// the two diverge.
//
// Do not add, rename, or remove a key here without making the matching change
// in a migration (a new one for an addition/rename/deprecation; see the
// existing ones for the pattern) in the same change.
package permission

// Key is a permission key from the `permissions` catalog table. Typed rather
// than a bare string so that RequirePermission and UserLoginRow.Has cannot
// accidentally be called with an arbitrary string that was never declared as
// a real permission.
type Key string

const (
	POSStatusManage      Key = "pos.status.manage"
	POSTicketReopen      Key = "pos.ticket.reopen"
	POSRefund            Key = "pos.refund"
	POSCashDrawerOpen    Key = "pos.cash_drawer.open"
	POSAnalytics         Key = "pos.analytics"
	CatalogManage        Key = "catalog.manage"
	InventoryManage      Key = "inventory.manage"
	HACCPManage          Key = "haccp.manage"
	CustomersManage      Key = "customers.manage"
	StaffManage          Key = "staff.manage"
	StaffScheduleManage  Key = "staff.schedule.manage"
	ReportsSalesRead     Key = "reports.sales.read"
	ReportsFinancialRead Key = "reports.financial.read"
	SettingsManage       Key = "settings.manage"
	BookingsManage       Key = "bookings.manage"
	PlatformsManage      Key = "platforms.manage"
	KioskManage          Key = "kiosk.manage"
	SeatingPlanManage    Key = "seating_plan.manage"
)

// All lists every permission key declared above, in catalog (sort_order) order.
var All = []Key{
	POSStatusManage,
	POSTicketReopen,
	POSRefund,
	POSCashDrawerOpen,
	POSAnalytics,
	CatalogManage,
	InventoryManage,
	HACCPManage,
	CustomersManage,
	StaffManage,
	StaffScheduleManage,
	ReportsSalesRead,
	ReportsFinancialRead,
	SettingsManage,
	BookingsManage,
	PlatformsManage,
	KioskManage,
	SeatingPlanManage,
}
