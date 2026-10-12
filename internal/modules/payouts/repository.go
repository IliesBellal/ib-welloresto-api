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
	// MarkDone clôt le payout une fois le relevé envoyé, en gardant ses totaux.
	MarkDone(ctx context.Context, payoutID string, done Done) error

	// ChannelsByPaymentIntent donne le canal de chaque PaymentIntent connu.
	ChannelsByPaymentIntent(ctx context.Context, paymentIntentIDs []string) (map[string]Channel, error)
	// MerchantByStripeAccount renvoie le restaurateur d'un compte connecté (nil si inconnu).
	MerchantByStripeAccount(ctx context.Context, accountID string) (*Merchant, error)
	// MerchantByID renvoie un restaurateur (nil si inconnu).
	MerchantByID(ctx context.Context, merchantID string) (*Merchant, error)
	// ConnectedAccounts liste les comptes Stripe connectés (rattrapage).
	ConnectedAccounts(ctx context.Context) ([]string, error)

	// ListForMerchant renvoie les virements dont le relevé a été envoyé à un établissement.
	ListForMerchant(ctx context.Context, merchantID string, limit int) ([]Listed, error)
	// GetForMerchant renvoie un de ces virements (nil s'il n'existe pas ou appartient à un autre établissement).
	GetForMerchant(ctx context.Context, merchantID, payoutID string) (*Listed, error)

	// InvoiceCandidates renvoie les établissements à facturer : virements traités
	// et non facturés arrivés avant `before`, aucun virement du même établissement
	// encore en cours de traitement avant `before`, et pas encore de facture pour
	// periodMonth.
	InvoiceCandidates(ctx context.Context, before, periodMonth time.Time) ([]string, error)
	// PayoutsToInvoice renvoie les virements à facturer d'un établissement.
	PayoutsToInvoice(ctx context.Context, merchantID string, before time.Time) ([]Line, error)
	// SkipInvoicing sort des virements de la facturation (commission nulle ou négative).
	SkipInvoicing(ctx context.Context, payoutIDs []string) error
	// IssueInvoice émet la facture mensuelle : numéro attribué, facture insérée et
	// virements rattachés dans une seule transaction. Renvoie nil, nil si
	// l'établissement a déjà une facture pour ce mois.
	IssueInvoice(ctx context.Context, in NewInvoice) (*Invoice, error)
	// InvoiceLines renvoie les virements d'une facture.
	InvoiceLines(ctx context.Context, invoiceID int64) ([]Line, error)
	// UnsentInvoices renvoie les factures émises mais pas encore envoyées.
	UnsentInvoices(ctx context.Context, limit int) ([]Invoice, error)
	// RecordInvoiceFailure compte un envoi en échec ; failed=true abandonne l'envoi.
	RecordInvoiceFailure(ctx context.Context, invoiceID int64, reason string, failed bool) error
	// MarkInvoiceSent clôt une facture une fois son mail parti.
	MarkInvoiceSent(ctx context.Context, invoiceID int64, sent InvoiceSent) error
	// ListInvoicesForMerchant renvoie les factures envoyées à un établissement.
	ListInvoicesForMerchant(ctx context.Context, merchantID string, limit int) ([]Invoice, error)
	// GetInvoiceForMerchant renvoie une de ces factures (nil si inconnue ou d'un autre établissement).
	GetInvoiceForMerchant(ctx context.Context, merchantID, number string) (*Invoice, error)
}

// Done est ce que MarkDone enregistre.
type Done struct {
	MerchantID   string
	Recipient    string
	StatementKey string
	Totals       Line
}

// NewInvoice est une facture mensuelle à émettre.
type NewInvoice struct {
	Series      string
	MerchantID  string
	PeriodMonth time.Time
	PayoutIDs   []string
	AmountTTC   int64
	PeriodStart time.Time
	PeriodEnd   time.Time
	IssuedAt    time.Time
}

// InvoiceSent est ce que MarkInvoiceSent enregistre.
type InvoiceSent struct {
	Recipient  string
	InvoiceKey string
	RecapKey   string
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
	t := done.Totals
	_, err := r.db.ExecContext(ctx, `
		UPDATE payout_documents
		SET status = 'done', last_error = NULL, sent_at = now(),
		    merchant_id = $2, recipient = $3, statement_key = $4,
		    scannorder_amount = $5, kiosk_amount = $6, other_amount = $7,
		    refunds_amount = $8, adjustments_amount = $9, commission_ttc = $10,
		    stripe_fees = $11, discrepancy = $12
		WHERE payout_id = $1`,
		payoutID, nullIfEmpty(done.MerchantID), done.Recipient, nullIfEmpty(done.StatementKey),
		t.ScanNOrder, t.Kiosk, t.Other, t.Refunds, t.Adjustments, t.Commission, t.StripeFees, t.Discrepancy)
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

const merchantSelect = `
	SELECT m.id::text, COALESCE(m.fullname, ''), COALESCE(m.address, ''),
	       COALESCE(m.siret, ''), COALESCE(m.vat_number, ''), COALESCE(m.email, '')
	FROM merchant m`

func (r *sqlRepository) MerchantByStripeAccount(ctx context.Context, accountID string) (*Merchant, error) {
	var m Merchant
	err := r.db.QueryRowContext(ctx, merchantSelect+`
		JOIN stripe_accounts sa ON sa.merchant_id = m.id::text
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

func (r *sqlRepository) MerchantByID(ctx context.Context, merchantID string) (*Merchant, error) {
	var m Merchant
	err := r.db.QueryRowContext(ctx, merchantSelect+` WHERE m.id::text = $1 LIMIT 1`, merchantID).
		Scan(&m.ID, &m.Name, &m.Address, &m.SIRET, &m.VATNumber, &m.Email)
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
	SELECT payout_id, amount, currency, arrival_date, sent_at, commission_ttc, COALESCE(statement_key, '')
	FROM payout_documents
	WHERE merchant_id = $1 AND status = 'done'`

func scanListed(row interface{ Scan(...any) error }) (*Listed, error) {
	var l Listed
	if err := row.Scan(&l.PayoutID, &l.Amount, &l.Currency, &l.ArrivalDate, &l.SentAt, &l.CommissionTTC, &l.StatementKey); err != nil {
		return nil, err
	}
	l.HasStatement = l.StatementKey != ""
	return &l, nil
}

func (r *sqlRepository) ListForMerchant(ctx context.Context, merchantID string, limit int) ([]Listed, error) {
	rows, err := r.db.QueryContext(ctx, listedSelect+` ORDER BY arrival_date DESC, id DESC LIMIT $2`, merchantID, limit)
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
	l, err := scanListed(r.db.QueryRowContext(ctx, listedSelect+` AND payout_id = $2`, merchantID, payoutID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (r *sqlRepository) InvoiceCandidates(ctx context.Context, before, periodMonth time.Time) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT d.merchant_id
		FROM payout_documents d
		WHERE d.status = 'done' AND d.invoicing_state = 'pending'
		  AND d.merchant_id IS NOT NULL AND d.arrival_date < $1
		  AND NOT EXISTS (
		        SELECT 1 FROM commission_invoices i
		        WHERE i.merchant_id = d.merchant_id AND i.period_month = $2::date)
		  AND NOT EXISTS (
		        SELECT 1 FROM payout_documents p
		        JOIN stripe_accounts sa ON sa.account_id = p.stripe_account_id
		        WHERE sa.merchant_id = d.merchant_id AND p.status = 'pending' AND p.arrival_date < $1)
		ORDER BY d.merchant_id`, before, periodMonth)
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

const lineColumns = `payout_id, arrival_date, scannorder_amount, kiosk_amount, other_amount,
	refunds_amount, adjustments_amount, commission_ttc, stripe_fees, discrepancy, amount`

func scanLines(rows *sql.Rows) ([]Line, error) {
	defer rows.Close()
	var out []Line
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.PayoutID, &l.ArrivalDate, &l.ScanNOrder, &l.Kiosk, &l.Other,
			&l.Refunds, &l.Adjustments, &l.Commission, &l.StripeFees, &l.Discrepancy, &l.Net); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *sqlRepository) PayoutsToInvoice(ctx context.Context, merchantID string, before time.Time) ([]Line, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+lineColumns+` FROM payout_documents
		WHERE merchant_id = $1 AND status = 'done' AND invoicing_state = 'pending' AND arrival_date < $2
		ORDER BY arrival_date, id`, merchantID, before)
	if err != nil {
		return nil, err
	}
	return scanLines(rows)
}

func (r *sqlRepository) SkipInvoicing(ctx context.Context, payoutIDs []string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE payout_documents SET invoicing_state = 'skipped'
		WHERE payout_id = ANY($1) AND invoicing_state = 'pending'`, payoutIDs)
	return err
}

const invoiceColumns = `id, number, series, merchant_id, period_month, amount_ttc, amount_ht, amount_vat,
	payout_count, period_start, period_end, issued_at, sent_at, attempts,
	COALESCE(invoice_key, ''), COALESCE(recap_key, '')`

func scanInvoice(row interface{ Scan(...any) error }) (*Invoice, error) {
	var inv Invoice
	var sentAt sql.NullTime
	if err := row.Scan(&inv.ID, &inv.Number, &inv.Series, &inv.MerchantID, &inv.PeriodMonth,
		&inv.AmountTTC, &inv.AmountHT, &inv.AmountVAT, &inv.PayoutCount,
		&inv.PeriodStart, &inv.PeriodEnd, &inv.IssuedAt, &sentAt, &inv.Attempts,
		&inv.InvoiceKey, &inv.RecapKey); err != nil {
		return nil, err
	}
	inv.SentAt = sentAt.Time
	inv.HasInvoice = inv.InvoiceKey != ""
	inv.HasRecap = inv.RecapKey != ""
	return &inv, nil
}

func (r *sqlRepository) IssueInvoice(ctx context.Context, in NewInvoice) (*Invoice, error) {
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
			(number, series, merchant_id, period_month, amount_ttc, amount_ht, amount_vat,
			 payout_count, period_start, period_end, issued_at)
		VALUES ($1, $2, $3, $4::date, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+invoiceColumns,
		number, in.Series, in.MerchantID, in.PeriodMonth, in.AmountTTC, ht, vat,
		len(in.PayoutIDs), in.PeriodStart, in.PeriodEnd, in.IssuedAt))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, nil // déjà une facture pour ce mois
		}
		return nil, fmt.Errorf("insertion de la facture: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE payout_documents SET invoicing_state = 'invoiced', commission_invoice_id = $1
		WHERE payout_id = ANY($2) AND invoicing_state = 'pending'`, inv.ID, in.PayoutIDs)
	if err != nil {
		return nil, fmt.Errorf("rattachement des virements: %w", err)
	}
	if n, _ := res.RowsAffected(); int(n) != len(in.PayoutIDs) {
		return nil, fmt.Errorf("rattachement des virements: %d sur %d rattachés (virements déjà facturés ?)", n, len(in.PayoutIDs))
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return inv, nil
}

func (r *sqlRepository) InvoiceLines(ctx context.Context, invoiceID int64) ([]Line, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+lineColumns+` FROM payout_documents
		WHERE commission_invoice_id = $1
		ORDER BY arrival_date, id`, invoiceID)
	if err != nil {
		return nil, err
	}
	return scanLines(rows)
}

func (r *sqlRepository) UnsentInvoices(ctx context.Context, limit int) ([]Invoice, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+invoiceColumns+` FROM commission_invoices
		WHERE status = 'pending' ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

func (r *sqlRepository) RecordInvoiceFailure(ctx context.Context, invoiceID int64, reason string, failed bool) error {
	status := "pending"
	if failed {
		status = "failed"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE commission_invoices
		SET attempts = attempts + 1, last_error = $2, status = $3
		WHERE id = $1`, invoiceID, reason, status)
	return err
}

func (r *sqlRepository) MarkInvoiceSent(ctx context.Context, invoiceID int64, sent InvoiceSent) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE commission_invoices
		SET status = 'sent', last_error = NULL, sent_at = now(),
		    recipient = $2, invoice_key = $3, recap_key = $4
		WHERE id = $1`, invoiceID, sent.Recipient, nullIfEmpty(sent.InvoiceKey), nullIfEmpty(sent.RecapKey))
	return err
}

func (r *sqlRepository) ListInvoicesForMerchant(ctx context.Context, merchantID string, limit int) ([]Invoice, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+invoiceColumns+` FROM commission_invoices
		WHERE merchant_id = $1 AND status = 'sent'
		ORDER BY period_month DESC, id DESC LIMIT $2`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

func (r *sqlRepository) GetInvoiceForMerchant(ctx context.Context, merchantID, number string) (*Invoice, error) {
	inv, err := scanInvoice(r.db.QueryRowContext(ctx, `
		SELECT `+invoiceColumns+` FROM commission_invoices
		WHERE merchant_id = $1 AND number = $2 AND status = 'sent'`, merchantID, number))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return inv, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
