package session

import (
	"context"
	"fmt"
	"time"

	"github.com/samuelncui/acp2larkbot/acp"
	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/state"
)

type Manager struct {
	store   state.Store
	runtime *Runtime
}

type ResolveRequest struct {
	Chat      config.ChatConfig
	SenderID  string
	ThreadID  string
	RequestID string
	Client    acp.Client
}

type ResolveResult struct {
	Key       string
	AgentID   string
	CWD       string
	SessionID string
	Ephemeral bool
}

func NewManager(store state.Store) *Manager {
	return &Manager{store: store, runtime: NewRuntime()}
}

func (m *Manager) Runtime() *Runtime {
	return m.runtime
}

func (m *Manager) Resolve(ctx context.Context, req ResolveRequest) (*ResolveResult, error) {
	key := Key(req.Chat, req.SenderID, req.ThreadID)
	switch req.Chat.Session.Strategy {
	case config.SessionStatic:
		return &ResolveResult{Key: key, AgentID: req.Chat.Agent, CWD: req.Chat.CWD, SessionID: req.Chat.Session.ID}, nil
	case config.SessionAutoCreate:
		return m.resolveAuto(ctx, req, key)
	case config.SessionEphemeral:
		sess, err := req.Client.OpenSession(ctx, acp.OpenSessionRequest{RequestID: req.RequestID, AgentID: req.Chat.Agent, CWD: req.Chat.CWD})
		if err != nil {
			return nil, fmt.Errorf("open ephemeral session failed, %w", err)
		}
		return &ResolveResult{Key: key, AgentID: req.Chat.Agent, CWD: req.Chat.CWD, SessionID: sess.ID, Ephemeral: true}, nil
	default:
		return nil, fmt.Errorf("unsupported session strategy %q", req.Chat.Session.Strategy)
	}
}

func (m *Manager) Reset(ctx context.Context, chat config.ChatConfig, senderID string, threadID string) error {
	if chat.Session.Strategy != config.SessionAutoCreate {
		return nil
	}
	return m.store.DeleteSession(ctx, Key(chat, senderID, threadID))
}

func (m *Manager) resolveAuto(ctx context.Context, req ResolveRequest, key string) (*ResolveResult, error) {
	rec, err := m.store.GetSession(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get auto session failed, %w", err)
	}
	now := time.Now()
	if rec != nil && now.Before(rec.ExpiresAt) && now.Before(rec.IdleUntil) {
		rec.UpdatedAt = now
		rec.IdleUntil = now.Add(req.Chat.Session.IdleTimeout.Duration)
		if err := m.store.PutSession(ctx, *rec); err != nil {
			return nil, fmt.Errorf("touch auto session failed, %w", err)
		}
		return &ResolveResult{Key: key, AgentID: req.Chat.Agent, CWD: req.Chat.CWD, SessionID: rec.SessionID}, nil
	}
	sess, err := req.Client.OpenSession(ctx, acp.OpenSessionRequest{RequestID: req.RequestID, AgentID: req.Chat.Agent, CWD: req.Chat.CWD})
	if err != nil {
		return nil, fmt.Errorf("open auto session failed, %w", err)
	}
	if err := m.store.PutSession(ctx, state.SessionRecord{
		Key: key, ChatID: req.Chat.ID, SenderID: req.SenderID, ThreadID: req.ThreadID,
		AgentID: req.Chat.Agent, CWD: req.Chat.CWD, SessionID: sess.ID,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(req.Chat.Session.TTL.Duration), IdleUntil: now.Add(req.Chat.Session.IdleTimeout.Duration),
	}); err != nil {
		return nil, fmt.Errorf("put auto session failed, %w", err)
	}
	return &ResolveResult{Key: key, AgentID: req.Chat.Agent, CWD: req.Chat.CWD, SessionID: sess.ID}, nil
}
