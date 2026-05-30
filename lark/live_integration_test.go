package lark

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jinzhu/configor"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/samuelncui/acp2larkbot/config"
)

type liveGatewayTestConfig struct {
	Lark struct {
		AppID     string `yaml:"app_id" env:"ACP2LARKBOT_LARK_APP_ID"`
		AppSecret string `yaml:"app_secret" env:"ACP2LARKBOT_LARK_APP_SECRET"`
		Domain    string `yaml:"domain" env:"ACP2LARKBOT_LARK_DOMAIN"`
	} `yaml:"lark"`
}

func TestLiveGatewayWebsocketConnect(t *testing.T) {
	cfg := loadLiveGatewayTestConfig(t)
	if cfg.Lark.AppID == "" || cfg.Lark.AppSecret == "" {
		t.Skip("set ACP2LARKBOT_LARK_APP_ID and ACP2LARKBOT_LARK_APP_SECRET to run live websocket test")
	}

	opts := []larkws.ClientOption{larkws.WithAutoReconnect(false)}
	if cfg.Lark.Domain != "" {
		opts = append(opts, larkws.WithDomain(cfg.Lark.Domain))
	}
	client := larkws.NewClient(cfg.Lark.AppID, cfg.Lark.AppSecret, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Start(ctx)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("connect live websocket failed: %v", err)
		}
		return
	case <-time.After(5 * time.Second):
		return
	}
}

func TestLiveGatewayTokenAuth(t *testing.T) {
	cfg := loadLiveGatewayTestConfig(t)
	if cfg.Lark.AppID == "" || cfg.Lark.AppSecret == "" {
		t.Skip("set ACP2LARKBOT_LARK_APP_ID and ACP2LARKBOT_LARK_APP_SECRET to run live token test")
	}

	gateway := NewLiveGateway(config.LarkConfig{
		AppID:     cfg.Lark.AppID,
		AppSecret: cfg.Lark.AppSecret,
		Domain:    cfg.Lark.Domain,
	})
	_, err := gateway.SendText(context.Background(), "", "")
	if err == nil {
		t.Fatal("SendText with empty chat id should fail")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected context error while checking live token auth: %v", err)
	}
}

func loadLiveGatewayTestConfig(t *testing.T) liveGatewayTestConfig {
	t.Helper()
	var cfg liveGatewayTestConfig
	if err := configor.New(&configor.Config{ENVPrefix: "ACP2LARKBOT", Silent: true}).Load(&cfg); err != nil {
		t.Fatalf("load live gateway test config failed: %v", err)
	}
	return cfg
}
