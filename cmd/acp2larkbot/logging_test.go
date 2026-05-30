package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/config"
)

func TestSetupLoggerWritesToRotatedLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	closeLog, err := setupLogger(config.LogConfig{
		Level: "info",
		File: config.LogFile{
			Enabled:    true,
			Path:       path,
			MaxSizeMB:  1,
			MaxBackups: 2,
		},
	})
	if err != nil {
		t.Fatalf("setupLogger returned error: %v", err)
	}
	logrus.Info("hello rotated file")
	closeLog()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	if !strings.Contains(string(data), "hello rotated file") {
		t.Fatalf("expected log file to contain message, got %q", string(data))
	}
}
