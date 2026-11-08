package fiscalarchive

import (
	"context"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
)

// PreChainPeriod renvoie la période d'un établissement qui précède sa première
// clôture journalière, à partir de from (jour local) : de from — ou du jour de
// création de l'établissement, s'il est plus tardif — à la veille de cette
// première clôture. ok vaut false s'il n'y a rien à archiver (aucune clôture,
// ou première clôture au plus tard à from). done vaut true quand une archive à
// la demande couvre déjà exactement cette période.
func PreChainPeriod(ctx context.Context, db *dbx.DB, merchantID string, from, created time.Time) (start, end time.Time, done, ok bool, err error) {
	first, hasChain, err := chainStart(ctx, db, merchantID)
	if err != nil || !hasChain {
		return start, end, false, false, err
	}
	start, end = dateOnly(from), first.AddDate(0, 0, -1)
	if c := dateOnly(created); c.After(start) {
		start = c
	}
	if end.Before(start) {
		return start, end, false, false, nil
	}
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM fiscal_archives
		               WHERE merchant_id = ? AND kind = 'PERIOD' AND period_start = ?::date AND period_end = ?::date)`,
		merchantID, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&done); err != nil {
		return start, end, false, false, fmt.Errorf("fiscalarchive: pre-chain archive lookup: %w", err)
	}
	return start, end, done, true, nil
}
