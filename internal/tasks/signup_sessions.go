package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

// signupSessionRetentionDays is how long signup_sessions rows are kept
// after creation — well beyond the 24h idempotent-replay window
// (signup.SignupSessionTTL), which is enforced at read time (expires_at),
// not by this purge. 30 days matches the brief's own retention figure and
// leaves enough margin for support to look up a recent signup attempt.
const signupSessionRetentionDays = 30

// CleanupExpiredSignupSessions deletes old signup_sessions rows. Purely
// housekeeping: a session past its replay window is already unusable for
// idempotent replay regardless of whether the row still exists — see
// internal/modules/signup/handler.go, which checks state, not expires_at,
// once a row is found. Same shape as CleanupExpiredPasswordResets
// (internal/tasks/password_resets.go) — see docs/PASSWORD_RESET.md for the
// pattern this follows.
func (tm *TasksManager) CleanupExpiredSignupSessions() {
	if tm.DB == nil {
		tm.logWarn("[CRON] CleanupExpiredSignupSessions: base indisponible, tâche ignorée")
		return
	}

	ctx := context.Background()
	db := dbx.GetDB(ctx, tm.DB)

	cutoff := time.Now().UTC().AddDate(0, 0, -signupSessionRetentionDays)

	res, err := db.ExecContext(ctx, `DELETE FROM signup_sessions WHERE created_at < ?`, cutoff)
	if err != nil {
		tm.logError("[CRON] CleanupExpiredSignupSessions: échec", zap.Error(err))
		return
	}

	deleted, err := res.RowsAffected()
	if err != nil {
		tm.logInfo("[CRON] CleanupExpiredSignupSessions: terminé (nombre de lignes indisponible)",
			zap.Int("older_than_days", signupSessionRetentionDays))
		return
	}

	tm.logInfo("[CRON] CleanupExpiredSignupSessions: terminé",
		zap.Int64("deleted", deleted),
		zap.Int("older_than_days", signupSessionRetentionDays))
}
