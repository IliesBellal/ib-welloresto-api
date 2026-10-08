package fiscalarchive

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
)

// MonthRef désigne un mois clos d'un établissement (clôture mensuelle écrite).
type MonthRef struct {
	MerchantID string
	Start      time.Time // 1er du mois, jour local
}

// PendingMonths renvoie les mois clos sans archive mensuelle, les plus anciens
// d'abord, au plus limit. merchantID vide : tous les établissements, actifs
// ou non (un établissement désactivé garde ses obligations de conservation).
func PendingMonths(ctx context.Context, db *dbx.DB, merchantID string, limit int) ([]MonthRef, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.merchant_id, to_char(c.period_start, 'YYYY-MM-DD')
		FROM fiscal_closures c
		WHERE c.period_type = 'MONTH' AND (?::text = '' OR c.merchant_id = ?::text)
		  AND NOT EXISTS (SELECT 1 FROM fiscal_archives a
		                  WHERE a.merchant_id = c.merchant_id AND a.kind = 'MONTH' AND a.period_start = c.period_start)
		ORDER BY c.period_start, c.merchant_id
		LIMIT ?`, merchantID, merchantID, limit)
	if err != nil {
		return nil, fmt.Errorf("fiscalarchive: pending months: %w", err)
	}
	defer rows.Close()
	var out []MonthRef
	for rows.Next() {
		var m MonthRef
		var start string
		if err := rows.Scan(&m.MerchantID, &start); err != nil {
			return nil, err
		}
		if m.Start, err = time.Parse("2006-01-02", start); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DueResult résume un passage de GenerateDue.
type DueResult struct {
	Generated int // archives écrites
	Failed    int // mois en échec, retentés au passage suivant
	Remaining int // mois non tentés, échéance atteinte
}

// DueHooks : signalements d'un passage de GenerateDue.
type DueHooks struct {
	OnError   func(MonthRef, error)         // mois en échec
	OnAnomaly func(MonthRef, *VerifyReport) // archive écrite, contrôle croisé en écart
}

// GenerateDue génère les archives mensuelles manquantes (au plus limit), une à
// la fois, tant que l'échéance n'est pas atteinte. Un mois en échec est
// signalé à hooks.OnError et retenté au passage suivant ; il ne bloque pas
// les autres. Une archive écrite dont le contrôle croisé relève des écarts
// est signalée à hooks.OnAnomaly. Generate reste idempotent : un mois écrit
// entre-temps par une autre instance est simplement renvoyé.
func GenerateDue(ctx context.Context, database *sql.DB, store Store, merchantID string, limit int,
	deadline time.Time, hooks DueHooks) (DueResult, error) {
	var res DueResult
	months, err := PendingMonths(ctx, dbx.GetDB(ctx, database), merchantID, limit)
	if err != nil {
		return res, err
	}
	for i, m := range months {
		if !time.Now().Before(deadline) {
			res.Remaining = len(months) - i
			break
		}
		a, err := Generate(ctx, database, store, Request{
			MerchantID:  m.MerchantID,
			Kind:        KindMonth,
			Start:       m.Start,
			End:         m.Start.AddDate(0, 1, -1),
			GeneratedBy: GeneratedBySystem,
		})
		if err != nil {
			res.Failed++
			if hooks.OnError != nil {
				hooks.OnError(m, err)
			}
			continue
		}
		res.Generated++
		if a.Check != nil && !a.Check.OK() && hooks.OnAnomaly != nil {
			hooks.OnAnomaly(m, a.Check)
		}
	}
	return res, nil
}
