package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	larksdk "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcardkit "github.com/larksuite/oapi-sdk-go/v3/service/cardkit/v1"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/config"
)

const (
	larkReceiveIDTypeChat = "chat_id"
	larkMsgTypeText       = "text"
	larkMsgTypeCard       = "interactive"
)

type LiveGateway struct {
	cfg      config.LarkConfig
	client   *larksdk.Client
	wsClient *larkws.Client
	wsOpts   []larkws.ClientOption

	selfMu sync.Mutex
	self   map[string]struct{}
}

func NewLiveGateway(cfg config.LarkConfig) *LiveGateway {
	opts := []larksdk.ClientOptionFunc{}
	wsOpts := []larkws.ClientOption{}
	if openBaseURL, ok := larkOpenBaseURL(cfg.Domain); ok {
		opts = append(opts, larksdk.WithOpenBaseUrl(openBaseURL))
	}
	if wsDomain, ok := larkWSDomain(cfg.Domain); ok {
		wsOpts = append(wsOpts, larkws.WithDomain(wsDomain))
	}

	return &LiveGateway{
		cfg:      cfg,
		client:   larksdk.NewClient(cfg.AppID, cfg.AppSecret, opts...),
		wsClient: larkws.NewClient(cfg.AppID, cfg.AppSecret, wsOpts...),
		wsOpts:   wsOpts,
		self:     map[string]struct{}{},
	}
}

func larkOpenBaseURL(domain string) (string, bool) {
	switch normalizeLarkDomain(domain) {
	case "":
		return "", false
	case "feishu":
		return "https://open.feishu.cn", true
	case "lark", "larksuite":
		return "https://open.larksuite.com", true
	}
	if strings.HasPrefix(domain, "https://") || strings.HasPrefix(domain, "http://") {
		return domain, true
	}
	return "https://" + domain, true
}

func larkWSDomain(domain string) (string, bool) {
	switch normalizeLarkDomain(domain) {
	case "":
		return "", false
	case "feishu":
		return "https://open.feishu.cn", true
	case "lark", "larksuite":
		return "https://open.larksuite.com", true
	}
	if strings.HasPrefix(domain, "https://") || strings.HasPrefix(domain, "http://") {
		u, err := url.Parse(domain)
		if err != nil || u.Host == "" {
			return "", false
		}
		return u.Scheme + "://" + u.Host, true
	}
	return "https://" + domain, true
}

func normalizeLarkDomain(domain string) string {
	return strings.TrimSpace(strings.ToLower(domain))
}

func (g *LiveGateway) Events(ctx context.Context) (<-chan Event, error) {
	out := make(chan Event, 64)
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		ev := g.convertMessageReceive(event)
		logrus.WithFields(logrus.Fields{"chat_id": ev.ChatID, "sender_id": ev.SenderID, "message_id": ev.MessageID, "text_len": len(ev.Text)}).Info("lark message received")
		if ev.MessageID == "" && ev.EventID == "" {
			return nil
		}
		select {
		case out <- ev:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	wsOpts := append([]larkws.ClientOption{}, g.wsOpts...)
	wsOpts = append(wsOpts, larkws.WithEventHandler(handler), larkws.WithLogLevel(larkcore.LogLevelDebug))
	g.wsClient = larkws.NewClient(g.cfg.AppID, g.cfg.AppSecret, wsOpts...)

	go func() {
		defer close(out)
		errCh := make(chan error, 1)
		go func() {
			errCh <- g.wsClient.Start(ctx)
		}()
		select {
		case <-ctx.Done():
			return
		case <-errCh:
			return
		}
	}()
	return out, nil
}

func (g *LiveGateway) SendText(ctx context.Context, chatID string, text string) (*SentMessage, error) {
	content, err := textContent(text)
	if err != nil {
		return nil, err
	}
	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkMsgTypeText).
		Content(content).
		Build()
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkReceiveIDTypeChat).
		Body(body).
		Build()
	resp, err := g.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create lark text message failed, %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("create lark text message failed, code=%d msg=%q", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return nil, fmt.Errorf("create lark text message returned empty message id")
	}
	g.RememberSelfMessage(*resp.Data.MessageId)
	return &SentMessage{MessageID: *resp.Data.MessageId}, nil
}

func (g *LiveGateway) UpdateText(ctx context.Context, messageID string, text string) error {
	content, err := textContent(text)
	if err != nil {
		return err
	}
	body := larkim.NewUpdateMessageReqBodyBuilder().
		MsgType(larkMsgTypeText).
		Content(content).
		Build()
	req := larkim.NewUpdateMessageReqBuilder().MessageId(messageID).Body(body).Build()
	resp, err := g.client.Im.V1.Message.Update(ctx, req)
	if err != nil {
		return fmt.Errorf("update lark text message %q failed, %w", messageID, err)
	}
	if !resp.Success() {
		return fmt.Errorf("update lark text message %q failed, code=%d msg=%q", messageID, resp.Code, resp.Msg)
	}
	g.RememberSelfMessage(messageID)
	return nil
}

func (g *LiveGateway) CreateCard(ctx context.Context, chatID string, card Card) (*SentMessage, error) {
	content, err := cardContent(card)
	if err != nil {
		return nil, err
	}
	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkMsgTypeCard).
		Content(content).
		Build()
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkReceiveIDTypeChat).
		Body(body).
		Build()
	resp, err := g.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create lark card message failed, %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("create lark card message failed, code=%d msg=%q", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return nil, fmt.Errorf("create lark card message returned empty message id")
	}
	g.RememberSelfMessage(*resp.Data.MessageId)
	return &SentMessage{MessageID: *resp.Data.MessageId}, nil
}

func (g *LiveGateway) UpdateCard(ctx context.Context, messageID string, card Card) error {
	content, err := cardContent(card)
	if err != nil {
		return err
	}
	body := larkim.NewPatchMessageReqBodyBuilder().Content(content).Build()
	req := larkim.NewPatchMessageReqBuilder().MessageId(messageID).Body(body).Build()
	resp, err := g.client.Im.V1.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("patch lark card message %q failed, %w", messageID, err)
	}
	if !resp.Success() {
		return fmt.Errorf("patch lark card message %q failed, code=%d msg=%q", messageID, resp.Code, resp.Msg)
	}
	g.RememberSelfMessage(messageID)
	return nil
}

func (g *LiveGateway) RememberSelfMessage(messageID string) {
	if messageID == "" {
		return
	}
	g.selfMu.Lock()
	defer g.selfMu.Unlock()
	g.self[messageID] = struct{}{}
}

// CreateStreamingCard creates a streaming_mode=true CardKit card and returns its card_id.
func (g *LiveGateway) CreateStreamingCard(ctx context.Context, card Card) (string, error) {
	body := card.Raw
	if body == nil {
		body = map[string]any{
			"schema": "2.0",
			"body": map[string]any{
				"elements": []map[string]any{{
					"tag":     "markdown",
					"content": card.Text,
				}},
			},
		}
	}
	body = cloneMap(body)
	configMap, _ := body["config"].(map[string]any)
	if configMap == nil {
		configMap = map[string]any{}
		body["config"] = configMap
	}
	configMap["streaming_mode"] = true
	configMap["update_multi"] = true
	if _, ok := configMap["width_mode"]; !ok {
		configMap["width_mode"] = "fill"
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal cardkit body failed, %w", err)
	}
	req := larkcardkit.NewCreateCardReqBuilder().
		Body(larkcardkit.NewCreateCardReqBodyBuilder().
			Type("card_json").
			Data(string(data)).
			Build()).
		Build()
	resp, err := g.client.Cardkit.V1.Card.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("create cardkit card failed, %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("create cardkit card failed, code=%d msg=%q", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.CardId == nil || *resp.Data.CardId == "" {
		return "", fmt.Errorf("create cardkit card returned empty card_id")
	}
	return *resp.Data.CardId, nil
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	bs, err := json.Marshal(src)
	if err != nil {
		copied := make(map[string]any, len(src))
		for k, v := range src {
			copied[k] = v
		}
		return copied
	}
	var dst map[string]any
	if err := json.Unmarshal(bs, &dst); err != nil {
		copied := make(map[string]any, len(src))
		for k, v := range src {
			copied[k] = v
		}
		return copied
	}
	return dst
}

// SendCardByID sends an existing card_id to a chat via an IM card message.
func (g *LiveGateway) SendCardByID(ctx context.Context, chatID string, cardID string) (*SentMessage, error) {
	contentBytes, err := json.Marshal(map[string]any{
		"type": "card",
		"data": map[string]any{"card_id": cardID},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal cardkit message content failed, %w", err)
	}
	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkMsgTypeCard).
		Content(string(contentBytes)).
		Build()
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkReceiveIDTypeChat).
		Body(body).
		Build()
	resp, err := g.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send cardkit card message failed, %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("send cardkit card message failed, code=%d msg=%q", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return nil, fmt.Errorf("send cardkit card message returned empty message id")
	}
	g.RememberSelfMessage(*resp.Data.MessageId)
	return &SentMessage{MessageID: *resp.Data.MessageId}, nil
}

// InsertStreamingElementsBefore inserts new elements before targetElementID in a streaming CardKit card.
func (g *LiveGateway) InsertStreamingElementsBefore(ctx context.Context, cardID, targetElementID string, elements []map[string]any, sequence int) error {
	data, err := json.Marshal(elements)
	if err != nil {
		return fmt.Errorf("marshal cardkit elements failed, %w", err)
	}
	req := larkcardkit.NewCreateCardElementReqBuilder().
		CardId(cardID).
		Body(larkcardkit.NewCreateCardElementReqBodyBuilder().
			Type("insert_before").
			TargetElementId(targetElementID).
			Elements(string(data)).
			Sequence(sequence).
			Build()).
		Build()
	resp, err := g.client.Cardkit.V1.CardElement.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("insert cardkit elements failed, %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("insert cardkit elements failed, code=%d msg=%q", resp.Code, resp.Msg)
	}
	return nil
}

// UpdateStreamingElement overwrites a card element's text; CardKit streams the diff.
// sequence must strictly increase.
func (g *LiveGateway) UpdateStreamingElement(ctx context.Context, cardID, elementID, content string, sequence int) error {
	req := larkcardkit.NewContentCardElementReqBuilder().
		CardId(cardID).
		ElementId(elementID).
		Body(larkcardkit.NewContentCardElementReqBodyBuilder().
			Content(content).
			Sequence(sequence).
			Build()).
		Build()
	resp, err := g.client.Cardkit.V1.CardElement.Content(ctx, req)
	if err != nil {
		return fmt.Errorf("update cardkit element content failed, %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("update cardkit element content failed, code=%d msg=%q", resp.Code, resp.Msg)
	}
	return nil
}

// FinalizeStreamingCard switches streaming_mode back to false and ends the streaming session.
func (g *LiveGateway) FinalizeStreamingCard(ctx context.Context, cardID string, sequence int) error {
	settings := map[string]any{
		"config": map[string]any{
			"streaming_mode": false,
			"update_multi":   true,
			"width_mode":     "fill",
		},
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal cardkit settings failed, %w", err)
	}
	req := larkcardkit.NewSettingsCardReqBuilder().
		CardId(cardID).
		Body(larkcardkit.NewSettingsCardReqBodyBuilder().
			Settings(string(data)).
			Sequence(sequence).
			Build()).
		Build()
	resp, err := g.client.Cardkit.V1.Card.Settings(ctx, req)
	if err != nil {
		return fmt.Errorf("finalize cardkit card failed, %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("finalize cardkit card failed, code=%d msg=%q", resp.Code, resp.Msg)
	}
	return nil
}

func (g *LiveGateway) convertMessageReceive(event *larkim.P2MessageReceiveV1) Event {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return Event{}
	}
	msg := event.Event.Message
	sender := event.Event.Sender
	ev := Event{
		MessageID:   ptr(msg.MessageId),
		ChatID:      ptr(msg.ChatId),
		ChatType:    ptr(msg.ChatType),
		MessageType: ptr(msg.MessageType),
		EventType:   EventMessage,
		Text:        parseText(ptr(msg.Content)),
		ThreadID:    ptr(msg.ThreadId),
		Mentions:    mentionIDs(msg.Mentions),
	}
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		ev.EventID = event.EventV2Base.Header.EventID
		if event.EventV2Base.Header.EventType != "" {
			ev.EventType = event.EventV2Base.Header.EventType
		}
	}
	if sender != nil {
		ev.SenderType = ptr(sender.SenderType)
		if sender.SenderId != nil {
			ev.SenderID = firstNonEmpty(ptr(sender.SenderId.OpenId), ptr(sender.SenderId.UserId), ptr(sender.SenderId.UnionId))
		}
	}
	return ev
}

func textContent(text string) (string, error) {
	bs, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return "", fmt.Errorf("marshal lark text content failed, %w", err)
	}
	return string(bs), nil
}

func cardContent(card Card) (string, error) {
	if card.Raw != nil {
		bs, err := json.Marshal(card.Raw)
		if err != nil {
			return "", fmt.Errorf("marshal lark raw card content failed, %w", err)
		}
		return string(bs), nil
	}
	content := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"elements": []map[string]any{{
			"tag": "div",
			"text": map[string]string{
				"tag":     "lark_md",
				"content": card.Text,
			},
		}},
	}
	bs, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("marshal lark card content failed, %w", err)
	}
	return string(bs), nil
}

func parseText(content string) string {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &body); err != nil {
		return content
	}
	return body.Text
}

func mentionIDs(mentions []*larkim.MentionEvent) []string {
	ids := make([]string, 0, len(mentions))
	for _, mention := range mentions {
		if mention == nil || mention.Id == nil {
			continue
		}
		ids = append(ids, firstNonEmpty(ptr(mention.Id.OpenId), ptr(mention.Id.UserId), ptr(mention.Id.UnionId)))
	}
	return ids
}

func ptr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
