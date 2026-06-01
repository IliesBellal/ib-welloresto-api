package logger

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"

	"go.uber.org/zap"
)

func DBStatsFields(db *sql.DB) []zap.Field {
	if db == nil {
		return []zap.Field{zap.Bool("db_stats_available", false)}
	}

	stats := db.Stats()

	return []zap.Field{
		zap.Bool("db_stats_available", true),
		zap.Int("db_max_open_connections", stats.MaxOpenConnections),
		zap.Int("db_open_connections", stats.OpenConnections),
		zap.Int("db_in_use", stats.InUse),
		zap.Int("db_idle", stats.Idle),
		zap.Int64("db_wait_count", stats.WaitCount),
		zap.Duration("db_wait_duration", stats.WaitDuration),
		zap.Int64("db_max_idle_closed", stats.MaxIdleClosed),
		zap.Int64("db_max_idle_time_closed", stats.MaxIdleTimeClosed),
		zap.Int64("db_max_lifetime_closed", stats.MaxLifetimeClosed),
	}
}

func DBErrorKind(err error) string {
	if err == nil {
		return ""
	}

	lower := strings.ToLower(err.Error())

	switch {
	case errors.Is(err, context.Canceled) || strings.Contains(lower, "context canceled"):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "context deadline exceeded"):
		return "deadline_exceeded"
	case errors.Is(err, sql.ErrConnDone):
		return "sql_connection_done"
	case strings.Contains(lower, "broken pipe"):
		return "broken_pipe"
	case strings.Contains(lower, "connection reset by peer"):
		return "connection_reset_by_peer"
	case strings.Contains(lower, "bad connection") || strings.Contains(lower, "invalid connection"):
		return "bad_connection"
	case strings.Contains(lower, "too many connections"):
		return "too_many_connections"
	case strings.Contains(lower, "connection refused"):
		return "connection_refused"
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "temporary failure in name resolution"):
		return "dns_error"
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}

	return "db_error"
}

func IsInfrastructureDBError(err error) bool {
	switch DBErrorKind(err) {
	case "deadline_exceeded", "sql_connection_done", "broken_pipe", "connection_reset_by_peer", "bad_connection", "too_many_connections", "connection_refused", "dns_error", "network_timeout", "network_error":
		return true
	default:
		return false
	}
}

func DBErrorFields(err error) []zap.Field {
	if err == nil {
		return nil
	}

	fields := []zap.Field{
		zap.String("db_error_kind", DBErrorKind(err)),
		zap.Bool("db_error_infrastructure", IsInfrastructureDBError(err)),
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		fields = append(fields,
			zap.Bool("db_error_timeout", netErr.Timeout()),
			zap.Bool("db_error_temporary", netErr.Temporary()),
		)
	}

	return fields
}

func DBFailureFields(db *sql.DB, err error) []zap.Field {
	fields := DBStatsFields(db)
	fields = append(fields, DBErrorFields(err)...)
	return fields
}