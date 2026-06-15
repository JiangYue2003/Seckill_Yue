package model

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm/logger"
)

type gormLogWriter struct{}

func (g gormLogWriter) Printf(format string, args ...interface{}) {
	logx.Infof("[gorm] "+format, args...)
}

func newGormLogger() logger.Interface {
	return logger.New(
		gormLogWriter{},
		logger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}
