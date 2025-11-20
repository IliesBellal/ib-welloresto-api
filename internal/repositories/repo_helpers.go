package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// --- Helper Functions ---

func nullStringToPtr(s sql.NullString) *string {
	if s.Valid {
		return &s.String
	}
	return nil
}

func nullInt64ToPtr(i sql.NullInt64) *int64 {
	if i.Valid {
		return &i.Int64
	}
	return nil
}

func nullFloat64ToPtr(f sql.NullFloat64) *float64 {
	if f.Valid {
		return &f.Float64
	}
	return nil
}

func nullFloat64Ptr(f sql.NullFloat64) *float64 {
	if f.Valid {
		return &f.Float64
	}
	return nil
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if t.Valid {
		return &t.Time
	}
	return nil
}

func nilIfZeroTime(t sql.NullTime) *time.Time {
	if t.Valid && !t.Time.IsZero() {
		return &t.Time
	}
	return nil
}

func nilIfNullInt64Discount(discountID sql.NullInt64, price int64) *int64 {
	if discountID.Valid {
		return &price
	}
	return nil
}

func FormatQueryForLog(query string, args ...interface{}) string {
	out := query
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			out = fmt.Sprintf("%s [%q]", out, v)
		case *string:
			if v == nil {
				out = fmt.Sprintf("%s [NULL]", out)
			} else {
				out = fmt.Sprintf("%s [%q]", out, *v)
			}
		default:
			out = fmt.Sprintf("%s [%v]", out, v)
		}
	}
	return out
}

func debugQuery(ctx context.Context, db *sql.DB, log *zap.Logger, step, query string, args ...interface{}) (*sql.Rows, error) {
	log.Info("SQL START",
		zap.String("step", step),
		zap.String("query", query),
		zap.Any("args", args),
	)

	t0 := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	elapsed := time.Since(t0)

	if err != nil {
		log.Error("SQL ERROR",
			zap.String("step", step),
			zap.Error(err),
			zap.Duration("elapsed", elapsed),
			zap.Bool("ctx_done", ctx.Err() != nil),
			zap.String("ctx_error", fmt.Sprint(ctx.Err())),
		)

		// La clé ultime :
		if ctx.Err() != nil {
			log.Error("CTX CANCEL SOURCE", zap.Stack("stack"))
		}

		return nil, err
	}

	log.Info("SQL OK",
		zap.String("step", step),
		zap.Duration("elapsed", elapsed),
	)

	return rows, nil
}
