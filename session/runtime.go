package session

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type RuntimeRequest struct {
	RequestID string
	ChatID    string
	SenderID  string
	SessionID string
	StartedAt time.Time
	Cancel    context.CancelFunc
}

type Runtime struct {
	mu     sync.Mutex
	byChat map[string]*RuntimeRequest
	byReq  map[string]*RuntimeRequest
}

func NewRuntime() *Runtime {
	return &Runtime{byChat: map[string]*RuntimeRequest{}, byReq: map[string]*RuntimeRequest{}}
}

func (r *Runtime) Start(req RuntimeRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byChat[req.ChatID] = &req
	r.byReq[req.RequestID] = &req
}

func (r *Runtime) Finish(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	req := r.byReq[requestID]
	if req == nil {
		return
	}
	delete(r.byReq, requestID)
	if current := r.byChat[req.ChatID]; current != nil && current.RequestID == requestID {
		delete(r.byChat, req.ChatID)
	}
}

func (r *Runtime) Cancel(chatID string, senderID string, admin bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req := r.byChat[chatID]
	if req == nil {
		return fmt.Errorf("no running request")
	}
	if !admin && req.SenderID != senderID {
		return fmt.Errorf("not request owner")
	}
	req.Cancel()
	return nil
}

func (r *Runtime) Status(chatID string) *RuntimeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	req := r.byChat[chatID]
	if req == nil {
		return nil
	}
	copy := *req
	return &copy
}
