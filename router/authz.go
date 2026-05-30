package router

import (
	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/lark"
)

type Decision struct {
	Allowed  bool
	Reason   string
	Chat     config.ChatConfig
	SenderID string
	ThreadID string
	Role     string
	Command  string
}

type Authz struct {
	cfg *config.Config
}

func NewAuthz(cfg *config.Config) *Authz {
	return &Authz{cfg: cfg}
}

func (a *Authz) Evaluate(ev lark.Event, chat config.ChatConfig, cmd *Command) Decision {
	if ev.SenderID == "" {
		return Decision{Allowed: false, Reason: "sender_missing", Chat: chat, ThreadID: ev.ThreadID}
	}
	role, ok := a.role(ev.SenderID)
	if !ok && a.cfg.Authz.Access.Default == "deny" {
		return Decision{Allowed: false, Reason: "user_denied", Chat: chat, SenderID: ev.SenderID, ThreadID: ev.ThreadID}
	}
	if role == "" {
		role = "user"
	}
	if !a.chatAllows(chat.ID, ev.SenderID) {
		return Decision{Allowed: false, Reason: "chat_user_denied", Chat: chat, SenderID: ev.SenderID, ThreadID: ev.ThreadID, Role: role}
	}
	if cmd != nil {
		if !a.commandAllows(role, cmd.Name) {
			return Decision{Allowed: false, Reason: "command_denied", Chat: chat, SenderID: ev.SenderID, ThreadID: ev.ThreadID, Role: role, Command: cmd.Name}
		}
	}
	return Decision{Allowed: true, Chat: chat, SenderID: ev.SenderID, ThreadID: ev.ThreadID, Role: role}
}

func (a *Authz) CanUseDirectChat(userID string) bool {
	if userID == "" {
		return false
	}
	_, ok := a.role(userID)
	return ok
}

func (a *Authz) role(userID string) (string, bool) {
	for _, user := range a.cfg.Authz.Access.Users {
		if user.ID == userID {
			return user.Role, true
		}
	}
	for _, admin := range a.cfg.Authz.Admins {
		if admin == userID {
			return "admin", true
		}
	}
	return "", false
}

func (a *Authz) chatAllows(chatID string, userID string) bool {
	for _, chat := range a.cfg.Authz.Access.Chats {
		if chat.ID != chatID {
			continue
		}
		for _, user := range chat.Users {
			if user == userID {
				return true
			}
		}
		return false
	}
	return true
}

func (a *Authz) commandAllows(role string, name string) bool {
	rule, ok := a.cfg.Commands.Rules[name]
	if !ok {
		return false
	}
	for _, allowed := range rule.Roles {
		if allowed == role {
			return true
		}
	}
	return false
}

func IsAdmin(decision Decision) bool {
	return decision.Role == "admin"
}
