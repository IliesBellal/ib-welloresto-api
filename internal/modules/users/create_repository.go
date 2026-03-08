package users

import (
	"context"
	"database/sql"
)

// CreateUser inserts a new user row inside the provided transaction.
// The caller is responsible for committing or rolling back the transaction.
func (r *UsersRepository) CreateUser(ctx context.Context, tx *sql.Tx, userID, name, firstName, lastName, userName, email, tel, hashedPassword, token string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO users
			(user_id, name, first_name, last_name, userName, email, tel, password, token)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, name, firstName, lastName, userName, email, tel, hashedPassword, token,
	)
	return err
}
