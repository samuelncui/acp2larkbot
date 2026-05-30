package lark

import "context"

const (
	EventMessage      = "message"
	EventMessagePatch = "message_update"
	EventCard         = "card"
	SenderBot         = "bot"
	MessageText       = "text"
)

type Event struct {
	EventID     string
	MessageID   string
	ChatID      string
	ChatType    string
	SenderID    string
	SenderType  string
	SenderAppID string
	MessageType string
	EventType   string
	Text        string
	ThreadID    string
	Mentions    []string
}

type SentMessage struct {
	MessageID string
}

type Card struct {
	Text string
	Raw  map[string]any
}

type Gateway interface {
	Events(ctx context.Context) (<-chan Event, error)
	SendText(ctx context.Context, chatID string, text string) (*SentMessage, error)
	UpdateText(ctx context.Context, messageID string, text string) error
	CreateCard(ctx context.Context, chatID string, card Card) (*SentMessage, error)
	UpdateCard(ctx context.Context, messageID string, card Card) error
	RememberSelfMessage(messageID string)

	CreateStreamingCard(ctx context.Context, card Card) (string, error)
	SendCardByID(ctx context.Context, chatID string, cardID string) (*SentMessage, error)
	InsertStreamingElementsBefore(ctx context.Context, cardID, targetElementID string, elements []map[string]any, sequence int) error
	UpdateStreamingElement(ctx context.Context, cardID, elementID, content string, sequence int) error
	FinalizeStreamingCard(ctx context.Context, cardID string, sequence int) error
}
