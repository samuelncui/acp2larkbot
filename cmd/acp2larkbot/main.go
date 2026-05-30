package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/app"
	"github.com/samuelncui/acp2larkbot/config"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config")
	validateOnly := flag.Bool("validate-only", false, "load and validate config, then exit")
	flag.Parse()

	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		logrus.WithError(err).Error("load config failed")
		os.Exit(1)
	}
	if *validateOnly {
		fmt.Println("config ok")
		return
	}

	closeLog, err := setupLogger(cfg.Log)
	if err != nil {
		logrus.WithError(err).Error("setup logger failed")
		os.Exit(1)
	}
	defer closeLog()
	log.SetOutput(logrus.StandardLogger().Writer())
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
