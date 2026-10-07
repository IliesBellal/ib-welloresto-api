package accounting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// Archivage des exports comptables (migration 166,
// docs/EXPORT_COMPTABLE_MODES_CLOTURE.md) : chaque PDF est déposé dans le
// bucket R2 privé sous un nom unique horodaté, jamais écrasé, référencé dans
// accounting_exports et téléchargé par lien signé à durée limitée.

// accountingExportLinkTTL est la durée de validité d'un lien de téléchargement.
const accountingExportLinkTTL = time.Hour

// AccountingExport est une ligne de accounting_exports, telle que listée aux
// clients (sans la clé R2 interne).
type AccountingExport struct {
	ID          int64     `json:"id"`
	PeriodFrom  string    `json:"period_from"` // YYYY-MM-DD, calendrier de l'établissement
	PeriodTo    string    `json:"period_to"`   // YYYY-MM-DD, dernier jour inclus
	ClosingMode string    `json:"closing_mode"`
	Channels    []string  `json:"channels"` // vide = tous les canaux
	Filename    string    `json:"filename"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int       `json:"size_bytes"`
	GeneratedBy string    `json:"generated_by"`
	GeneratedAt time.Time `json:"generated_at"`
	r2Key       string
}

// ListAccountingExportsResponse répond à GET /pos/accounting/exports.
type ListAccountingExportsResponse struct {
	Status  string             `json:"status"`
	Exports []AccountingExport `json:"exports"`
}

// AccountingExportLinkResponse répond à GET /pos/accounting/exports/{id}/download.
type AccountingExportLinkResponse struct {
	Status      string `json:"status"`
	ExportID    int64  `json:"export_id"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"` // lien signé, valable une heure
}

// accountingExportFilename construit un nom de fichier unique : période,
// canaux retenus (s'il y a un filtre), horodatage UTC de génération. Un export
// régénéré n'écrase donc jamais celui déjà envoyé.
func accountingExportFilename(fromLocal, lastDayLocal time.Time, sources []string, generatedAt time.Time) string {
	name := fmt.Sprintf("WR_rapport_comptable_%s_%s", fromLocal.Format("20060102"), lastDayLocal.Format("20060102"))
	if len(sources) > 0 {
		name += "_" + strings.ToLower(strings.Join(sources, "-"))
	}
	return name + "_" + generatedAt.UTC().Format("20060102T150405Z") + ".pdf"
}

// InsertAccountingExport enregistre un export généré et renvoie son id.
func (r *AccountingRepository) InsertAccountingExport(ctx context.Context, merchantID string, e AccountingExport) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	id, err := db.InsertReturningID(ctx, `
		INSERT INTO accounting_exports
			(merchant_id, period_from, period_to, closing_mode, channels, filename, r2_key, sha256, size_bytes, generated_by, generated_at)
		VALUES (?, CAST(? AS date), CAST(? AS date), ?, ?, ?, ?, ?, ?, ?, ?)
	`, "id", merchantID, e.PeriodFrom, e.PeriodTo, e.ClosingMode, strings.Join(e.Channels, ","),
		e.Filename, e.r2Key, e.SHA256, e.SizeBytes, e.GeneratedBy, e.GeneratedAt)
	if err != nil {
		return 0, fmt.Errorf("insert accounting export: %w", err)
	}
	return id, nil
}

const accountingExportColumns = `id, period_from, period_to, closing_mode, channels, filename, r2_key, sha256, size_bytes, generated_by, generated_at`

func scanAccountingExport(scan func(dest ...interface{}) error) (AccountingExport, error) {
	var e AccountingExport
	var from, to time.Time
	var channels string
	if err := scan(&e.ID, &from, &to, &e.ClosingMode, &channels, &e.Filename, &e.r2Key, &e.SHA256, &e.SizeBytes, &e.GeneratedBy, &e.GeneratedAt); err != nil {
		return e, err
	}
	e.PeriodFrom = from.Format("2006-01-02")
	e.PeriodTo = to.Format("2006-01-02")
	e.Channels = []string{}
	if channels != "" {
		e.Channels = strings.Split(channels, ",")
	}
	e.SHA256 = strings.TrimSpace(e.SHA256)
	return e, nil
}

// ListAccountingExports renvoie les 200 derniers exports de l'établissement,
// du plus récent au plus ancien.
func (r *AccountingRepository) ListAccountingExports(ctx context.Context, merchantID string) ([]AccountingExport, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
		SELECT `+accountingExportColumns+`
		FROM accounting_exports
		WHERE merchant_id = ?
		ORDER BY generated_at DESC, id DESC
		LIMIT 200
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list accounting exports: %w", err)
	}
	defer rows.Close()

	out := []AccountingExport{}
	for rows.Next() {
		e, err := scanAccountingExport(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan accounting export: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetAccountingExport renvoie un export de l'établissement, models.ErrNotFound
// s'il n'existe pas ou appartient à un autre établissement.
func (r *AccountingRepository) GetAccountingExport(ctx context.Context, merchantID string, exportID int64) (AccountingExport, error) {
	db := dbx.GetDB(ctx, r.database)
	e, err := scanAccountingExport(db.QueryRowContext(ctx, `
		SELECT `+accountingExportColumns+`
		FROM accounting_exports
		WHERE id = ? AND merchant_id = ?
	`, exportID, merchantID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return e, models.ErrNotFound
	}
	if err != nil {
		return e, fmt.Errorf("get accounting export: %w", err)
	}
	return e, nil
}

// ListAccountingExports liste les exports de l'établissement de l'utilisateur.
func (s *AccountingService) ListAccountingExports(ctx context.Context) (*ListAccountingExportsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	exports, err := s.repo.ListAccountingExports(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	return &ListAccountingExportsResponse{Status: "1", Exports: exports}, nil
}

// AccountingExportLink renvoie un nouveau lien signé (une heure) vers un export
// archivé de l'établissement de l'utilisateur.
func (s *AccountingService) AccountingExportLink(ctx context.Context, exportID int64, signer accountingExportSigner) (*AccountingExportLinkResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	e, err := s.repo.GetAccountingExport(ctx, user.MerchantID, exportID)
	if err != nil {
		return nil, err
	}
	url, err := signer.GenerateSignedURL(ctx, e.r2Key, accountingExportLinkTTL)
	if err != nil {
		return nil, err
	}
	return &AccountingExportLinkResponse{Status: "1", ExportID: e.ID, Filename: e.Filename, DownloadURL: url}, nil
}

// accountingExportSigner est la partie du client R2 privé utilisée pour les
// liens de téléchargement.
type accountingExportSigner interface {
	GenerateSignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}
