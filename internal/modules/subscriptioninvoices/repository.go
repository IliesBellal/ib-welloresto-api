package subscriptioninvoices

import (
	"context"
	"database/sql"
	"errors"
)

// Repository est l'accès SQL du module (Postgres uniquement).
type Repository interface {
	// InsertPending enregistre une facture à envoyer ; sans effet si elle l'est déjà.
	InsertPending(ctx context.Context, ref Ref) error
	// ListPending renvoie les factures à traiter, de la plus ancienne à la plus récente.
	ListPending(ctx context.Context, limit int) ([]Pending, error)
	// RecordFailure compte une tentative en échec ; failed=true abandonne la facture.
	RecordFailure(ctx context.Context, invoiceID, reason string, failed bool) error
	// MarkDone clôt la facture une fois envoyée.
	MarkDone(ctx context.Context, invoiceID string, done Done) error

	// Merchant renvoie le restaurateur d'un établissement (nil si inconnu).
	Merchant(ctx context.Context, merchantID string) (*Merchant, error)
	// MerchantBySubscription renvoie l'établissement d'un abonnement Stripe ("" si inconnu).
	MerchantBySubscription(ctx context.Context, subscriptionID string) (string, error)

	// ListForMerchant renvoie les factures envoyées à un établissement.
	ListForMerchant(ctx context.Context, merchantID string, limit int) ([]Listed, error)
	// GetForMerchant renvoie une de ces factures (nil si inconnue ou d'un autre établissement).
	GetForMerchant(ctx context.Context, merchantID, invoiceID string) (*Listed, error)
}

// Done est ce que MarkDone enregistre.
type Done struct {
	Number    string
	Recipient string
	PDFKey    string
}

type sqlRepository struct {
	db *sql.DB
}

// NewRepository construit le dépôt SQL du module.
func NewRepository(db *sql.DB) Repository {
	return &sqlRepository{db: db}
}

func (r *sqlRepository) InsertPending(ctx context.Context, ref Ref) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO subscription_invoice_documents
			(stripe_invoice_id, merchant_id, number, amount_paid, currency, period_start, period_end, paid_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (stripe_invoice_id) DO NOTHING`,
		ref.InvoiceID, ref.MerchantID, ref.Number, ref.AmountPaid, ref.Currency, ref.PeriodStart, ref.PeriodEnd, ref.PaidAt)
	return err
}

func (r *sqlRepository) ListPending(ctx context.Context, limit int) ([]Pending, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT stripe_invoice_id, merchant_id, COALESCE(number, ''), amount_paid, currency,
		       period_start, period_end, paid_at, attempts
		FROM subscription_invoice_documents
		WHERE status = 'pending'
		ORDER BY id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Pending
	for rows.Next() {
		var p Pending
		if err := rows.Scan(&p.InvoiceID, &p.MerchantID, &p.Number, &p.AmountPaid, &p.Currency,
			&p.PeriodStart, &p.PeriodEnd, &p.PaidAt, &p.Attempts); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *sqlRepository) RecordFailure(ctx context.Context, invoiceID, reason string, failed bool) error {
	status := "pending"
	if failed {
		status = "failed"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE subscription_invoice_documents
		SET attempts = attempts + 1, last_error = $2, status = $3
		WHERE stripe_invoice_id = $1`, invoiceID, reason, status)
	return err
}

func (r *sqlRepository) MarkDone(ctx context.Context, invoiceID string, done Done) error {
	var pdfKey any
	if done.PDFKey != "" {
		pdfKey = done.PDFKey
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE subscription_invoice_documents
		SET status = 'done', last_error = NULL, sent_at = now(),
		    number = $2, recipient = $3, pdf_key = $4
		WHERE stripe_invoice_id = $1`, invoiceID, done.Number, done.Recipient, pdfKey)
	return err
}

func (r *sqlRepository) Merchant(ctx context.Context, merchantID string) (*Merchant, error) {
	var m Merchant
	// Adresse du responsable : le premier admin actif, comme
	// subscriptions.GetMerchantOwnerContact.
	err := r.db.QueryRowContext(ctx, `
		SELECT m.id::text, COALESCE(m.fullname, ''), COALESCE(m.address, ''), COALESCE(m.email, ''),
		       COALESCE((
		           SELECT u.email
		           FROM users_rights ur
		           JOIN users u ON u.user_id = ur.user_id
		           WHERE ur.merchant_id = m.id::text AND ur.admin = TRUE AND ur.enabled = TRUE
		           ORDER BY u.created_at ASC
		           LIMIT 1), '')
		FROM merchant m WHERE m.id::text = $1 LIMIT 1`, merchantID).Scan(&m.ID, &m.Name, &m.Address, &m.Email, &m.OwnerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *sqlRepository) MerchantBySubscription(ctx context.Context, subscriptionID string) (string, error) {
	var merchantID string
	err := r.db.QueryRowContext(ctx,
		`SELECT merchant_id FROM subscriptions WHERE stripe_subscription_id = $1 LIMIT 1`, subscriptionID).Scan(&merchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return merchantID, err
}

const listedSelect = `
	SELECT stripe_invoice_id, COALESCE(number, ''), amount_paid, currency,
	       period_start, period_end, paid_at, sent_at, COALESCE(pdf_key, '')
	FROM subscription_invoice_documents
	WHERE merchant_id = $1 AND status = 'done'`

func scanListed(row interface{ Scan(...any) error }) (*Listed, error) {
	var l Listed
	if err := row.Scan(&l.InvoiceID, &l.Number, &l.AmountPaid, &l.Currency,
		&l.PeriodStart, &l.PeriodEnd, &l.PaidAt, &l.SentAt, &l.PDFKey); err != nil {
		return nil, err
	}
	l.HasPDF = l.PDFKey != ""
	return &l, nil
}

func (r *sqlRepository) ListForMerchant(ctx context.Context, merchantID string, limit int) ([]Listed, error) {
	rows, err := r.db.QueryContext(ctx, listedSelect+` ORDER BY paid_at DESC, id DESC LIMIT $2`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Listed{}
	for rows.Next() {
		l, err := scanListed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r *sqlRepository) GetForMerchant(ctx context.Context, merchantID, invoiceID string) (*Listed, error) {
	l, err := scanListed(r.db.QueryRowContext(ctx, listedSelect+` AND stripe_invoice_id = $2`, merchantID, invoiceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}
