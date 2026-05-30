package state

import (
	"context"
	"time"
)

type SessionRecord struct {
	Key       string    `json:"key"`
	ChatID    string    `json:"chat_id"`
	SenderID  string    `json:"sender_id"`
	ThreadID  string    `json:"thread_id"`
	AgentID   string    `json:"agent_id"`
	CWD       string    `json:"cwd"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IdleUntil time.Time `json:"idle_until"`
}

type Store interface {
	Close() error
	GetSession(ctx context.Context, key string) (*SessionRecord, error)
	PutSession(ctx context.Context, rec SessionRecord) error
	DeleteSession(ctx context.Context, key string) error
	CleanupSessions(ctx context.Context, now time.Time) error
	Cleanup(ctx context.Context, now time.Time, unknownChatInterval time.Duration) error
	MarkSeen(ctx context.Context, key string, ttl time.Duration, now time.Time) (bool, error)
	AllowUnknownChatReply(ctx context.Context, chatID string, interval time.Duration, now time.Time) (bool, error)
}
