package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/app"
	"github.com/samuelncui/acp2larkbot/config"
)

func runCmd(args []string) {
	fs := newFlagSet("run")
	configPath := fs.StringP("config", "c", "config.yaml", "path to YAML config")
	debug := fs.BoolP("debug", "d", false, "enable debug logging (overrides log.level)")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		logrus.WithError(err).Fatal("parse flags failed")
	}

	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		logrus.WithError(err).Error("load config failed")
		os.Exit(1)
	}
	if *debug {
		cfg.Log.Level = "debug"
	}

	closeLog, err := setupLogger(cfg.Log)
	if err != nil {
		logrus.WithError(err).Error("setup logger failed")
		os.Exit(1)
	}
	defer closeLog()
	logrus.WithFields(logrus.Fields{"file_enabled": cfg.Log.File.Enabled, "file_path": cfg.Log.File.Path}).Info("logger configured")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.New(cfg)
	if err != nil {
		logrus.WithError(err).Error("create app failed")
		os.Exit(1)
	}
	defer func() {
		if err := a.Close(); err != nil {
			logrus.WithError(err).Error("close app failed")
		}
	}()

	if err := a.Run(ctx); err != nil {
		logrus.WithError(err).Error("run app failed")
		os.Exit(1)
	}
}
