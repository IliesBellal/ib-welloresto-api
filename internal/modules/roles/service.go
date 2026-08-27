package roles

import (
	"context"
	"database/sql"
	"strings"

	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"
	usersModule "welloresto-api/internal/modules/users"
	"welloresto-api/internal/permission"
)

// Service is the RBAC lot 6 role-administration business layer. It depends
// on users.UsersRepository for exactly one thing — GetUsersRightsToken,
// reused as instructed rather than duplicated — and on middleware for the
// current-user context, the same pattern every other module's service layer
// already uses (see users.UsersService). This is safe from an import-cycle
// perspective only because internal/modules/auth no longer depends on this
// package for SystemKeyAdmin/SystemKeyStaff (moved to internal/permission —
// see that package's system_keys.go for the full explanation); roles used to
// be upstream of auth, so roles importing auth/middleware back would have
// been circular before that move.
type Service struct {
	repo      *Repository
	usersRepo *usersModule.UsersRepository
	audit     auditpkg.AuditService
	redis     *redisclient.Client
}

func NewService(repo *Repository, usersRepo *usersModule.UsersRepository, audit auditpkg.AuditService, redis *redisclient.Client) *Service {
	return &Service{repo: repo, usersRepo: usersRepo, audit: audit, redis: redis}
}

// ListPermissionCatalog returns the 15-entry catalog grouped by domain, in
// the order permissions.sort_order already establishes.
func (s *Service) ListPermissionCatalog(ctx context.Context) ([]PermissionDomainGroup, error) {
	items, err := s.repo.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}

	groups := make([]PermissionDomainGroup, 0)
	index := make(map[string]int)
	for _, p := range items {
		i, ok := index[p.Domain]
		if !ok {
			i = len(groups)
			index[p.Domain] = i
			groups = append(groups, PermissionDomainGroup{Domain: p.Domain})
		}
		groups[i].Permissions = append(groups[i].Permissions, p)
	}
	return groups, nil
}

// MyPermissions returns the caller's effective permissions (via Has(), so
// this always agrees with what RequirePermission would decide for every
// catalog key) and the role that carries them, if any.
func (s *Service) MyPermissions(ctx context.Context) (*MyPermissions, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	granted := make([]permission.Key, 0, len(permission.All))
	for _, key := range permission.All {
		if currentUser.Has(key) {
			granted = append(granted, key)
		}
	}

	result := &MyPermissions{
		Permissions: granted,
		// RBAC lot 9: HasAdminRole(), not the raw Rights.Admin column — that
		// column commonly stays true in production regardless of the
		// assigned role, which made this field claim admin for merchant
		// staff on a non-admin role. See HasAdminRole's doc comment.
		IsAdmin: currentUser.HasAdminRole(),
	}

	if currentUser.RoleID != nil {
		role, err := s.repo.GetRoleByID(ctx, currentUser.MerchantID, *currentUser.RoleID)
		if err != nil {
			return nil, err
		}
		if role != nil {
			result.Role = &MyPermissionsRole{ID: role.ID, Name: role.Name, SystemKey: role.SystemKey}
		}
	}
	return result, nil
}

// ListRoles returns every non-archived role of the caller's merchant,
// annotated with its permission and member counts for the list view.
func (s *Service) ListRoles(ctx context.Context) ([]RoleListItem, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	list, err := s.repo.ListRoles(ctx, currentUser.MerchantID)
	if err != nil {
		return nil, err
	}

	items := make([]RoleListItem, 0, len(list))
	for _, role := range list {
		perms, err := s.repo.GetRolePermissions(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		holders, err := s.repo.CountRoleHolders(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, RoleListItem{Role: role, PermissionCount: len(perms), MemberCount: holders})
	}
	return items, nil
}

// getMerchantRole fetches roleID scoped to merchantID, collapsing "unknown
// id" and "id belongs to another merchant" into the same models.ErrRoleNotFound
// — §1: a cross-tenant id must read as not-found, never as forbidden.
func (s *Service) getMerchantRole(ctx context.Context, merchantID, roleID string) (*Role, error) {
	role, err := s.repo.GetRoleByID(ctx, merchantID, roleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, models.ErrRoleNotFound
	}
	return role, nil
}

// GetRole returns a merchant-scoped role with its permissions.
func (s *Service) GetRole(ctx context.Context, roleID string) (*RoleWithPermissions, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	role, err := s.getMerchantRole(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	perms, err := s.repo.GetRolePermissions(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	return &RoleWithPermissions{Role: *role, Permissions: perms}, nil
}

// CreateRole creates a custom role for the caller's merchant. When
// DuplicateFromRoleID is set, the source role's current permission set is
// copied onto the new role (name/description always come from the request,
// never from the source — a duplicate is a starting point, not a clone of
// identity).
func (s *Service) CreateRole(ctx context.Context, req CreateRoleRequest) (*RoleWithPermissions, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, models.ErrRoleNameRequired
	}

	var seed []permission.Key
	if req.DuplicateFromRoleID != nil && strings.TrimSpace(*req.DuplicateFromRoleID) != "" {
		source, err := s.getMerchantRole(ctx, currentUser.MerchantID, strings.TrimSpace(*req.DuplicateFromRoleID))
		if err != nil {
			return nil, err
		}
		sourcePerms, err := s.repo.GetRolePermissions(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		seed = make([]permission.Key, len(sourcePerms))
		for i, p := range sourcePerms {
			seed[i] = p.Key
		}
	}

	roleID, err := s.repo.CreateRole(ctx, currentUser.MerchantID, name, strings.TrimSpace(req.Description), seed)
	if err != nil {
		return nil, err
	}

	role, err := s.repo.GetRoleByID(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	perms, err := s.repo.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}

	if s.audit != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "role.created", "role", roleID,
			nil, map[string]interface{}{"name": role.Name, "permissions": permissionKeyStrings(perms)})
	}

	return &RoleWithPermissions{Role: *role, Permissions: perms}, nil
}

// UpdateRole applies an optimistic-locked PATCH of name/description. Neither
// field is mandatory individually, but Version always is.
func (s *Service) UpdateRole(ctx context.Context, roleID string, req UpdateRoleRequest) (*RoleWithPermissions, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if req.Version <= 0 {
		return nil, models.ErrRoleVersionRequired
	}

	role, err := s.getMerchantRole(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	// G4: the admin role is not renamable.
	if role.SystemKey != nil && *role.SystemKey == permission.SystemKeyAdmin {
		return nil, models.ErrRoleImmutable
	}

	var trimmedName, trimmedDescription *string
	if req.Name != nil {
		v := strings.TrimSpace(*req.Name)
		if v == "" {
			return nil, models.ErrRoleNameRequired
		}
		trimmedName = &v
	}
	if req.Description != nil {
		v := strings.TrimSpace(*req.Description)
		trimmedDescription = &v
	}

	matched, err := s.repo.UpdateRoleNameDescription(ctx, roleID, trimmedName, trimmedDescription, req.Version)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, s.notFoundOrVersionConflict(ctx, roleID)
	}

	updated, err := s.repo.GetRoleByID(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	perms, err := s.repo.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}

	if s.audit != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "role.renamed", "role", roleID,
			map[string]interface{}{"name": role.Name, "description": role.Description},
			map[string]interface{}{"name": updated.Name, "description": updated.Description})
	}

	return &RoleWithPermissions{Role: *updated, Permissions: perms}, nil
}

// ReplacePermissions replaces roleID's entire permission set (PUT semantics —
// not a diff/patch), guarded by G1 (can't touch a role you hold), G2 (can't
// strip the establishment's last staff.manage holder), G4 (admin immutable),
// and optimistic locking on Version.
func (s *Service) ReplacePermissions(ctx context.Context, roleID string, req ReplacePermissionsRequest) (*RoleWithPermissions, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if req.Version <= 0 {
		return nil, models.ErrRoleVersionRequired
	}

	role, err := s.getMerchantRole(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	// G4: the admin role's permissions are not modifiable — it holds the
	// whole catalog by construction, including keys added after it existed.
	if role.SystemKey != nil && *role.SystemKey == permission.SystemKeyAdmin {
		return nil, models.ErrRoleImmutable
	}
	// G1: cannot edit the permissions of a role you yourself currently hold —
	// without this, "I can't change my own role" is a one-click bypass via
	// editing that role's grants instead.
	if currentUser.RoleID != nil && *currentUser.RoleID == roleID {
		return nil, models.ErrRoleSelfModification
	}

	newKeys, err := dedupeValidatedKeys(req.PermissionKeys)
	if err != nil {
		return nil, err
	}
	newHasStaffManage := false
	for _, k := range newKeys {
		if k == permission.StaffManage {
			newHasStaffManage = true
			break
		}
	}

	oldPerms, err := s.repo.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}
	oldHadStaffManage := containsPermission(oldPerms, permission.StaffManage)

	// G2: removing staff.manage from this role must not leave the
	// establishment with zero active holders of it anywhere else.
	if oldHadStaffManage && !newHasStaffManage {
		others, err := s.repo.CountActiveStaffManageHoldersExcludingRole(ctx, currentUser.MerchantID, roleID)
		if err != nil {
			return nil, err
		}
		if others == 0 {
			return nil, models.ErrRoleStaffManageRequired
		}
	}

	// §3: snapshot current holders' tokens before the write so every session
	// affected by the change gets invalidated, even one that gets disabled
	// concurrently between this read and the write below (best-effort either
	// way — a missed token just waits out the 60-minute TTL, per §3).
	holderTokens, err := s.repo.ListRoleHolderTokens(ctx, roleID)
	if err != nil {
		return nil, err
	}

	matched, err := s.repo.ReplaceRolePermissions(ctx, roleID, newKeys, req.Version)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, s.notFoundOrVersionConflict(ctx, roleID)
	}

	s.invalidateTokens(ctx, holderTokens)

	updated, err := s.repo.GetRoleByID(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	newPerms, err := s.repo.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}

	if s.audit != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "role.permissions.changed", "role", roleID,
			map[string]interface{}{"permissions": permissionKeyStrings(oldPerms)},
			map[string]interface{}{"permissions": permissionKeyStrings(newPerms)})
	}

	return &RoleWithPermissions{Role: *updated, Permissions: newPerms}, nil
}

// ListRoleMembers returns every holder of a merchant-scoped role.
func (s *Service) ListRoleMembers(ctx context.Context, roleID string) ([]RoleMember, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	if _, err := s.getMerchantRole(ctx, currentUser.MerchantID, roleID); err != nil {
		return nil, err
	}
	members, err := s.repo.ListRoleMembers(ctx, roleID)
	if err != nil {
		return nil, err
	}
	return members, nil
}

// ArchiveRole archives a role, guarded by G4 (admin immutable), G5 (no
// current holder, enabled or not), and G6 (the staff role can't be archived
// while it is still the merchant's default for new accounts). Idempotent:
// archiving an already-archived role just returns it unchanged.
func (s *Service) ArchiveRole(ctx context.Context, roleID string) (*Role, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	role, err := s.getMerchantRole(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	if role.ArchivedAt != nil {
		return role, nil
	}
	if role.SystemKey != nil && *role.SystemKey == permission.SystemKeyAdmin {
		return nil, models.ErrRoleImmutable
	}

	holders, err := s.repo.CountRoleHolders(ctx, roleID)
	if err != nil {
		return nil, err
	}
	if holders > 0 {
		return nil, &RoleHasMembersError{Count: holders}
	}

	if role.SystemKey != nil && *role.SystemKey == permission.SystemKeyStaff {
		defaultRoleID, err := s.repo.GetMerchantDefaultRoleID(ctx, currentUser.MerchantID)
		if err != nil {
			return nil, err
		}
		if defaultRoleID == roleID {
			return nil, models.ErrRoleIsMerchantDefault
		}
	}

	// §3: fetched right before the write, defensive against a holder
	// assigned concurrently between the G5 check above and this archive —
	// always empty in the ordinary path since G5 already requires zero.
	holderTokens, err := s.repo.ListRoleHolderTokens(ctx, roleID)
	if err != nil {
		return nil, err
	}

	perms, err := s.repo.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}

	if err := s.repo.ArchiveRole(ctx, roleID); err != nil {
		return nil, err
	}
	s.invalidateTokens(ctx, holderTokens)

	if s.audit != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "role.archived", "role", roleID,
			map[string]interface{}{"name": role.Name, "permissions": permissionKeyStrings(perms)}, nil)
	}

	updated, err := s.repo.GetRoleByID(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetUserRole assigns targetUserID's single role within the caller's
// merchant, guarded by G1 (can't change your own role) and G2 (can't strip
// the establishment's last staff.manage holder).
func (s *Service) SetUserRole(ctx context.Context, targetUserID string, req SetUserRoleRequest) (*Role, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	targetUserID = strings.TrimSpace(targetUserID)
	roleID := strings.TrimSpace(req.RoleID)
	if targetUserID == "" {
		return nil, models.ErrMissingResourceID
	}
	if roleID == "" {
		return nil, models.ErrInvalidInput
	}
	// G1: cannot change your own role assignment.
	if targetUserID == currentUser.UserID {
		return nil, models.ErrRoleSelfModification
	}

	newRole, err := s.getMerchantRole(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	if newRole.ArchivedAt != nil {
		return nil, models.ErrRoleNotFound
	}

	_, currentRoleID, err := s.repo.GetUserRightsRoleID(ctx, currentUser.MerchantID, targetUserID)
	if err == sql.ErrNoRows {
		return nil, models.ErrMerchantUserNotFound
	}
	if err != nil {
		return nil, err
	}

	var oldRoleName interface{}
	if currentRoleID != nil {
		oldGrants, err := s.roleGrantsPermission(ctx, *currentRoleID, permission.StaffManage)
		if err != nil {
			return nil, err
		}
		if oldGrants {
			newGrants, err := s.roleGrantsPermission(ctx, roleID, permission.StaffManage)
			if err != nil {
				return nil, err
			}
			if !newGrants {
				// G2: this user may be the establishment's last active
				// staff.manage holder.
				count, err := s.repo.CountActiveStaffManageHolders(ctx, currentUser.MerchantID)
				if err != nil {
					return nil, err
				}
				if count <= 1 {
					return nil, models.ErrRoleStaffManageRequired
				}
			}
		}
		if oldRole, err := s.repo.GetRoleByIDUnscoped(ctx, *currentRoleID); err == nil && oldRole != nil {
			oldRoleName = oldRole.Name
		}
	}

	matched, err := s.repo.SetUserRole(ctx, currentUser.MerchantID, targetUserID, roleID)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, models.ErrMerchantUserNotFound
	}

	// §3: invalidate the target's cached session — GetUsersRightsToken is
	// users.UsersRepository's, reused rather than duplicated.
	if token, tokenErr := s.usersRepo.GetUsersRightsToken(ctx, currentUser.MerchantID, targetUserID); tokenErr == nil {
		s.invalidateTokens(ctx, []string{token})
	}

	if s.audit != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "user.role.changed", "merchant_user", targetUserID,
			map[string]interface{}{"role_id": nullableStringPtr(currentRoleID), "role_name": oldRoleName},
			map[string]interface{}{"role_id": newRole.ID, "role_name": newRole.Name})
	}

	return newRole, nil
}

// SetMerchantDefaultRole repoints the caller's merchant.default_role_id — the
// role every future users_rights row lands on absent another choice (see
// internal/modules/pos/create_service.go, internal/modules/users). No G-guard
// applies: this never changes an existing holder's grants.
func (s *Service) SetMerchantDefaultRole(ctx context.Context, req SetDefaultRoleRequest) (*Role, error) {
	currentUser, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	roleID := strings.TrimSpace(req.RoleID)
	if roleID == "" {
		return nil, models.ErrInvalidInput
	}

	role, err := s.getMerchantRole(ctx, currentUser.MerchantID, roleID)
	if err != nil {
		return nil, err
	}
	if role.ArchivedAt != nil {
		return nil, models.ErrRoleNotFound
	}

	oldRoleID, err := s.repo.GetMerchantDefaultRoleID(ctx, currentUser.MerchantID)
	if err != nil {
		return nil, err
	}

	if err := s.repo.SetMerchantDefaultRoleID(ctx, currentUser.MerchantID, roleID); err != nil {
		return nil, err
	}

	var oldRoleName interface{}
	if oldRoleID != "" {
		if oldRole, err := s.repo.GetRoleByIDUnscoped(ctx, oldRoleID); err == nil && oldRole != nil {
			oldRoleName = oldRole.Name
		}
	}

	if s.audit != nil {
		_ = s.audit.LogChange(ctx, currentUser.MerchantID, currentUser.UserID, "merchant.default_role.changed", "merchant", currentUser.MerchantID,
			map[string]interface{}{"role_id": nullableString(oldRoleID), "role_name": oldRoleName},
			map[string]interface{}{"role_id": role.ID, "role_name": role.Name})
	}

	return role, nil
}

// notFoundOrVersionConflict resolves an unmatched optimistic-locked write:
// the role either no longer exists / got archived concurrently (not found),
// or it does exist but at a different version (conflict, carrying that
// version so the front can offer a reload — §2).
func (s *Service) notFoundOrVersionConflict(ctx context.Context, roleID string) error {
	current, err := s.repo.GetRoleByIDUnscoped(ctx, roleID)
	if err != nil {
		return err
	}
	if current == nil || current.ArchivedAt != nil {
		return models.ErrRoleNotFound
	}
	return &VersionConflictError{CurrentVersion: current.Version}
}

func (s *Service) roleGrantsPermission(ctx context.Context, roleID string, key permission.Key) (bool, error) {
	perms, err := s.repo.GetRolePermissions(ctx, roleID)
	if err != nil {
		return false, err
	}
	return containsPermission(perms, key), nil
}

// invalidateTokens deletes every cached session for the given tokens.
// Best-effort: a Redis failure is logged, never surfaced as an operation
// failure — the database stays the source of truth, the cache is only an
// accelerator (§3).
func (s *Service) invalidateTokens(ctx context.Context, tokens []string) {
	if s.redis == nil {
		return
	}
	log := logger.FromContext(ctx)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if !s.redis.Delete(ctx, models.UserCachePrefix+token) {
			log.Warn("roles: failed to invalidate a cached session after a role/permission change")
		}
	}
}

func containsPermission(perms []Permission, key permission.Key) bool {
	for _, p := range perms {
		if p.Key == key {
			return true
		}
	}
	return false
}

func permissionKeyStrings(perms []Permission) []string {
	keys := make([]string, len(perms))
	for i, p := range perms {
		keys[i] = string(p.Key)
	}
	return keys
}

// dedupeValidatedKeys rejects any key absent from the catalog and drops
// duplicates, preserving first-occurrence order.
func dedupeValidatedKeys(keys []permission.Key) ([]permission.Key, error) {
	valid := make(map[permission.Key]bool, len(permission.All))
	for _, k := range permission.All {
		valid[k] = true
	}
	seen := make(map[permission.Key]bool, len(keys))
	out := make([]permission.Key, 0, len(keys))
	for _, k := range keys {
		if !valid[k] {
			return nil, models.ErrRolePermissionKeyUnknown
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, nil
}

func nullableString(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

func nullableStringPtr(v *string) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
