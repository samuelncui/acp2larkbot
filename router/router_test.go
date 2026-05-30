package router

import (
	"testing"

	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/lark"
)

func TestShouldHandleUsesGroupMentionConfig(t *testing.T) {
	r := New(&config.Config{Lark: config.LarkConfig{AppID: "cli_test_app", BotOpenID: "ou_bot", Trigger: config.LarkTrigger{RequireMentionInGroup: true}}})
	chat := config.ChatConfig{ID: "oc_group_1", Type: config.ChatTypeGroup}

	if r.ShouldHandle(lark.Event{MessageType: lark.MessageText}, chat) {
		t.Fatal("expected group message without @bot to be ignored")
	}
	if !r.ShouldHandle(lark.Event{MessageType: lark.MessageText, Mentions: []string{"ou_bot"}}, chat) {
		t.Fatal("expected group message with @bot to be handled")
	}
}

func TestShouldHandleUsesChatMentionOverride(t *testing.T) {
	r := New(&config.Config{Lark: config.LarkConfig{AppID: "cli_test_app", Trigger: config.LarkTrigger{RequireMentionInGroup: true}}})
	requireMention := false
	chat := config.ChatConfig{ID: "oc_group_1", Type: config.ChatTypeGroup, RequireMention: &requireMention}

	if !r.ShouldHandle(lark.Event{MessageType: lark.MessageText}, chat) {
		t.Fatal("expected chat override to allow group message without @bot")
	}
}

func TestShouldHandleUsesGlobalMentionConfig(t *testing.T) {
	r := New(&config.Config{Lark: config.LarkConfig{AppID: "cli_test_app", RequireMention: true}})
	chat := config.ChatConfig{ID: "oc_direct_1", Type: config.ChatTypeDirect}

	if r.ShouldHandle(lark.Event{MessageType: lark.MessageText}, chat) {
		t.Fatal("expected direct message without @bot to be ignored when global require_mention is true")
	}
	if !r.ShouldHandle(lark.Event{MessageType: lark.MessageText, Mentions: []string{"cli_test_app"}}, chat) {
		t.Fatal("expected direct message with @bot to be handled")
	}
}
