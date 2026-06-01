package lark

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/samuelncui/acp2larkbot/config"
)

type Renderer interface {
	Start(ctx context.Context, req StartRenderRequest) (*RenderHandle, error)
	Append(ctx context.Context, handle *RenderHandle, delta string) error
	AppendProcess(ctx context.Context, handle *RenderHandle, delta string) error
	Finish(ctx context.Context, handle *RenderHandle, final string) error
	Fail(ctx context.Context, handle *RenderHandle, err error) error
}

type StartRenderRequest struct {
	ChatID           string
	ReplyToMessageID string
}

type RenderHandle struct {
	MessageID string
	ChatID    string
	CardID    string
	ElementID string
	sequence  int
	buffer    string
	answer    string
	segment   string
	final     string
	process   []string
	blocks    []cardBlock
	blockSeq  int
	finalView string
	failed    string
	updates   int
	startedAt time.Time
	lastFlush time.Time
	mu        sync.Mutex

	// CardStreamingRenderer uses these fields instead of the generic ones above.
	state    *CardState
	lastSent map[string]string // elementID → content
}

type cardBlock struct {
	Kind      string
	Title     string
	Content   string
	ElementID string
}

type StreamingRenderer struct {
	cfg     config.StreamingConfig
	gw      Gateway
	filter  *SelfFilter
	limiter *streamingLimiter
	now     func() time.Time
}

func NewStreamingRenderer(cfg config.StreamingConfig, gw Gateway, filter *SelfFilter) *StreamingRenderer {
	return &StreamingRenderer{cfg: cfg, gw: gw, filter: filter, limiter: newStreamingLimiter(cfg.RateLimit), now: time.Now}
}

func (r *StreamingRenderer) Start(ctx context.Context, req StartRenderRequest) (*RenderHandle, error) {
	var sent *SentMessage
	if err := r.withRetry(ctx, req.ChatID, func() error {
		var err error
		sent, err = r.gw.SendText(ctx, req.ChatID, "Thinking...")
		return err
	}); err != nil {
		return nil, err
	}
	r.gw.RememberSelfMessage(sent.MessageID)
	r.filter.Remember(sent.MessageID)
	return &RenderHandle{MessageID: sent.MessageID, ChatID: req.ChatID, startedAt: r.now(), lastFlush: r.now()}, nil
}

func (r *StreamingRenderer) Append(ctx context.Context, handle *RenderHandle, delta string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.buffer += delta
	return r.maybeFlushLocked(ctx, handle, delta)
}

func (r *StreamingRenderer) AppendProcess(ctx context.Context, handle *RenderHandle, delta string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.buffer += delta
	return r.flushLocked(ctx, handle, false)
}

func (r *StreamingRenderer) Finish(ctx context.Context, handle *RenderHandle, final string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if final != "" {
		handle.buffer = final
	}
	return r.flushLocked(ctx, handle, true)
}

func (r *StreamingRenderer) Fail(ctx context.Context, handle *RenderHandle, err error) error {
	if err == nil {
		err = errors.New("unknown error")
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.buffer += "\n\nGeneration failed: " + err.Error()
	return r.flushLocked(ctx, handle, true)
}

func (r *StreamingRenderer) flushLocked(ctx context.Context, handle *RenderHandle, final bool) error {
	text := handle.buffer
	if final {
		text = truncateRunes(text, r.cfg.MaxFinalChars, r.cfg.TruncateNotice)
	} else {
		text = truncateRunes(text, r.cfg.MaxUpdateChars, "")
	}
	if err := r.withRetry(ctx, handle.ChatID, func() error { return r.gw.UpdateText(ctx, handle.MessageID, text) }); err != nil {
		if r.cfg.Fallback == "append_messages" && r.cfg.FallbackMaxMessages > 0 {
			var sent *SentMessage
			if sendErr := r.withRetry(ctx, handle.ChatID, func() error {
				var err error
				sent, err = r.gw.SendText(ctx, handle.ChatID, text)
				return err
			}); sendErr != nil {
				return err
			}
			r.gw.RememberSelfMessage(sent.MessageID)
			r.filter.Remember(sent.MessageID)
			return nil
		}
		return err
	}
	handle.updates++
	handle.lastFlush = r.now()
	return nil
}

func (r *StreamingRenderer) withRetry(ctx context.Context, chatID string, op func() error) error {
	return withStreamingRetry(ctx, r.cfg, r.limiter, chatID, op)
}

type streamingLimiter struct {
	mu              sync.Mutex
	globalNext      time.Time
	perChatNext     map[string]time.Time
	globalInterval  time.Duration
	perChatInterval time.Duration
}

func newStreamingLimiter(cfg config.RateLimitPair) *streamingLimiter {
	return &streamingLimiter{
		perChatNext:     map[string]time.Time{},
		globalInterval:  rateInterval(cfg.Global),
		perChatInterval: rateInterval(cfg.PerChat),
	}
}

func rateInterval(rate config.Rate) time.Duration {
	if rate.Limit <= 0 || rate.Window <= 0 {
		return 0
	}
	return rate.Window / time.Duration(rate.Limit)
}

func (l *streamingLimiter) Wait(ctx context.Context, chatID string) error {
	if l == nil || (l.globalInterval <= 0 && l.perChatInterval <= 0) {
		return nil
	}
	now := time.Now()
	l.mu.Lock()
	availableAt := now
	if l.globalInterval > 0 && l.globalNext.After(availableAt) {
		availableAt = l.globalNext
	}
	if l.perChatInterval > 0 && l.perChatNext[chatID].After(availableAt) {
		availableAt = l.perChatNext[chatID]
	}
	if l.globalInterval > 0 {
		l.globalNext = availableAt.Add(l.globalInterval)
	}
	if l.perChatInterval > 0 {
		l.perChatNext[chatID] = availableAt.Add(l.perChatInterval)
	}
	l.mu.Unlock()

	wait := time.Until(availableAt)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withStreamingRetry(ctx context.Context, cfg config.StreamingConfig, limiter *streamingLimiter, chatID string, op func() error) error {
	attempts := cfg.Retry.MaxRetries + 1
	if attempts <= 0 {
		attempts = 1
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if waitErr := limiter.Wait(ctx, chatID); waitErr != nil {
			return waitErr
		}
		err = op()
		if err == nil || attempt == attempts-1 || !shouldRetryStreaming(err, cfg.Retry) {
			return err
		}
		if sleepErr := sleepRetryBackoff(ctx, cfg.Retry, attempt); sleepErr != nil {
			return sleepErr
		}
	}
	return err
}

func shouldRetryStreaming(err error, cfg config.RetryConfig) bool {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "429") || strings.Contains(text, "rate limit") {
		return cfg.RetryAfter429
	}
	return true
}

func sleepRetryBackoff(ctx context.Context, cfg config.RetryConfig, attempt int) error {
	backoff := cfg.Backoff.Duration
	if backoff <= 0 {
		return nil
	}
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if cfg.MaxBackoff.Duration > 0 && backoff > cfg.MaxBackoff.Duration {
			backoff = cfg.MaxBackoff.Duration
			break
		}
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *StreamingRenderer) maybeFlushLocked(ctx context.Context, handle *RenderHandle, delta string) error {
	if handle.updates >= r.cfg.MaxUpdatesPerMessage {
		return nil
	}
	if r.now().Sub(handle.lastFlush) < r.cfg.UpdateInterval.Duration && len(delta) < r.cfg.MinUpdateChars {
		return nil
	}
	return r.flushLocked(ctx, handle, false)
}

func truncateRunes(text string, max int, suffix string) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + suffix
}
