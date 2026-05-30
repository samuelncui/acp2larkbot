package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/lark"
	"github.com/samuelncui/acp2larkbot/router"
	"github.com/samuelncui/acp2larkbot/state"
)

type fakeStore struct{}

func (s *fakeStore) Close() error { return nil }
func (s *fakeStore) GetSession(ctx context.Context, key string) (*state.SessionRecord, error) {
	return nil, nil
}
func (s *fakeStore) PutSession(ctx context.Context, rec state.SessionRecord) error { return nil }
func (s *fakeStore) DeleteSession(ctx context.Context, key string) error           { return nil }
func (s *fakeStore) CleanupSessions(ctx context.Context, now time.Time) error      { return nil }
func (s *fakeStore) Cleanup(ctx context.Context, now time.Time, unknownChatInterval time.Duration) error {
	return nil
}
func (s *fakeStore) MarkSeen(ctx context.Context, key string, ttl time.Duration, now time.Time) (bool, error) {
	return false, nil
}
func (s *fakeStore) AllowUnknownChatReply(ctx context.Context, chatID string, interval time.Duration, now time.Time) (bool, error) {
	return true, nil
}

func TestResolveChatAllowsKnownDirectMessage(t *testing.T) {
	cfg := testConfig()
	a := &App{cfg: cfg, router: router.New(cfg), authz: router.NewAuthz(cfg)}

	chat, ok := a.resolveChat(lark.Event{ChatID: "oc_dm_1", ChatType: "p2p", SenderID: "ou_allowed"})
	if !ok {
		t.Fatal("expected known direct sender to resolve synthetic direct chat")
	}
	if chat.Type != config.ChatTypeDirect {
		t.Fatalf("unexpected chat type %q", chat.Type)
	}
	if chat.Agent != cfg.ACP.DefaultAgent {
		t.Fatalf("unexpected agent %q", chat.Agent)
	}
	if chat.CWD != cfg.ACP.Agents[cfg.ACP.DefaultAgent].CWD {
		t.Fatalf("unexpected cwd %q", chat.CWD)
	}
	if chat.Session.Strategy != config.SessionAutoCreate {
		t.Fatalf("unexpected session strategy %q", chat.Session.Strategy)
	}
}

func TestHandleUnknownChatRepliesForMentionedUnknownGroup(t *testing.T) {
	cfg := testConfig()
	gw := lark.NewFakeGateway()
	a := &App{cfg: cfg, gw: gw, store: &fakeStore{}, authz: router.NewAuthz(cfg)}

	err := a.handleUnknownChat(context.Background(), lark.Event{
		ChatID:    "oc_group_1",
		ChatType:  config.ChatTypeGroup,
		SenderID:  "ou_allowed",
		Mentions:  []string{cfg.Lark.AppID},
		MessageID: "msg_1",
	})
	if err != nil {
		t.Fatalf("handleUnknownChat returned error: %v", err)
	}
	message := gw.Message("msg_1")
	if message == "" {
		t.Fatal("expected a reminder message to be sent")
	}
	if want := "This group chat has not enabled this bot yet."; !strings.Contains(message, want) {
		t.Fatalf("unexpected message %q", message)
	}
}

func TestHandleUnknownChatRepliesForUnauthorizedDirectMessage(t *testing.T) {
	cfg := testConfig()
	gw := lark.NewFakeGateway()
	a := &App{cfg: cfg, gw: gw, store: &fakeStore{}, authz: router.NewAuthz(cfg)}

	err := a.handleUnknownChat(context.Background(), lark.Event{ChatID: "oc_dm_2", ChatType: "p2p", SenderID: "ou_unknown"})
	if err != nil {
		t.Fatalf("handleUnknownChat returned error: %v", err)
	}
	message := gw.Message("msg_1")
	if message == "" {
		t.Fatal("expected a reminder message to be sent")
	}
	if want := "authz.access.users"; !strings.Contains(message, want) {
		t.Fatalf("expected reminder to mention %q, got %q", want, message)
	}
	if want := "ou_unknown"; !strings.Contains(message, want) {
		t.Fatalf("expected reminder to mention sender id %q, got %q", want, message)
	}
}

func TestHandleUnknownChatIgnoresUnmentionedUnknownGroup(t *testing.T) {
	cfg := testConfig()
	gw := lark.NewFakeGateway()
	a := &App{cfg: cfg, gw: gw, store: &fakeStore{}, authz: router.NewAuthz(cfg)}

	err := a.handleUnknownChat(context.Background(), lark.Event{ChatID: "oc_group_2", ChatType: config.ChatTypeGroup, SenderID: "ou_allowed"})
	if err != nil {
		t.Fatalf("handleUnknownChat returned error: %v", err)
	}
	if got := gw.Message("msg_1"); got != "" {
		t.Fatalf("expected no reminder message, got %q", got)
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Lark: config.LarkConfig{
			AppID: "cli_test_app",
		},
		Authz: config.AuthzConfig{
			Access: config.AccessConfig{
				Default: "deny",
				Users:   []config.UserRole{{ID: "ou_allowed", Role: "user"}},
			},
		},
		UnknownChat: config.UnknownChatConfig{
			Behavior:          config.UnknownIgnore,
			RateLimitInterval: config.Duration{Duration: time.Minute},
		},
		ACP: config.ACPConfig{
			DefaultAgent: "default",
			Agents: map[string]config.AgentConfig{
				"default": {Type: config.AgentTypeCmd, CWD: "/tmp"},
			},
		},
	}
}
