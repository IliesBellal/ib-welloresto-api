package auth

import (
	"testing"

	"welloresto-api/internal/permission"
)

// legacyBoolSetter lets each table-driven case flip exactly the one boolean
// field it is testing, without a giant switch statement.
type legacyBoolSetter func(*UserRowRights, bool)

// TestHas_HistoricalMode_FollowsCorrespondenceTable is the direct proof of
// the lot 2 correspondence table: for every catalog key with a historical
// boolean equivalent, true grants it and false denies it, exactly like today.
func TestHas_HistoricalMode_FollowsCorrespondenceTable(t *testing.T) {
	cases := []struct {
		key   permission.Key
		field string
		set   legacyBoolSetter
	}{
		{permission.POSStatusManage, "AccessReception", func(r *UserRowRights, v bool) { r.AccessReception = v }},
		{permission.POSCashDrawerOpen, "OpenCashDrawer", func(r *UserRowRights, v bool) { r.OpenCashDrawer = v }},
		{permission.CatalogManage, "CanManageMenu", func(r *UserRowRights, v bool) { r.CanManageMenu = v }},
		{permission.HACCPManage, "CanManageHACCP", func(r *UserRowRights, v bool) { r.CanManageHACCP = v }},
		{permission.CustomersManage, "CanManageCustomers", func(r *UserRowRights, v bool) { r.CanManageCustomers = v }},
		{permission.StaffManage, "CanManageUsers", func(r *UserRowRights, v bool) { r.CanManageUsers = v }},
		{permission.StaffScheduleManage, "CanManagePlannings", func(r *UserRowRights, v bool) { r.CanManagePlannings = v }},
		{permission.ReportsSalesRead, "CanViewReports", func(r *UserRowRights, v bool) { r.CanViewReports = v }},
		{permission.ReportsFinancialRead, "CanViewFinancials", func(r *UserRowRights, v bool) { r.CanViewFinancials = v }},
		{permission.SettingsManage, "CanManageSettings", func(r *UserRowRights, v bool) { r.CanManageSettings = v }},
	}

	for _, tc := range cases {
		t.Run(string(tc.key)+"/true", func(t *testing.T) {
			user := &UserLoginRow{}
			tc.set(&user.Rights, true)
			if !user.Has(tc.key) {
				t.Fatalf("Has(%s) = false, want true when %s = true", tc.key, tc.field)
			}
		})
		t.Run(string(tc.key)+"/false", func(t *testing.T) {
			user := &UserLoginRow{}
			tc.set(&user.Rights, false)
			if user.Has(tc.key) {
				t.Fatalf("Has(%s) = true, want false when %s = false and admin = false", tc.key, tc.field)
			}
		})
	}
}

// TestHas_HistoricalMode_AdminOnlyKeysNeedAdmin covers the three catalog keys
// with no historical boolean at all: nothing but Rights.Admin grants them in
// the historical world.
func TestHas_HistoricalMode_AdminOnlyKeysNeedAdmin(t *testing.T) {
	adminOnlyKeys := []permission.Key{
		permission.POSTicketReopen,
		permission.POSRefund,
		permission.InventoryManage,
	}

	for _, key := range adminOnlyKeys {
		t.Run(string(key)+"/non-admin denied", func(t *testing.T) {
			user := &UserLoginRow{Rights: UserRowRights{
				AccessReception: true, OpenCashDrawer: true,
				CanManageMenu: true, CanManageHACCP: true, CanManageCustomers: true,
				CanManageUsers: true, CanManagePlannings: true, CanViewReports: true,
				CanViewFinancials: true, CanManageSettings: true,
			}}
			if user.Has(key) {
				t.Fatalf("Has(%s) = true for a fully-flagged non-admin user; this key has no historical boolean and must require Admin", key)
			}
		})
		t.Run(string(key)+"/admin granted", func(t *testing.T) {
			user := &UserLoginRow{Rights: UserRowRights{Admin: true}}
			if !user.Has(key) {
				t.Fatalf("Has(%s) = false for an admin user, want true (Admin short-circuits everything)", key)
			}
		})
	}
}

// TestHas_HistoricalMode_AdminShortCircuitsEverything mirrors what
// middleware.IsAdmin/user.IsAdmin already assume everywhere else: an admin
// with every boolean false must still pass every permission check.
func TestHas_HistoricalMode_AdminShortCircuitsEverything(t *testing.T) {
	user := &UserLoginRow{Rights: UserRowRights{Admin: true}}
	for _, key := range permission.All {
		if !user.Has(key) {
			t.Fatalf("admin user denied %s, want granted", key)
		}
	}
}

// TestHas_RoleMode_IgnoresBooleansEvenWhenTheyContradict is the exclusivity
// test the spec calls for: once RoleID is set, the historical booleans must
// never be consulted, even when they say the opposite of the role.
func TestHas_RoleMode_IgnoresBooleansEvenWhenTheyContradict(t *testing.T) {
	roleID := "role-1"
	user := &UserLoginRow{
		RoleID:      &roleID,
		Permissions: []string{string(permission.POSTicketReopen)},
		// Every boolean says "grant everything" — role mode must ignore all of it.
		Rights: UserRowRights{
			Admin: true, AccessReception: true, OpenCashDrawer: true,
			CanManageMenu: true, CanManageHACCP: true, CanManageCustomers: true,
			CanManageUsers: true, CanManagePlannings: true, CanViewReports: true,
			CanViewFinancials: true, CanManageSettings: true,
		},
	}

	if !user.Has(permission.POSTicketReopen) {
		t.Fatal("expected pos.ticket.reopen to be granted: it is in Permissions")
	}
	for _, key := range permission.All {
		if key == permission.POSTicketReopen {
			continue
		}
		if user.Has(key) {
			t.Fatalf("Has(%s) = true, want false: role only grants pos.ticket.reopen, and Rights.Admin=true must be ignored in role mode", key)
		}
	}
}

// TestHas_RoleMode_SystemKeyAdminGrantsEverything covers the other half of
// the role-mode exclusivity: system_key = 'admin' grants every key, including
// one absent from Permissions — the role's booleans-of-role (its actual
// role_permissions rows) are irrelevant once the role itself is the admin
// system role.
func TestHas_RoleMode_SystemKeyAdminGrantsEverything(t *testing.T) {
	roleID := "role-admin-1"
	systemKey := permission.SystemKeyAdmin
	user := &UserLoginRow{
		RoleID:        &roleID,
		RoleSystemKey: &systemKey,
		Permissions:   nil, // deliberately empty: system_key alone must be enough
	}

	for _, key := range permission.All {
		if !user.Has(key) {
			t.Fatalf("Has(%s) = false for system_key=admin with empty Permissions, want true", key)
		}
	}
}

// TestHas_RoleMode_NonAdminSystemKeyStillNeedsExplicitGrant makes sure the
// system_key shortcut is admin-specific: a "staff" system role with no
// matching Permissions entry does not get a free pass.
func TestHas_RoleMode_NonAdminSystemKeyStillNeedsExplicitGrant(t *testing.T) {
	roleID := "role-staff-1"
	systemKey := permission.SystemKeyStaff
	user := &UserLoginRow{
		RoleID:        &roleID,
		RoleSystemKey: &systemKey,
		Permissions:   []string{string(permission.POSTicketReopen)},
	}

	if !user.Has(permission.POSTicketReopen) {
		t.Fatal("expected pos.ticket.reopen to be granted: it is in Permissions")
	}
	if user.Has(permission.SettingsManage) {
		t.Fatal("expected settings.manage to be denied: staff system_key is not admin and it is not in Permissions")
	}
}

// TestHasAdminRole_IgnoresStaleRightsAdminOnceRoleIsSet is the RBAC lot 9
// regression test: production accounts commonly still have
// users_rights.admin = true regardless of their assigned role (historical
// seeding, never cleared). HasAdminRole must not let that stale column claim
// admin for a user whose role is not the admin role — the same exclusivity
// Has() already enforces for catalog keys.
func TestHasAdminRole_IgnoresStaleRightsAdminOnceRoleIsSet(t *testing.T) {
	roleID := "role-staff-1"
	systemKey := permission.SystemKeyStaff
	user := &UserLoginRow{
		RoleID:        &roleID,
		RoleSystemKey: &systemKey,
		Rights:        UserRowRights{Admin: true}, // stale — must be ignored
	}

	if user.HasAdminRole() {
		t.Fatal("HasAdminRole() = true for a staff-system-key role with stale Rights.Admin=true, want false")
	}
}

// TestHasAdminRole_SystemKeyAdminGrantsRegardlessOfRightsAdmin covers the
// other half: an admin-role user is admin even if the legacy column happens
// to be false (e.g. cleared, or never set for a newly created account).
func TestHasAdminRole_SystemKeyAdminGrantsRegardlessOfRightsAdmin(t *testing.T) {
	roleID := "role-admin-1"
	systemKey := permission.SystemKeyAdmin
	user := &UserLoginRow{
		RoleID:        &roleID,
		RoleSystemKey: &systemKey,
		Rights:        UserRowRights{Admin: false},
	}

	if !user.HasAdminRole() {
		t.Fatal("HasAdminRole() = false for a system_key=admin role with Rights.Admin=false, want true")
	}
}

// TestHasAdminRole_HistoricalMode_FallsBackToRightsAdmin covers a user with
// no role_id yet (pre-lot-4 world) — HasAdminRole must behave exactly like
// the legacy Rights.Admin column, since there is no role to consult.
func TestHasAdminRole_HistoricalMode_FallsBackToRightsAdmin(t *testing.T) {
	adminUser := &UserLoginRow{Rights: UserRowRights{Admin: true}}
	if !adminUser.HasAdminRole() {
		t.Fatal("HasAdminRole() = false for RoleID=nil, Rights.Admin=true, want true")
	}

	nonAdminUser := &UserLoginRow{Rights: UserRowRights{Admin: false}}
	if nonAdminUser.HasAdminRole() {
		t.Fatal("HasAdminRole() = true for RoleID=nil, Rights.Admin=false, want false")
	}
}
