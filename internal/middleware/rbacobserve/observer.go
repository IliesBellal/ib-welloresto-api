// Package rbacobserve is the RBAC lot 2 observation phase: RequirePermission
// records every decision it makes (granted or not) so a permission/role
// matrix can be built from real traffic before any users_rights.role_id is
// ever assigned.
//
// The write path is deliberately modeled on
// internal/middleware/request_logger/logger.go, which already solves the
// same problem for api_request_logs: buffered channel, batched flush on a
// timer or when the batch fills up, drop-on-full instead of blocking, best
// effort (a write failure is logged, never propagated to the request). An
// observation that never influences the request/response is not worth
// reimplementing that machinery for.
//
// Convention — RequireAdmin observations: RequireAdmin gates on IsAdmin
// (« détient tous les droits »), not on a catalog permission.Key, so it has
// no real permission_key to record. It still observes, using the literal
// string "__admin__" as Observation.PermissionKey (see
// internal/middleware/require_permission.go's adminObservationKey) — these
// are exactly the routes a future lot may want to loosen from "admin only"
// to a specific permission, so they need to show up in the same traffic data
// as everything else instead of being invisible to it. access_observation
// has no FK on permission_key (see migrations/done/098_access_observation.up.sql),
// so "__admin__" needs no catalog entry.
package rbacobserve

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"

	"go.uber.org/zap"
)

type Observer struct {
	db            *dbx.DB
	log           *zap.Logger
	queue         chan Observation
	batchSize     int
	flushInterval time.Duration
	slowFlush     time.Duration
	lastSuccessAt time.Time
	failureStreak int
}

// NewObserver starts the background worker and returns the Observer.
// bufferSize is the channel capacity — once full, new observations are
// dropped (logged at Warn), never blocking the request that triggered them.
func NewObserver(db *dbx.DB, log *zap.Logger, bufferSize int) *Observer {
	if log == nil {
		log = zap.L()
	}

	o := &Observer{
		db:            db,
		log:           log.Named("rbac_observer"),
		queue:         make(chan Observation, bufferSize),
		batchSize:     50,
		flushInterval: 1 * time.Second,
		slowFlush:     2 * time.Second,
	}

	go o.worker()

	return o
}

// Observe enqueues one decision. Never blocks: if the queue is full, the
// observation is dropped and a warning is logged — an RBAC decision must
// never slow down or fail the request that produced it.
func (o *Observer) Observe(obs Observation) {
	select {
	case o.queue <- obs:
	default:
		o.log.Warn("rbac observation queue full; dropping observation",
			zap.String("merchant_id", obs.MerchantID),
			zap.String("permission_key", obs.PermissionKey),
			zap.String("route", obs.Route),
			zap.Bool("granted", obs.Granted),
		)
	}
}

func (o *Observer) worker() {
	var batch []Observation
	ticker := time.NewTicker(o.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case obs := <-o.queue:
			batch = append(batch, obs)
			if len(batch) >= o.batchSize {
				o.flush(batch)
				batch = make([]Observation, 0, o.batchSize)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				o.flush(batch)
				batch = make([]Observation, 0, o.batchSize)
			}
		}
	}
}

// flush upserts a batch into access_observation: a first sighting of a
// (merchant, user, permission, route) tuple inserts a fresh row (hits=1); a
// repeat sighting bumps hits and last_seen, and overwrites granted with the
// latest decision — the row reflects the current answer, not the history of
// every answer ever given for that tuple.
func (o *Observer) flush(batch []Observation) {
	if len(batch) == 0 {
		return
	}
	if o.db == nil {
		o.log.Error("rbac observation flush skipped: database unavailable", zap.Int("batch_size", len(batch)))
		return
	}

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	utcNow := dbx.UTCNow()
	valueStrings := make([]string, 0, len(batch))
	valueArgs := make([]interface{}, 0, len(batch)*5)
	for _, obs := range batch {
		valueStrings = append(valueStrings, fmt.Sprintf("(?, ?, ?, ?, ?, %s, %s, 1)", utcNow, utcNow))
		valueArgs = append(valueArgs, obs.MerchantID, obs.UserID, obs.PermissionKey, obs.Route, obs.Granted)
	}

	insert := `INSERT INTO access_observation
		(merchant_id, user_id, permission_key, route, granted, first_seen, last_seen, hits) VALUES ` +
		strings.Join(valueStrings, ",")

	var stmt string
	if dbx.ActiveDialect() == dbx.Postgres {
		stmt = insert + `
			ON CONFLICT (merchant_id, user_id, permission_key, route) DO UPDATE SET
				granted = EXCLUDED.granted,
				last_seen = ` + utcNow + `,
				hits = access_observation.hits + 1`
	} else {
		stmt = insert + `
			ON DUPLICATE KEY UPDATE
				granted = VALUES(granted),
				last_seen = ` + utcNow + `,
				hits = hits + 1`
	}

	_, err := o.db.ExecContext(ctx, stmt, valueArgs...)
	finishedAt := time.Now()

	fields := []zap.Field{
		zap.Int("rbac_observation_flush_batch_size", len(batch)),
		zap.Duration("rbac_observation_flush_duration", finishedAt.Sub(startedAt)),
	}
	if err != nil {
		o.failureStreak++
		fields = append(fields, zap.Error(err), zap.Int("rbac_observation_consecutive_failures", o.failureStreak))
		o.log.Error("rbac observation flush failed", fields...)
		return
	}

	previousFailures := o.failureStreak
	o.failureStreak = 0
	o.lastSuccessAt = finishedAt

	if previousFailures > 0 {
		o.log.Warn("rbac observation flush recovered", append(fields, zap.Int("previous_failures", previousFailures))...)
		return
	}
	if finishedAt.Sub(startedAt) >= o.slowFlush {
		o.log.Warn("rbac observation flush slow", fields...)
	}
}
