package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/fiscal"
	accountingModule "welloresto-api/internal/modules/pos/accounting"

	"go.uber.org/zap"
)

const (
	// accountingExportRunBudget borne un passage horaire : au-delà, les mois
	// restants attendent le passage suivant.
	accountingExportRunBudget = 10 * time.Minute
	// accountingExportRunLimit : mois lus par passage.
	accountingExportRunLimit = 500
	// accountingExportTaskLock : une seule instance génère à la fois (les autres
	// passent leur tour), pour ne pas produire deux PDF du même mois.
	accountingExportTaskLock = "accounting-exports:task"
)

// RunAccountingExports génère l'export comptable (PDF) de chaque mois dont la
// clôture fiscale mensuelle est écrite, du premier au dernier jour, tous canaux
// (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md, « Génération automatique »). Toutes
// les heures, sur chaque instance ; une seule travaille à la fois. Placée après
// RunFiscalClosures : elle lit les clôtures mensuelles que celle-ci vient
// d'écrire. Idempotente : un mois déjà exporté n'est pas refait.
func (tm *TasksManager) RunAccountingExports() {
	if tm.AccountingExportService == nil || tm.FiscalArchiveStore == nil {
		tm.logError("[CRON] RunAccountingExports: service d'export comptable ou stockage privé R2 indisponible, exports mensuels non générés")
		return
	}
	ctx := context.Background()
	release, ok, err := fiscal.TryLock(ctx, tm.DB, accountingExportTaskLock)
	if err != nil {
		tm.logError("[CRON] RunAccountingExports: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunAccountingExports: une autre instance génère les exports")
		return
	}
	defer release()

	start := time.Now()
	res, err := tm.AccountingExportService.GenerateDueMonthlyExports(ctx, tm.FiscalArchiveStore, accountingExportRunLimit,
		start.Add(accountingExportRunBudget), func(m accountingModule.MonthRef, reason string) {
			tm.logWarn("[CRON] RunAccountingExports: export mensuel non généré, retenté au prochain passage",
				zap.String("merchant_id", m.MerchantID), zap.String("du", m.From), zap.String("au", m.To), zap.String("raison", reason))
		})
	if err != nil {
		tm.logError("[CRON] RunAccountingExports: lecture des mois à exporter échouée", zap.Error(err))
		return
	}
	if res.Generated+res.Failed+res.Waiting+res.Remaining > 0 {
		tm.logInfo("[CRON] RunAccountingExports: terminé",
			zap.Int("exports", res.Generated), zap.Int("echecs", res.Failed),
			zap.Int("en_attente_registres", res.Waiting), zap.Int("reportes", res.Remaining),
			zap.Duration("duree", time.Since(start)))
	}
}
