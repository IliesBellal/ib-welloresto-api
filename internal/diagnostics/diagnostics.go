// Package diagnostics enregistre des événements de diagnostic (anomalies à
// comprendre après coup) dans la table diagnostic_events, tous domaines
// confondus : docs/DIAGNOSTIC_EVENTS.md.
//
// Record ne bloque jamais et n'échoue jamais pour l'appelant : l'événement est
// toujours écrit dans les logs applicatifs, puis mis en file pour la base. File
// pleine, base indisponible ou table absente : l'événement reste dans les logs.
package diagnostics

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"

	"go.uber.org/zap"
)

const (
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"

	queueSize    = 500
	writeTimeout = 5 * time.Second
)

// Event est un événement de diagnostic. Kind est la clé stable (minuscules et
// underscores) ; Data porte le contenu propre au type et doit être sérialisable
// en JSON.
type Event struct {
	Kind        string
	Severity    string // défaut : warn
	MerchantID  string
	UserID      string
	SubjectType string
	SubjectID   string
	Data        map[string]any
}

type row struct {
	Event
	requestID string
}

type recorder struct {
	db    *sql.DB
	log   *zap.Logger
	queue chan row
}

var (
	mu      sync.RWMutex
	current *recorder
)

// Init démarre l'écriture en base. Sans appel (tests, outils), Record se
// contente des logs applicatifs.
func Init(db *sql.DB, log *zap.Logger) {
	if log == nil {
		log = zap.NewNop()
	}
	r := &recorder{db: db, log: log, queue: make(chan row, queueSize)}
	mu.Lock()
	current = r
	mu.Unlock()
	if db != nil {
		go r.run()
	}
}

// Record enregistre l'événement. L'identité (établissement, utilisateur) et
// l'identifiant de requête sont lus dans ctx quand l'événement ne les porte pas.
func Record(ctx context.Context, e Event) {
	if e.Severity == "" {
		e.Severity = SeverityWarn
	}
	if e.MerchantID == "" {
		e.MerchantID, _ = ctx.Value(logger.MerchantIDKey).(string)
	}
	if e.UserID == "" {
		e.UserID, _ = ctx.Value(logger.UserIDKey).(string)
	}
	r := row{Event: e, requestID: logger.RequestID(ctx)}

	logger.FromContext(ctx).Warn("diagnostic_event",
		zap.String("kind", e.Kind),
		zap.String("severity", e.Severity),
		zap.String("merchant_id", e.MerchantID),
		zap.String("subject_type", e.SubjectType),
		zap.String("subject_id", e.SubjectID),
		zap.Any("data", e.Data),
	)

	mu.RLock()
	rec := current
	mu.RUnlock()
	if rec == nil || rec.db == nil {
		return
	}
	select {
	case rec.queue <- r:
	default:
		rec.log.Warn("diagnostic_event queue full; event kept in logs only", zap.String("kind", e.Kind))
	}
}

func (r *recorder) run() {
	for e := range r.queue {
		r.write(e)
	}
}

func (r *recorder) write(e row) {
	data, err := json.Marshal(e.Data)
	if err != nil || e.Data == nil {
		data = []byte("{}")
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	_, err = r.db.ExecContext(ctx, dbx.Rebind(`INSERT INTO diagnostic_events
		(kind, severity, merchant_id, user_id, request_id, subject_type, subject_id, data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		e.Kind, e.Severity, nullable(e.MerchantID), nullable(e.UserID), nullable(e.requestID),
		nullable(e.SubjectType), nullable(e.SubjectID), string(data))
	if err != nil {
		// Table absente (migration 182 pas encore appliquée) ou base indisponible :
		// l'événement est déjà dans les logs applicatifs.
		r.log.Warn("diagnostic_event not stored", zap.String("kind", e.Kind), zap.Error(err))
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
