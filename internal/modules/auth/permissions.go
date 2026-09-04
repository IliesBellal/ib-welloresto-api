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
//   - RoleID non nil -> les droits viennent uniquement des permissions du
//     rôle (Permissions, chargées par attachRolePermissions) — les booléens
//     sont ignorés, même s'ils contredisent le rôle.
//
// Cette bascule est par utilisateur, ce qui permet de migrer un établissement
// à la fois et de revenir en arrière en remettant role_id à NULL.
//
// RBAC lot 11, phase 3 : le court-circuit "system_key == admin renvoie
// toujours true, sans consulter Permissions" a été retiré. Le rôle admin est
// désormais un rôle comme les autres — il n'a d'autorité que par les lignes
// qu'il porte réellement dans role_permissions. Ce retrait n'est sûr que
// parce que la phase 2 garantit (invariant testé + réconciliation
// automatique, voir TestSystemAdminRolesContainFullCatalog_Postgres et
// internal/tasks/rbac.go) que le rôle admin de chaque établissement porte
// l'intégralité du catalogue — sans quoi ce changement aurait pu priver
// silencieusement des comptes admin de droits qu'ils avaient jusque-là par le
// court-circuit. permission.SystemKeyAdmin garde un seul rôle après ce
// retrait : marquer le rôle comme non supprimable et non modifiable
// (models.ErrRoleImmutable, garde G4) — voir HasAdminRole, qui reste, elle,
// un usage d'affichage légitime de system_key.
func (u *UserLoginRow) Has(key permission.Key) bool {
	if u.RoleID != nil {
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
// Rights.Admin frequently stays true in production regardless of the
// assigned role (historical seeding), so a caller that wants "is this user's
// *role* admin" must use this method and not read Rights.Admin directly, or
// it will report admin for merchant staff whose role_id points at a
// non-admin role.
//
// RBAC lot 11, phase 4: the former UserLoginRow.IsAdmin() method — the raw
// Rights.Admin column alone, consulted by middleware.RequireAdmin() for an
// unrelated, non-catalog authorization decision — has been removed along
// with RequireAdmin itself. HasAdminRole is display-only (login's `admin`
// flag, GET /me/permissions's `is_admin`) and was never the authorization
// path; it is unaffected by that removal.
func (u *UserLoginRow) HasAdminRole() bool {
	if u.RoleID != nil {
		return u.RoleSystemKey != nil && *u.RoleSystemKey == permission.SystemKeyAdmin
	}
	return u.Rights.Admin
}
