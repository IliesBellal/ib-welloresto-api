package payouts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Repository est l'accès SQL du module (Postgres uniquement).
type Repository interface {
	// InsertPending enregistre un payout à justifier ; sans effet s'il l'est déjà.
	InsertPending(ctx context.Context, ref Ref) error
	// ListPending renvoie les payouts à traiter, du plus ancien au plus récent.
	ListPending(ctx context.Context, limit int) ([]Pending, error)
	// RecordFailure compte une tentative en échec ; failed=true abandonne le payout.
	RecordFailure(ctx context.Context, payoutID, reason string, failed bool) error
	// MarkDone clôt le payout une fois les justificatifs envoyés.
	MarkDone(ctx context.Context, payoutID string, done Done) error

	// ChannelsByPaymentIntent donne le canal de chaque PaymentIntent connu.
	ChannelsByPaymentIntent(ctx context.Context, paymentIntentIDs []string) (map[string]Channel, error)
	// MerchantByStripeAccount renvoie le restaurateur d'un compte connecté (nil si inconnu).
	MerchantByStripeAccount(ctx context.Context, accountID string) (*Merchant, error)
	// ConnectedAccounts liste les comptes Stripe connectés (rattrapage).
	ConnectedAccounts(ctx context.Context) ([]string, error)

	// ListForMerchant renvoie les payouts dont les justificatifs ont été envoyés à un établissement.
	ListForMerchant(ctx context.Context, merchantID string, limit int) ([]Listed, error)
	// GetForMerchant renvoie un de ces payouts (nil s'il n'existe pas ou appartient à un autre établissement).
	GetForMerchant(ctx context.Context, merchantID, payoutID string) (*Listed, error)

	// InvoiceForPayout renvoie la facture déjà émise pour un payout (nil sinon).
	InvoiceForPayout(ctx context.Context, payoutID string) (*Invoice, error)
	// CreateInvoice émet la facture d'un payout : numéro attribué et facture
	// insérée dans la même transaction. Si le payout a déjà une facture
	// (émission concurrente), renvoie celle-ci.
	CreateInvoice(ctx context.Context, in NewInvoice) (*Invoice, error)
}

// Done est ce que MarkDone enregistre.
type Done struct {
	MerchantID   string
	Recipient    string
	StatementKey string
	InvoiceKey   string
}

// NewInvoice est une facture à émettre.
type NewInvoice struct {
	Series      string
	MerchantID  string
	PayoutID    string
	AmountTTC   int64
	PeriodStart time.Time
	PeriodEnd   time.Time
	IssuedAt    time.Time
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
		INSERT INTO payout_documents (payout_id, stripe_account_id, amount, currency, arrival_date)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (payout_id) DO NOTHING`,
		ref.PayoutID, ref.AccountID, ref.Amount, ref.Currency, ref.ArrivalDate)
	return err
}

func (r *sqlRepository) ListPending(ctx context.Context, limit int) ([]Pending, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT payout_id, stripe_account_id, amount, currency, arrival_date, attempts
		FROM payout_documents
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
		if err := rows.Scan(&p.PayoutID, &p.AccountID, &p.Amount, &p.Currency, &p.ArrivalDate, &p.Attempts); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *sqlRepository) RecordFailure(ctx context.Context, payoutID, reason string, failed bool) error {
	status := "pending"
	if failed {
		status = "failed"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE payout_documents
		SET attempts = attempts + 1, last_error = $2, status = $3
		WHERE payout_id = $1`, payoutID, reason, status)
	return err
}

func (r *sqlRepository) MarkDone(ctx context.Context, payoutID string, done Done) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE payout_documents
		SET status = 'done', last_error = NULL, sent_at = now(),
		    merchant_id = $2, recipient = $3, statement_key = $4, invoice_key = $5
		WHERE payout_id = $1`,
		payoutID, done.MerchantID, done.Recipient, nullIfEmpty(done.StatementKey), nullIfEmpty(done.InvoiceKey))
	return err
}

func (r *sqlRepository) ChannelsByPaymentIntent(ctx context.Context, paymentIntentIDs []string) (map[string]Channel, error) {
	out := make(map[string]Channel, len(paymentIntentIDs))
	if len(paymentIntentIDs) == 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT sp.payment_intent_id, COALESCE(o.order_source, ''), (o.kiosk_id IS NOT NULL OR sp.kiosk_id IS NOT NULL)
		FROM stripe_payments sp
		JOIN orders o ON o.order_id = sp.order_id
		WHERE sp.payment_intent_id = ANY($1)`, paymentIntentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var paymentIntentID, source string
		var hasKiosk bool
		if err := rows.Scan(&paymentIntentID, &source, &hasKiosk); err != nil {
			return nil, err
		}
		out[paymentIntentID] = channelOf(source, hasKiosk)
	}
	return out, rows.Err()
}

// channelOf déduit le canal d'une commande de sa source et de la présence d'une borne.
func channelOf(orderSource string, hasKiosk bool) Channel {
	switch {
	case orderSource == "KIOSK" || hasKiosk:
		return ChannelKiosk
	case orderSource == "SCANNORDER":
		return ChannelScanNOrder
	default:
		return ChannelOther
	}
}

func (r *sqlRepository) MerchantByStripeAccount(ctx context.Context, accountID string) (*Merchant, error) {
	var m Merchant
	err := r.db.QueryRowContext(ctx, `
		SELECT m.id::text, COALESCE(m.fullname, ''), COALESCE(m.address, ''),
		       COALESCE(m.siret, ''), COALESCE(m.vat_number, ''), COALESCE(m.email, '')
		FROM stripe_accounts sa
		JOIN merchant m ON m.id::text = sa.merchant_id
		WHERE sa.account_id = $1
		LIMIT 1`, accountID).Scan(&m.ID, &m.Name, &m.Address, &m.SIRET, &m.VATNumber, &m.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *sqlRepository) ConnectedAccounts(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT account_id FROM stripe_accounts
		WHERE account_id IS NOT NULL AND account_id <> ''
		ORDER BY account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

const listedSelect = `
	SELECT d.payout_id, d.amount, d.currency, d.arrival_date, d.sent_at,
	       COALESCE(i.number, ''), COALESCE(i.amount_ttc, 0),
	       COALESCE(d.statement_key, ''), COALESCE(d.invoice_key, '')
	FROM payout_documents d
	LEFT JOIN commission_invoices i ON i.payout_id = d.payout_id
	WHERE d.merchant_id = $1 AND d.status = 'done'`

func scanListed(row interface{ Scan(...any) error }) (*Listed, error) {
	var l Listed
	if err := row.Scan(&l.PayoutID, &l.Amount, &l.Currency, &l.ArrivalDate, &l.SentAt,
		&l.InvoiceNumber, &l.CommissionTTC, &l.StatementKey, &l.InvoiceKey); err != nil {
		return nil, err
	}
	l.HasStatement = l.StatementKey != ""
	l.HasInvoice = l.InvoiceKey != ""
	return &l, nil
}

func (r *sqlRepository) ListForMerchant(ctx context.Context, merchantID string, limit int) ([]Listed, error) {
	rows, err := r.db.QueryContext(ctx, listedSelect+` ORDER BY d.arrival_date DESC, d.id DESC LIMIT $2`, merchantID, limit)
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

func (r *sqlRepository) GetForMerchant(ctx context.Context, merchantID, payoutID string) (*Listed, error) {
	l, err := scanListed(r.db.QueryRowContext(ctx, listedSelect+` AND d.payout_id = $2`, merchantID, payoutID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

const invoiceColumns = `id, number, series, merchant_id, payout_id, amount_ttc, amount_ht, amount_vat, period_start, period_end, issued_at`

func scanInvoice(row interface{ Scan(...any) error }) (*Invoice, error) {
	var inv Invoice
	err := row.Scan(&inv.ID, &inv.Number, &inv.Series, &inv.MerchantID, &inv.PayoutID,
		&inv.AmountTTC, &inv.AmountHT, &inv.AmountVAT, &inv.PeriodStart, &inv.PeriodEnd, &inv.IssuedAt)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *sqlRepository) InvoiceForPayout(ctx context.Context, payoutID string) (*Invoice, error) {
	inv, err := scanInvoice(r.db.QueryRowContext(ctx,
		`SELECT `+invoiceColumns+` FROM commission_invoices WHERE payout_id = $1`, payoutID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return inv, err
}

func (r *sqlRepository) CreateInvoice(ctx context.Context, in NewInvoice) (*Invoice, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	year := in.IssuedAt.In(parisLocation).Year()

	// Le compteur est incrémenté dans la transaction de l'insertion : en cas
	// d'échec, tout est annulé et aucun numéro n'est perdu.
	var last int
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoice_counters (series, year, last_number) VALUES ($1, $2, 1)
		ON CONFLICT (series, year) DO UPDATE SET last_number = invoice_counters.last_number + 1
		RETURNING last_number`, in.Series, year).Scan(&last); err != nil {
		return nil, fmt.Errorf("compteur de factures: %w", err)
	}

	number := fmt.Sprintf("%s-%d-%06d", in.Series, year, last)
	ht, vat := splitVAT(in.AmountTTC)

	inv, err := scanInvoice(tx.QueryRowContext(ctx, `
		INSERT INTO commission_invoices
			(number, series, merchant_id, payout_id, amount_ttc, amount_ht, amount_vat, period_start, period_end, issued_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+invoiceColumns,
		number, in.Series, in.MerchantID, in.PayoutID, in.AmountTTC, ht, vat, in.PeriodStart, in.PeriodEnd, in.IssuedAt))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Facture déjà émise pour ce payout (émission concurrente) : on la reprend.
			_ = tx.Rollback()
			return r.InvoiceForPayout(ctx, in.PayoutID)
		}
		return nil, fmt.Errorf("insertion de la facture: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return inv, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
