package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// passwordResetRetentionDays is how long password_resets rows are kept after
// creation. Well beyond the 30-minute validity of a link: the rows are what the
// per-account rate limit counts (a 1-hour window) and what support reads to
// diagnose "I never got the email". A week is enough for both.
const passwordResetRetentionDays = 7

// CleanupExpiredPasswordResets deletes spent password-reset rows.
//
// Purely housekeeping: an old row is already unusable, since consumption
// requires used_at IS NULL AND expires_at > now(). Keeping the table small also
// keeps the daily DELETE short — it holds the API's only database connection
// while it runs (see internal/database/postgres.go).
//
// See docs/PASSWORD_RESET.md.
func (tm *TasksManager) CleanupExpiredPasswordResets() {
	if tm.DB == nil {
		tm.logWarn("[CRON] CleanupExpiredPasswordResets: base indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)

	// The cutoff is computed in Go rather than in SQL: `INTERVAL '7 days'`
	// (Postgres) and DATE_SUB (MySQL) have no common syntax, a parameter does.
	cutoff := time.Now().UTC().AddDate(0, 0, -passwordResetRetentionDays)

	res, err := db.ExecContext(ctx, `DELETE FROM password_resets WHERE created_at < ?`, cutoff)
	if err != nil {
		tm.logError("[CRON] CleanupExpiredPasswordResets: échec", zap.Error(err))
		return
	}

	deleted, err := res.RowsAffected()
	if err != nil {
		// The delete went through; only the count is unavailable.
		tm.logInfo("[CRON] CleanupExpiredPasswordResets: terminé (nombre de lignes indisponible)",
			zap.Int("older_than_days", passwordResetRetentionDays))
		return
	}

	tm.logInfo("[CRON] CleanupExpiredPasswordResets: terminé",
		zap.Int64("deleted", deleted),
		zap.Int("older_than_days", passwordResetRetentionDays))
}
