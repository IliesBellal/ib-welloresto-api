package outbound

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
)

const outboundMessageIDPrefix = "outbound-msg"

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Insert(ctx context.Context, params CreateParams) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO outbound_messages (
			id, channel, provider, provider_message_id, domain, domain_ref_id, recipient, status, sent_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, %s, %s)
	`, dbx.UTCNow(), dbx.UTCNow()),
		helpers.GeneratePrefixedID(outboundMessageIDPrefix),
		strings.TrimSpace(params.Channel),
		strings.TrimSpace(params.Provider),
		strings.TrimSpace(params.ProviderMessageID),
		strings.TrimSpace(params.Domain),
		strings.TrimSpace(params.DomainRefID),
		strings.TrimSpace(params.Recipient),
		NormalizeStatus(params.Status),
	)
	return err
}

func (r *Repository) FindStatusByProviderMessageID(ctx context.Context, providerMessageID string) (string, bool, error) {
	db := dbx.GetDB(ctx, r.db)
	var status string
	err := db.QueryRowContext(ctx, `
		SELECT status
		FROM outbound_messages
		WHERE provider_message_id = ?
		LIMIT 1
	`, strings.TrimSpace(providerMessageID)).Scan(&status)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return NormalizeStatus(status), true, nil
}

func (r *Repository) UpdateStatusByProviderMessageID(ctx context.Context, providerMessageID, status string) error {
	db := dbx.GetDB(ctx, r.db)
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE outbound_messages
		SET status = ?, updated_at = %s
		WHERE provider_message_id = ?
	`, dbx.UTCNow()), NormalizeStatus(status), strings.TrimSpace(providerMessageID))
	return err
}
