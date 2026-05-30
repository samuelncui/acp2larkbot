package acp

import (
	"context"
	"fmt"
	"sync/atomic"
)

type FakeClient struct {
	next   int64
	Events []Event
}

func (c *FakeClient) OpenSession(ctx context.Context, req OpenSessionRequest) (*Session, error) {
	id := atomic.AddInt64(&c.next, 1)
	return &Session{ID: fmt.Sprintf("sess_%d", id)}, nil
}

func (c *FakeClient) Send(ctx context.Context, req SendRequest) (<-chan Event, error) {
	out := make(chan Event, len(c.Events)+1)
	go func() {
		defer close(out)
		if len(c.Events) == 0 {
			out <- Event{RequestID: req.RequestID, Type: EventDelta, Delta: req.Message.Content}
			out <- Event{RequestID: req.RequestID, Type: EventFinish, Final: req.Message.Content}
			return
		}
		for _, ev := range c.Events {
			ev.RequestID = req.RequestID
			out <- ev
		}
	}()
	return out, nil
}

func (c *FakeClient) CloseSession(ctx context.Context, sessionID string) error {
	return nil
}
