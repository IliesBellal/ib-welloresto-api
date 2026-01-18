package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New() *zap.Logger {
	env := os.Getenv("ENV")

	cfg := zap.NewProductionConfig()

	if env == "local" {
		cfg = zap.NewDevelopmentConfig()
	}

	level := zapcore.InfoLevel
	if err := level.Set(os.Getenv("LOG_LEVEL")); err == nil {
		cfg.Level = zap.NewAtomicLevelAt(level)
	}

	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logger, _ := cfg.Build()
	return logger
}
