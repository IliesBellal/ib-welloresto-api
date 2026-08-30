package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// apiRequestLogRetentionDays est la durée de conservation des lignes
// api_request_logs. Purement technique (débogage d'intégration), sans valeur
// probante contrairement à audit_logs (chaînage de hash, non concerné par
// cette tâche) : 30 jours suffisent.
const apiRequestLogRetentionDays = 30

// apiRequestLogPurgeBatchSize borne chaque DELETE pour ne pas tenir un verrou
// long sur une table de plusieurs centaines de milliers de lignes.
const apiRequestLogPurgeBatchSize = 5000

// CleanupOldRequestLogs purge par lots les lignes api_request_logs plus
// vieilles que apiRequestLogRetentionDays.
//
// Le double SELECT imbriqué (au lieu de DELETE ... WHERE id IN (SELECT id
// FROM api_request_logs ...)) évite l'erreur MySQL "can't specify target
// table for update in FROM clause" si jamais cette tâche tourne sous le
// dialecte MySQL ; Postgres l'accepte aussi bien.
//
// Nécessite l'index sur created_at posé par la migration
// 109_api_request_logs_created_at_index, sans quoi chaque lot ferait un
// parcours complet de la table.
func (tm *TasksManager) CleanupOldRequestLogs() {
	if tm.DB == nil {
		tm.logWarn("[CRON] CleanupOldRequestLogs: base indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)

	cutoff := time.Now().UTC().AddDate(0, 0, -apiRequestLogRetentionDays)

	var totalDeleted int64
	for {
		res, err := db.ExecContext(ctx, `DELETE FROM api_request_logs
			WHERE id IN (
				SELECT id FROM (
					SELECT id FROM api_request_logs WHERE created_at < ? LIMIT ?
				) AS batch
			)`, cutoff, apiRequestLogPurgeBatchSize)
		if err != nil {
			tm.logError("[CRON] CleanupOldRequestLogs: échec",
				zap.Error(err), zap.Int64("deleted_before_failure", totalDeleted))
			return
		}

		deleted, err := res.RowsAffected()
		if err != nil {
			tm.logInfo("[CRON] CleanupOldRequestLogs: terminé (nombre de lignes indisponible)",
				zap.Int("older_than_days", apiRequestLogRetentionDays))
			return
		}

		totalDeleted += deleted
		if deleted < apiRequestLogPurgeBatchSize {
			break
		}
	}

	tm.logInfo("[CRON] CleanupOldRequestLogs: terminé",
		zap.Int64("deleted", totalDeleted),
		zap.Int("older_than_days", apiRequestLogRetentionDays))
}
