package router

import (
	"fmt"
	"strings"

	"github.com/samuelncui/acp2larkbot/config"
)

type CommandRuntime interface {
	CancelCurrent(chatID string, senderID string, requireOwnerOrAdmin bool, admin bool) error
	Status(chatID string, redact bool) string
	Reset(decision Decision) error
}

func HandleCommand(rt CommandRuntime, cmd Command, decision Decision, rule config.CommandRule) (string, error) {
	switch cmd.Name {
	case "cancel":
		if err := rt.CancelCurrent(decision.Chat.ID, decision.SenderID, rule.OnlyRequestOwnerOrAdmin, IsAdmin(decision)); err != nil {
			return "Cancel failed", err
		}
		return "Current request canceled", nil
	case "status":
		return rt.Status(decision.Chat.ID, rule.Redact), nil
	case "reset":
		if !IsAdmin(decision) {
			return "Permission denied", fmt.Errorf("reset requires admin")
		}
		if rule.RequireConfirm && strings.Join(cmd.Args, " ") != rule.ConfirmText {
			return fmt.Sprintf("Send /reset %s to confirm reset", rule.ConfirmText), nil
		}
		if err := rt.Reset(decision); err != nil {
			return "Reset failed", err
		}
		return "Current session reset", nil
	default:
		return "Unknown command", fmt.Errorf("unknown command %q", cmd.Name)
	}
}
