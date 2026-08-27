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
