package router

import (
	"strings"

	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/lark"
)

type Router struct {
	cfg   *config.Config
	chats map[string]config.ChatConfig
}

func New(cfg *config.Config) *Router {
	chats := map[string]config.ChatConfig{}
	for _, chat := range cfg.Chats {
		chats[chat.ID] = chat
	}
	return &Router{cfg: cfg, chats: chats}
}

func (r *Router) Chat(chatID string) (config.ChatConfig, bool) {
	chat, ok := r.chats[chatID]
	return chat, ok
}

func (r *Router) ParseCommand(text string) (*Command, bool) {
	if !r.cfg.Commands.Enabled || !strings.HasPrefix(text, r.cfg.Commands.Prefix) {
		return nil, false
	}
	fields := strings.Fields(strings.TrimPrefix(text, r.cfg.Commands.Prefix))
	if len(fields) == 0 {
		return nil, false
	}
	name := fields[0]
	if _, ok := r.cfg.Commands.Rules[name]; !ok {
		// Unregistered /xxx commands are not local commands; pass them through to ACP
		// unchanged (for example, agent commands such as /clear or /help).
		return nil, false
	}
	return &Command{Name: name, Args: fields[1:], Raw: text}, true
}

func (r *Router) ShouldHandle(ev lark.Event, chat config.ChatConfig) bool {
	if ev.MessageType != lark.MessageText {
		return false
	}
	if !r.requireMention(chat) {
		return true
	}
	return mentionsAny(ev, mentionTargetIDs(r.cfg.Lark))

}

func (r *Router) requireMention(chat config.ChatConfig) bool {
	if chat.RequireMention != nil {
		return *chat.RequireMention
	}
	if chat.Type == config.ChatTypeGroup {
		return r.cfg.Lark.RequireMention || r.cfg.Lark.Trigger.RequireMentionInGroup
	}
	return r.cfg.Lark.RequireMention
}

func mentionsAny(ev lark.Event, targets []string) bool {
	for _, target := range targets {
		if mentions(ev, target) {
			return true
		}
	}
	return false
}

func mentions(ev lark.Event, target string) bool {
	if target == "" {
		return false
	}
	for _, mention := range ev.Mentions {
		if mention == target {
			return true
		}
	}
	return false
}

func mentionTargetIDs(cfg config.LarkConfig) []string {
	targets := []string{cfg.BotOpenID, cfg.BotUserID, cfg.AppID}
	out := make([]string, 0, len(targets))
	seen := map[string]struct{}{}
	for _, target := range targets {
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

type Command struct {
	Name string
	Args []string
	Raw  string
}
