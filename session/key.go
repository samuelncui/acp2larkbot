package session

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/samuelncui/acp2larkbot/config"
)

func Key(chat config.ChatConfig, senderID string, threadID string) string {
	parts := []string{chat.Agent, chat.CWD, chat.ID, chat.Session.Scope, chat.Session.ID, chat.Session.Prefix}
	if chat.Session.Scope == config.SessionScopeSender {
		parts = append(parts, senderID)
	}
	if chat.Session.Scope == config.SessionScopeThread {
		parts = append(parts, senderID, threadID)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func Short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
