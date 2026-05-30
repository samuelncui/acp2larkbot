package lark

import (
	"testing"
	"time"

	"github.com/samuelncui/acp2larkbot/config"
)

func TestSelfFilter(t *testing.T) {
	f := NewSelfFilter(config.LarkConfig{
		AppID:   "app",
		Ignore:  config.LarkIgnore{SenderTypes: []string{SenderBot}, SelfAppID: "app", MessageIDTTL: config.Duration{Duration: time.Hour}, MaxMessageIDs: 10},
		Trigger: config.LarkTrigger{IgnoreUpdateEvents: true, IgnoreCardEvents: true},
	})
	if !f.Ignore(Event{SenderType: SenderBot}) {
		t.Fatal("bot sender should be ignored")
	}
	if !f.Ignore(Event{SenderAppID: "app"}) {
		t.Fatal("self app sender should be ignored")
	}
	f.Remember("msg_self")
	if !f.Ignore(Event{MessageID: "msg_self"}) {
		t.Fatal("remembered message should be ignored")
	}
}

func TestSelfFilterUsesSenderTypesConfig(t *testing.T) {
	f := NewSelfFilter(config.LarkConfig{
		Ignore:  config.LarkIgnore{SenderTypes: []string{"system"}, MessageIDTTL: config.Duration{Duration: time.Hour}, MaxMessageIDs: 10},
		Trigger: config.LarkTrigger{IgnoreUpdateEvents: true, IgnoreCardEvents: true},
	})
	if f.Ignore(Event{SenderType: SenderBot}) {
		t.Fatal("bot sender should not be ignored when it is not configured")
	}
	if !f.Ignore(Event{SenderType: "system"}) {
		t.Fatal("configured sender type should be ignored")
	}
}
