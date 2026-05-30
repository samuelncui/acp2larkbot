package app

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/acp"
	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/lark"
	"github.com/samuelncui/acp2larkbot/router"
	"github.com/samuelncui/acp2larkbot/session"
	"github.com/samuelncui/acp2larkbot/state"
	"github.com/samuelncui/acp2larkbot/worker"
)

type App struct {
	cfg      *config.Config
	store    state.Store
	gw       lark.Gateway
	filter   *lark.SelfFilter
	router   *router.Router
	authz    *router.Authz
	registry *acp.Registry
	workers  *worker.Manager
}

func New(cfg *config.Config) (*App, error) {
	store, err := state.OpenBolt(cfg.State.Path)
	if err != nil {
		return nil, err
	}
	gw := lark.NewLiveGateway(cfg.Lark)
	filter := lark.NewSelfFilter(cfg.Lark)
	var renderer lark.Renderer
	switch cfg.Lark.Streaming.Mode {
	case config.StreamingModeCard:
		renderer = lark.NewCardRenderer(cfg.Lark.Streaming, gw, filter)
	case config.StreamingModeCardStreaming:
		renderer = lark.NewFallbackRenderer(
			lark.NewCardStreamingRenderer(cfg.Lark.Streaming, gw, filter),
			lark.NewStreamingRenderer(cfg.Lark.Streaming, gw, filter),
		)
	default:
		renderer = lark.NewStreamingRenderer(cfg.Lark.Streaming, gw, filter)
	}
	registry, err := acp.NewRegistry(cfg.ACP)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	sessions := session.NewManager(store)
	return &App{
		cfg: cfg, store: store, gw: gw, filter: filter,
		router: router.New(cfg), authz: router.NewAuthz(cfg),
		registry: registry,
		workers:  worker.NewManager(sessions, store, registry, renderer, cfg.Lark.Streaming.MaxStreamDuration.Duration),
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	go a.cleanupState(ctx)
	events, err := a.gw.Events(ctx)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if err := a.HandleEvent(ctx, ev); err != nil {
				logrus.WithError(err).Warn("handle event failed")
			}
		}
	}
}

func (a *App) Close() error {
	if a.workers != nil {
		a.workers.Close()
	}
	if a.registry != nil {
		if err := a.registry.Close(); err != nil {
			_ = a.store.Close()
			return err
		}
	}
	return a.store.Close()
}

func (a *App) cleanupState(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := a.store.Cleanup(ctx, now, a.cfg.UnknownChat.RateLimitInterval.Duration); err != nil {
				logrus.WithError(err).Warn("cleanup state failed")
			}
		}
	}
}

func (a *App) HandleEvent(ctx context.Context, ev lark.Event) error {
	if a.filter.Ignore(ev) {
		logrus.WithFields(logrus.Fields{"message_id": ev.MessageID, "sender_type": ev.SenderType}).Info("event ignored by self filter")
		return nil
	}
	if a.cfg.Dedupe.Enabled {
		key := ev.EventID
		if key == "" {
			key = ev.MessageID
		}
		if key != "" {
			seen, err := a.store.MarkSeen(ctx, key, a.cfg.Dedupe.TTL.Duration, time.Now())
			if err != nil {
				return fmt.Errorf("dedupe failed, %w", err)
			}
			if seen {
				logrus.WithField("key", key).Info("event deduped")
				return nil
			}
		}
	}
	chat, ok := a.resolveChat(ev)
	if !ok {
		logrus.WithField("chat_id", ev.ChatID).Info("event for unknown chat")
		return a.handleUnknownChat(ctx, ev)
	}
	if !a.router.ShouldHandle(ev, chat) {
		logrus.WithFields(logrus.Fields{"chat_id": ev.ChatID, "message_type": ev.MessageType}).Info("event not handled by router")
		return nil
	}
	cmd, isCommand := a.router.ParseCommand(ev.Text)
	decision := a.authz.Evaluate(ev, chat, cmd)
	if !decision.Allowed {
		logrus.WithFields(logrus.Fields{"reason": decision.Reason, "sender_id": ev.SenderID}).Info("event denied by authz")
		_, err := a.gw.SendText(ctx, ev.ChatID, "Permission denied")
		return err
	}
	if isCommand {
		msg, err := router.HandleCommand(a.workers, *cmd, decision, a.cfg.Commands.Rules[cmd.Name])
		_, sendErr := a.gw.SendText(ctx, ev.ChatID, msg)
		if sendErr != nil {
			return sendErr
		}
		return err
	}
	logrus.WithFields(logrus.Fields{"chat_id": chat.ID, "agent": chat.Agent, "sender_id": ev.SenderID, "request_id": requestID(ev)}).Info("enqueue job")
	return a.workers.Enqueue(ctx, worker.Job{Event: ev, Chat: chat, Decision: decision, RequestID: requestID(ev)})
}

func (a *App) resolveChat(ev lark.Event) (config.ChatConfig, bool) {
	if chat, ok := a.router.Chat(ev.ChatID); ok {
		return chat, true
	}
	if !isDirectChat(ev.ChatType) || !a.authz.CanUseDirectChat(ev.SenderID) {
		return config.ChatConfig{}, false
	}
	agent, ok := a.cfg.ACP.Agents[a.cfg.ACP.DefaultAgent]
	if !ok {
		return config.ChatConfig{}, false
	}
	return config.ChatConfig{
		ID:     ev.ChatID,
		Type:   config.ChatTypeDirect,
		UserID: ev.SenderID,
		Name:   ev.SenderID,
		Agent:  a.cfg.ACP.DefaultAgent,
		CWD:    agent.CWD,
		Queue: config.QueueConfig{
			MaxPending: 5,
			OnFull:     config.QueueReject,
		},
		Session: config.SessionPolicy{
			Strategy:           config.SessionAutoCreate,
			Scope:              config.SessionScopeSender,
			Prefix:             "lark-direct",
			AllowSharedSession: false,
			TTL:                config.Duration{Duration: 7 * 24 * time.Hour},
			IdleTimeout:        config.Duration{Duration: 24 * time.Hour},
		},
	}, true
}

func (a *App) handleUnknownChat(ctx context.Context, ev lark.Event) error {
	if message, ok := a.unknownChatMessage(ev); ok {
		return a.sendUnknownChatReply(ctx, ev.ChatID, message)
	}
	if a.cfg.UnknownChat.Behavior == config.UnknownIgnore {
		return nil
	}
	allowed, err := a.store.AllowUnknownChatReply(ctx, ev.ChatID, a.cfg.UnknownChat.RateLimitInterval.Duration, time.Now())
	if err != nil || !allowed {
		return err
	}
	message := a.cfg.UnknownChat.Message
	if a.cfg.UnknownChat.IncludeChatID {
		message += "\nchat_id: " + ev.ChatID
	}
	_, err = a.gw.SendText(ctx, ev.ChatID, message)
	return err
}

func (a *App) unknownChatMessage(ev lark.Event) (string, bool) {
	if isDirectChat(ev.ChatType) && !a.authz.CanUseDirectChat(ev.SenderID) {
		return fmt.Sprintf("You are not authorized to use this bot yet.\nAdd your open_id to authz.access.users in the config, for example:\n  - id: %s\n    role: user", ev.SenderID), true
	}
	if isGroupChat(ev.ChatType) && mentionsBot(ev, a.cfg.Lark) {
		return fmt.Sprintf("This group chat has not enabled this bot yet.\nAdd the current chat_id to chats[] in the config, and allow users in authz.access.users / authz.access.chats as needed.\nchat_id: %s\nyour open_id: %s", ev.ChatID, ev.SenderID), true
	}
	return "", false
}

func (a *App) sendUnknownChatReply(ctx context.Context, chatID string, message string) error {
	allowed, err := a.store.AllowUnknownChatReply(ctx, chatID, a.cfg.UnknownChat.RateLimitInterval.Duration, time.Now())
	if err != nil || !allowed {
		return err
	}
	_, err = a.gw.SendText(ctx, chatID, message)
	return err
}

func isDirectChat(chatType string) bool {
	return chatType == config.ChatTypeDirect || chatType == "p2p"
}

func isGroupChat(chatType string) bool {
	return chatType == config.ChatTypeGroup
}

func mentionsBot(ev lark.Event, cfg config.LarkConfig) bool {
	targets := []string{cfg.BotOpenID, cfg.BotUserID, cfg.AppID}
	for _, mention := range ev.Mentions {
		for _, target := range targets {
			if target != "" && mention == target {
				return true
			}
		}
	}
	return false
}

func requestID(ev lark.Event) string {
	if ev.EventID != "" {
		return ev.EventID
	}
	if ev.MessageID != "" {
		return ev.MessageID
	}
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
