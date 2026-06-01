package lark

import (
	"fmt"
	"strings"
)

// ─── Action state ──────────────────────────────────────────────

type ActionType string

const (
	ActionTool     ActionType = "tool_call"
	ActionThinking ActionType = "thinking"
	ActionProcess  ActionType = "process"
)

type ActionState string

const (
	ActRunning ActionState = "running"
	ActDone    ActionState = "done"
	ActFailed  ActionState = "failed"
)

// Action represents a single step in the agent workflow.
type Action struct {
	ID    string      // element_id: "panel_0"
	Seq   int         // 0, 1, 2, ...
	Type  ActionType  // tool_call / thinking / process
	Title string      // "Tool: search_files"
	State ActionState // running / done / failed
	// Detail is the body content of the action panel.
	Detail string
}

// CardState is the authoritative model for the streaming card.
type CardState struct {
	Actions     []Action
	FinalAnswer string
	Finished    bool
}

// ─── Card JSON builders ────────────────────────────────────────

const finalAnswerElementID = "answer_final"

// buildInitialCard returns the card JSON used when creating the streaming card.
// It contains only the final answer placeholder.
func buildInitialCard() map[string]any {
	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": true,
			"update_multi":   true,
			"width_mode":     "fill",
		},
		"body": map[string]any{
			"elements": []map[string]any{
				markdownElement(finalAnswerElementID, "_Waiting..._"),
			},
		},
	}
}

// buildFinalCard returns the card JSON used when finalizing (UpdateCard after stream is closed).
func buildFinalCard(s *CardState) map[string]any {
	var elements []map[string]any

	lastIdx := len(s.Actions) - 1
	for i, a := range s.Actions {
		elements = append(elements, buildPanel(a, true, i == lastIdx))
	}

	answer := s.FinalAnswer
	if answer == "" {
		answer = "_Waiting..._"
	}
	elements = append(elements, markdownElement(finalAnswerElementID, answer))

	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": false,
			"update_multi":   true,
			"width_mode":     "fill",
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
}

// buildPanel renders a single action as a collapsible_panel.
// During streaming (final=false), all panels are expanded and use 🐾 icon.
// After finalize (final=true), correct icon + collapse old panels.
func buildPanel(a Action, final bool, isLast bool) map[string]any {
	icon := actionStreamingIcon
	if final {
		icon = actionFinalIcon(a.Type, a.State)
	}

	expanded := true
	if final && !isLast {
		expanded = false
	}

	header := fmt.Sprintf("%s %s", icon, titleForAction(a))
	content := a.Detail
	if content == "" {
		content = "_Running..._"
	}

	return map[string]any{
		"tag":        "collapsible_panel",
		"element_id": a.ID,
		"expanded":   expanded,
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": header,
			},
		},
		"elements": []map[string]any{
			markdownElement(a.ID+"_content", content),
		},
	}
}

// buildPanelForInsert renders a panel for InsertStreamingElementsBefore.
// During streaming: 🐾 icon, expanded=true.
func buildPanelForInsert(a Action) map[string]any {
	return buildPanel(a, false, true)
}

// ─── Icons ─────────────────────────────────────────────────────

const actionStreamingIcon = "🐾"

func actionFinalIcon(typ ActionType, state ActionState) string {
	switch state {
	case ActDone:
		switch typ {
		case ActionThinking:
			return "💡"
		default:
			return "✅"
		}
	case ActFailed:
		return "❌"
	default:
		return "🐾"
	}
}

func titleForAction(a Action) string {
	if a.Title != "" {
		return a.Title
	}
	switch a.Type {
	case ActionTool:
		return "Tool"
	case ActionThinking:
		return "Thinking"
	default:
		return "Process"
	}
}

// ─── Action content rendering ──────────────────────────────────

func renderActionContent(a Action) string {
	content := a.Detail
	if content == "" {
		return "_Running..._"
	}
	return content
}

// ─── History compaction ────────────────────────────────────────

const maxActions = 8

// compactActions merges old actions into a summary panel when count exceeds maxActions.
func compactActions(actions []Action) []Action {
	if len(actions) <= maxActions {
		return actions
	}
	overflow := len(actions) - maxActions + 1
	merged := actions[:overflow]
	rest := actions[overflow:]

	lines := make([]string, len(merged))
	for i, a := range merged {
		lines[i] = fmt.Sprintf("> %s", titleForAction(a))
	}
	summary := Action{
		ID:    merged[0].ID,
		Seq:   merged[0].Seq,
		Type:  ActionProcess,
		Title: fmt.Sprintf("📋 %d earlier actions", len(merged)),
		State: ActDone,
		Detail: strings.Join(lines, "\n"),
	}
	if summary.Detail == "" {
		summary.Detail = "_No detail_"
	}

	result := make([]Action, 0, len(rest)+1)
	result = append(result, summary)
	// Re-index
	for i := range rest {
		rest[i].ID = fmt.Sprintf("panel_%d", i+1)
		rest[i].Seq = i + 1
	}
	result = append(result, rest...)
	return result
}
