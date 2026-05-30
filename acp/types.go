package acp

import "context"

const (
	MethodSessionOpen  = "session.open"
	MethodMessageSend  = "message.send"
	MethodSessionClose = "session.close"
	MethodMessageEvent = "message.event"

	EventDelta   EventType = "delta"
	EventFinish  EventType = "finish"
	EventError   EventType = "error"
	EventProcess EventType = "process"
)

type EventType string

type Client interface {
	OpenSession(ctx context.Context, req OpenSessionRequest) (*Session, error)
	Send(ctx context.Context, req SendRequest) (<-chan Event, error)
	CloseSession(ctx context.Context, sessionID string) error
}

type Session struct {
	ID string
}

type OpenSessionRequest struct {
	RequestID string
	AgentID   string
	CWD       string
	Metadata  map[string]string
}

type SendRequest struct {
	RequestID string
	SessionID string
	Message   Message
}

type Message struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type Event struct {
	RequestID string
	Type      EventType
	Delta     string
	Final     string
	Err       string
	Process   string
}
