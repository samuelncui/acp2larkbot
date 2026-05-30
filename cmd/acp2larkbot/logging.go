package main

import (
	"io"
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/samuelncui/acp2larkbot/config"
)

func setupLogger(cfg config.LogConfig) (func(), error) {
	logrus.SetLevel(logLevel(cfg.Level))
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	if !cfg.File.Enabled {
		logrus.SetOutput(os.Stderr)
		return func() {}, nil
	}

	rotator := &lumberjack.Logger{
		Filename:   cfg.File.Path,
		MaxSize:    cfg.File.MaxSizeMB,
		MaxBackups: cfg.File.MaxBackups,
		LocalTime:  true,
	}
	logrus.SetOutput(io.MultiWriter(os.Stderr, rotator))
	return func() { _ = rotator.Close() }, nil
}

func logLevel(level string) logrus.Level {
	switch level {
	case "debug":
		return logrus.DebugLevel
	case "warn":
		return logrus.WarnLevel
	case "error":
		return logrus.ErrorLevel
	default:
		return logrus.InfoLevel
	}
}
