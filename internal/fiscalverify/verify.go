package fiscalverify

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/version"
)

// ArchiveFetcher relit le fichier d'une archive (bucket R2 privé : r2.Client).
type ArchiveFetcher interface {
	GetFile(ctx context.Context, key string) ([]byte, error)
}

// Options d'une vérification.
type Options struct {
	MerchantID string
	// From et To : jours locaux de l'établissement, bornes incluses (seule la
	// date compte). From nul : depuis le début ; To nul : aujourd'hui.
	From, To time.Time
	// Archives : relit chaque fichier d'archive de la période et le contrôle
	// (empreinte, contrôle croisé). Nil : seule la ligne scellée est vérifiée.
	Archives ArchiveFetcher
	// Now : horloge (tests) ; nul = maintenant.
	Now time.Time
}

type verifier struct {
	db       *dbx.DB
	opts     Options
	r        *Report
	loc      *time.Location
	merchant string
	from, to time.Time // instants UTC, [from, to[
	fromDay  string    // AAAA-MM-JJ, vide = début
	toDay    string
	// attested : premier ticket v2 ; les commandes closes avant relèvent de
	// la version antérieure.
	attested *time.Time
}

// Run vérifie un établissement sur une période. Toutes les lectures se font
// dans une transaction en lecture seule (un seul instantané). Une erreur n'est
// renvoyée que si la vérification n'a pas pu se dérouler ; les anomalies sont
// dans le rapport.
func Run(ctx context.Context, database *sql.DB, opts Options) (*Report, error) {
	started := time.Now()
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("fiscalverify: begin: %w", err)
	}
	defer tx.Rollback()
	v := &verifier{db: dbx.Wrap(tx), opts: opts, merchant: opts.MerchantID,
		r: &Report{Software: version.Product, Version: version.Version, MerchantID: opts.MerchantID, GeneratedAt: now.UTC()}}

	var name, siret, tz sql.NullString
	if err := v.db.QueryRowContext(ctx, `SELECT fullname, siret, timezone FROM merchant WHERE id::text = ?`, opts.MerchantID).
		Scan(&name, &siret, &tz); err != nil {
		return nil, fmt.Errorf("fiscalverify: merchant %s: %w", opts.MerchantID, err)
	}
	v.r.MerchantName, v.r.Siret, v.r.Timezone = name.String, siret.String, tz.String
	if v.r.Timezone == "" {
		v.r.Timezone = "Europe/Paris"
	}
	if v.loc, err = time.LoadLocation(v.r.Timezone); err != nil {
		return nil, fmt.Errorf("fiscalverify: timezone %q: %w", v.r.Timezone, err)
	}

	to := opts.To
	if to.IsZero() {
		to = now.In(v.loc)
	}
	v.toDay = to.Format("2006-01-02")
	v.to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, v.loc).AddDate(0, 0, 1).UTC()
	if !opts.From.IsZero() {
		v.fromDay = opts.From.Format("2006-01-02")
		v.from = time.Date(opts.From.Year(), opts.From.Month(), opts.From.Day(), 0, 0, 0, 0, v.loc).UTC()
		if !v.from.Before(v.to) {
			return nil, fmt.Errorf("fiscalverify: empty period %s → %s", v.fromDay, v.toDay)
		}
	} else {
		v.from = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	v.r.From, v.r.To = v.fromDay, v.toDay

	var first sql.NullTime
	if err := v.db.QueryRowContext(ctx, `
		SELECT min(created_at) FROM receipts WHERE merchant_id = ? AND hash_version >= 2`, v.merchant).Scan(&first); err != nil {
		return nil, fmt.Errorf("fiscalverify: attested version start: %w", err)
	}
	if first.Valid {
		t := first.Time.UTC()
		v.attested, v.r.AttestedSince = &t, &t
	}

	for _, step := range []func(context.Context) error{
		v.payments, v.receipts, v.cashRegisters, v.auditLogs, v.closureChain, v.archives, v.ordersChain,
		v.numbering, v.dayClosures, v.aggregateClosures, v.ordersVsReceipts,
	} {
		if err := step(ctx); err != nil {
			return nil, err
		}
	}
	v.r.Duration = time.Since(started).Round(time.Millisecond).String()
	return v.r, nil
}

// attestedAt : un fait daté de t relève-t-il de la version attestée ?
func (v *verifier) attestedAt(t time.Time) bool {
	return v.attested != nil && !t.Before(*v.attested)
}
