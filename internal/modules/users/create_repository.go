package users

import (
	"context"
	"database/sql"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

// CreateUser inserts a new user row inside the provided transaction.
// The caller is responsible for committing or rolling back the transaction.
func (r *UsersRepository) CreateUser(ctx context.Context, userID, fullName, firstName, lastName, email, tel, hashedPassword, token string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
		INSERT INTO users
			(user_id, name, first_name, last_name, email, tel, password, token)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, fullName, firstName, lastName, email, tel, hashedPassword, token,
	)
	return err
}

// InsertUserRights creates a row in users_rights to link a user to a merchant.
// Returns the generated rights ID.
//
// role_id comes from merchant.default_role_id (RBAC lot 4), never
// hardcoded — fails explicitly (models.ErrMerchantDefaultRoleNotSet) rather
// than inserting a row with no role_id if the establishment has none
// configured yet. See migrations/done/099_merchant_default_role_admin.up.sql.
func (r *UsersRepository) InsertUserRights(ctx context.Context, userID, merchantID string, admin bool, rightsToken string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	roleID, err := r.MerchantDefaultRoleID(ctx, merchantID)
	if err != nil {
		return 0, err
	}

	id, err := db.InsertReturningID(ctx, `
		INSERT INTO users_rights
			(user_id, merchant_id, token, admin, role_id, enabled, login_enabled)
		VALUES (?, ?, ?, ?, ?, TRUE, TRUE)`, "id",
		userID, merchantID, rightsToken, admin, roleID,
	)
	return int(id), err
}

// MerchantDefaultRoleID returns merchant.default_role_id for merchantID, or
// models.ErrMerchantDefaultRoleNotSet if the establishment has no default
// role configured yet. Returns sql.ErrNoRows if merchantID does not match any
// merchant. Same pattern as pos.POSRepository.MerchantDefaultRoleID.
func (r *UsersRepository) MerchantDefaultRoleID(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	castExpr := "CAST(id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		castExpr = "CAST(id AS TEXT)"
	}

	var roleID sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT default_role_id FROM merchant WHERE "+castExpr+" = ?", merchantID,
	).Scan(&roleID); err != nil {
		return "", err
	}
	if !roleID.Valid {
		return "", models.ErrMerchantDefaultRoleNotSet
	}
	return roleID.String, nil
}
