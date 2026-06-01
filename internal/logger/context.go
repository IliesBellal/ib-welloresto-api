package logger

import (
	"context"

	"go.uber.org/zap"
)

type ctxKey string

const (
	RequestIDKey  ctxKey = "request_id"
	LoggerKey     ctxKey = "logger"
	UserIDKey     ctxKey = "user_id"
	MerchantIDKey ctxKey = "merchant_id"
)

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIDKey, id)
}

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(RequestIDKey).(string); ok {
		return v
	}
	return ""
}
func WithLogger(ctx context.Context, log *zap.Logger) context.Context {
	return context.WithValue(ctx, LoggerKey, log)
}
func FromContext(ctx context.Context) *zap.Logger {
	if log, ok := ctx.Value(LoggerKey).(*zap.Logger); ok && log != nil {
		return log
	}
	return zap.L() // fallback
}
