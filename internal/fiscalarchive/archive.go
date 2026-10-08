// Package fiscalarchive produit les archives fiscales (conformité caisse,
// lot D : docs/attestation-conformite-05-lot-D-brief.md ; BOI-TVA-DECLA-30-10-30
// §220 à §250, constat C7 de l'audit).
//
// Une archive couvre une période close d'un établissement (un mois,
// automatiquement, ou une période à la demande) : un ZIP de fichiers CSV
// ligne par ligne (tickets, lignes, TVA, commandes, paiements, journal,
// clôtures, registres), une notice en français et un manifeste des
// empreintes. Le fichier est stocké dans le bucket R2 privé ; sa ligne dans
// fiscal_archives est chaînée et signée (chaîne fiscal_archives), ce qui fige
// l'archive et lui donne date certaine.
package fiscalarchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/version"
)

// Natures d'archive (fiscal_archives.kind).
const (
	KindMonth  = "MONTH"  // automatique, une par mois clos
	KindPeriod = "PERIOD" // à la demande, période close quelconque
)

// GeneratedBySystem est l'auteur d'une archive automatique.
const GeneratedBySystem = "SYSTEM"

// ErrPeriodNotClosed : la période demandée n'est pas entièrement couverte par
// des clôtures journalières (ou, pour un mois, par sa clôture mensuelle).
// Une archive ne fige que des jours clos.
var ErrPeriodNotClosed = errors.New("fiscalarchive: period not fiscally closed")

// Store stocke le fichier d'archive (bucket R2 privé : r2.Client).
type Store interface {
	UploadPrivateFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
}

// Request décrit une archive à produire. Start et End sont des jours locaux
// de l'établissement (seule la date compte), bornes incluses.
type Request struct {
	MerchantID  string
	Kind        string
	Start, End  time.Time
	GeneratedBy string
}

// Archive est une ligne de fiscal_archives.
type Archive struct {
	ID              int64
	MerchantID      string
	Kind            string
	PeriodStart     string // AAAA-MM-JJ
	PeriodEnd       string
	Timezone        string
	Filename        string
	R2Key           string
	SHA256          string
	ManifestSHA256  string
	SizeBytes       int64
	SoftwareVersion string
	GeneratedBy     string
	GeneratedAt     time.Time
	PreviousHash    string
	Hash            string
	Signature       string

	// Check : contrôle croisé du ZIP (Verify) juste après sa construction ;
	// nil pour une archive relue en base. Un écart n'empêche pas l'archive :
	// elle fige les données telles qu'elles sont, l'appelant le signale.
	Check *VerifyReport
}

// Payload est la charge scellée d'une archive (chaîne fiscal_archives) : la
// même structure, reconstruite depuis la ligne, doit redonner l'empreinte.
type Payload struct {
	MerchantID      string `json:"merchant_id"`
	Kind            string `json:"kind"`
	PeriodStart     string `json:"period_start"`
	PeriodEnd       string `json:"period_end"`
	Timezone        string `json:"timezone"`
	Filename        string `json:"filename"`
	SHA256          string `json:"sha256"`
	ManifestSHA256  string `json:"manifest_sha256"`
	SizeBytes       int64  `json:"size_bytes"`
	SoftwareVersion string `json:"software_version"`
	GeneratedBy     string `json:"generated_by"`
	GeneratedAt     string `json:"generated_at"`
}

// PayloadOf reconstruit la charge scellée d'une archive.
func PayloadOf(a Archive) Payload {
	return Payload{
		MerchantID: a.MerchantID, Kind: a.Kind, PeriodStart: a.PeriodStart, PeriodEnd: a.PeriodEnd,
		Timezone: a.Timezone, Filename: a.Filename, SHA256: a.SHA256, ManifestSHA256: a.ManifestSHA256,
		SizeBytes: a.SizeBytes, SoftwareVersion: a.SoftwareVersion, GeneratedBy: a.GeneratedBy,
		GeneratedAt: fiscal.FormatTime(a.GeneratedAt),
	}
}

// Generate produit, stocke et scelle une archive. Pour un mois (KindMonth),
// idempotent : si l'archive du mois existe, elle est renvoyée telle quelle.
//
// Étapes : contrôle que la période est close ; lecture de toutes les données
// dans une transaction en lecture seule (un seul instantané) ; ZIP en
// mémoire ; envoi au stockage privé (clé unique, jamais écrasée) ; puis, dans
// une courte transaction, verrou de la chaîne fiscal_archives et insertion de
// la ligne scellée.
func Generate(ctx context.Context, database *sql.DB, store Store, req Request) (*Archive, error) {
	if req.Kind != KindMonth && req.Kind != KindPeriod {
		return nil, fmt.Errorf("fiscalarchive: unknown kind %q", req.Kind)
	}
	start, end := dateOnly(req.Start), dateOnly(req.End)
	if end.Before(start) {
		return nil, fmt.Errorf("fiscalarchive: period end before start")
	}
	if req.Kind == KindMonth {
		if start.Day() != 1 || !end.Equal(start.AddDate(0, 1, -1)) {
			return nil, fmt.Errorf("fiscalarchive: a MONTH archive covers a whole calendar month")
		}
		if existing, err := findMonth(ctx, dbx.GetDB(ctx, database), req.MerchantID, start); err != nil || existing != nil {
			return existing, err
		}
	}
	if err := checkClosed(ctx, dbx.GetDB(ctx, database), req.MerchantID, req.Kind, start, end); err != nil {
		return nil, err
	}

	generatedAt := fiscal.Now()
	built, err := Build(ctx, database, req.MerchantID, start, end, req.Kind, generatedAt)
	if err != nil {
		return nil, err
	}
	check, err := Verify(built.Zip)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(built.Zip)
	a := Archive{
		MerchantID:      req.MerchantID,
		Kind:            req.Kind,
		PeriodStart:     start.Format("2006-01-02"),
		PeriodEnd:       end.Format("2006-01-02"),
		Timezone:        built.Timezone,
		Filename:        built.Filename,
		SHA256:          fmt.Sprintf("%x", sum),
		ManifestSHA256:  built.ManifestSHA256,
		SizeBytes:       int64(len(built.Zip)),
		SoftwareVersion: version.Version,
		GeneratedBy:     req.GeneratedBy,
		GeneratedAt:     generatedAt,
		Check:           check,
	}
	a.R2Key = fmt.Sprintf("fiscal-archives/%s/%s/%d_%s", req.MerchantID, start.Format("2006-01"), generatedAt.UnixMicro(), a.Filename)
	if _, err := store.UploadPrivateFile(ctx, a.R2Key, bytes.NewReader(built.Zip), "application/zip"); err != nil {
		return nil, fmt.Errorf("fiscalarchive: upload %s: %w", a.R2Key, err)
	}

	var out *Archive
	err = dbutils.RunInTx(ctx, database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, database)
		if err := fiscal.LockChain(txCtx, fiscal.ChainFiscalArchives, req.MerchantID); err != nil {
			return err
		}
		if req.Kind == KindMonth {
			// Une autre instance l'a écrite pendant la génération : on garde
			// la sienne (le fichier envoyé ici reste orphelin, sans effet).
			existing, err := findMonth(txCtx, db, req.MerchantID, start)
			if err != nil || existing != nil {
				out = existing
				return err
			}
		}
		var prev sql.NullString
		if err := db.QueryRowContext(txCtx, `
			SELECT hash FROM fiscal_archives WHERE merchant_id = ?
			ORDER BY generated_at DESC, id DESC LIMIT 1`, req.MerchantID).Scan(&prev); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("fiscalarchive: chain head: %w", err)
		}
		a.PreviousHash = fiscal.PrevOrGenesis(prev.String)
		h, s, err := fiscal.Seal(fiscal.ChainFiscalArchives, a.PreviousHash, PayloadOf(a))
		if err != nil {
			return err
		}
		a.Hash, a.Signature = h, s
		if err := db.QueryRowContext(txCtx, `
			INSERT INTO fiscal_archives
			  (merchant_id, kind, period_start, period_end, timezone, filename, r2_key, sha256, manifest_sha256,
			   size_bytes, software_version, generated_by, generated_at, previous_hash, hash, signature, hash_version)
			VALUES (?, ?, ?::date, ?::date, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id`,
			a.MerchantID, a.Kind, a.PeriodStart, a.PeriodEnd, a.Timezone, a.Filename, a.R2Key, a.SHA256, a.ManifestSHA256,
			a.SizeBytes, a.SoftwareVersion, a.GeneratedBy, a.GeneratedAt, a.PreviousHash, a.Hash, a.Signature, fiscal.HashVersion).
			Scan(&a.ID); err != nil {
			return fmt.Errorf("fiscalarchive: insert: %w", err)
		}
		out = &a
		return nil
	})
	return out, err
}

const archiveColumns = `id, merchant_id, kind, to_char(period_start, 'YYYY-MM-DD'), to_char(period_end, 'YYYY-MM-DD'), timezone,
	filename, r2_key, sha256, manifest_sha256, size_bytes, software_version, generated_by, generated_at,
	previous_hash, hash, signature`

func scanArchive(scan func(dest ...any) error) (Archive, error) {
	var a Archive
	err := scan(&a.ID, &a.MerchantID, &a.Kind, &a.PeriodStart, &a.PeriodEnd, &a.Timezone, &a.Filename, &a.R2Key,
		&a.SHA256, &a.ManifestSHA256, &a.SizeBytes, &a.SoftwareVersion, &a.GeneratedBy, &a.GeneratedAt,
		&a.PreviousHash, &a.Hash, &a.Signature)
	return a, err
}

func findMonth(ctx context.Context, db *dbx.DB, merchantID string, start time.Time) (*Archive, error) {
	a, err := scanArchive(db.QueryRowContext(ctx, `
		SELECT `+archiveColumns+` FROM fiscal_archives
		WHERE merchant_id = ? AND kind = 'MONTH' AND period_start = ?::date`,
		merchantID, start.Format("2006-01-02")).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: find month: %w", err)
	}
	return &a, nil
}

// List renvoie les archives d'un établissement, la plus récente d'abord.
func List(ctx context.Context, db *dbx.DB, merchantID string) ([]Archive, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+archiveColumns+` FROM fiscal_archives
		WHERE merchant_id = ? ORDER BY generated_at DESC, id DESC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Archive
	for rows.Next() {
		a, err := scanArchive(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// checkClosed : chaque jour de la période a sa clôture journalière ; pour un
// mois, sa clôture mensuelle est écrite.
func checkClosed(ctx context.Context, db *dbx.DB, merchantID, kind string, start, end time.Time) error {
	days := int(end.Sub(start).Hours()/24) + 1
	var closedDays int
	var month bool
	if err := db.QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM fiscal_closures
		   WHERE merchant_id = ? AND period_type = 'DAY' AND period_start BETWEEN ?::date AND ?::date),
		  EXISTS (SELECT 1 FROM fiscal_closures
		          WHERE merchant_id = ? AND period_type = 'MONTH' AND period_start = ?::date)`,
		merchantID, start.Format("2006-01-02"), end.Format("2006-01-02"), merchantID, start.Format("2006-01-02")).
		Scan(&closedDays, &month); err != nil {
		return fmt.Errorf("fiscalarchive: check closures: %w", err)
	}
	if closedDays != days || (kind == KindMonth && !month) {
		return fmt.Errorf("%w: %d jour(s) clos sur %d", ErrPeriodNotClosed, closedDays, days)
	}
	return nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// safeFilePart garde un fragment de nom de fichier lisible partout.
func safeFilePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "inconnu"
	}
	return b.String()
}

// Get renvoie une archive d'un établissement, nil si elle n'existe pas (ou
// appartient à un autre établissement).
func Get(ctx context.Context, db *dbx.DB, merchantID string, id int64) (*Archive, error) {
	a, err := scanArchive(db.QueryRowContext(ctx, `
		SELECT `+archiveColumns+` FROM fiscal_archives WHERE merchant_id = ? AND id = ?`, merchantID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: get: %w", err)
	}
	return &a, nil
}
