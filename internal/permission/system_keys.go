package permission

// System role keys. A role with a non-nil SystemKey is one of the two
// baseline roles every merchant gets (see roles.Repository.EnsureSystemRoles)
// — custom roles created later have SystemKey == nil.
//
// Declared here, not in internal/modules/roles (their original home), because
// internal/modules/auth needs them for UserLoginRow.Has()'s admin
// short-circuit, and internal/modules/roles needs internal/modules/auth for
// RBAC lot 6 (current-user context in its service layer) — roles importing
// auth while auth imports roles would be an import cycle. permission has no
// dependencies of its own and both packages already import it, so this is
// the natural shared home. roles.SystemKeyAdmin/SystemKeyStaff remain valid
// (re-exported as aliases in internal/modules/roles/models.go) so existing
// call sites there are unaffected.
const (
	SystemKeyAdmin = "admin"
	SystemKeyStaff = "staff"
)
