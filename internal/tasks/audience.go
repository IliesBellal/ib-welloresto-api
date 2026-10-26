package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// audienceRetentionMonths : conservation de la mesure d'audience ScanNOrder
// (sno_events, sno_order_attributions — migration 194) et des bornes
// (kiosk_events — migration 195). 25 mois : durée maximale admise par la CNIL
// pour une mesure d'audience exemptée de consentement
// (docs/mesure-audience.md, D17). Purge quotidienne.
const audienceRetentionMonths = 25

const audiencePurgeBatchSize = 5000

// CleanupOldAudienceEvents purge par lots la mesure d'audience plus ancienne
// que audienceRetentionMonths (index sur created_at, migrations 194 et 195).
func (tm *TasksManager) CleanupOldAudienceEvents() {
	if tm.DB == nil {
		tm.logWarn("[CRON] CleanupOldAudienceEvents: base indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)
	cutoff := time.Now().UTC().AddDate(0, -audienceRetentionMonths, 0)

	purge := func(table, key string) (int64, error) {
		var total int64
		for {
			res, err := db.ExecContext(ctx, `DELETE FROM `+table+`
				WHERE `+key+` IN (
					SELECT `+key+` FROM (
						SELECT `+key+` FROM `+table+` WHERE created_at < ? LIMIT ?
					) AS batch
				)`, cutoff, audiencePurgeBatchSize)
			if err != nil {
				return total, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return total, nil
			}
			total += n
			if n < audiencePurgeBatchSize {
				return total, nil
			}
		}
	}

	events, err := purge("sno_events", "id")
	if err != nil {
		tm.logError("[CRON] CleanupOldAudienceEvents: échec (sno_events)", zap.Error(err), zap.Int64("deleted_before_failure", events))
		return
	}
	attributions, err := purge("sno_order_attributions", "order_id")
	if err != nil {
		tm.logError("[CRON] CleanupOldAudienceEvents: échec (sno_order_attributions)", zap.Error(err), zap.Int64("deleted_before_failure", attributions))
		return
	}
	kioskEvents, err := purge("kiosk_events", "id")
	if err != nil {
		tm.logError("[CRON] CleanupOldAudienceEvents: échec (kiosk_events)", zap.Error(err), zap.Int64("deleted_before_failure", kioskEvents))
		return
	}
	tm.logInfo("[CRON] CleanupOldAudienceEvents: terminé",
		zap.Int64("events_deleted", events),
		zap.Int64("attributions_deleted", attributions),
		zap.Int64("kiosk_events_deleted", kioskEvents),
		zap.Int("older_than_months", audienceRetentionMonths))
}
