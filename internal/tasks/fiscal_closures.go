package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"

	"go.uber.org/zap"
)

// RunFiscalClosures clôt, pour chaque établissement actif, les jours échus
// pas encore clos (clôtures journalières, mensuelles et annuelles scellées,
// docs/attestation-conformite-02-lot-B-brief.md). Tourne toutes les heures sur
// chaque instance : fiscal.CloseDueDays est idempotent. Sans condition : un
// établissement sans clôture commence au dernier jour échu (le rattrapage des
// jours antérieurs se fait une fois, avant, avec cmd/backfill_fiscal_closures).
//
// Même schéma que CloseOrders : la liste des établissements est collectée et
// le curseur fermé avant le travail.
func (tm *TasksManager) RunFiscalClosures() {
	ctx := context.Background()
	rows, err := dbx.GetDB(ctx, tm.DB).QueryContext(ctx, `
		SELECT id::text, COALESCE(timezone, ''), creation_date FROM merchant WHERE is_active ORDER BY id`)
	if err != nil {
		tm.logError("[CRON] RunFiscalClosures: lecture des établissements échouée", zap.Error(err))
		return
	}
	type merchantRef struct {
		id, timezone string
		created      time.Time
	}
	var merchants []merchantRef
	for rows.Next() {
		var m merchantRef
		if err := rows.Scan(&m.id, &m.timezone, &m.created); err != nil {
			tm.logError("[CRON] RunFiscalClosures: scan échoué", zap.Error(err))
			continue
		}
		merchants = append(merchants, m)
	}
	if err := rows.Err(); err != nil {
		tm.logError("[CRON] RunFiscalClosures: itération interrompue", zap.Error(err))
	}
	rows.Close()

	total := 0
	for _, m := range merchants {
		n, err := fiscal.CloseDueDays(ctx, tm.DB, m.id, m.timezone, nil, m.created, time.Now())
		total += n
		if err != nil {
			tm.logError("[CRON] RunFiscalClosures: clôture échouée",
				zap.String("merchant_id", m.id), zap.Int("jours_clos", n), zap.Error(err))
		}
	}
	if total > 0 {
		tm.logInfo("[CRON] RunFiscalClosures: terminé", zap.Int("jours_clos", total))
	}
}
