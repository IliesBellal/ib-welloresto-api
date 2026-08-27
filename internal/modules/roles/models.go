package roles

import (
	"time"

	"welloresto-api/internal/permission"
)

// System role keys. A role with a non-nil SystemKey is one of the two
// baseline roles every merchant gets (see EnsureSystemRoles) — custom roles
// created later have SystemKey == nil.
//
// Re-exported from internal/permission (their canonical home as of RBAC lot
// 6 — see that package's system_keys.go for why) so every existing reference
// to roles.SystemKeyAdmin/SystemKeyStaff in this package keeps compiling
// unchanged.
const (
	SystemKeyAdmin = permission.SystemKeyAdmin
	SystemKeyStaff = permission.SystemKeyStaff
)

// Permission mirrors a row of the `permissions` table — the fixed catalog of
// grantable actions (see migrations/done/095_roles_permissions_catalog.up.sql
// and internal/permission for the Go-side mirror of the key list).
type Permission struct {
	Key          permission.Key `json:"key" db:"key"`
	Domain       string         `json:"domain" db:"domain"`
	Label        string         `json:"label" db:"label"`
	Description  string         `json:"description" db:"description"`
	IsSensitive  bool           `json:"is_sensitive" db:"is_sensitive"`
	SortOrder    int            `json:"sort_order" db:"sort_order"`
	DeprecatedAt *time.Time     `json:"deprecated_at,omitempty" db:"deprecated_at"`
}

// Role mirrors a row of the `roles` table — a per-merchant named bundle of
// permissions. SystemKey is non-nil only for the two roles EnsureSystemRoles
// creates ("admin", "staff").
type Role struct {
	ID          string     `json:"id" db:"id"`
	MerchantID  string     `json:"merchant_id" db:"merchant_id"`
	Name        string     `json:"name" db:"name"`
	Description string     `json:"description" db:"description"`
	SystemKey   *string    `json:"system_key,omitempty" db:"system_key"`
	Version     int        `json:"version" db:"version"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty" db:"archived_at"`
}

// RoleWithPermissions is a Role joined with the permissions it grants, via
// role_permissions.
type RoleWithPermissions struct {
	Role
	Permissions []Permission `json:"permissions"`
}

// RoleListItem is a Role annotated with the two counts the list view needs
// (GET /roles) without pulling the full permission/member rows.
type RoleListItem struct {
	Role
	PermissionCount int `json:"permission_count"`
	MemberCount     int `json:"member_count"`
}

// RoleMember is one holder of a role — the shape GET /roles/{id}/members
// returns. Enabled mirrors users_rights.enabled for that specific link (a
// role can be held by a since-disabled link — see lot 4's assign_admin_role,
// which deliberately assigns role_id to disabled rows too).
type RoleMember struct {
	UserID    string `json:"user_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Enabled   bool   `json:"enabled"`
}

// PermissionDomainGroup is one domain's slice of the catalog — GET
// /permissions groups the flat 15-row catalog this way for the front.
type PermissionDomainGroup struct {
	Domain      string       `json:"domain"`
	Permissions []Permission `json:"permissions"`
}

// MyPermissions is the caller's effective permissions (GET /me/permissions):
// which role carries them (nil if the caller has no role_id yet — the
// legacy/pre-lot-4 world), the flat catalog-key list Has() grants them today,
// and whether they are an administrator (role admin OR legacy Rights.Admin).
type MyPermissions struct {
	Role        *MyPermissionsRole `json:"role"`
	Permissions []permission.Key   `json:"permissions"`
	IsAdmin     bool               `json:"is_admin"`
}

type MyPermissionsRole struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	SystemKey *string `json:"system_key,omitempty"`
}

// CreateRoleRequest is the POST /roles body. DuplicateFromRoleID, when set,
// copies the source role's permission_keys onto the new role in the same
// transaction — Name/Description are still taken from the request, never
// from the source (a duplicate is a starting point, not a clone of identity).
type CreateRoleRequest struct {
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	DuplicateFromRoleID *string `json:"duplicate_from_role_id,omitempty"`
}

// UpdateRoleRequest is the PATCH /roles/{id} body — name and/or description
// only (permissions go through PUT /roles/{id}/permissions instead). Version
// is the optimistic-locking token read at GET time; required on every write.
type UpdateRoleRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Version     int     `json:"version"`
}

// ReplacePermissionsRequest is the PUT /roles/{id}/permissions body — the
// full target set (this replaces, it does not diff/patch).
type ReplacePermissionsRequest struct {
	PermissionKeys []permission.Key `json:"permission_keys"`
	Version        int              `json:"version"`
}

// SetUserRoleRequest is the PUT /users/{id}/role body.
type SetUserRoleRequest struct {
	RoleID string `json:"role_id"`
}

// SetDefaultRoleRequest is the PUT /merchant/default-role body.
type SetDefaultRoleRequest struct {
	RoleID string `json:"role_id"`
}
