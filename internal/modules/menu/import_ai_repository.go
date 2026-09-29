package menu

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
)

// ErrAIDraftAlreadyRunning : le marchand a déjà une extraction en cours
// (index unique partiel uq_menu_import_drafts_one_running).
var ErrAIDraftAlreadyRunning = errors.New("import_ai_already_running")

// ErrAIDraftNotFound : brouillon inexistant ou d'un autre marchand.
var ErrAIDraftNotFound = errors.New("import_ai_draft_not_found")

// AIDraftRepository lit et écrit menu_import_drafts et menu_import_ai_credits
// (migration 161). Postgres uniquement, comme toute nouvelle table.
type AIDraftRepository struct {
	db *sql.DB
}

func NewAIDraftRepository(db *sql.DB) *AIDraftRepository {
	return &AIDraftRepository{db: db}
}

const aiDraftColumns = `id::text, merchant_id, created_by, source, status, consumes_credit, pages,
	COALESCE(error, ''), created_at, updated_at, expires_at, committed_at, files_purged_at`

func scanAIDraft(row interface{ Scan(...any) error }) (*AIDraft, error) {
	var d AIDraft
	var pages []byte
	if err := row.Scan(&d.ID, &d.MerchantID, &d.CreatedBy, &d.Source, &d.Status, &d.ConsumesCredit, &pages,
		&d.Error, &d.CreatedAt, &d.UpdatedAt, &d.ExpiresAt, &d.CommittedAt, &d.FilesPurgedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(pages, &d.Pages); err != nil {
		return nil, fmt.Errorf("brouillon %s : pages illisibles: %w", d.ID, err)
	}
	return &d, nil
}

// CreateDraft insère un brouillon. ErrAIDraftAlreadyRunning si le marchand en
// a déjà un en attente ou en cours.
func (r *AIDraftRepository) CreateDraft(ctx context.Context, d *AIDraft) error {
	pages, err := json.Marshal(d.Pages)
	if err != nil {
		return err
	}
	_, err = dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		INSERT INTO menu_import_drafts (id, merchant_id, created_by, source, status, consumes_credit, pages, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.MerchantID, d.CreatedBy, d.Source, d.Status, d.ConsumesCredit, string(pages), d.ExpiresAt)
	if dbx.IsDuplicateEntry(err) {
		return ErrAIDraftAlreadyRunning
	}
	return err
}

// GetDraft lit un brouillon du marchand.
func (r *AIDraftRepository) GetDraft(ctx context.Context, merchantID, id string) (*AIDraft, error) {
	d, err := scanAIDraft(dbx.GetDB(ctx, r.db).QueryRowContext(ctx,
		`SELECT `+aiDraftColumns+` FROM menu_import_drafts WHERE id::text = ? AND merchant_id = ?`, id, merchantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAIDraftNotFound
	}
	return d, err
}

// ListOpenDrafts rend les brouillons encore exploitables du marchand, du plus
// récent au plus ancien.
func (r *AIDraftRepository) ListOpenDrafts(ctx context.Context, merchantID string) ([]AIDraft, error) {
	rows, err := dbx.GetDB(ctx, r.db).QueryContext(ctx, `
		SELECT `+aiDraftColumns+` FROM menu_import_drafts
		WHERE merchant_id = ? AND status IN ('pending', 'processing', 'ready', 'failed') AND expires_at > now()
		ORDER BY created_at DESC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drafts []AIDraft
	for rows.Next() {
		d, err := scanAIDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, *d)
	}
	return drafts, rows.Err()
}

// ClaimDraft passe un brouillon en cours de lecture. Faux s'il ne l'est pas
// (déjà pris, abandonné) : une seule instance le traite.
func (r *AIDraftRepository) ClaimDraft(ctx context.Context, id string) (bool, error) {
	res, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		UPDATE menu_import_drafts SET status = 'processing', updated_at = now()
		WHERE id::text = ? AND status IN ('pending', 'ready', 'failed')`, id)
	if dbx.IsDuplicateEntry(err) {
		// Relance pendant qu'une autre lecture du marchand tourne.
		return false, ErrAIDraftAlreadyRunning
	}
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SavePages enregistre la progression de la lecture (appelé après chaque
// photo). updated_at sert aussi de pouls pour la reprise des lectures
// interrompues.
func (r *AIDraftRepository) SavePages(ctx context.Context, id string, pages []AIDraftPage) error {
	payload, err := json.Marshal(pages)
	if err != nil {
		return err
	}
	_, err = dbx.GetDB(ctx, r.db).ExecContext(ctx,
		`UPDATE menu_import_drafts SET pages = ?, updated_at = now() WHERE id::text = ?`, string(payload), id)
	return err
}

// FinishDraft clôt une lecture : statut final, crédit décompté ou non, erreur.
func (r *AIDraftRepository) FinishDraft(ctx context.Context, id, status string, consumesCredit bool, errMsg string) error {
	_, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		UPDATE menu_import_drafts SET status = ?, consumes_credit = ?, error = NULLIF(?, ''), updated_at = now()
		WHERE id::text = ?`, status, consumesCredit, errMsg, id)
	return err
}

// MarkCommitted marque le brouillon importé, après un commit réussi.
func (r *AIDraftRepository) MarkCommitted(ctx context.Context, merchantID, id string) error {
	_, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		UPDATE menu_import_drafts SET status = 'committed', committed_at = now(), updated_at = now()
		WHERE id::text = ? AND merchant_id = ?`, id, merchantID)
	return err
}

// AbandonDraft expire un brouillon à la demande du marchand. Une lecture en
// cours n'est pas abandonnable (elle se termine d'elle-même).
func (r *AIDraftRepository) AbandonDraft(ctx context.Context, merchantID, id string) (bool, error) {
	res, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		UPDATE menu_import_drafts SET status = 'expired', expires_at = now(), updated_at = now()
		WHERE id::text = ? AND merchant_id = ? AND status IN ('pending', 'ready', 'failed')`, id, merchantID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// CountUsedCredits compte les extractions décomptées du marchand.
func (r *AIDraftRepository) CountUsedCredits(ctx context.Context, merchantID string) (int, error) {
	var n int
	err := dbx.GetDB(ctx, r.db).QueryRowContext(ctx,
		`SELECT count(*) FROM menu_import_drafts WHERE merchant_id = ? AND consumes_credit`, merchantID).Scan(&n)
	return n, err
}

// GetCreditsOverride rend le nombre de crédits posé par le staff, ou faux
// s'il n'y en a pas (le défaut s'applique).
func (r *AIDraftRepository) GetCreditsOverride(ctx context.Context, merchantID string) (int, bool, error) {
	var credits int
	err := dbx.GetDB(ctx, r.db).QueryRowContext(ctx,
		`SELECT credits FROM menu_import_ai_credits WHERE merchant_id = ?`, merchantID).Scan(&credits)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return credits, err == nil, err
}

// SetCreditsOverride pose le nombre total de crédits d'un marchand.
func (r *AIDraftRepository) SetCreditsOverride(ctx context.Context, merchantID string, credits int, updatedBy string) error {
	_, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		INSERT INTO menu_import_ai_credits (merchant_id, credits, updated_by, updated_at)
		VALUES (?, ?, ?, now())
		ON CONFLICT (merchant_id) DO UPDATE
		SET credits = EXCLUDED.credits, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		merchantID, credits, updatedBy)
	return err
}

// FailStaleDrafts clôt en échec, sans décompter de crédit, les lectures sans
// progrès depuis staleAfter (instance redémarrée en pleine lecture). Les
// photos déjà lues restent : une relance ne refait que les autres.
func (r *AIDraftRepository) FailStaleDrafts(ctx context.Context, staleAfter time.Duration) (int64, error) {
	res, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		UPDATE menu_import_drafts
		SET status = 'failed', consumes_credit = FALSE,
		    error = 'lecture interrompue (redémarrage du serveur) : relancez les photos restantes',
		    updated_at = now()
		WHERE status IN ('pending', 'processing') AND updated_at < ?`, time.Now().Add(-staleAfter))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ExpireDrafts expire les brouillons arrivés à échéance sans être importés.
func (r *AIDraftRepository) ExpireDrafts(ctx context.Context) (int64, error) {
	res, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `
		UPDATE menu_import_drafts SET status = 'expired', updated_at = now()
		WHERE status IN ('ready', 'failed') AND expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListDraftsToPurge rend les brouillons clos (importés ou expirés) depuis plus
// de purgeAfter dont les photos sont encore dans R2.
func (r *AIDraftRepository) ListDraftsToPurge(ctx context.Context, purgeAfter time.Duration, limit int) ([]AIDraft, error) {
	rows, err := dbx.GetDB(ctx, r.db).QueryContext(ctx, `
		SELECT `+aiDraftColumns+` FROM menu_import_drafts
		WHERE files_purged_at IS NULL AND status IN ('committed', 'expired') AND updated_at < ?
		ORDER BY updated_at LIMIT ?`, time.Now().Add(-purgeAfter), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drafts []AIDraft
	for rows.Next() {
		d, err := scanAIDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, *d)
	}
	return drafts, rows.Err()
}

// MarkFilesPurged note que les photos du brouillon ont été supprimées de R2.
func (r *AIDraftRepository) MarkFilesPurged(ctx context.Context, id string) error {
	_, err := dbx.GetDB(ctx, r.db).ExecContext(ctx,
		`UPDATE menu_import_drafts SET files_purged_at = now() WHERE id::text = ?`, id)
	return err
}
