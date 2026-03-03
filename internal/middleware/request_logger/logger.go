package requestlogger

import (
	"context"
	"database/sql"
	"log"
	"time"
)

type LogEntry struct {
	UserID     *int64
	MerchantID *int64
	Method     string
	URL        string
	Payload    []byte
	StatusCode int
	IP         string
}

type Logger struct {
	db    *sql.DB
	queue chan LogEntry
}

func NewLogger(db *sql.DB, bufferSize int) *Logger {
	l := &Logger{
		db:    db,
		queue: make(chan LogEntry, bufferSize),
	}

	go l.worker()

	return l
}

func (l *Logger) Log(entry LogEntry) {
	select {
	case l.queue <- entry:
	default:
		// channel plein → on drop pour éviter blocage
		log.Println("request log dropped (buffer full)")
	}
}

func (l *Logger) worker() {
	for entry := range l.queue {

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		_, err := l.db.ExecContext(ctx, `
			INSERT INTO api_request_logs
			(user_id, merchant_id, method, url, payload, status_code, ip)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			entry.UserID,
			entry.MerchantID,
			entry.Method,
			entry.URL,
			entry.Payload,
			entry.StatusCode,
			entry.IP,
		)

		cancel()

		if err != nil {
			log.Println("failed to insert request log:", err)
		}
	}
}
