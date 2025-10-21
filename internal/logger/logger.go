package logger

import (
	"go.uber.org/zap"
)

var Logger *zap.Logger

func InitLogger() (*zap.Logger, error) {
	var err error
	Logger, err = zap.NewProduction()
	defer Logger.Sync()
	if err != nil {
		return nil, err
	}
	return Logger, nil
}
