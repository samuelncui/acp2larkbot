package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type FakeGateway struct {
	mu            sync.Mutex
	events        chan Event
	messages      map[string]string
	cards         map[string]map[string]any
	messageToCard map[string]string
	nextID        int
}

func NewFakeGateway() *FakeGateway {
	return &FakeGateway{events: make(chan Event, 16), messages: map[string]string{}, cards: map[string]map[string]any{}, messageToCard: map[string]string{}}
}

func (g *FakeGateway) Events(ctx context.Context) (<-chan Event, error) {
	return g.events, nil
}

func (g *FakeGateway) Emit(ev Event) {
	g.events <- ev
}

func (g *FakeGateway) SendText(ctx context.Context, chatID string, text string) (*SentMessage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	id := fmt.Sprintf("msg_%d", g.nextID)
	g.messages[id] = text
	return &SentMessage{MessageID: id}, nil
}

func (g *FakeGateway) UpdateText(ctx context.Context, messageID string, text string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.messages[messageID] = text
	return nil
}

func (g *FakeGateway) CreateCard(ctx context.Context, chatID string, card Card) (*SentMessage, error) {
	return g.SendText(ctx, chatID, fakeCardText(card))
}

func (g *FakeGateway) UpdateCard(ctx context.Context, messageID string, card Card) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.messages[messageID] = fakeCardText(card)
	delete(g.messageToCard, messageID)
	return nil
}

func (g *FakeGateway) RememberSelfMessage(messageID string) {}

func (g *FakeGateway) CreateStreamingCard(ctx context.Context, card Card) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	id := fmt.Sprintf("card_%d", g.nextID)
	g.cards[id] = fakeCardRaw(card)
	g.messages[id] = fakeCardText(card)
	return id, nil
}

func (g *FakeGateway) SendCardByID(ctx context.Context, chatID string, cardID string) (*SentMessage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	id := fmt.Sprintf("msg_%d", g.nextID)
	g.messages[id] = g.messages[cardID]
	g.messageToCard[id] = cardID
	return &SentMessage{MessageID: id}, nil
}

func (g *FakeGateway) InsertStreamingElementsBefore(ctx context.Context, cardID, targetElementID string, elements []map[string]any, sequence int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	card, ok := g.cards[cardID]
	if !ok {
		return nil
	}
	insertElementsBefore(card, targetElementID, elements)
	g.messages[cardID] = mustJSON(card)
	for messageID, linkedCardID := range g.messageToCard {
		if linkedCardID == cardID {
			g.messages[messageID] = g.messages[cardID]
		}
	}
	return nil
}

func (g *FakeGateway) UpdateStreamingElement(ctx context.Context, cardID, elementID, content string, sequence int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if card, ok := g.cards[cardID]; ok {
		updateElementContent(card, elementID, content)
		g.messages[cardID] = mustJSON(card)
	} else {
		g.messages[cardID] = content
	}
	for messageID, linkedCardID := range g.messageToCard {
		if linkedCardID == cardID {
			g.messages[messageID] = content
			if card, ok := g.cards[cardID]; ok {
				g.messages[messageID] = mustJSON(card)
			}
		}
	}
	return nil
}

func insertElementsBefore(card map[string]any, targetElementID string, elements []map[string]any) bool {
	body, ok := card["body"].(map[string]any)
	if !ok {
		return false
	}
	rawElements, ok := body["elements"].([]any)
	if !ok {
		return false
	}
	insertAt := -1
	for i, item := range rawElements {
		if hasElementID(item, targetElementID) {
			insertAt = i
			break
		}
	}
	if insertAt < 0 {
		return false
	}
	newItems := make([]any, 0, len(rawElements)+len(elements))
	newItems = append(newItems, rawElements[:insertAt]...)
	for _, element := range elements {
		newItems = append(newItems, element)
	}
	newItems = append(newItems, rawElements[insertAt:]...)
	body["elements"] = newItems
	return true
}

func hasElementID(node any, elementID string) bool {
	switch v := node.(type) {
	case map[string]any:
		if id, _ := v["element_id"].(string); id == elementID {
			return true
		}
		for _, child := range v {
			if hasElementID(child, elementID) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if hasElementID(item, elementID) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range v {
			if hasElementID(item, elementID) {
				return true
			}
		}
	}
	return false
}

func (g *FakeGateway) FinalizeStreamingCard(ctx context.Context, cardID string, sequence int) error {
	return nil
}

func (g *FakeGateway) Message(messageID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cardID, ok := g.messageToCard[messageID]; ok {
		return g.messages[cardID]
	}
	return g.messages[messageID]
}

func fakeCardText(card Card) string {
	if card.Raw != nil {
		return mustJSON(card.Raw)
	}
	return card.Text
}

func fakeCardRaw(card Card) map[string]any {
	if card.Raw == nil {
		return map[string]any{
			"schema": "2.0",
			"body": map[string]any{
				"elements": []any{map[string]any{"tag": "markdown", "content": card.Text}},
			},
		}
	}
	bs, err := json.Marshal(card.Raw)
	if err != nil {
		return card.Raw
	}
	var out map[string]any
	if err := json.Unmarshal(bs, &out); err != nil {
		return card.Raw
	}
	return out
}

func updateElementContent(node any, elementID string, content string) bool {
	switch v := node.(type) {
	case map[string]any:
		if id, _ := v["element_id"].(string); id == elementID {
			v["content"] = content
			return true
		}
		for _, child := range v {
			if updateElementContent(child, elementID, content) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if updateElementContent(item, elementID, content) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range v {
			if updateElementContent(item, elementID, content) {
				return true
			}
		}
	}
	return false
}

func mustJSON(v any) string {
	bs, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(bs)
}
