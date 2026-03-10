package users

import (
	"context"
	"database/sql"
)

// CreateUser inserts a new user row inside the provided transaction.
// The caller is responsible for committing or rolling back the transaction.
func (r *UsersRepository) CreateUser(ctx context.Context, tx *sql.Tx, userID, fullName, firstName, lastName, userName, email, tel, hashedPassword, token string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO users
			(user_id, name, first_name, last_name, userName, email, tel, password, token)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, fullName, firstName, lastName, userName, email, tel, hashedPassword, token,
	)
	return err
}

// InsertUserRights creates a row in users_rights to link a user to a merchant.
// Returns the generated rights ID.
func (r *UsersRepository) InsertUserRights(ctx context.Context, tx *sql.Tx, userID, merchantID string, admin bool, rightsToken string) (int, error) {
	adminVal := 0
	if admin {
		adminVal = 1
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO users_rights
			(user_id, merchant_id, token, admin, enabled)
		VALUES (?, ?, ?, ?, 1)`,
		userID, merchantID, rightsToken, adminVal,
	)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	return int(id), err
}
