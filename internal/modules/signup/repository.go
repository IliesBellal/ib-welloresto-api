package signup

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"welloresto-api/internal/database/dbx"
)

const (
	StatePending   = "pending"
	StateCompleted = "completed"
	StateFailed    = "failed"
)

// CachedResponse is what signup_sessions.payload holds once a session
// leaves StatePending — a self-contained HTTP response, replayed verbatim
// for a repeated call with the same Idempotency-Key. Standard idempotency-key
// semantics: the original outcome is cached and replayed whether it was a
// success or a business-rule rejection — a client that reuses a key after a
// failed attempt gets the same failure back, never a silent reprocessing
// with different input.
type CachedResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

type Session struct {
	ID           string
	Email        sql.NullString
	Provider     string
	State        string
	ContextToken sql.NullString
	Payload      []byte
	ExpiresAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// TryBeginSession attempts to claim id (the Idempotency-Key header value) as
// a new pending session. Returns created=true if this call won — the caller
// should proceed with processing and finish with CompleteSession/FailSession.
// created=false means a row already existed under this id; the caller
// should fetch it via GetSession to decide whether to replay a terminal
// response or reject a still-pending concurrent duplicate.
func (r *Repository) TryBeginSession(ctx context.Context, id, email, provider, contextToken string, requestPayload []byte, expiresAt time.Time) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx, `
		INSERT INTO signup_sessions (id, email, provider, state, context_token, payload, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING
	`, id, email, provider, StatePending, nullIfEmpty(contextToken), requestPayload, expiresAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetSession returns the session for id, or (nil, nil) if none exists.
func (r *Repository) GetSession(ctx context.Context, id string) (*Session, error) {
	db := dbx.GetDB(ctx, r.database)

	row := db.QueryRowContext(ctx, `
		SELECT id, email, provider, state, context_token, payload, expires_at, created_at, updated_at
		FROM signup_sessions
		WHERE id = ?
	`, id)

	s := &Session{}
	if err := row.Scan(&s.ID, &s.Email, &s.Provider, &s.State, &s.ContextToken, &s.Payload, &s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

// finishSession is the shared implementation of CompleteSession/FailSession.
func (r *Repository) finishSession(ctx context.Context, id, state string, status int, body []byte) error {
	db := dbx.GetDB(ctx, r.database)

	cached, err := json.Marshal(CachedResponse{Status: status, Body: body})
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `
		UPDATE signup_sessions SET state = ?, payload = ?, updated_at = `+dbx.UTCNow()+`
		WHERE id = ?
	`, state, cached, id)
	return err
}

// CompleteSession caches a successful response for replay.
func (r *Repository) CompleteSession(ctx context.Context, id string, status int, body []byte) error {
	return r.finishSession(ctx, id, StateCompleted, status, body)
}

// FailSession caches a rejected response for replay — same idempotency-key
// semantics as CompleteSession (see CachedResponse's doc comment).
func (r *Repository) FailSession(ctx context.Context, id string, status int, body []byte) error {
	return r.finishSession(ctx, id, StateFailed, status, body)
}

func nullIfEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
