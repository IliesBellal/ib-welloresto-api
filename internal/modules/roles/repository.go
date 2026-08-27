package roles

import (
	"context"
	"database/sql"
	"fmt"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/permission"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// ListPermissions returns the full permission catalog, ordered the same way
// it is declared in migrations/done/095_roles_permissions_catalog.up.sql.
func (r *Repository) ListPermissions(ctx context.Context) ([]Permission, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT key, domain, label, description, is_sensitive, sort_order, deprecated_at
		FROM permissions
		ORDER BY sort_order ASC, key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Permission, 0)
	for rows.Next() {
		p, err := scanPermission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *p)
	}
	return items, rows.Err()
}

// ListRoles returns every non-archived role of a merchant.
func (r *Repository) ListRoles(ctx context.Context, merchantID string) ([]Role, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at
		FROM roles
		WHERE merchant_id = ? AND archived_at IS NULL
		ORDER BY name ASC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Role, 0)
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *role)
	}
	return items, rows.Err()
}

// GetRoleByID returns a merchant's role by id, including archived ones.
// Returns (nil, nil) when no such role exists for this merchant — same
// "absent, not an error" convention as the rest of the codebase.
func (r *Repository) GetRoleByID(ctx context.Context, merchantID, roleID string) (*Role, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at
		FROM roles
		WHERE merchant_id = ? AND id = ?
	`, merchantID, roleID)

	role, err := scanRole(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return role, err
}

// GetRolePermissions returns the permissions granted to a role, ordered like
// ListPermissions. Returns an empty (non-nil) slice for a role with no
// permission and for an unknown role id alike — callers that need to
// distinguish the two should check GetRoleByID first.
func (r *Repository) GetRolePermissions(ctx context.Context, roleID string) ([]Permission, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at
		FROM role_permissions rp
		INNER JOIN permissions p ON p.key = rp.permission_key
		WHERE rp.role_id = ?
		ORDER BY p.sort_order ASC, p.key ASC
	`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Permission, 0)
	for rows.Next() {
		p, err := scanPermission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *p)
	}
	return items, rows.Err()
}

// systemRolePermissions is the permission set each system role is seeded
// with the moment it is created: the admin role gets every catalog
// permission; the staff role gets none.
//
// It wasn't always empty: until RBAC lot 8 (2026-08-27) it held pos.access
// and pos.discount.apply, the two catalog keys that never guarded a route.
// That lot removed both from the catalog entirely (see
// docs/decisions.md and migrations/done/100_deprecate_pos_access_and_discount_apply.up.sql)
// on the finding that everything a floor employee does day to day —
// including taking an order and applying a discount — is, and remains,
// unguarded by design; the thirteen permissions left in the catalog all gate
// management/correction gestures a floor employee doesn't perform. An empty
// default is the correct shape for that role now, not a placeholder to fill:
// see docs/RBAC_ROUTES.md for the reasoning, and do not backfill this map
// with an unrelated permission just to make it non-empty.
//
// Only 'admin' is reconciled against this list on every later
// EnsureSystemRoles call (see ensureSystemRole) — it holds everything by
// construction and is not client-editable, so backfilling a catalog addition
// (e.g. migration 097's pos.status.manage) onto pre-existing admin roles is
// safe and intended. 'staff' is explicitly client-editable — its entry here
// is a creation-time default only; reconciling it later would silently
// overwrite a merchant's customized "Employé polyvalent" every time this
// runs (migration catch-up, restore, deploy script), with no error and no
// trace. It never revokes a permission either way; shrinking a system role's
// permission set is not something this lot needs and is not implemented.
var systemRolePermissions = map[string][]permission.Key{
	SystemKeyAdmin: permission.All,
	SystemKeyStaff: {},
}

// systemRoleNames are the display names EnsureSystemRoles gives each system
// role on first creation. Only applied at creation time — renaming a system
// role afterwards (not implemented in this lot) would not be overwritten by a
// later EnsureSystemRoles call, since it only fills in what is missing.
var systemRoleNames = map[string]string{
	SystemKeyAdmin: "Administrateur",
	SystemKeyStaff: "Employé polyvalent",
}

// EnsureSystemRoles makes sure a merchant has its "admin" and "staff" system
// roles, creating whichever is missing, reconciling only "admin" against
// systemRolePermissions (see its doc comment — this is what lets a catalog
// addition backfill onto pre-existing admin roles without touching a
// client-customized staff role), and returns both role ids.
//
// Idempotent: calling it twice for the same merchant never creates a second
// pair of roles, and re-running it after the catalog grew only adds the newly
// missing grants to "admin" — it never duplicates an existing role_permissions
// row. "staff" is never modified past creation: not its permissions, not its
// name. Safe to call from inside an existing transaction (dbutils.RunInTx
// no-ops into the current transaction when one is already open on ctx).
func (r *Repository) EnsureSystemRoles(ctx context.Context, merchantID string) (adminRoleID, staffRoleID string, err error) {
	adminRoleID, err = r.ensureSystemRole(ctx, merchantID, SystemKeyAdmin)
	if err != nil {
		return "", "", fmt.Errorf("ensure %s role: %w", SystemKeyAdmin, err)
	}
	staffRoleID, err = r.ensureSystemRole(ctx, merchantID, SystemKeyStaff)
	if err != nil {
		return "", "", fmt.Errorf("ensure %s role: %w", SystemKeyStaff, err)
	}
	return adminRoleID, staffRoleID, nil
}

func (r *Repository) ensureSystemRole(ctx context.Context, merchantID, systemKey string) (string, error) {
	var roleID string
	err := dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.db)

		var existingID string
		lookupErr := db.QueryRowContext(txCtx, `
			SELECT id FROM roles WHERE merchant_id = ? AND system_key = ?
		`, merchantID, systemKey).Scan(&existingID)

		switch {
		case lookupErr == nil:
			roleID = existingID
		case lookupErr == sql.ErrNoRows:
			roleID = helpers.GeneratePrefixedID(helpers.RoleIDPrefix)
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO roles (id, merchant_id, name, system_key) VALUES (?, ?, ?, ?)
			`, roleID, merchantID, systemRoleNames[systemKey], systemKey); err != nil {
				return fmt.Errorf("insert role: %w", err)
			}
			// Freshly created: seed it with its baseline permission set,
			// regardless of systemKey — this is the one-time default, not the
			// reconciliation gated below.
			return grantMissingPermissions(txCtx, db, roleID, systemRolePermissions[systemKey])
		default:
			return fmt.Errorf("lookup existing role: %w", lookupErr)
		}

		// Existing role: only 'admin' is reconciled against the catalog here.
		// 'staff' is client-editable and must never be touched again once
		// created (see systemRolePermissions' doc comment) — a plain "found it,
		// leave it" for anything else.
		if systemKey != SystemKeyAdmin {
			return nil
		}
		return grantMissingPermissions(txCtx, db, roleID, systemRolePermissions[systemKey])
	})
	return roleID, err
}

// grantMissingPermissions grants roleID whichever of keys it does not already
// carry. Never revokes anything already granted.
func grantMissingPermissions(ctx context.Context, db *dbx.DB, roleID string, keys []permission.Key) error {
	granted, err := loadGrantedPermissionKeys(ctx, db, roleID)
	if err != nil {
		return fmt.Errorf("load existing role permissions: %w", err)
	}
	for _, key := range keys {
		if granted[key] {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO role_permissions (role_id, permission_key) VALUES (?, ?)
		`, roleID, string(key)); err != nil {
			return fmt.Errorf("insert role_permission %s: %w", key, err)
		}
	}
	return nil
}

// loadGrantedPermissionKeys returns the set of permission keys already
// granted to a role, used by ensureSystemRole to compute what is still
// missing without relying on a dialect-specific upsert.
func loadGrantedPermissionKeys(ctx context.Context, db *dbx.DB, roleID string) (map[permission.Key]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT permission_key FROM role_permissions WHERE role_id = ?`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	granted := make(map[permission.Key]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		granted[permission.Key(key)] = true
	}
	return granted, rows.Err()
}

// merchantJoinCast returns the SQL fragment used to compare merchant.id
// (integer PK) against the varchar merchant_id string that circulates
// everywhere else. Same pattern as every other package in this repo (each
// declares its own copy rather than sharing one — see
// pos.POSRepository.MerchantDefaultRoleID for the identical fragment).
func merchantJoinCast() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(id AS TEXT)"
	}
	return "CAST(id AS CHAR)"
}

// CreateRole inserts a new custom role (system_key NULL, version 1) for
// merchantID, optionally seeding it with seedPermissions (the caller resolves
// duplicate_from_role_id into a key list beforehand via GetRolePermissions).
// Runs in a transaction so a failure seeding permissions never leaves a
// permission-less role behind under a name the caller thinks succeeded.
func (r *Repository) CreateRole(ctx context.Context, merchantID, name, description string, seedPermissions []permission.Key) (string, error) {
	roleID := helpers.GeneratePrefixedID(helpers.RoleIDPrefix)
	err := dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.db)
		if _, err := db.ExecContext(txCtx, `
			INSERT INTO roles (id, merchant_id, name, description) VALUES (?, ?, ?, ?)
		`, roleID, merchantID, name, description); err != nil {
			return fmt.Errorf("insert role: %w", err)
		}
		for _, key := range seedPermissions {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO role_permissions (role_id, permission_key) VALUES (?, ?)
			`, roleID, string(key)); err != nil {
				return fmt.Errorf("seed role_permission %s: %w", key, err)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return roleID, nil
}

// UpdateRoleNameDescription applies an optimistic-locked PATCH: name/description
// (either may be nil, meaning "leave unchanged") plus a mandatory version bump.
// matched is false when no row satisfied "id = roleID AND version = expectedVersion
// AND archived_at IS NULL" — the caller re-fetches the role to learn whether
// that is a version conflict or the role vanished/got archived concurrently.
func (r *Repository) UpdateRoleNameDescription(ctx context.Context, roleID string, name, description *string, expectedVersion int) (matched bool, err error) {
	db := dbx.GetDB(ctx, r.db)

	current, err := r.GetRoleByIDUnscoped(ctx, roleID)
	if err != nil {
		return false, err
	}
	newName := current.Name
	if name != nil {
		newName = *name
	}
	newDescription := current.Description
	if description != nil {
		newDescription = *description
	}

	res, err := db.ExecContext(ctx, `
		UPDATE roles
		SET name = ?, description = ?, version = version + 1, updated_at = `+dbx.UTCNow()+`
		WHERE id = ? AND version = ? AND archived_at IS NULL
	`, newName, newDescription, roleID, expectedVersion)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ReplaceRolePermissions replaces roleID's entire permission set inside a
// transaction: the version bump is the compare-and-swap (0 rows affected ==
// stale version, everything else rolls back untouched), then the DELETE+INSERT
// pair runs only once that CAS has succeeded.
func (r *Repository) ReplaceRolePermissions(ctx context.Context, roleID string, keys []permission.Key, expectedVersion int) (matched bool, err error) {
	err = dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.db)

		res, err := db.ExecContext(txCtx, `
			UPDATE roles
			SET version = version + 1, updated_at = `+dbx.UTCNow()+`
			WHERE id = ? AND version = ? AND archived_at IS NULL
		`, roleID, expectedVersion)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			matched = false
			return nil
		}
		matched = true

		if _, err := db.ExecContext(txCtx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
			return fmt.Errorf("clear role_permissions: %w", err)
		}
		for _, key := range keys {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO role_permissions (role_id, permission_key) VALUES (?, ?)
			`, roleID, string(key)); err != nil {
				return fmt.Errorf("insert role_permission %s: %w", key, err)
			}
		}
		return nil
	})
	return matched, err
}

// ArchiveRole sets archived_at on a role. Callers are responsible for every
// guard (G4/G5/G6) before calling this — it performs no check itself.
func (r *Repository) ArchiveRole(ctx context.Context, roleID string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		UPDATE roles SET archived_at = `+dbx.UTCNow()+` WHERE id = ?
	`, roleID)
	return err
}

// GetRoleByIDUnscoped looks up a role by id alone, with no merchant filter —
// for internal use once the caller has already established the role belongs
// to the right merchant (e.g. UpdateRoleNameDescription re-reading current
// name/description to apply a partial PATCH). Never expose this to a
// merchant-scoped lookup path; use GetRoleByID for anything reachable from a
// request.
func (r *Repository) GetRoleByIDUnscoped(ctx context.Context, roleID string) (*Role, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at
		FROM roles
		WHERE id = ?
	`, roleID)
	return scanRole(row)
}

// CountRoleHolders returns how many users_rights rows reference roleID,
// regardless of enabled — G5 counts every wearer, not just active sessions,
// since even a disabled row is a real assignment that would dangle once the
// role is archived.
func (r *Repository) CountRoleHolders(ctx context.Context, roleID string) (int, error) {
	db := dbx.GetDB(ctx, r.db)
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users_rights WHERE role_id = ?`, roleID).Scan(&count)
	return count, err
}

// ListRoleHolderTokens returns the session tokens of every ENABLED holder of
// roleID — the set to purge from Redis when the role's permissions change
// (§3). Disabled holders have no live session worth invalidating.
func (r *Repository) ListRoleHolderTokens(ctx context.Context, roleID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT token FROM users_rights WHERE role_id = ? AND enabled = TRUE
	`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// ListRoleMembers returns every users_rights holder of roleID (enabled or
// not — see CountRoleHolders' doc for why), joined to users for display.
func (r *Repository) ListRoleMembers(ctx context.Context, roleID string) ([]RoleMember, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT u.user_id, u.first_name, u.last_name, u.email, ur.enabled
		FROM users_rights ur
		INNER JOIN users u ON u.user_id = ur.user_id
		WHERE ur.role_id = ?
		ORDER BY u.last_name ASC, u.first_name ASC
	`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]RoleMember, 0)
	for rows.Next() {
		var m RoleMember
		if err := rows.Scan(&m.UserID, &m.FirstName, &m.LastName, &m.Email, &m.Enabled); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// CountActiveStaffManageHolders returns how many ENABLED users_rights rows of
// merchantID currently hold a role granting staff.manage — G2's baseline
// count before a hypothetical change.
func (r *Repository) CountActiveStaffManageHolders(ctx context.Context, merchantID string) (int, error) {
	return r.countActiveStaffManageHolders(ctx, merchantID, "")
}

// CountActiveStaffManageHoldersExcludingRole is the same count as
// CountActiveStaffManageHolders but ignoring any holder currently on
// excludeRoleID — used to ask "if this role's grant of staff.manage
// disappeared right now, would anyone still have it?" without needing to
// know how many users hold excludeRoleID.
func (r *Repository) CountActiveStaffManageHoldersExcludingRole(ctx context.Context, merchantID, excludeRoleID string) (int, error) {
	return r.countActiveStaffManageHolders(ctx, merchantID, excludeRoleID)
}

func (r *Repository) countActiveStaffManageHolders(ctx context.Context, merchantID, excludeRoleID string) (int, error) {
	db := dbx.GetDB(ctx, r.db)
	query := `
		SELECT COUNT(DISTINCT ur.id)
		FROM users_rights ur
		INNER JOIN role_permissions rp ON rp.role_id = ur.role_id
		WHERE ur.merchant_id = ? AND ur.enabled = TRUE AND rp.permission_key = ?
	`
	args := []interface{}{merchantID, string(permission.StaffManage)}
	if excludeRoleID != "" {
		query += ` AND ur.role_id <> ?`
		args = append(args, excludeRoleID)
	}
	var count int
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// GetUserRightsRoleID looks up the active (enabled) users_rights row for
// (merchantID, userID), returning its id and current role_id (nil if unset —
// the orphan-row case lot 4's runbook flags). Returns sql.ErrNoRows if no
// enabled link exists — same "no link, no row" convention as
// users.UsersRepository.GetUsersRightsToken.
func (r *Repository) GetUserRightsRoleID(ctx context.Context, merchantID, userID string) (rightsID int, currentRoleID *string, err error) {
	db := dbx.GetDB(ctx, r.db)
	err = db.QueryRowContext(ctx, `
		SELECT id, role_id FROM users_rights WHERE merchant_id = ? AND user_id = ? AND enabled = TRUE
	`, merchantID, userID).Scan(&rightsID, &currentRoleID)
	return rightsID, currentRoleID, err
}

// SetUserRole points a user's active users_rights row at roleID. matched is
// false when no enabled (merchantID, userID) link exists — the caller maps
// that to "not found" rather than silently no-op'ing.
func (r *Repository) SetUserRole(ctx context.Context, merchantID, userID, roleID string) (matched bool, err error) {
	db := dbx.GetDB(ctx, r.db)
	res, err := db.ExecContext(ctx, `
		UPDATE users_rights SET role_id = ? WHERE merchant_id = ? AND user_id = ? AND enabled = TRUE
	`, roleID, merchantID, userID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// GetMerchantDefaultRoleID returns merchant.default_role_id, or "" if it is
// NULL (no error — unlike pos.POSRepository.MerchantDefaultRoleID, an unset
// default is a legitimate state to ask about here, not a failure to seed a
// new users_rights row). Returns sql.ErrNoRows if merchantID matches no
// merchant.
func (r *Repository) GetMerchantDefaultRoleID(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.db)
	var roleID sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT default_role_id FROM merchant WHERE `+merchantJoinCast()+` = ?
	`, merchantID).Scan(&roleID)
	if err != nil {
		return "", err
	}
	return roleID.String, nil
}

// SetMerchantDefaultRoleID unconditionally repoints merchant.default_role_id
// at roleID — this is an explicit admin action (PUT /merchant/default-role),
// unlike pos.POSRepository.SetDefaultRoleID's "only if still NULL" creation-time
// default.
func (r *Repository) SetMerchantDefaultRoleID(ctx context.Context, merchantID, roleID string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, `
		UPDATE merchant SET default_role_id = ? WHERE `+merchantJoinCast()+` = ?
	`, roleID, merchantID)
	return err
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanPermission(row rowScanner) (*Permission, error) {
	p := &Permission{}
	if err := row.Scan(&p.Key, &p.Domain, &p.Label, &p.Description, &p.IsSensitive, &p.SortOrder, &p.DeprecatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

func scanRole(row rowScanner) (*Role, error) {
	role := &Role{}
	if err := row.Scan(&role.ID, &role.MerchantID, &role.Name, &role.Description, &role.SystemKey, &role.Version, &role.CreatedAt, &role.UpdatedAt, &role.ArchivedAt); err != nil {
		return nil, err
	}
	return role, nil
}
