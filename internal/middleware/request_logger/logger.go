package requestlogger

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"
)

type Logger struct {
	db            *sql.DB
	queue         chan LogEntry
	batchSize     int
	flushInterval time.Duration
}

func NewLogger(db *sql.DB, bufferSize int) *Logger {
	l := &Logger{
		db:            db,
		queue:         make(chan LogEntry, bufferSize),
		batchSize:     50,              // Insérer par groupe de 50 max
		flushInterval: 1 * time.Second, // Ou insérer au moins toutes les secondes
	}

	go l.worker()

	return l
}

func (l *Logger) Log(entry LogEntry) {
	select {
	case l.queue <- entry:
	default:
		log.Println("request log dropped (buffer full)")
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

func (l *Logger) flush(batch []LogEntry) {
	if len(batch) == 0 {
		return
	}

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

	stmt := `INSERT INTO api_request_logs 
		(user_id, merchant_id, method, url, payload, status_code, ip) VALUES ` +
		strings.Join(valueStrings, ",")

	_, err := l.db.ExecContext(ctx, stmt, valueArgs...)
	if err != nil {
		log.Println("failed to flush request logs:", err)
	}
}
