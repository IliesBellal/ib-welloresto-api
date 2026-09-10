//go:build postgres_integration

// External test package: internal/modules/roles imports internal/modules/users
// (GetUsersRightsToken reuse, RBAC lot 6), so a test that needs both — to
// seed real roles via roles.Repository.EnsureSystemRoles rather than
// duplicating that logic — must live outside package users to avoid an
// import cycle.
package users_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	rolesModule "welloresto-api/internal/modules/roles"
	"welloresto-api/internal/modules/users"
)

// LOT A Semaine 1, Chantier 4 (docs/decisions.md) : CreateUserRequest.RoleID,
// when set, overrides merchant.default_role_id (CreateMemberSheet.tsx's new
// role selector) — but only for a role that actually belongs to the target
// merchant, mirroring the same scoping roles.Service.SetUserRole already
// enforces for the "Droits" tab (UsersRepository.RoleBelongsToMerchant).
func TestUsersService_CreateUser_RoleIDOverride_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const userA = "itest-users-roleid-a"
	const userB = "itest-users-roleid-b"
	var merchant1, merchant2 int64

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id IN ($1, $2)`, userA, userB)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id IN ($1, $2)`, userA, userB)
		for _, id := range []int64{merchant1, merchant2} {
			if id != 0 {
				mID := strconv.FormatInt(id, 10)
				_, _ = db.ExecContext(ctx, `UPDATE merchant SET default_role_id = NULL WHERE id = $1`, id)
				_, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE merchant_id = $1`, mID)
				_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, id)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	seedMerchant := func(name, siret string) int64 {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
			VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $2, 'Europe/Paris', 1.0, 2.0)
			RETURNING id`, name, siret).Scan(&id); err != nil {
			t.Fatalf("seed merchant %s: %v", name, err)
		}
		return id
	}
	merchant1 = seedMerchant("ITest RoleID M1", "siret-roleid-m1")
	merchant2 = seedMerchant("ITest RoleID M2", "siret-roleid-m2")

	rolesRepo := rolesModule.NewRepository(db)
	_, m1StaffRoleID, err := rolesRepo.EnsureSystemRoles(ctx, strconv.FormatInt(merchant1, 10))
	if err != nil {
		t.Fatalf("EnsureSystemRoles(m1): %v", err)
	}
	m2AdminRoleID, _, err := rolesRepo.EnsureSystemRoles(ctx, strconv.FormatInt(merchant2, 10))
	if err != nil {
		t.Fatalf("EnsureSystemRoles(m2): %v", err)
	}

	repo := users.NewUserRepository(db)
	svc := users.NewUsersService(repo, nil, nil, nil, nil)

	// A: role_id explicitly set to merchant1's own staff role -> honored,
	// overriding whatever merchant1.default_role_id is.
	m1 := strconv.FormatInt(merchant1, 10)
	_, err = svc.CreateUser(ctx, users.CreateUserRequest{
		FirstName: "ITest", LastName: "RoleIDOverride", Email: "itest-roleid-a@example.com",
		Password: "Sup3r$ecret!", Tel: "+33611119991",
		MerchantID: &m1, RoleID: &m1StaffRoleID,
	})
	if err != nil {
		t.Fatalf("CreateUser (A, same-merchant role_id): %v", err)
	}
	var createdUserAID string
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM users WHERE email = 'itest-roleid-a@example.com'`).Scan(&createdUserAID); err != nil {
		t.Fatalf("look up created user A: %v", err)
	}
	var gotRoleID string
	if err := db.QueryRowContext(ctx, `SELECT role_id FROM users_rights WHERE user_id = $1 AND merchant_id = $2`, createdUserAID, m1).Scan(&gotRoleID); err != nil {
		t.Fatalf("read back role_id for A: %v", err)
	}
	if gotRoleID != m1StaffRoleID {
		t.Fatalf("A's users_rights.role_id = %q, want %q (merchant1's staff role)", gotRoleID, m1StaffRoleID)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE user_id = $1`, createdUserAID) })
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, createdUserAID) })

	// B: role_id set to a role that belongs to a DIFFERENT merchant (m2's
	// admin role) while creating for merchant1 -> rejected, nothing written.
	_, err = svc.CreateUser(ctx, users.CreateUserRequest{
		FirstName: "ITest", LastName: "RoleIDCrossMerchant", Email: "itest-roleid-b@example.com",
		Password: "Sup3r$ecret!", Tel: "+33611119992",
		MerchantID: &m1, RoleID: &m2AdminRoleID,
	})
	if !errors.Is(err, models.ErrRoleNotFound) {
		t.Fatalf("CreateUser (B, cross-merchant role_id): err = %v, want models.ErrRoleNotFound", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE user_id = $1 OR email = 'itest-roleid-b@example.com'`, userB).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the cross-merchant role_id request to be rejected before any write, found %d row(s)", count)
	}
}
