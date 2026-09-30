package googleauth

import (
	"context"
	"database/sql"

	"welloresto-api/internal/database/dbx"
)

type ExistingUser struct {
	UserID      string
	HasPassword bool
}

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// FindTokenByGoogleSub returns the users_rights.token for the user whose
// google_sub matches, or "" if no such user exists. Same arbitrary-first-match
// convention as auth.AuthRepository.Login when a user holds several
// users_rights links (no ORDER BY there either) — not something this
// chantier changes.
func (r *Repository) FindTokenByGoogleSub(ctx context.Context, googleSub string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var token string
	err := db.QueryRowContext(ctx, `
		SELECT ur.token
		FROM users u
		INNER JOIN users_rights ur ON ur.user_id = u.user_id
		WHERE u.google_sub = ?
		LIMIT 1
	`, googleSub).Scan(&token)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return token, err
}

// FindUserByEmail returns the user matching email (case-insensitive, same
// comparison as uq_users_email_lower), or (nil, nil) if none exists.
// HasPassword distinguishes the two "address known" branches of §5.2.3:
// users.password is NOT NULL in this schema, so "no password" is the empty
// string, not NULL — the sentinel a chantier-7c Google signup writes for an
// account that has never had a password.
func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*ExistingUser, error) {
	db := dbx.GetDB(ctx, r.database)

	var u ExistingUser
	var password string
	err := db.QueryRowContext(ctx, `
		SELECT user_id, password FROM users WHERE lower(email) = lower(?)
	`, email).Scan(&u.UserID, &password)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.HasPassword = password != ""
	return &u, nil
}

// LinkGoogleSub attaches googleSub to an existing user — the automatic
// rattachement branch of §5.2.3 (address known, no password). uq_users_google_sub
// (migration 130) is the safety net for the concurrent case: two
// simultaneous Google sign-ins somehow resolving to the same sub racing to
// link two different users would violate it, surfacing as a plain SQL
// error — unreachable in practice (sub is Google's own stable per-account
// identifier), but the constraint exists precisely so this can never
// silently double-link.
func (r *Repository) LinkGoogleSub(ctx context.Context, userID, googleSub string) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `UPDATE users SET google_sub = ? WHERE user_id = ?`, googleSub, userID)
	return err
}

// LinkState is a user's Google rattachement as seen from the account
// settings: whether a google_sub is attached, and whether the account also
// has a password (the condition for being allowed to detach it).
type LinkState struct {
	Linked      bool
	HasPassword bool
}

// GetLinkState returns userID's LinkState, or (nil, nil) if no such user.
func (r *Repository) GetLinkState(ctx context.Context, userID string) (*LinkState, error) {
	db := dbx.GetDB(ctx, r.database)

	var s LinkState
	err := db.QueryRowContext(ctx, `
		SELECT google_sub IS NOT NULL, password <> '' FROM users WHERE user_id = ?
	`, userID).Scan(&s.Linked, &s.HasPassword)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FindUserIDByGoogleSub returns the user_id holding googleSub, or "" if none.
// Unlike FindTokenByGoogleSub, does not require a users_rights row.
func (r *Repository) FindUserIDByGoogleSub(ctx context.Context, googleSub string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var userID string
	err := db.QueryRowContext(ctx, `SELECT user_id FROM users WHERE google_sub = ?`, googleSub).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return userID, err
}

// LinkGoogleSubIfUnlinked attaches googleSub to userID from the account
// settings. The WHERE guard (google_sub IS NULL) makes "already linked to a
// different Google account" atomic with the write: matched=false then means
// the caller must unlink first. An account with a password becomes 'both' —
// it now genuinely has two working login methods, same rule as
// auth.AuthRepository.SetPasswordForGoogleAccount.
func (r *Repository) LinkGoogleSubIfUnlinked(ctx context.Context, userID, googleSub string) (matched bool, err error) {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE users
		SET google_sub = ?,
		    auth_provider = CASE WHEN password <> '' THEN 'both' ELSE auth_provider END
		WHERE user_id = ? AND google_sub IS NULL
	`, googleSub, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UnlinkGoogleSub detaches userID's Google identity. Guarded on
// password <> '' in the same statement: an account without a password must
// keep Google, or it would be left with no way to log in. matched=false
// covers both "not linked" and "no password" — the caller disambiguates
// with GetLinkState.
func (r *Repository) UnlinkGoogleSub(ctx context.Context, userID string) (matched bool, err error) {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE users
		SET google_sub = NULL, auth_provider = 'password'
		WHERE user_id = ? AND google_sub IS NOT NULL AND password <> ''
	`, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
