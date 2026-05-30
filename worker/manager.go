package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/samuelncui/acp2larkbot/acp"
	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/lark"
	"github.com/samuelncui/acp2larkbot/router"
	"github.com/samuelncui/acp2larkbot/session"
	"github.com/samuelncui/acp2larkbot/state"
)

type Manager struct {
	mu       sync.Mutex
	workers  map[string]*Worker
	closed   bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	sessions *session.Manager
	store    state.Store
	clients  *acp.Registry
	renderer lark.Renderer
	timeout  time.Duration
}

func NewManager(sessions *session.Manager, store state.Store, clients *acp.Registry, renderer lark.Renderer, timeout time.Duration) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{workers: map[string]*Worker{}, ctx: ctx, cancel: cancel, sessions: sessions, store: store, clients: clients, renderer: renderer, timeout: timeout}
}

func (m *Manager) Enqueue(ctx context.Context, job Job) error {
	worker, err := m.worker(job.Chat)
	if err != nil {
		return err
	}
	return worker.Enqueue(ctx, job)
}

func (m *Manager) CancelCurrent(chatID string, senderID string, requireOwnerOrAdmin bool, admin bool) error {
	return m.sessions.Runtime().Cancel(chatID, senderID, admin || !requireOwnerOrAdmin)
}

func (m *Manager) Status(chatID string, redact bool) string {
	if req := m.sessions.Runtime().Status(chatID); req != nil {
		if redact {
			return "Running request=[redacted] session=[redacted]"
		}
		return fmt.Sprintf("Running request=%s session=%s", req.RequestID, session.Short(req.SessionID))
	}
	return "Idle"
}

func (m *Manager) Reset(decision router.Decision) error {
	// Runtime reset is intentionally conservative. Persistent session deletion is
	// scoped to the current command context to avoid destructive broad deletion.
	if req := m.sessions.Runtime().Status(decision.Chat.ID); req != nil {
		return fmt.Errorf("request %q still running", req.RequestID)
	}
	return m.sessions.Reset(context.Background(), decision.Chat, decision.SenderID, decision.ThreadID)
}

func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	workers := make([]*Worker, 0, len(m.workers))
	for _, w := range m.workers {
		workers = append(workers, w)
	}
	m.mu.Unlock()
	for _, w := range workers {
		w.Close()
	}
	m.wg.Wait()
}

func (m *Manager) worker(chat config.ChatConfig) (*Worker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("worker manager closed")
	}
	w := m.workers[chat.ID]
	if w != nil {
		return w, nil
	}
	w = NewWorker(m.ctx, &m.wg, chat, m.sessions, m.clients, m.renderer, m.timeout)
	m.workers[chat.ID] = w
	return w, nil
}

type Job struct {
	Event     lark.Event
	Chat      config.ChatConfig
	Decision  router.Decision
	RequestID string
}
