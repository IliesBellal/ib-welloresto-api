package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// diagnosticEventRetentionDays est la durée de conservation des lignes
// diagnostic_events (docs/DIAGNOSTIC_EVENTS.md). Purement technique, sans valeur
// probante : de quoi retrouver une anomalie signalée plusieurs mois après.
const diagnosticEventRetentionDays = 180

// diagnosticEventPurgeBatchSize borne chaque DELETE (verrou court).
const diagnosticEventPurgeBatchSize = 5000

// CleanupOldDiagnosticEvents purge par lots les événements de diagnostic plus
// vieux que diagnosticEventRetentionDays. Sans la table (migration 182 pas
// encore appliquée), l'échec est journalisé et sans conséquence.
func (tm *TasksManager) CleanupOldDiagnosticEvents() {
	if tm.DB == nil {
		tm.logWarn("[CRON] CleanupOldDiagnosticEvents: base indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)
	cutoff := time.Now().UTC().AddDate(0, 0, -diagnosticEventRetentionDays)

	var totalDeleted int64
	for {
		res, err := db.ExecContext(ctx, `DELETE FROM diagnostic_events
			WHERE id IN (
				SELECT id FROM (
					SELECT id FROM diagnostic_events WHERE created_at < ? LIMIT ?
				) AS batch
			)`, cutoff, diagnosticEventPurgeBatchSize)
		if err != nil {
			tm.logError("[CRON] CleanupOldDiagnosticEvents: échec",
				zap.Error(err), zap.Int64("deleted_before_failure", totalDeleted))
			return
		}
		deleted, err := res.RowsAffected()
		if err != nil || deleted < diagnosticEventPurgeBatchSize {
			totalDeleted += deleted
			break
		}
		totalDeleted += deleted
	}

	tm.logInfo("[CRON] CleanupOldDiagnosticEvents: terminé",
		zap.Int64("deleted", totalDeleted),
		zap.Int("older_than_days", diagnosticEventRetentionDays))
}
