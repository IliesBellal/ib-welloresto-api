package requestlogger

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
	appLogger "welloresto-api/internal/logger"

	"go.uber.org/zap"
)

type Logger struct {
	db            *sql.DB
	log           *zap.Logger
	queue         chan LogEntry
	batchSize     int
	flushInterval time.Duration
	slowFlush     time.Duration
	lastSuccessAt time.Time
	failureStreak int
}

func NewLogger(db *sql.DB, log *zap.Logger, bufferSize int) *Logger {
	if log == nil {
		log = zap.L()
	}

	l := &Logger{
		db:            db,
		log:           log.Named("request_logger"),
		queue:         make(chan LogEntry, bufferSize),
		batchSize:     50,              // Insérer par groupe de 50 max
		flushInterval: 1 * time.Second, // Ou insérer au moins toutes les secondes
		slowFlush:     2 * time.Second,
	}

	go l.worker()

	return l
}

func (l *Logger) Log(entry LogEntry) {
	select {
	case l.queue <- entry:
	default:
		fields := []zap.Field{
			zap.String("method", entry.Method),
			zap.String("url", entry.URL),
			zap.Int("status_code", entry.StatusCode),
		}
		fields = append(fields, l.queueFields()...)
		l.log.Warn("request logger queue full; dropping log entry", fields...)
	}
}

func (l *Logger) worker() {
	var batch []LogEntry
	ticker := time.NewTicker(l.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case entry := <-l.queue:
			batch = append(batch, entry)
			if len(batch) >= l.batchSize {
				l.flush(batch)
				batch = make([]LogEntry, 0, l.batchSize) // Reset du slice
			}
		case <-ticker.C:
			if len(batch) > 0 {
				l.flush(batch)
				batch = make([]LogEntry, 0, l.batchSize) // Reset
			}
		}
	}
}

func (l *Logger) queueFields() []zap.Field {
	usage := 0.0
	if cap(l.queue) > 0 {
		usage = float64(len(l.queue)) / float64(cap(l.queue))
	}

	return []zap.Field{
		zap.Int("request_log_queue_length", len(l.queue)),
		zap.Int("request_log_queue_capacity", cap(l.queue)),
		zap.Float64("request_log_queue_usage", usage),
	}
}

func (l *Logger) healthFields(now time.Time) []zap.Field {
	fields := l.queueFields()
	if !l.lastSuccessAt.IsZero() {
		fields = append(fields,
			zap.Time("request_log_last_success_at", l.lastSuccessAt),
			zap.Duration("request_log_time_since_last_success", now.Sub(l.lastSuccessAt)),
		)
	}
	return fields
}

func (l *Logger) flush(batch []LogEntry) {
	if len(batch) == 0 {
		return
	}
	if l.db == nil {
		fields := []zap.Field{zap.Int("request_log_flush_batch_size", len(batch))}
		fields = append(fields, l.queueFields()...)
		l.log.Error("request log flush skipped: database unavailable", fields...)
		return
	}

	startedAt := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Construction de la requête bulk insert
	valueStrings := make([]string, 0, len(batch))
	valueArgs := make([]interface{}, 0, len(batch)*7) // 7 colonnes

	for _, entry := range batch {
		valueStrings = append(valueStrings, "(?, ?, ?, ?, ?, ?, ?)")
		valueArgs = append(valueArgs,
			entry.UserID, entry.MerchantID, entry.Method,
			entry.URL, entry.Payload, entry.StatusCode, entry.IP,
		)
	}

	stmt := dbx.Rebind(`INSERT INTO api_request_logs
		(user_id, merchant_id, method, url, payload, status_code, ip) VALUES ` +
		strings.Join(valueStrings, ","))

	_, err := l.db.ExecContext(ctx, stmt, valueArgs...)
	finishedAt := time.Now()
	fields := []zap.Field{
		zap.Int("request_log_flush_batch_size", len(batch)),
		zap.Duration("request_log_flush_timeout", 5*time.Second),
		zap.Duration("request_log_flush_duration", finishedAt.Sub(startedAt)),
	}
	fields = append(fields, l.healthFields(finishedAt)...)
	fields = append(fields, appLogger.DBStatsFields(l.db)...)
	if err != nil {
		l.failureStreak++
		fields = append(fields,
			zap.Error(err),
			zap.Int("request_log_consecutive_failures", l.failureStreak),
		)
		fields = append(fields, appLogger.DBErrorFields(err)...)
		if l.failureStreak >= 3 {
			fields = append(fields, zap.Bool("request_log_degraded_detected", true))
		}
		l.log.Error("request log flush failed", fields...)
		return
	}

	previousFailures := l.failureStreak
	l.failureStreak = 0
	l.lastSuccessAt = finishedAt

	if previousFailures > 0 {
		fields = append(fields,
			zap.Int("request_log_previous_failures", previousFailures),
			zap.Time("request_log_recovered_at", finishedAt),
		)
		l.log.Warn("request log flush recovered", fields...)
		return
	}

	if finishedAt.Sub(startedAt) >= l.slowFlush {
		l.log.Warn("request log flush slow", fields...)
	}
}
