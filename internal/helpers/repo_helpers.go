package helpers

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// --- Helper Functions ---

func NullStringToPtr(s sql.NullString) *string {
	if s.Valid {
		return &s.String
	}
	return nil
}

func nullInt64ToString(last sql.NullInt64) (string, error) {
	value := last.Int64 // si last.Valid = false → 0
	return strconv.FormatInt(value+1, 10), nil
}

func NullInt64ToPtr(i sql.NullInt64) *int64 {
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

func NullFloat64Ptr(f sql.NullFloat64) *float64 {
	if f.Valid {
		return &f.Float64
	}
	return nil
}

func NullTimePtr(t sql.NullTime) *time.Time {
	if t.Valid {
		return &t.Time
	}
	return nil
}

func NullTimeToNullUnixInt(nt sql.NullTime) *int {
	if !nt.Valid {
		return nil
	}

	v := int(nt.Time.UTC().Unix())
	return &v
}

func nilIfZeroTime(t sql.NullTime) *time.Time {
	if t.Valid && !t.Time.IsZero() {
		return &t.Time
	}
	return nil
}

func NilIfNullInt64Discount(discountID sql.NullInt64, price int64) *int64 {
	if discountID.Valid {
		return &price
	}
	return nil
}

func RoundToNearestInt(val float64) int {
	return int(math.Round(val))
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

func DumpRawRow(rows *sql.Rows) []interface{} {
	cols, _ := rows.Columns()
	raw := make([]interface{}, len(cols))
	rawPtrs := make([]interface{}, len(cols))

	for i := range raw {
		rawPtrs[i] = &raw[i]
	}

	// Lire en brut, sans types
	rows.Scan(rawPtrs...)
	return raw
}

func SafeString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func Int64ToStringPtr(v int64) *string {
	s := strconv.FormatInt(v, 10)
	return &s
}

func DebugSQL(log *zap.Logger, query string, args []interface{}) {
	log.Info("SQL EXEC",
		zap.String("query", query),
		zap.Any("args", args),
	)
}

func IntToInt64Ptr(v int) *int64 {
	i := int64(v)
	return &i
}

func IntPtrToInt64Ptr(v *int) *int64 {
	if v == nil {
		return nil
	}
	i := int64(*v)
	return &i
}
