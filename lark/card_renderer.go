package lark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samuelncui/acp2larkbot/config"
)

const cardProcessHistoryMax = 8

const (
	cardStreamingFinalElementID = "answer_final"
	cardBlockKindTool           = "tool"
	cardBlockKindAnswer         = "answer"
)

type CardRenderer struct {
	cfg     config.StreamingConfig
	gw      Gateway
	filter  *SelfFilter
	limiter *streamingLimiter
	now     func() time.Time
}

func NewCardRenderer(cfg config.StreamingConfig, gw Gateway, filter *SelfFilter) *CardRenderer {
	return &CardRenderer{cfg: cfg, gw: gw, filter: filter, limiter: newStreamingLimiter(cfg.RateLimit), now: time.Now}
}

func (r *CardRenderer) Start(ctx context.Context, req StartRenderRequest) (*RenderHandle, error) {
	card := Card{Raw: buildStructuredCard(nil, false)}
	var sent *SentMessage
	if err := r.withRetry(ctx, req.ChatID, func() error {
		var err error
		sent, err = r.gw.CreateCard(ctx, req.ChatID, card)
		return err
	}); err != nil {
		return nil, fmt.Errorf("card: create card failed, %w", err)
	}
	r.gw.RememberSelfMessage(sent.MessageID)
	r.filter.Remember(sent.MessageID)
	return &RenderHandle{MessageID: sent.MessageID, ChatID: req.ChatID, startedAt: r.now(), lastFlush: r.now()}, nil
}

func (r *CardRenderer) Append(ctx context.Context, handle *RenderHandle, delta string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.answer += delta
	return r.maybeFlushLocked(ctx, handle, delta)
}

func (r *CardRenderer) AppendProcess(ctx context.Context, handle *RenderHandle, delta string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	appendProcess(handle, delta)
	return r.flushLocked(ctx, handle, false)
}

func (r *CardRenderer) Finish(ctx context.Context, handle *RenderHandle, final string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if final != "" {
		handle.final = final
	}
	return r.flushLocked(ctx, handle, true)
}

func (r *CardRenderer) Fail(ctx context.Context, handle *RenderHandle, err error) error {
	if err == nil {
		err = errors.New("unknown error")
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.failed = err.Error()
	return r.flushLocked(ctx, handle, true)
}

func (r *CardRenderer) flushLocked(ctx context.Context, handle *RenderHandle, final bool) error {
	card := Card{Raw: buildStructuredCard(handle, final)}
	if err := truncateCardMarkdown(card.Raw, final, r.cfg); err != nil {
		return err
	}
	if err := r.withRetry(ctx, handle.ChatID, func() error { return r.gw.UpdateCard(ctx, handle.MessageID, card) }); err != nil {
		return err
	}
	handle.updates++
	handle.lastFlush = r.now()
	return nil
}

func (r *CardRenderer) withRetry(ctx context.Context, chatID string, op func() error) error {
	return withStreamingRetry(ctx, r.cfg, r.limiter, chatID, op)
}

func (r *CardRenderer) maybeFlushLocked(ctx context.Context, handle *RenderHandle, delta string) error {
	if handle.updates >= r.cfg.MaxUpdatesPerMessage {
		return nil
	}
	if r.now().Sub(handle.lastFlush) < r.cfg.UpdateInterval.Duration && len(delta) < r.cfg.MinUpdateChars {
		return nil
	}
	return r.flushLocked(ctx, handle, false)
}

// CardStreamingRenderer renders a structured streaming card with collapsible action panels
// above a typewriter-animated final answer.
type CardStreamingRenderer struct {
	cfg     config.StreamingConfig
	gw      Gateway
	filter  *SelfFilter
	limiter *streamingLimiter
	now     func() time.Time
}

func NewCardStreamingRenderer(cfg config.StreamingConfig, gw Gateway, filter *SelfFilter) *CardStreamingRenderer {
	return &CardStreamingRenderer{cfg: cfg, gw: gw, filter: filter, limiter: newStreamingLimiter(cfg.RateLimit), now: time.Now}
}

func (r *CardStreamingRenderer) Start(ctx context.Context, req StartRenderRequest) (*RenderHandle, error) {
	var cardID string
	if err := r.withRetry(ctx, req.ChatID, func() error {
		var err error
		cardID, err = r.gw.CreateStreamingCard(ctx, Card{Raw: buildInitialCard()})
		return err
	}); err != nil {
		return nil, fmt.Errorf("card_streaming: create card failed, %w", err)
	}
	var sent *SentMessage
	if err := r.withRetry(ctx, req.ChatID, func() error {
		var err error
		sent, err = r.gw.SendCardByID(ctx, req.ChatID, cardID)
		return err
	}); err != nil {
		return nil, fmt.Errorf("card_streaming: send card failed, %w", err)
	}
	r.gw.RememberSelfMessage(sent.MessageID)
	r.filter.Remember(sent.MessageID)

	state := &CardState{
		Actions:     nil,
		FinalAnswer: "",
		Finished:    false,
	}
	return &RenderHandle{
		MessageID: sent.MessageID,
		ChatID:    req.ChatID,
		CardID:    cardID,
		state:     state,
		lastSent:  map[string]string{},
		sequence:  1,
		startedAt: r.now(),
		lastFlush: r.now(),
	}, nil
}

func (r *CardStreamingRenderer) Append(ctx context.Context, handle *RenderHandle, delta string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.state.FinalAnswer += delta
	return r.syncFinalAnswerLocked(ctx, handle)
}

func (r *CardStreamingRenderer) AppendProcess(ctx context.Context, handle *RenderHandle, delta string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()

	text := strings.TrimSpace(delta)
	if text == "" {
		return nil
	}

	// Determine action type from delta text
	actType, actTitle := classifyProcessDelta(text)
	actions := handle.state.Actions

	// Check if this is a continuation of the last action
	if len(actions) > 0 && actions[len(actions)-1].Type == actType &&
		(actType == ActionTool || actType == ActionProcess) &&
		isProcessContinuation(text) {
		// Update last action detail
		last := &actions[len(actions)-1]
		last.Detail = strings.TrimSpace(last.Detail + "\n" + formatProcessDetail(text))
		return r.syncActionContentLocked(ctx, handle, last)
	}

	// New action
	act := Action{
		ID:    fmt.Sprintf("panel_%d", len(actions)),
		Seq:   len(actions),
		Type:  actType,
		Title: actTitle,
		State: ActRunning,
		Detail: formatProcessDetail(text),
	}
	handle.state.Actions = append(handle.state.Actions, act)

	// Insert panel before answer_final
	seq := handle.sequence
	handle.sequence++
	if err := r.withRetry(ctx, handle.ChatID, func() error {
		return r.gw.InsertStreamingElementsBefore(ctx, handle.CardID, finalAnswerElementID, []map[string]any{buildPanelForInsert(act)}, seq)
	}); err != nil {
		return err
	}
	handle.lastSent[act.ID+"_content"] = act.Detail
	handle.updates++
	handle.lastFlush = r.now()

	// Compact if too many actions
	if len(handle.state.Actions) > maxActions {
		handle.state.Actions = compactActions(handle.state.Actions)
	}
	return nil
}

func (r *CardStreamingRenderer) Finish(ctx context.Context, handle *RenderHandle, final string) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()

	if final != "" {
		handle.state.FinalAnswer = final
	}
	// Ensure final answer is synced
	if err := r.syncFinalAnswerLocked(ctx, handle); err != nil {
		return err
	}

	handle.state.Finished = true

	// 1. Finalize streaming (streaming_mode: false)
	if err := r.finalizeLocked(ctx, handle); err != nil {
		return err
	}

	// 2. Send final static card with correct icons + collapsed panels
	return r.patchFinalCardLocked(ctx, handle)
}

func (r *CardStreamingRenderer) Fail(ctx context.Context, handle *RenderHandle, err error) error {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()

	// Add error action if there's a last running action
	actions := handle.state.Actions
	if len(actions) > 0 && actions[len(actions)-1].State == ActRunning {
		actions[len(actions)-1].State = ActFailed
		actions[len(actions)-1].Detail = strings.TrimSpace(actions[len(actions)-1].Detail + "\n\n❌ " + err.Error())
	} else {
		// Standalone error action
		act := Action{
			ID:     fmt.Sprintf("panel_%d", len(actions)),
			Seq:    len(actions),
			Type:   ActionProcess,
			Title:  "Error",
			State:  ActFailed,
			Detail: err.Error(),
		}
		handle.state.Actions = append(handle.state.Actions, act)
	}

	handle.state.Finished = true

	if err := r.finalizeLocked(ctx, handle); err != nil {
		return err
	}
	return r.patchFinalCardLocked(ctx, handle)
}

// ─── Sync helpers ──────────────────────────────────────────────

func (r *CardStreamingRenderer) syncFinalAnswerLocked(ctx context.Context, handle *RenderHandle) error {
	content := handle.state.FinalAnswer
	if content == "" {
		content = "_Waiting..._"
	}
	content = truncateRunes(content, r.cfg.MaxUpdateChars, "")
	if content == handle.lastSent[finalAnswerElementID] {
		return nil
	}
	seq := handle.sequence
	handle.sequence++
	if err := r.withRetry(ctx, handle.ChatID, func() error {
		return r.gw.UpdateStreamingElement(ctx, handle.CardID, finalAnswerElementID, content, seq)
	}); err != nil {
		return err
	}
	handle.lastSent[finalAnswerElementID] = content
	handle.updates++
	handle.lastFlush = r.now()
	return nil
}

func (r *CardStreamingRenderer) syncActionContentLocked(ctx context.Context, handle *RenderHandle, a *Action) error {
	content := renderActionContent(*a)
	content = truncateRunes(content, r.cfg.MaxUpdateChars, "")
	elementID := a.ID + "_content"
	if content == handle.lastSent[elementID] {
		return nil
	}
	seq := handle.sequence
	handle.sequence++
	if err := r.withRetry(ctx, handle.ChatID, func() error {
		return r.gw.UpdateStreamingElement(ctx, handle.CardID, elementID, content, seq)
	}); err != nil {
		return err
	}
	handle.lastSent[elementID] = content
	handle.updates++
	handle.lastFlush = r.now()
	return nil
}

func (r *CardStreamingRenderer) finalizeLocked(ctx context.Context, handle *RenderHandle) error {
	seq := handle.sequence
	handle.sequence++
	return r.withRetry(ctx, handle.ChatID, func() error {
		return r.gw.FinalizeStreamingCard(ctx, handle.CardID, seq)
	})
}

func (r *CardStreamingRenderer) patchFinalCardLocked(ctx context.Context, handle *RenderHandle) error {
	if handle.MessageID == "" {
		return nil
	}
	card := Card{Raw: buildFinalCard(handle.state)}
	return r.withRetry(ctx, handle.ChatID, func() error {
		return r.gw.UpdateCard(ctx, handle.MessageID, card)
	})
}

func (r *CardStreamingRenderer) withRetry(ctx context.Context, chatID string, op func() error) error {
	return withStreamingRetry(ctx, r.cfg, r.limiter, chatID, op)
}

// ─── Process delta classification ──────────────────────────────

// classifyProcessDelta determines the action type and title from a process delta string.
func classifyProcessDelta(text string) (ActionType, string) {
	clean := strings.TrimSpace(text)
	clean = strings.TrimPrefix(clean, "[Process] ")
	clean = strings.TrimPrefix(clean, "[Process]")

	// Tool call patterns
	for _, prefix := range []string{"Starting ", "Calling ", "Running ", "Executing "} {
		if strings.HasPrefix(clean, prefix) {
			title := strings.TrimSpace(strings.TrimPrefix(clean, prefix))
			if before, _, ok := cutProcessOutput(title); ok {
				title = strings.TrimSpace(before)
			}
			return ActionTool, firstLineOrDefault(title, "Tool")
		}
	}
	// Tool result pattern
	if strings.HasPrefix(clean, "✓ ") || strings.HasPrefix(clean, "✅ ") ||
		strings.Contains(clean, "completed") || strings.Contains(clean, "output:") {
		return ActionTool, ""
	}

	// Thinking pattern
	if strings.Contains(strings.ToLower(clean), "thinking") ||
		strings.Contains(strings.ToLower(clean), "analyzing") ||
		strings.Contains(strings.ToLower(clean), "reasoning") {
		return ActionThinking, "Thinking"
	}

	return ActionProcess, firstLineOrDefault(clean, "Process")
}

func isProcessContinuation(text string) bool {
	clean := strings.TrimSpace(text)
	clean = strings.TrimPrefix(clean, "[Process] ")
	clean = strings.TrimPrefix(clean, "[Process]")
	_, _, hasOutput := cutProcessOutput(clean)
	return hasOutput || strings.Contains(clean, "completed") || strings.Contains(clean, "✓")
}

func formatProcessDetail(text string) string {
	clean := strings.TrimSpace(text)
	clean = strings.TrimPrefix(clean, "[Process] ")
	clean = strings.TrimPrefix(clean, "[Process]")
	if before, after, ok := cutProcessOutput(clean); ok {
		before = strings.TrimSpace(before)
		if before != "" {
			return fmt.Sprintf("**%s**\n%s", before, codeFence(trimProcessOutput(strings.TrimSpace(after))))
		}
		return codeFence(trimProcessOutput(strings.TrimSpace(after)))
	}
	return clean
}

func appendProcess(handle *RenderHandle, delta string) {
	text := strings.TrimSpace(delta)
	if text == "" {
		return
	}
	handle.process = append(handle.process, text)
}

func buildStructuredCard(handle *RenderHandle, final bool) map[string]any {
	status, draftAnswer, finalAnswer, processMarkdown, failed := renderCardSections(handle, final)

	processElements := []map[string]any{markdownElement("", processMarkdown)}
	if failed != "" {
		processElements = append(processElements, markdownElement("", codeFence(trimProcessOutput(failed))))
	}

	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"width_mode": "fill",
		},
		"body": map[string]any{
			"elements": []map[string]any{
				{
					"tag":      "collapsible_panel",
					"expanded": false,
					"header": map[string]any{
						"title": map[string]any{
							"tag":     "plain_text",
							"content": "Process · " + status,
						},
					},
					"elements": processElements,
				},
				collapsibleBlockElement(cardBlock{Title: summarizeAnswerTitle(draftAnswer), Content: draftAnswer}),
				markdownElement("", finalAnswer),
			},
		},
	}
}

func buildStructuredStreamingCard(handle *RenderHandle, final bool) map[string]any {
	_, _, finalAnswer, _, _ := renderCardSections(handle, final)
	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": true,
			"update_multi":   true,
			"width_mode":     "fill",
		},
		"body": map[string]any{
			"elements": []map[string]any{
				markdownElement(cardStreamingFinalElementID, finalAnswer),
			},
		},
	}
}

func buildStreamingSnapshotCard(handle *RenderHandle, final bool) map[string]any {
	elements := []map[string]any{}
	if handle != nil {
		for _, block := range handle.blocks {
			elements = append(elements, collapsibleBlockElement(block))
		}
	}
	elements = append(elements, markdownElement(cardStreamingFinalElementID, renderFinalAnswer(handle, final)))
	config := map[string]any{
		"width_mode": "fill",
	}
	if final {
		config["streaming_mode"] = false
	}
	return map[string]any{
		"schema": "2.0",
		"config": config,
		"body": map[string]any{
			"elements": elements,
		},
	}
}

func collapsibleBlockElement(block cardBlock) map[string]any {
	return map[string]any{
		"tag":      "collapsible_panel",
		"expanded": false,
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": block.Title,
			},
		},
		"elements": []map[string]any{
			markdownElement(block.ElementID, block.Content),
		},
	}
}

func renderDraftAnswer(handle *RenderHandle) string {
	if handle == nil || strings.TrimSpace(handle.answer) == "" {
		if handle != nil && handle.failed != "" {
			return "_Interrupted_"
		}
		return "_Thinking..._"
	}
	return handle.answer
}

func renderFinalAnswer(handle *RenderHandle, final bool) string {
	if handle == nil {
		return "_Waiting for final answer..._"
	}
	if strings.TrimSpace(handle.final) != "" {
		return handle.final
	}
	if final && strings.TrimSpace(handle.answer) != "" {
		return handle.answer
	}
	if !final && strings.TrimSpace(handle.segment) != "" {
		return handle.segment
	}
	if handle.failed != "" {
		return "_Generation failed_"
	}
	return "_Waiting for final answer..._"
}

func renderCardSections(handle *RenderHandle, final bool) (status string, draftAnswer string, finalAnswer string, processMarkdown string, failed string) {
	status = "🤖 Thinking"
	draftAnswer = "_Thinking..._"
	finalAnswer = "_Waiting for final answer..._"
	processes := []string(nil)
	if handle != nil {
		failed = handle.failed
		if failed != "" {
			status = "❌ Generation failed"
		} else if final || strings.TrimSpace(handle.final) != "" {
			status = "✅ Completed"
		} else if strings.TrimSpace(handle.answer) != "" || len(handle.process) > 0 {
			status = "🤖 Processing"
		}
		if strings.TrimSpace(handle.answer) != "" {
			draftAnswer = handle.answer
		} else if failed != "" {
			draftAnswer = "_Interrupted_"
		}
		if strings.TrimSpace(handle.final) != "" {
			finalAnswer = handle.final
		} else if final && strings.TrimSpace(handle.answer) != "" {
			finalAnswer = handle.answer
		}
		processes = handle.process
	}
	processMarkdown = buildProcessPanelMarkdown(processes)
	return
}

func buildProcessPanelMarkdown(processes []string) string {
	if len(processes) == 0 {
		return "_No process updates yet_"
	}
	parts := make([]string, 0, len(processes)+1)
	if len(processes) > cardProcessHistoryMax {
		parts = append(parts, fmt.Sprintf("_Showing only the latest %d process updates. Earlier updates were folded._", cardProcessHistoryMax))
		processes = processes[len(processes)-cardProcessHistoryMax:]
	}
	for _, item := range processes {
		parts = append(parts, formatProcessMarkdown(item))
	}
	return strings.Join(parts, "\n\n")
}

func markdownElement(elementID string, content string) map[string]any {
	element := map[string]any{
		"tag":     "markdown",
		"content": content,
	}
	if elementID != "" {
		element["element_id"] = elementID
	}
	return element
}

func truncateCardMarkdown(card map[string]any, final bool, cfg config.StreamingConfig) error {
	max := cfg.MaxUpdateChars
	suffix := ""
	if final {
		max = cfg.MaxFinalChars
		suffix = cfg.TruncateNotice
	}
	truncateNestedMarkdown(card, max, suffix)
	return nil
}

func truncateNestedMarkdown(node any, max int, suffix string) {
	switch value := node.(type) {
	case map[string]any:
		if tag, _ := value["tag"].(string); tag == "markdown" {
			if content, ok := value["content"].(string); ok {
				value["content"] = truncateRunes(content, max, suffix)
			}
		}
		for _, child := range value {
			truncateNestedMarkdown(child, max, suffix)
		}
	case []map[string]any:
		for _, item := range value {
			truncateNestedMarkdown(item, max, suffix)
		}
	case []any:
		for _, item := range value {
			truncateNestedMarkdown(item, max, suffix)
		}
	}
}

func formatProcessMarkdown(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "[Process] ")
	text = strings.TrimPrefix(text, "[Process]")
	if strings.HasPrefix(text, "[") && strings.Contains(text, "] ") {
		if idx := strings.Index(text, "] "); idx >= 0 {
			text = text[idx+2:]
		}
	}
	if text == "" {
		return "- _Empty process update_"
	}
	if before, after, ok := cutProcessOutput(text); ok {
		title := strings.TrimSpace(before)
		if title == "" {
			title = "Output"
		}
		return fmt.Sprintf("- %s\n%s", title, codeFence(trimProcessOutput(strings.TrimSpace(after))))
	}
	return "- " + text
}

func summarizeToolTitle(text string) string {
	clean := strings.TrimSpace(text)
	clean = strings.TrimPrefix(clean, "[Process] ")
	clean = strings.TrimPrefix(clean, "[Process]")
	for _, prefix := range []string{"Starting ", "Calling ", "Running "} {
		if strings.HasPrefix(clean, prefix) {
			clean = strings.TrimSpace(strings.TrimPrefix(clean, prefix))
			break
		}
	}
	if before, _, ok := cutProcessOutput(clean); ok {
		clean = strings.TrimSpace(before)
	}
	return firstLineOrDefault(clean, "Process")
}

func summarizeAnswerTitle(text string) string {
	return firstLineOrDefault(strings.TrimSpace(text), "Answer")
}

func firstLineOrDefault(text string, fallback string) string {
	if text == "" {
		return fallback
	}
	line := strings.TrimSpace(strings.Split(text, "\n")[0])
	line = strings.Trim(line, "*_`#- ")
	runes := []rune(line)
	if len(runes) > 32 {
		line = string(runes[:32]) + "..."
	}
	if line == "" {
		return fallback
	}
	return line
}

func isToolContinuation(text string) bool {
	clean := strings.TrimSpace(text)
	clean = strings.TrimPrefix(clean, "[Process] ")
	clean = strings.TrimPrefix(clean, "[Process]")
	_, _, hasOutput := cutProcessOutput(clean)
	return hasOutput || strings.Contains(clean, "completed")
}

func cutProcessOutput(text string) (string, string, bool) {
	if before, after, ok := strings.Cut(text, "output:\n"); ok {
		return before, after, true
	}
	if before, after, ok := strings.Cut(text, "Output:\n"); ok {
		return before, after, true
	}
	return "", "", false
}

func codeFence(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "```", "'''")
	if text == "" {
		return "```text\n(empty)\n```"
	}
	return "```text\n" + text + "\n```"
}

func trimProcessOutput(text string) string {
	const maxRunes = 500
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "\n...[truncated]"
}
