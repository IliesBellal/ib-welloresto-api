package users

import (
	"context"
	"database/sql"
)

// CreateUser inserts a new user row inside the provided transaction.
// The caller is responsible for committing or rolling back the transaction.
func (r *UsersRepository) CreateUser(ctx context.Context, tx *sql.Tx, req CreateUserRequest, hashedPassword, token string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO users
			(user_id, first_name, last_name, email, tel, password, token)
		VALUES
			(?, ?, ?, ?, ?, ?, ?)`,
		req.UserID, req.FirstName, req.LastName, req.Email, req.Tel, hashedPassword, token,
	)
	return err
}
