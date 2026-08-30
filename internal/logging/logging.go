package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(level, format string) (*zap.Logger, error) {
	var cfg zap.Config
	if format == "console" {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}

	lvl := zapcore.InfoLevel
	if err := lvl.UnmarshalText([]byte(level)); err == nil {
		cfg.Level = zap.NewAtomicLevelAt(lvl)
	}

	if format == "console" {
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	logger, err := cfg.Build(zap.AddCallerSkip(0))
	if err != nil {
		return nil, err
	}

	return logger, nil
}

func NewNop() *zap.Logger {
	return zap.NewNop()
}

func Must(level, format string) *zap.Logger {
	l, err := New(level, format)
	if err != nil {
		panic(err)
	}
	return l
}
