package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// deviceTokenRetentionDays est la durée pendant laquelle un refresh token de
// borne ou d'écran CDS mort (révoqué ou expiré) reste en base avant purge.
// Chaque refresh d'access token fait tourner le refresh token (une ligne
// révoquée de plus), donc la table grossit en continu sans cette purge.
//
// Supprimer une ligne morte ne change rien pour l'appareil : un token révoqué
// ou expiré est déjà rejeté exactement comme un token inconnu
// (kiosk.Service.RefreshDeviceToken, cds.Service.RefreshDeviceToken), et la
// reprise d'une borne s'appuie sur kiosks.last_heartbeat_at, pas sur ces
// tables. Les 30 jours ne servent qu'au support.
const deviceTokenRetentionDays = 30

// deviceTokenTables : tables de refresh tokens device purgées par
// CleanupDeadDeviceTokens. Même schéma (expires_at, revoked_at).
var deviceTokenTables = []string{"kiosk_device_tokens", "cds_device_tokens"}

// CleanupDeadDeviceTokens supprime les refresh tokens de borne et d'écran CDS
// révoqués ou expirés depuis plus de deviceTokenRetentionDays.
func (tm *TasksManager) CleanupDeadDeviceTokens() {
	if tm.DB == nil {
		tm.logWarn("[CRON] CleanupDeadDeviceTokens: base indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)

	cutoff := time.Now().UTC().AddDate(0, 0, -deviceTokenRetentionDays)

	for _, table := range deviceTokenTables {
		res, err := db.ExecContext(ctx, `DELETE FROM `+table+`
			WHERE revoked_at < ? OR expires_at < ?`, cutoff, cutoff)
		if err != nil {
			tm.logError("[CRON] CleanupDeadDeviceTokens: échec",
				zap.String("table", table), zap.Error(err))
			continue
		}

		deleted, err := res.RowsAffected()
		if err != nil {
			tm.logInfo("[CRON] CleanupDeadDeviceTokens: terminé (nombre de lignes indisponible)",
				zap.String("table", table), zap.Int("older_than_days", deviceTokenRetentionDays))
			continue
		}

		tm.logInfo("[CRON] CleanupDeadDeviceTokens: terminé",
			zap.String("table", table),
			zap.Int64("deleted", deleted),
			zap.Int("older_than_days", deviceTokenRetentionDays))
	}
}
