package logger

import "go.uber.org/zap"

func New() *zap.Logger {
	log, _ := zap.NewProduction()
	return log
}
