package router

import (
	"testing"

	"github.com/samuelncui/acp2larkbot/config"
)

type fakeCommandRuntime struct {
	cancelRequireOwner bool
	cancelAdmin        bool
	statusRedact       bool
	resetDecision      Decision
}

func (r *fakeCommandRuntime) CancelCurrent(chatID string, senderID string, requireOwnerOrAdmin bool, admin bool) error {
	r.cancelRequireOwner = requireOwnerOrAdmin
	r.cancelAdmin = admin
	return nil
}

func (r *fakeCommandRuntime) Status(chatID string, redact bool) string {
	r.statusRedact = redact
	return "status"
}

func (r *fakeCommandRuntime) Reset(decision Decision) error {
	r.resetDecision = decision
	return nil
}

func TestHandleCommandUsesCancelOwnerRule(t *testing.T) {
	rt := &fakeCommandRuntime{}
	_, err := HandleCommand(rt, Command{Name: config.CommandCancel}, Decision{Chat: config.ChatConfig{ID: "oc_1"}}, config.CommandRule{OnlyRequestOwnerOrAdmin: true})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if !rt.cancelRequireOwner {
		t.Fatal("expected cancel to pass only_request_owner_or_admin rule")
	}
}

func TestHandleCommandUsesStatusRedactRule(t *testing.T) {
	rt := &fakeCommandRuntime{}
	msg, err := HandleCommand(rt, Command{Name: config.CommandStatus}, Decision{Chat: config.ChatConfig{ID: "oc_1"}}, config.CommandRule{Redact: true})
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if msg != "status" || !rt.statusRedact {
		t.Fatalf("expected status redaction rule to be passed, msg=%q redact=%t", msg, rt.statusRedact)
	}
}

func TestHandleCommandUsesResetConfirmText(t *testing.T) {
	rt := &fakeCommandRuntime{}
	decision := Decision{Chat: config.ChatConfig{ID: "oc_1"}, Role: "admin"}
	rule := config.CommandRule{RequireConfirm: true, ConfirmText: "CONFIRM"}

	msg, err := HandleCommand(rt, Command{Name: config.CommandReset, Args: []string{"RESET"}}, decision, rule)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if msg != "Send /reset CONFIRM to confirm reset" {
		t.Fatalf("unexpected confirmation message %q", msg)
	}

	msg, err = HandleCommand(rt, Command{Name: config.CommandReset, Args: []string{"CONFIRM"}}, decision, rule)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if msg != "Current session reset" {
		t.Fatalf("unexpected reset message %q", msg)
	}
	if rt.resetDecision.Chat.ID != "oc_1" {
		t.Fatalf("reset decision chat id = %q", rt.resetDecision.Chat.ID)
	}
}

func TestHandleCommandRequiresResetAdmin(t *testing.T) {
	_, err := HandleCommand(&fakeCommandRuntime{}, Command{Name: config.CommandReset}, Decision{Role: "user"}, config.CommandRule{})
	if err == nil {
		t.Fatal("expected non-admin reset to fail")
	}
}
