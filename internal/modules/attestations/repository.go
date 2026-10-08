package attestations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// Attestation est une ligne de la table attestations (migration 176).
type Attestation struct {
	ID             int64
	MerchantID     string
	Software       string
	Version        string
	MajorRoot      string
	Content        Document
	SignerName     string
	SignedByUserID string
	SignedAt       time.Time
	Filename       string
	R2Key          string
	SHA256         string
	SizeBytes      int64
	GeneratedAt    time.Time
}

// merchantInfo : ce que le volet 2 reprend de l'établissement.
type merchantInfo struct {
	name, siret, address, city, timezone string
	created                              time.Time
	firstReceipt                         sql.NullTime
}

func loadMerchant(ctx context.Context, db *dbx.DB, merchantID string) (merchantInfo, error) {
	var m merchantInfo
	var name, siret, address, number, street, zip, city, tz sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT m.fullname, m.siret, m.address, m.street_number, m.street, m.zip_code, m.city, m.timezone, m.creation_date,
		       (SELECT min(r.created_at) FROM receipts r WHERE r.merchant_id = m.id::text)
		FROM merchant m WHERE m.id::text = ?`, merchantID).
		Scan(&name, &siret, &address, &number, &street, &zip, &city, &tz, &m.created, &m.firstReceipt); err != nil {
		return m, fmt.Errorf("attestations: merchant %s: %w", merchantID, err)
	}
	m.name, m.siret, m.city, m.timezone = strings.TrimSpace(name.String), strings.TrimSpace(siret.String),
		strings.TrimSpace(city.String), tz.String
	if m.timezone == "" {
		m.timezone = "Europe/Paris"
	}
	line := strings.TrimSpace(strings.Join([]string{number.String, street.String}, " "))
	cityLine := strings.TrimSpace(strings.Join([]string{zip.String, city.String}, " "))
	switch {
	case line != "" && cityLine != "":
		m.address = line + ", " + cityLine
	case strings.TrimSpace(address.String) != "":
		m.address = strings.TrimSpace(address.String)
	default:
		m.address = cityLine
	}
	return m, nil
}

const attestationColumns = `id, merchant_id, software, version, major_root, content::text, signer_name, signed_by_user_id,
	signed_at, filename, r2_key, sha256, size_bytes, generated_at`

func scanAttestation(scan func(dest ...any) error) (Attestation, error) {
	var a Attestation
	var content string
	if err := scan(&a.ID, &a.MerchantID, &a.Software, &a.Version, &a.MajorRoot, &content, &a.SignerName, &a.SignedByUserID,
		&a.SignedAt, &a.Filename, &a.R2Key, &a.SHA256, &a.SizeBytes, &a.GeneratedAt); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(content), &a.Content); err != nil {
		return a, fmt.Errorf("attestations: content %d: %w", a.ID, err)
	}
	return a, nil
}

func insert(ctx context.Context, db *dbx.DB, a *Attestation) error {
	content, err := json.Marshal(a.Content)
	if err != nil {
		return err
	}
	return db.QueryRowContext(ctx, `
		INSERT INTO attestations (merchant_id, software, version, major_root, content, signer_name, signed_by_user_id,
		                          signed_at, filename, r2_key, sha256, size_bytes, generated_at)
		VALUES (?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		a.MerchantID, a.Software, a.Version, a.MajorRoot, string(content), a.SignerName, a.SignedByUserID,
		a.SignedAt, a.Filename, a.R2Key, a.SHA256, a.SizeBytes, a.GeneratedAt).Scan(&a.ID)
}

func list(ctx context.Context, db *dbx.DB, merchantID string) ([]Attestation, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+attestationColumns+` FROM attestations
		WHERE merchant_id = ? ORDER BY generated_at DESC, id DESC`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("attestations: list: %w", err)
	}
	defer rows.Close()
	var out []Attestation
	for rows.Next() {
		a, err := scanAttestation(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// get : nil si l'attestation n'existe pas (ou est d'un autre établissement).
func get(ctx context.Context, db *dbx.DB, merchantID string, id int64) (*Attestation, error) {
	a, err := scanAttestation(db.QueryRowContext(ctx, `SELECT `+attestationColumns+` FROM attestations
		WHERE merchant_id = ? AND id = ?`, merchantID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("attestations: get: %w", err)
	}
	return &a, nil
}
