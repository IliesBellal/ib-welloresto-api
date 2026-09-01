package auth

import (
	"welloresto-api/internal/permission"
)

// legacyPermissionFallback maps each catalog permission key to the historical
// boolean field on UserRowRights that used to gate it, consulted only for a
// user with no role yet (RoleID == nil — see Has).
//
// Three catalog keys are deliberately absent: pos.ticket.reopen, pos.refund,
// inventory.manage. No boolean column ever granted these — historically only
// Rights.Admin has them, which Has handles before ever consulting this map.
//
// pos.access and pos.discount.apply are gone from the catalog entirely (RBAC
// lot 8, 2026-08-27 — see docs/decisions.md): neither ever guarded a route,
// and AccessWaiter (the field that used to back pos.access here) is gone
// from UserRowRights entirely, along with AccessDelivery, CanExportReports,
// CanExportFinancials and CanExportCustomers (dead-rights cleanup,
// 2026-08-27 — see docs/decisions.md) — none of the five ever had a fallback
// entry in this map, so removing them changes nothing here.
var legacyPermissionFallback = map[permission.Key]func(UserRowRights) bool{
	permission.POSStatusManage:      func(r UserRowRights) bool { return r.AccessReception },
	permission.POSCashDrawerOpen:    func(r UserRowRights) bool { return r.OpenCashDrawer },
	permission.CatalogManage:        func(r UserRowRights) bool { return r.CanManageMenu },
	permission.HACCPManage:          func(r UserRowRights) bool { return r.CanManageHACCP },
	permission.CustomersManage:      func(r UserRowRights) bool { return r.CanManageCustomers },
	permission.StaffManage:          func(r UserRowRights) bool { return r.CanManageUsers },
	permission.StaffScheduleManage:  func(r UserRowRights) bool { return r.CanManagePlannings },
	permission.ReportsSalesRead:     func(r UserRowRights) bool { return r.CanViewReports },
	permission.ReportsFinancialRead: func(r UserRowRights) bool { return r.CanViewFinancials },
	permission.SettingsManage:       func(r UserRowRights) bool { return r.CanManageSettings },
}

// Has indique si l'utilisateur détient le droit demandé sur son établissement
// courant.
//
// Deux mondes coexistent pendant la transition :
//   - RoleID nil     -> l'utilisateur n'a pas encore de rôle, on retombe sur
//     les colonnes booléennes historiques (comportement identique à
//     aujourd'hui) ;
//   - RoleID non nil -> les droits viennent du rôle, les booléens sont
//     ignorés — même s'ils contredisent le rôle.
//
// Cette bascule est par utilisateur, ce qui permet de migrer un établissement
// à la fois et de revenir en arrière en remettant role_id à NULL.
func (u *UserLoginRow) Has(key permission.Key) bool {
	if u.RoleID != nil {
		if u.RoleSystemKey != nil && *u.RoleSystemKey == permission.SystemKeyAdmin {
			return true
		}
		for _, granted := range u.Permissions {
			if granted == string(key) {
				return true
			}
		}
		return false
	}

	// Monde historique : admin court-circuite tout, comme aujourd'hui.
	if u.Rights.Admin {
		return true
	}
	if fallback, ok := legacyPermissionFallback[key]; ok {
		return fallback(u.Rights)
	}
	return false
}

// HasAdminRole reports whether the user's RBAC ROLE is the admin role — the
// same role_id-gated logic as Has's admin branch above, exposed as a named
// method for callers (RBAC lot 9: the login response's `admin` flag and
// GET /me/permissions's `is_admin`) that need it outside Has itself.
//
// Deliberately distinct from IsAdmin() (models.go), which is the legacy
// Rights.Admin column alone, consulted by middleware.RequireAdmin() for an
// unrelated, non-catalog authorization decision — do not merge the two.
// Rights.Admin frequently stays true in production regardless of the
// assigned role (historical seeding), so a caller that wants "is this user's
// *role* admin" must use this method, not IsAdmin(), or it will report admin
// for merchant staff whose role_id points at a non-admin role.
func (u *UserLoginRow) HasAdminRole() bool {
	if u.RoleID != nil {
		return u.RoleSystemKey != nil && *u.RoleSystemKey == permission.SystemKeyAdmin
	}
	return u.Rights.Admin
}
