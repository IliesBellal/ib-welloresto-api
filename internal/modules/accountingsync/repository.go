package accountingsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Repository est l'accès SQL du module (Postgres uniquement).
type Repository interface {
	// GetConnection renvoie la connexion d'un établissement (nil si aucune).
	GetConnection(ctx context.Context, merchantID string) (*Connection, error)
	// CreateConnection enregistre une connexion ; ErrAlreadyConnected si
	// l'établissement en a déjà une.
	CreateConnection(ctx context.Context, c Connection) error
	// SaveMapping enregistre la correspondance et l'état de l'outil, et active
	// la connexion (sync_from fixé à la première activation seulement).
	SaveMapping(ctx context.Context, merchantID string, mapping Mapping, state json.RawMessage, syncFrom time.Time) error
	// WithCredentialsLock verrouille la connexion le temps de fn, qui reçoit les
	// identifiants chiffrés actuels et renvoie les nouveaux (nil : inchangés).
	WithCredentialsLock(ctx context.Context, merchantID string, fn func(current []byte) ([]byte, error)) error
	// MarkError passe la connexion en erreur (reconnexion nécessaire).
	MarkError(ctx context.Context, merchantID, reason string) error
	// RecordSync note le passage de la synchronisation (lastError vide = sain).
	RecordSync(ctx context.Context, merchantID, lastError string) error
	// DeleteConnection supprime la connexion (le journal des envois reste).
	DeleteConnection(ctx context.Context, merchantID string) error
	// TryLockSync prend le verrou d'envoi d'un établissement (verrou consultatif
	// de session, partagé entre instances) ; ok=false si un autre envoi est en
	// cours. Deux envois simultanés du même jour créeraient un doublon chez
	// l'outil, qui n'a pas d'idempotence.
	TryLockSync(ctx context.Context, merchantID string) (release func(), ok bool, err error)
	// ListActive renvoie les établissements à synchroniser.
	ListActive(ctx context.Context) ([]Connection, error)

	// MerchantTimezone renvoie le fuseau de l'établissement ("" si inconnu).
	MerchantTimezone(ctx context.Context, merchantID string) (string, error)

	// PendingClosures renvoie, du plus ancien au plus récent, les clôtures
	// journalières à partir de from jamais envoyées ou en échec retentable.
	PendingClosures(ctx context.Context, merchantID, provider string, from time.Time, maxAttempts, limit int) ([]Closure, error)
	// RecordItem enregistre l'issue d'un essai d'envoi.
	RecordItem(ctx context.Context, u ItemUpdate) error
	// ListItems renvoie les derniers envois d'un établissement.
	ListItems(ctx context.Context, merchantID, provider string, limit int) ([]Item, error)
	// ResetItem remet un envoi en échec/incertain/abandonné en file (attempts à 0).
	ResetItem(ctx context.Context, merchantID, provider, date string) error
}

type sqlRepository struct {
	db *sql.DB
}

// NewRepository construit le dépôt SQL du module.
func NewRepository(db *sql.DB) Repository {
	return &sqlRepository{db: db}
}

const connectionColumns = `merchant_id, provider, status, credentials, config, state, COALESCE(account_label, ''),
	sync_from, connected_at, activated_at, last_sync_at, COALESCE(last_error, '')`

type scanner interface{ Scan(dest ...any) error }

func scanConnection(s scanner) (*Connection, error) {
	var c Connection
	var cfg, state []byte
	if err := s.Scan(&c.MerchantID, &c.Provider, &c.Status, &c.Credentials, &cfg, &state, &c.AccountLabel,
		&c.SyncFrom, &c.ConnectedAt, &c.ActivatedAt, &c.LastSyncAt, &c.LastError); err != nil {
		return nil, err
	}
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &c.Config); err != nil {
			return nil, fmt.Errorf("lecture de la correspondance de comptes: %w", err)
		}
	}
	c.State = state
	return &c, nil
}

func (r *sqlRepository) GetConnection(ctx context.Context, merchantID string) (*Connection, error) {
	c, err := scanConnection(r.db.QueryRowContext(ctx,
		`SELECT `+connectionColumns+` FROM accounting_connections WHERE merchant_id = $1`, merchantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

func (r *sqlRepository) CreateConnection(ctx context.Context, c Connection) error {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO accounting_connections (merchant_id, provider, status, credentials, account_label)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (merchant_id) DO NOTHING`,
		c.MerchantID, c.Provider, StatusSetup, c.Credentials, c.AccountLabel)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAlreadyConnected
	}
	return nil
}

func (r *sqlRepository) SaveMapping(ctx context.Context, merchantID string, mapping Mapping, state json.RawMessage, syncFrom time.Time) error {
	cfg, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	if len(state) == 0 {
		state = json.RawMessage(`{}`)
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE accounting_connections
		SET config = $2::jsonb, state = $3::jsonb, status = 'active',
		    sync_from = COALESCE(sync_from, $4::date), activated_at = COALESCE(activated_at, now()),
		    last_error = NULL, updated_at = now()
		WHERE merchant_id = $1`,
		merchantID, string(cfg), string(state), syncFrom.Format("2006-01-02"))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotConnected
	}
	return nil
}

func (r *sqlRepository) WithCredentialsLock(ctx context.Context, merchantID string, fn func(current []byte) ([]byte, error)) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // sans effet après Commit

	var current []byte
	err = tx.QueryRowContext(ctx,
		`SELECT credentials FROM accounting_connections WHERE merchant_id = $1 FOR UPDATE`, merchantID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotConnected
	}
	if err != nil {
		return err
	}
	updated, err := fn(current)
	if err != nil {
		return err
	}
	if updated != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE accounting_connections SET credentials = $2, updated_at = now() WHERE merchant_id = $1`,
			merchantID, updated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *sqlRepository) MarkError(ctx context.Context, merchantID, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE accounting_connections SET status = 'error', last_error = $2, updated_at = now()
		WHERE merchant_id = $1`, merchantID, reason)
	return err
}

func (r *sqlRepository) RecordSync(ctx context.Context, merchantID, lastError string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE accounting_connections SET last_sync_at = now(), last_error = NULLIF($2, ''), updated_at = now()
		WHERE merchant_id = $1`, merchantID, lastError)
	return err
}

func (r *sqlRepository) DeleteConnection(ctx context.Context, merchantID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM accounting_connections WHERE merchant_id = $1`, merchantID)
	return err
}

func (r *sqlRepository) ListActive(ctx context.Context) ([]Connection, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+connectionColumns+` FROM accounting_connections WHERE status = 'active' ORDER BY merchant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *sqlRepository) TryLockSync(ctx context.Context, merchantID string) (func(), bool, error) {
	// Un verrou de session vit sur UNE connexion : on la réserve jusqu'à la libération.
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	var ok bool
	key := "accounting-sync:" + merchantID
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, key).Scan(&ok); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if !ok {
		_ = conn.Close()
		return nil, false, nil
	}
	return func() {
		// Contexte propre : l'appelant a pu être annulé, le verrou doit tomber quand même.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, key)
		_ = conn.Close()
	}, true, nil
}

func (r *sqlRepository) MerchantTimezone(ctx context.Context, merchantID string) (string, error) {
	var tz sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT timezone FROM merchant WHERE id::text = $1`, merchantID).Scan(&tz)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tz.String, err
}

func (r *sqlRepository) PendingClosures(ctx context.Context, merchantID, provider string, from time.Time, maxAttempts, limit int) ([]Closure, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT f.id, to_char(f.period_start, 'YYYY-MM-DD'), f.hash,
		       f.vat_by_rate::text, f.payments_by_mop::text, COALESCE(i.attempts, 0)
		FROM fiscal_closures f
		LEFT JOIN accounting_sync_items i
		       ON i.merchant_id = f.merchant_id AND i.provider = $2 AND i.period_start = f.period_start
		WHERE f.merchant_id = $1 AND f.period_type = 'DAY' AND f.period_start >= $3::date
		  AND (i.id IS NULL OR (i.status = 'failed' AND i.attempts < $4))
		ORDER BY f.period_start
		LIMIT $5`, merchantID, provider, from.Format("2006-01-02"), maxAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Closure
	for rows.Next() {
		var c Closure
		var vat, pay string
		if err := rows.Scan(&c.ID, &c.Date, &c.Hash, &vat, &pay, &c.Attempts); err != nil {
			return nil, err
		}
		c.VATRaw, c.PaymentsRaw = []byte(vat), []byte(pay)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *sqlRepository) RecordItem(ctx context.Context, u ItemUpdate) error {
	var entry any
	if len(u.Entry) > 0 {
		entry = string(u.Entry)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO accounting_sync_items
			(merchant_id, provider, period_start, closure_id, closure_hash, status, attempts,
			 external_id, entry, difference, last_error, sent_at)
		VALUES ($1, $2, $3::date, $4, $5, $6, 1, NULLIF($7, ''), $8::jsonb, $9, NULLIF($10, ''),
		        CASE WHEN $6::text = 'sent' THEN now() END)
		ON CONFLICT (merchant_id, provider, period_start) DO UPDATE SET
			closure_id = EXCLUDED.closure_id, closure_hash = EXCLUDED.closure_hash,
			status = EXCLUDED.status, attempts = accounting_sync_items.attempts + 1,
			external_id = COALESCE(EXCLUDED.external_id, accounting_sync_items.external_id),
			entry = COALESCE(EXCLUDED.entry, accounting_sync_items.entry),
			difference = EXCLUDED.difference, last_error = EXCLUDED.last_error,
			sent_at = COALESCE(EXCLUDED.sent_at, accounting_sync_items.sent_at), updated_at = now()`,
		u.MerchantID, u.Provider, u.Date, u.ClosureID, u.ClosureHash, u.Status,
		u.ExternalID, entry, u.Difference, u.LastError)
	return err
}

func (r *sqlRepository) ListItems(ctx context.Context, merchantID, provider string, limit int) ([]Item, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT to_char(period_start, 'YYYY-MM-DD'), status, attempts, COALESCE(external_id, ''),
		       difference, COALESCE(last_error, ''), sent_at, entry::text
		FROM accounting_sync_items
		WHERE merchant_id = $1 AND provider = $2
		ORDER BY period_start DESC
		LIMIT $3`, merchantID, provider, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var entry sql.NullString
		if err := rows.Scan(&it.Date, &it.Status, &it.Attempts, &it.ExternalID, &it.Difference, &it.LastError, &it.SentAt, &entry); err != nil {
			return nil, err
		}
		if entry.Valid {
			it.Entry = json.RawMessage(entry.String)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *sqlRepository) ResetItem(ctx context.Context, merchantID, provider, date string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE accounting_sync_items SET status = 'failed', attempts = 0, updated_at = now()
		WHERE merchant_id = $1 AND provider = $2 AND period_start = $3::date
		  AND status IN ('failed', 'uncertain', 'abandoned')`, merchantID, provider, date)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists bool
		if err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM accounting_sync_items
			               WHERE merchant_id = $1 AND provider = $2 AND period_start = $3::date)`,
			merchantID, provider, date).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrItemNotFound
		}
		return ErrItemNotRetryable
	}
	return nil
}
