//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"
)

// TestResolveAccessibleMerchants_Postgres is PROMPT 23 Phase 1's mandatory
// coverage — "le test qui compte plus que les autres": a user must never be
// handed an establishment where they don't hold permission.POSAnalytics.
// Covers, in one seeded fixture, every trap the brief names explicitly:
//   - a link the user holds pos.analytics on (role world) -> included
//   - a link on a DIFFERENT establishment where their role does NOT carry
//     pos.analytics -> excluded (proves this is per-link, not per-user)
//   - a disabled link, otherwise identical to an included one -> excluded
//   - a login_enabled=false link -> excluded
//   - the legacy world (role_id NULL): admin=true -> included,
//     admin=false -> excluded (pos.analytics has no legacyPermissionFallback
//     entry, so only Rights.Admin can grant it there)
//   - 4 unrelated user_id='' rows across 4 other establishments, all
//     admin=true with pos.analytics on their role -> NEVER appear, not even
//     when the probing userID is itself "" (the exact failure mode the
//     brief calls "la faille la plus probable de tout ce lot")
func TestResolveAccessibleMerchants_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 6)
	defer m.cleanup()

	const userID = "itest-scope-user"

	// Role world: a role WITH pos.analytics on m[0], a role WITHOUT it on m[1].
	roleWithAnalytics := seedScopeRole(t, ctx, db, m.id(0), "itest-role-with-analytics", []permission.Key{permission.POSAnalytics})
	roleWithoutAnalytics := seedScopeRole(t, ctx, db, m.id(1), "itest-role-without-analytics", []permission.Key{permission.ReportsSalesRead})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(0), token: "itest-scope-tok-0", roleID: &roleWithAnalytics, enabled: true, loginEnabled: true})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(1), token: "itest-scope-tok-1", roleID: &roleWithoutAnalytics, enabled: true, loginEnabled: true})

	// Disabled link, otherwise identical to m[0]'s (would be granted if enabled were ignored).
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(2), token: "itest-scope-tok-2", roleID: &roleWithAnalytics, enabled: false, loginEnabled: true})

	// login_enabled=false link, otherwise identical.
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(3), token: "itest-scope-tok-3", roleID: &roleWithAnalytics, enabled: true, loginEnabled: false})

	// Legacy world: role_id NULL, admin=true -> granted; admin=false -> refused.
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(4), token: "itest-scope-tok-4", roleID: nil, admin: true, enabled: true, loginEnabled: true})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(5), token: "itest-scope-tok-5", roleID: nil, admin: false, enabled: true, loginEnabled: true})

	// 4 unrelated user_id='' rows, admin role with pos.analytics, across 4
	// OTHER establishments never referenced by userID above.
	orphanMerchants := seedScopeMerchants(t, ctx, db, 4)
	defer orphanMerchants.cleanup()
	orphanRole := seedScopeRole(t, ctx, db, orphanMerchants.id(0), "itest-orphan-role", []permission.Key{permission.POSAnalytics})
	for i := 0; i < 4; i++ {
		seedScopeUsersRights(t, ctx, db, scopeLink{userID: "", merchantID: orphanMerchants.id(i), token: "itest-scope-orphan-tok-" + itoa(int64(i)), roleID: &orphanRole, admin: true, enabled: true, loginEnabled: true})
	}

	got, err := repo.ResolveAccessibleMerchants(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: m.id(0)})
	if err != nil {
		t.Fatalf("ResolveAccessibleMerchants: %v", err)
	}
	sort.Strings(got)
	want := []string{m.id(0), m.id(4)}
	sort.Strings(want)
	if !equalStringSlices(got, want) {
		t.Fatalf("expected accessible scope %+v, got %+v", want, got)
	}

	// The exact failure mode PROMPT 23 calls out: probing with an empty
	// userID must NEVER return the 4 orphan establishments, even though 4
	// real users_rights rows with user_id='' and pos.analytics exist.
	gotEmpty, err := repo.ResolveAccessibleMerchants(ctx, &auth.UserLoginRow{UserID: "", MerchantID: ""})
	if err != nil {
		t.Fatalf("ResolveAccessibleMerchants(empty userID): %v", err)
	}
	if len(gotEmpty) != 0 {
		t.Fatalf("expected zero accessible merchants for an empty userID, got %+v — the empty-user_id rows leaked", gotEmpty)
	}
}

// TestResolveAccessibleMerchants_BothRBACWorlds is PROMPT 23 Phase 1's other
// explicit requirement: the role world (role_id set, resolved via
// role_permissions) and the legacy world (role_id NULL, resolved via
// users_rights.admin — pos.analytics has no boolean fallback column) must
// both work, tested separately, through the SAME function.
func TestResolveAccessibleMerchants_BothRBACWorlds(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 1)
	defer m.cleanup()

	t.Run("role world: role carries pos.analytics", func(t *testing.T) {
		const userID = "itest-scope-role-world-user"
		role := seedScopeRole(t, ctx, db, m.id(0), "itest-role-world-role", []permission.Key{permission.POSAnalytics})
		seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(0), token: "itest-scope-role-world-tok", roleID: &role, enabled: true, loginEnabled: true})

		got, err := repo.ResolveAccessibleMerchants(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: m.id(0)})
		if err != nil {
			t.Fatalf("ResolveAccessibleMerchants: %v", err)
		}
		if !equalStringSlices(got, []string{m.id(0)}) {
			t.Fatalf("expected [%s], got %+v", m.id(0), got)
		}
	})

	t.Run("legacy world: role_id NULL, admin=true", func(t *testing.T) {
		const userID = "itest-scope-legacy-admin-user"
		seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(0), token: "itest-scope-legacy-admin-tok", roleID: nil, admin: true, enabled: true, loginEnabled: true})

		got, err := repo.ResolveAccessibleMerchants(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: m.id(0)})
		if err != nil {
			t.Fatalf("ResolveAccessibleMerchants: %v", err)
		}
		if !equalStringSlices(got, []string{m.id(0)}) {
			t.Fatalf("expected [%s], got %+v", m.id(0), got)
		}
	})

	t.Run("legacy world: role_id NULL, admin=false is refused (no fallback for pos.analytics)", func(t *testing.T) {
		const userID = "itest-scope-legacy-nonadmin-user"
		seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: m.id(0), token: "itest-scope-legacy-nonadmin-tok", roleID: nil, admin: false, enabled: true, loginEnabled: true})

		got, err := repo.ResolveAccessibleMerchants(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: m.id(0)})
		if err != nil {
			t.Fatalf("ResolveAccessibleMerchants: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected zero accessible merchants, got %+v", got)
		}
	})
}

// TestHasForMerchant_AgreesWithHasOnTokenMerchant is PROMPT 23 Phase 2's
// mandatory divergence test: HasForMerchant, evaluated on the user's OWN
// token merchant, must return exactly what UserLoginRow.Has returns for the
// same key — proving the two independent implementations of "does this user
// hold this permission" have not drifted apart. Covers both RBAC worlds.
func TestHasForMerchant_AgreesWithHasOnTokenMerchant(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 1)
	defer m.cleanup()
	merchantID := m.id(0)

	t.Run("role world", func(t *testing.T) {
		const userID = "itest-agree-role-user"
		roleID := seedScopeRole(t, ctx, db, merchantID, "itest-agree-role", []permission.Key{permission.ReportsStaffPerformanceRead})
		seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantID, token: "itest-agree-role-tok", roleID: &roleID, enabled: true, loginEnabled: true})

		user := &auth.UserLoginRow{
			UserID:      userID,
			MerchantID:  merchantID,
			RoleID:      &roleID,
			Permissions: []string{string(permission.ReportsStaffPerformanceRead)},
		}

		for _, key := range []permission.Key{permission.ReportsStaffPerformanceRead, permission.CustomersManage, permission.POSAnalytics} {
			want := user.Has(key)
			got, err := repo.HasForMerchant(ctx, userID, merchantID, key)
			if err != nil {
				t.Fatalf("HasForMerchant(%s): %v", key, err)
			}
			if got != want {
				t.Fatalf("key %s: Has()=%v but HasForMerchant()=%v — the two implementations diverged", key, want, got)
			}
		}
	})

	t.Run("legacy world, admin=true", func(t *testing.T) {
		const userID = "itest-agree-legacy-admin-user"
		seedScopeUsersRights(t, ctx, db, scopeLink{userID: userID, merchantID: merchantID, token: "itest-agree-legacy-admin-tok", roleID: nil, admin: true, enabled: true, loginEnabled: true})

		user := &auth.UserLoginRow{UserID: userID, MerchantID: merchantID, Rights: auth.UserRowRights{Admin: true}}

		for _, key := range []permission.Key{permission.ReportsStaffPerformanceRead, permission.CustomersManage, permission.POSAnalytics, permission.ReportsSalesRead} {
			want := user.Has(key)
			got, err := repo.HasForMerchant(ctx, userID, merchantID, key)
			if err != nil {
				t.Fatalf("HasForMerchant(%s): %v", key, err)
			}
			if got != want {
				t.Fatalf("key %s: Has()=%v but HasForMerchant()=%v — the two implementations diverged", key, want, got)
			}
		}
	})

	t.Run("legacy world, admin=false, with and without a fallback entry", func(t *testing.T) {
		const userID = "itest-agree-legacy-nonadmin-user"
		seedScopeUsersRightsFull(t, ctx, db, userID, merchantID, "itest-agree-legacy-nonadmin-tok", nil, false, true, true, scopeBoolRights{viewReports: true, manageCustomers: false})

		user := &auth.UserLoginRow{
			UserID:     userID,
			MerchantID: merchantID,
			Rights:     auth.UserRowRights{Admin: false, CanViewReports: true, CanManageCustomers: false},
		}

		// ReportsSalesRead has a fallback entry keyed on CanViewReports (true here).
		// CustomersManage has a fallback entry keyed on CanManageCustomers (false here).
		// POSAnalytics has NO fallback entry at all — always false without a role.
		for _, key := range []permission.Key{permission.ReportsSalesRead, permission.CustomersManage, permission.POSAnalytics} {
			want := user.Has(key)
			got, err := repo.HasForMerchant(ctx, userID, merchantID, key)
			if err != nil {
				t.Fatalf("HasForMerchant(%s): %v", key, err)
			}
			if got != want {
				t.Fatalf("key %s: Has()=%v but HasForMerchant()=%v — the two implementations diverged", key, want, got)
			}
		}
	})
}

// TestHasForMerchant_InactiveOrMissingLinkIsFalse covers HasForMerchant's own
// edge cases: no link at all, and a link that exists but is disabled or
// login_enabled=false — none of these are errors, all are a plain false.
func TestHasForMerchant_InactiveOrMissingLinkIsFalse(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	m := seedScopeMerchants(t, ctx, db, 1)
	defer m.cleanup()
	merchantID := m.id(0)

	got, err := repo.HasForMerchant(ctx, "itest-no-such-user", merchantID, permission.ReportsStaffPerformanceRead)
	if err != nil {
		t.Fatalf("HasForMerchant (no link): %v", err)
	}
	if got {
		t.Fatalf("expected false for a user with no link to this merchant, got true")
	}

	const disabledUser = "itest-hfm-disabled-user"
	role := seedScopeRole(t, ctx, db, merchantID, "itest-hfm-disabled-role", []permission.Key{permission.ReportsStaffPerformanceRead})
	seedScopeUsersRights(t, ctx, db, scopeLink{userID: disabledUser, merchantID: merchantID, token: "itest-hfm-disabled-tok", roleID: &role, enabled: false, loginEnabled: true})

	got, err = repo.HasForMerchant(ctx, disabledUser, merchantID, permission.ReportsStaffPerformanceRead)
	if err != nil {
		t.Fatalf("HasForMerchant (disabled link): %v", err)
	}
	if got {
		t.Fatalf("expected false for a disabled link that would otherwise grant the key, got true")
	}
}

// ---- shared fixture helpers ----

type scopeMerchants struct {
	db  *sql.DB
	ctx context.Context
	ids []int64
}

func (m scopeMerchants) id(i int) string { return itoa(m.ids[i]) }

func (m scopeMerchants) cleanup() {
	for _, id := range m.ids {
		_, _ = m.db.ExecContext(m.ctx, `DELETE FROM users_rights WHERE merchant_id = $1`, itoa(id))
		_, _ = m.db.ExecContext(m.ctx, `DELETE FROM roles WHERE merchant_id = $1`, itoa(id))
		_, _ = m.db.ExecContext(m.ctx, `DELETE FROM merchant WHERE id = $1`, id)
	}
}

func seedScopeMerchants(t *testing.T, ctx context.Context, db *sql.DB, n int) scopeMerchants {
	t.Helper()
	m := scopeMerchants{db: db, ctx: ctx}
	for i := 0; i < n; i++ {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
			VALUES ('ITest Scope Merchant', 'addr', '1', 'street', '75001', 'Paris', 'sc-'||substr(gen_random_uuid()::text, 1, 8), 'https://example.com', '0600000000', 'mt-'||substr(gen_random_uuid()::text, 1, 8), 'Europe/Paris', 1.0, 2.0)
			RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("seed scope merchant: %v", err)
		}
		m.ids = append(m.ids, id)
	}
	return m
}

// seedScopeRole creates one role on merchantID carrying exactly keys.
func seedScopeRole(t *testing.T, ctx context.Context, db *sql.DB, merchantID, name string, keys []permission.Key) string {
	t.Helper()
	roleID := "role-" + name + "-" + merchantID
	if _, err := db.ExecContext(ctx, `
		INSERT INTO roles (id, merchant_id, name) VALUES ($1, $2, $3)
	`, roleID, merchantID, name); err != nil {
		t.Fatalf("seed role %s: %v", name, err)
	}
	for _, key := range keys {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO role_permissions (role_id, permission_key) VALUES ($1, $2)
		`, roleID, string(key)); err != nil {
			t.Fatalf("grant %s to role %s: %v", key, name, err)
		}
	}
	return roleID
}

type scopeLink struct {
	userID       string
	merchantID   string
	token        string
	roleID       *string
	admin        bool
	enabled      bool
	loginEnabled bool
}

func seedScopeUsersRights(t *testing.T, ctx context.Context, db *sql.DB, l scopeLink) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, role_id, admin, enabled, login_enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, l.userID, l.merchantID, l.token, l.roleID, l.admin, l.enabled, l.loginEnabled); err != nil {
		t.Fatalf("seed users_rights (user=%q merchant=%s): %v", l.userID, l.merchantID, err)
	}
}

type scopeBoolRights struct {
	viewReports     bool
	manageCustomers bool
}

// seedScopeUsersRightsFull seeds a users_rights row with a specific subset of
// the historical boolean columns set, for legacy-world fallback tests.
func seedScopeUsersRightsFull(t *testing.T, ctx context.Context, db *sql.DB, userID, merchantID, token string, roleID *string, admin, enabled, loginEnabled bool, rights scopeBoolRights) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, role_id, admin, enabled, login_enabled, view_reports, manage_customers)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, userID, merchantID, token, roleID, admin, enabled, loginEnabled, rights.viewReports, rights.manageCustomers); err != nil {
		t.Fatalf("seed users_rights full (user=%q merchant=%s): %v", userID, merchantID, err)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
