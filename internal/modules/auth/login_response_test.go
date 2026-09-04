package auth

import (
	"reflect"
	"testing"

	"welloresto-api/internal/permission"
)

// TestBuildLoginResponse_PermissionsPassthrough is the RBAC lot 9 regression
// test for the new top-level Permissions field: buildLoginResponse must
// forward UserLoginRow.Permissions to the wire exactly, with no silent drop,
// dedup, or reordering. This is the "inversement" direction called for in
// the spec — TestFilterValid (internal/permission) already covers "a key
// that isn't in the catalog must not survive"; this covers "a key that is in
// the catalog must survive the trip to the login response untouched".
func TestBuildLoginResponse_PermissionsPassthrough(t *testing.T) {
	want := []string{string(permission.StaffManage), string(permission.SettingsManage)}
	user := &UserLoginRow{Permissions: want}

	resp := buildLoginResponse(user, nil)

	if !reflect.DeepEqual(resp.Permissions, want) {
		t.Fatalf("buildLoginResponse(...).Permissions = %v, want %v", resp.Permissions, want)
	}
}

// TestBuildLoginResponse_PermissionsEmpty covers the pre-lot-4 / no-role case
// (attachRolePermissions never sets Permissions when RoleID is nil): the
// login response must carry an empty slice, not panic on a nil one.
func TestBuildLoginResponse_PermissionsEmpty(t *testing.T) {
	user := &UserLoginRow{}
	resp := buildLoginResponse(user, nil)
	if len(resp.Permissions) != 0 {
		t.Fatalf("buildLoginResponse(...).Permissions = %v, want empty", resp.Permissions)
	}
}

// TestBuildLoginResponse_PrintMerchantCashReport_AlwaysTrue is the RBAC lot
// 12 regression test: CanPrintCashReport() (Rights.Admin ||
// Rights.PrintMerchantCashReport) was decommissioned — capabilities.actions.
// print_merchant_cash_report must now be true unconditionally (the JSON
// field itself is kept, per docs/decisions.md, since wello_resto_flutter and
// wello-back-office both parse it), regardless of Admin or the underlying
// boolean right.
func TestBuildLoginResponse_PrintMerchantCashReport_AlwaysTrue(t *testing.T) {
	cases := []struct {
		name  string
		admin bool
		right bool
	}{
		{"neither admin nor the right", false, false},
		{"the right only", false, true},
		{"admin only", true, false},
		{"both", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := &UserLoginRow{}
			user.Rights.Admin = tc.admin
			user.Rights.PrintMerchantCashReport = tc.right

			resp := buildLoginResponse(user, nil)

			if resp.Capabilities == nil || !resp.Capabilities.Actions.PrintMerchantCashReport {
				t.Fatalf("capabilities.actions.print_merchant_cash_report = %v, want true (open to all)",
					resp.Capabilities.Actions.PrintMerchantCashReport)
			}
			// access.permissions.print_merchant_cash_report is a different,
			// untouched field (reads Rights.PrintMerchantCashReport raw,
			// never went through CanPrintCashReport()) — must still reflect
			// the real per-user right, not the new constant.
			if resp.Access == nil || resp.Access.Permissions.PrintMerchantCashReport != tc.right {
				t.Fatalf("access.permissions.print_merchant_cash_report = %v, want %v (untouched by this lot)",
					resp.Access.Permissions.PrintMerchantCashReport, tc.right)
			}
		})
	}
}
