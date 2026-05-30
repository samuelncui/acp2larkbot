package session

import (
	"testing"

	"github.com/samuelncui/acp2larkbot/config"
)

func TestKeyIncludesSender(t *testing.T) {
	chat := config.ChatConfig{ID: "oc", Agent: "a", CWD: "/workspace", Session: config.SessionPolicy{Scope: config.SessionScopeSender, Prefix: "p"}}
	if Key(chat, "u1", "") == Key(chat, "u2", "") {
		t.Fatal("different sender should produce different session keys")
	}
}

func TestKeyChatScopeIgnoresSenderAndThread(t *testing.T) {
	chat := config.ChatConfig{ID: "oc", Agent: "a", CWD: "/workspace", Session: config.SessionPolicy{Scope: config.SessionScopeChat, Prefix: "p"}}
	if Key(chat, "u1", "t1") != Key(chat, "u2", "t2") {
		t.Fatal("chat scope should share session key across senders and threads")
	}
}
