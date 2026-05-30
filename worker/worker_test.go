package worker

import (
	"errors"
	"testing"

	"github.com/samuelncui/acp2larkbot/config"
)

func TestShouldRefreshSession(t *testing.T) {
	chat := config.ChatConfig{Session: config.SessionPolicy{Strategy: config.SessionAutoCreate}}
	if !shouldRefreshSession(chat, errors.New("session/prompt failed, code=-32602 message=\"Invalid params\"")) {
		t.Fatal("expected Invalid params on auto_create session to trigger refresh")
	}
	if shouldRefreshSession(config.ChatConfig{Session: config.SessionPolicy{Strategy: config.SessionEphemeral}}, errors.New("session/prompt failed, code=-32602 message=\"Invalid params\"")) {
		t.Fatal("did not expect non-auto_create session to refresh")
	}
	if shouldRefreshSession(chat, errors.New("other error")) {
		t.Fatal("did not expect unrelated error to refresh session")
	}
}
