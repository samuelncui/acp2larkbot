package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/acp"
	"github.com/samuelncui/acp2larkbot/config"
	"github.com/samuelncui/acp2larkbot/lark"
	"github.com/samuelncui/acp2larkbot/session"
)

type Worker struct {
	ctx      context.Context
	chat     config.ChatConfig
	queue    chan Job
	done     chan struct{}
	closeOne sync.Once
	sessions *session.Manager
	clients  *acp.Registry
	renderer lark.Renderer
	timeout  time.Duration
}

func NewWorker(ctx context.Context, wg *sync.WaitGroup, chat config.ChatConfig, sessions *session.Manager, clients *acp.Registry, renderer lark.Renderer, timeout time.Duration) *Worker {
	w := &Worker{ctx: ctx, chat: chat, queue: make(chan Job, chat.Queue.MaxPending), done: make(chan struct{}), sessions: sessions, clients: clients, renderer: renderer, timeout: timeout}
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.loop()
	}()
	go func() {
		<-ctx.Done()
		w.Close()
	}()
	return w
}

func (w *Worker) Enqueue(ctx context.Context, job Job) error {
	select {
	case w.queue <- job:
		return nil
	case <-w.done:
		return fmt.Errorf("worker closed")
	case <-w.ctx.Done():
		return w.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	default:
		if w.chat.Queue.OnFull == config.QueueDropOldest {
			select {
			case <-w.queue:
			default:
			}
			w.queue <- job
			return nil
		}
		return fmt.Errorf("chat %q queue full", w.chat.ID)
	}
}

func (w *Worker) Close() {
	w.closeOne.Do(func() { close(w.done) })
}

func (w *Worker) loop() {
	defer func() {
		if r := recover(); r != nil {
			logrus.WithField("panic", r).Error("worker loop panic")
		}
	}()
	for {
		select {
		case <-w.done:
			return
		case job := <-w.queue:
			w.handle(w.ctx, job)
		}
	}
}

func (w *Worker) handle(ctx context.Context, job Job) {
	logrus.WithFields(logrus.Fields{"chat_id": job.Chat.ID, "agent": job.Chat.Agent, "request_id": job.RequestID}).Info("worker handling job")
	client, ok := w.clients.Get(job.Chat.Agent)
	if !ok {
		logrus.WithField("agent", job.Chat.Agent).Warn("worker: agent client not found")
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	if w.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, w.timeout)
	}
	defer cancel()
	resolved, err := w.sessions.Resolve(ctx, session.ResolveRequest{Chat: job.Chat, SenderID: job.Event.SenderID, ThreadID: job.Event.ThreadID, RequestID: job.RequestID, Client: client})
	if err != nil {
		logrus.WithError(err).WithField("request_id", job.RequestID).Error("worker: resolve session failed")
		return
	}
	logrus.WithFields(logrus.Fields{"session_id": resolved.SessionID, "ephemeral": resolved.Ephemeral}).Info("worker session resolved")
	if resolved.Ephemeral {
		defer func() { _ = client.CloseSession(context.Background(), resolved.SessionID) }()
	}
	handle, err := w.renderer.Start(ctx, lark.StartRenderRequest{ChatID: job.Chat.ID, ReplyToMessageID: job.Event.MessageID})
	if err != nil {
		logrus.WithError(err).WithField("request_id", job.RequestID).Error("worker: renderer start failed")
		return
	}
	logrus.WithFields(logrus.Fields{"request_id": job.RequestID, "message_id": handle.MessageID, "card_id": handle.CardID}).Info("worker renderer started")
	events, resolved, err := w.sendWithRecovery(ctx, client, job, resolved)
	if err != nil {
		logrus.WithError(err).WithField("request_id", job.RequestID).Error("worker: client.Send failed")
		_ = w.renderer.Fail(ctx, handle, err)
		return
	}
	logrus.WithFields(logrus.Fields{"request_id": job.RequestID, "session_id": resolved.SessionID}).Info("worker prompt dispatched")
	w.sessions.Runtime().Start(session.RuntimeRequest{RequestID: job.RequestID, ChatID: job.Chat.ID, SenderID: job.Event.SenderID, SessionID: resolved.SessionID, StartedAt: time.Now(), Cancel: cancel})
	defer w.sessions.Runtime().Finish(job.RequestID)
	retried := false
eventLoop:
	for {
		logrus.WithField("request_id", job.RequestID).Info("worker awaiting acp events")
		select {
		case <-ctx.Done():
			logrus.WithError(ctx.Err()).WithField("request_id", job.RequestID).Warn("worker request canceled")
			_ = w.renderer.Fail(context.Background(), handle, ctx.Err())
			return
		case ev, ok := <-events:
			if !ok {
				break eventLoop
			}
			switch ev.Type {
			case acp.EventDelta:
				logrus.WithField("len", len(ev.Delta)).Debug("acp delta")
				if err := w.renderer.Append(ctx, handle, ev.Delta); err != nil {
					logrus.WithError(err).WithField("request_id", job.RequestID).Error("renderer append failed")
				}
			case acp.EventProcess:
				logrus.WithFields(logrus.Fields{"request_id": job.RequestID, "len": len(ev.Process)}).Debug("acp process")
				if err := w.renderer.AppendProcess(ctx, handle, ev.Process); err != nil {
					logrus.WithError(err).WithField("request_id", job.RequestID).Error("renderer append failed")
				}
			case acp.EventFinish:
				logrus.WithFields(logrus.Fields{"request_id": job.RequestID, "final_len": len(ev.Final)}).Info("acp finish")
				if err := w.renderer.Finish(ctx, handle, ev.Final); err != nil {
					logrus.WithError(err).WithField("request_id", job.RequestID).Error("renderer finish failed")
				}
			case acp.EventError:
				if !retried && shouldRefreshSession(job.Chat, fmt.Errorf("%s", ev.Err)) {
					retried = true
					logrus.WithFields(logrus.Fields{"request_id": job.RequestID, "session_id": resolved.SessionID}).Warn("worker refreshing stale session")
					if err := w.renderer.AppendProcess(ctx, handle, "[Process] Session expired, refreshing session"); err != nil {
						logrus.WithError(err).WithField("request_id", job.RequestID).Error("renderer append failed")
					}
					refreshed, refreshEvents, refreshErr := w.refreshSession(ctx, client, job)
					if refreshErr != nil {
						logrus.WithError(refreshErr).WithField("request_id", job.RequestID).Error("worker refresh session failed")
						if err := w.renderer.Fail(ctx, handle, refreshErr); err != nil {
							logrus.WithError(err).WithField("request_id", job.RequestID).Error("renderer fail failed")
						}
						return
					}
					resolved = refreshed
					events = refreshEvents
					w.sessions.Runtime().Start(session.RuntimeRequest{RequestID: job.RequestID, ChatID: job.Chat.ID, SenderID: job.Event.SenderID, SessionID: resolved.SessionID, StartedAt: time.Now(), Cancel: cancel})
					continue eventLoop
				}
				logrus.WithFields(logrus.Fields{"err": ev.Err, "request_id": job.RequestID}).Error("acp error")
				if err := w.renderer.Fail(ctx, handle, fmt.Errorf("%s", ev.Err)); err != nil {
					logrus.WithError(err).WithField("request_id", job.RequestID).Error("renderer fail failed")
				}
			}
		}
	}
	logrus.WithField("request_id", job.RequestID).Info("worker job done")
}

func (w *Worker) sendWithRecovery(ctx context.Context, client acp.Client, job Job, resolved *session.ResolveResult) (<-chan acp.Event, *session.ResolveResult, error) {
	events, err := client.Send(ctx, acp.SendRequest{RequestID: job.RequestID, SessionID: resolved.SessionID, Message: acp.Message{Type: "text", Content: job.Event.Text}})
	if err == nil || !shouldRefreshSession(job.Chat, err) {
		return events, resolved, err
	}
	if resetErr := w.sessions.Reset(ctx, job.Chat, job.Event.SenderID, job.Event.ThreadID); resetErr != nil {
		return nil, resolved, fmt.Errorf("reset stale session failed, %w", resetErr)
	}
	refreshed, resolveErr := w.sessions.Resolve(ctx, session.ResolveRequest{Chat: job.Chat, SenderID: job.Event.SenderID, ThreadID: job.Event.ThreadID, RequestID: job.RequestID, Client: client})
	if resolveErr != nil {
		return nil, resolved, fmt.Errorf("resolve refreshed session failed, %w", resolveErr)
	}
	events, err = client.Send(ctx, acp.SendRequest{RequestID: job.RequestID, SessionID: refreshed.SessionID, Message: acp.Message{Type: "text", Content: job.Event.Text}})
	return events, refreshed, err
}

func shouldRefreshSession(chat config.ChatConfig, err error) bool {
	if err == nil || chat.Session.Strategy != config.SessionAutoCreate {
		return false
	}
	return strings.Contains(err.Error(), "Invalid params")
}

func (w *Worker) refreshSession(ctx context.Context, client acp.Client, job Job) (*session.ResolveResult, <-chan acp.Event, error) {
	if err := w.sessions.Reset(ctx, job.Chat, job.Event.SenderID, job.Event.ThreadID); err != nil {
		return nil, nil, fmt.Errorf("reset stale session failed, %w", err)
	}
	resolved, err := w.sessions.Resolve(ctx, session.ResolveRequest{Chat: job.Chat, SenderID: job.Event.SenderID, ThreadID: job.Event.ThreadID, RequestID: job.RequestID, Client: client})
	if err != nil {
		return nil, nil, fmt.Errorf("resolve refreshed session failed, %w", err)
	}
	events, resolved, err := w.sendWithRecovery(ctx, client, job, resolved)
	return resolved, events, err
}
