package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/fiscalarchive"

	"go.uber.org/zap"
)

const (
	// fiscalArchiveRunBudget borne un passage horaire : au-delà, les mois
	// restants attendent le passage suivant (rattrapage initial étalé sur
	// quelques heures plutôt qu'une tâche qui ne finit plus).
	fiscalArchiveRunBudget = 10 * time.Minute
	// fiscalArchiveRunLimit : mois lus par passage. Large devant le budget ;
	// un mois en échec permanent ne bloque pas ceux qui suivent.
	fiscalArchiveRunLimit = 500
	// fiscalArchiveTaskLock : une seule instance génère à la fois (les
	// autres passent leur tour). Generate est idempotent de toute façon ;
	// le verrou évite seulement de construire et d'envoyer deux fois le même
	// mois.
	fiscalArchiveTaskLock = "fiscal:archives-task"
)

// RunFiscalArchives génère les archives fiscales mensuelles manquantes (lot D
// de la conformité caisse, docs/attestation-conformite-05-lot-D-brief.md) :
// une par mois dont la clôture mensuelle est écrite, pour chaque
// établissement. Toutes les heures, sur chaque instance ; une seule travaille
// à la fois. Pas de commande de rattrapage : après le rattrapage des clôtures
// (cmd/backfill_fiscal_closures), cette tâche archive les mois passés
// d'elle-même, du plus ancien au plus récent, dans la limite de
// fiscalArchiveRunBudget par passage.
func (tm *TasksManager) RunFiscalArchives() {
	if tm.FiscalArchiveStore == nil {
		tm.logError("[CRON] RunFiscalArchives: stockage privé R2 indisponible, archives fiscales non générées")
		return
	}
	ctx := context.Background()
	release, ok, err := fiscal.TryLock(ctx, tm.DB, fiscalArchiveTaskLock)
	if err != nil {
		tm.logError("[CRON] RunFiscalArchives: verrou de tâche échoué", zap.Error(err))
		return
	}
	if !ok {
		tm.logDebug("[CRON] RunFiscalArchives: une autre instance génère les archives")
		return
	}
	defer release()

	start := time.Now()
	res, err := fiscalarchive.GenerateDue(ctx, tm.DB, tm.FiscalArchiveStore, "", fiscalArchiveRunLimit,
		start.Add(fiscalArchiveRunBudget), fiscalarchive.DueHooks{
			OnError: func(m fiscalarchive.MonthRef, err error) {
				tm.logError("[CRON] RunFiscalArchives: archive mensuelle échouée",
					zap.String("merchant_id", m.MerchantID), zap.String("mois", m.Start.Format("2006-01")), zap.Error(err))
			},
			// L'archive est écrite ; l'écart (tickets / clôtures) est à
			// examiner par la vérification d'intégrité (lot E).
			OnAnomaly: func(m fiscalarchive.MonthRef, r *fiscalarchive.VerifyReport) {
				tm.logError("[CRON] RunFiscalArchives: contrôle croisé de l'archive en écart",
					zap.String("merchant_id", m.MerchantID), zap.String("mois", m.Start.Format("2006-01")),
					zap.Int("anomalies", r.Anomalies), zap.Strings("details", r.Problems))
			},
		})
	if err != nil {
		tm.logError("[CRON] RunFiscalArchives: lecture des mois à archiver échouée", zap.Error(err))
		return
	}
	if res.Generated+res.Failed+res.Remaining > 0 {
		tm.logInfo("[CRON] RunFiscalArchives: terminé",
			zap.Int("archives", res.Generated), zap.Int("echecs", res.Failed),
			zap.Int("reportes", res.Remaining), zap.Duration("duree", time.Since(start)))
	}
}
