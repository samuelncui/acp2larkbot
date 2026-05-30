package lark

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samuelncui/acp2larkbot/config"
)

func TestCardRendererIncludesProcessAndFinalAnswer(t *testing.T) {
	gw := NewFakeGateway()
	filter := NewSelfFilter(config.LarkConfig{Ignore: config.LarkIgnore{MessageIDTTL: config.Duration{Duration: time.Hour}, MaxMessageIDs: 100}})
	r := NewCardRenderer(testStreamingConfig(), gw, filter)

	handle, err := r.Start(context.Background(), StartRenderRequest{ChatID: "oc_test"})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := r.AppendProcess(context.Background(), handle, "\n\n[Process] Starting bash: pwd"); err != nil {
		t.Fatalf("AppendProcess returned error: %v", err)
	}
	if err := r.Append(context.Background(), handle, "hello"); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if err := r.Finish(context.Background(), handle, "final answer"); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	card := mustDecodeCard(t, gw.Message(handle.MessageID))
	elements := mustCardElements(t, card)
	if len(elements) != 3 {
		t.Fatalf("expected 3 top-level elements, got %d", len(elements))
	}
	if tag := elements[0]["tag"]; tag != "collapsible_panel" {
		t.Fatalf("expected first element to be collapsible_panel, got %#v", tag)
	}
	if expanded := elements[0]["expanded"]; expanded != false {
		t.Fatalf("expected process panel default collapsed, got %#v", expanded)
	}
	processElements, ok := elements[0]["elements"].([]any)
	if !ok || len(processElements) == 0 {
		t.Fatalf("expected process panel inner elements, got %#v", elements[0]["elements"])
	}
	processMarkdown := mustMarkdownContent(t, processElements[0])
	if want := "Starting bash: pwd"; !contains(processMarkdown, want) {
		t.Fatalf("expected process panel to contain %q, got %q", want, processMarkdown)
	}
	if tag := elements[1]["tag"]; tag != "collapsible_panel" {
		t.Fatalf("expected second element to be collapsible_panel, got %#v", tag)
	}
	if draft := mustMarkdownContent(t, mustFirstInnerElement(t, elements[1])); !contains(draft, "hello") {
		t.Fatalf("expected draft answer to contain hello, got %q", draft)
	}
	if final := mustMarkdownContent(t, elements[2]); !contains(final, "final answer") {
		t.Fatalf("expected final answer to contain final answer, got %q", final)
	}
}

func TestCardStreamingRendererShowsErrorSection(t *testing.T) {
	gw := NewFakeGateway()
	filter := NewSelfFilter(config.LarkConfig{Ignore: config.LarkIgnore{MessageIDTTL: config.Duration{Duration: time.Hour}, MaxMessageIDs: 100}})
	r := NewCardStreamingRenderer(testStreamingConfig(), gw, filter)

	handle, err := r.Start(context.Background(), StartRenderRequest{ChatID: "oc_test"})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := r.AppendProcess(context.Background(), handle, "[Process] bash output:\n/Users/test"); err != nil {
		t.Fatalf("AppendProcess returned error: %v", err)
	}
	if err := r.Fail(context.Background(), handle, errors.New("boom")); err != nil {
		t.Fatalf("Fail returned error: %v", err)
	}

	card := mustDecodeCard(t, gw.Message(handle.MessageID))
	elements := mustCardElements(t, card)
	if len(elements) != 2 {
		t.Fatalf("expected one folded block and final answer, got %d elements", len(elements))
	}
	if tag := elements[0]["tag"]; tag != "collapsible_panel" {
		t.Fatalf("expected first element to be collapsible_panel, got %#v", tag)
	}
	blockElements, ok := elements[0]["elements"].([]any)
	if !ok || len(blockElements) < 1 {
		t.Fatalf("expected folded block to include markdown, got %#v", elements[0]["elements"])
	}
	if got := mustMarkdownContent(t, blockElements[0]); !contains(got, "bash") || !contains(got, "/Users/test") || !contains(got, "boom") {
		t.Fatalf("expected folded block markdown to include bash output and boom, got %q", got)
	}
	if got := mustMarkdownContent(t, elements[1]); !contains(got, "Generation failed") {
		t.Fatalf("expected final markdown to show failure, got %q", got)
	}
}

func TestCardStreamingRendererUsesStreamingCardAndElementUpdates(t *testing.T) {
	gw := NewFakeGateway()
	filter := NewSelfFilter(config.LarkConfig{Ignore: config.LarkIgnore{MessageIDTTL: config.Duration{Duration: time.Hour}, MaxMessageIDs: 100}})
	r := NewCardStreamingRenderer(testStreamingConfig(), gw, filter)

	handle, err := r.Start(context.Background(), StartRenderRequest{ChatID: "oc_test"})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if handle.CardID == "" {
		t.Fatal("expected streaming renderer to allocate card id")
	}
	if err := r.AppendProcess(context.Background(), handle, "[Process] Starting bash: pwd"); err != nil {
		t.Fatalf("AppendProcess returned error: %v", err)
	}
	if err := r.Append(context.Background(), handle, "draft"); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if err := r.Finish(context.Background(), handle, "final"); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	card := mustDecodeCard(t, gw.Message(handle.MessageID))
	elements := mustCardElements(t, card)
	if len(elements) != 2 {
		t.Fatalf("expected one folded block and final answer, got %d elements", len(elements))
	}
	blockElements, ok := elements[0]["elements"].([]any)
	if !ok || len(blockElements) == 0 {
		t.Fatalf("expected folded block elements, got %#v", elements[0]["elements"])
	}
	processNode := mustMap(t, blockElements[0])
	if got := processNode["element_id"]; got == "" {
		t.Fatalf("expected inserted block to have element_id, got %#v", got)
	}
	if got := elements[1]["element_id"]; got != cardStreamingFinalElementID {
		t.Fatalf("expected final element_id %q, got %#v", cardStreamingFinalElementID, got)
	}
	if got := mustMarkdownContent(t, blockElements[0]); !contains(got, "Starting bash: pwd") {
		t.Fatalf("expected folded block content inserted before final, got %q", got)
	}
	if got := mustMarkdownContent(t, elements[1]); !contains(got, "final") {
		t.Fatalf("expected final content updated via streaming element, got %q", got)
	}
}

func TestCardStreamingRendererFoldsIntermediateAnswerBeforeTool(t *testing.T) {
	gw := NewFakeGateway()
	filter := NewSelfFilter(config.LarkConfig{Ignore: config.LarkIgnore{MessageIDTTL: config.Duration{Duration: time.Hour}, MaxMessageIDs: 100}})
	r := NewCardStreamingRenderer(testStreamingConfig(), gw, filter)

	handle, err := r.Start(context.Background(), StartRenderRequest{ChatID: "oc_test"})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := r.Append(context.Background(), handle, "First, a short explanation"); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if err := r.AppendProcess(context.Background(), handle, "[Process] Starting bash: pwd"); err != nil {
		t.Fatalf("AppendProcess returned error: %v", err)
	}
	if err := r.Finish(context.Background(), handle, "Final conclusion"); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	card := mustDecodeCard(t, gw.Message(handle.MessageID))
	elements := mustCardElements(t, card)
	if len(elements) != 3 {
		t.Fatalf("expected intermediate answer block, tool block, final answer; got %d elements", len(elements))
	}
	for i := 0; i < 2; i++ {
		if tag := elements[i]["tag"]; tag != "collapsible_panel" {
			t.Fatalf("expected element %d to be collapsed panel, got %#v", i, tag)
		}
		if expanded := elements[i]["expanded"]; expanded != false {
			t.Fatalf("expected element %d collapsed by default, got %#v", i, expanded)
		}
	}
	if got := mustMarkdownContent(t, mustFirstInnerElement(t, elements[0])); !contains(got, "First, a short explanation") {
		t.Fatalf("expected first folded block to contain intermediate answer, got %q", got)
	}
	if got := mustMarkdownContent(t, mustFirstInnerElement(t, elements[1])); !contains(got, "Starting bash: pwd") {
		t.Fatalf("expected second folded block to contain tool use, got %q", got)
	}
	if got := mustMarkdownContent(t, elements[2]); !contains(got, "Final conclusion") {
		t.Fatalf("expected final answer to be non-collapsible markdown, got %q", got)
	}
}

func TestCardStreamingRendererThrottlesFinalAnswerUpdates(t *testing.T) {
	gw := NewFakeGateway()
	filter := NewSelfFilter(config.LarkConfig{Ignore: config.LarkIgnore{MessageIDTTL: config.Duration{Duration: time.Hour}, MaxMessageIDs: 100}})
	r := NewCardStreamingRenderer(testStreamingConfig(), gw, filter)
	now := time.Unix(100, 0)
	r.now = func() time.Time { return now }

	handle, err := r.Start(context.Background(), StartRenderRequest{ChatID: "oc_test"})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := r.Append(context.Background(), handle, "a"); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if err := r.Append(context.Background(), handle, "b"); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if handle.updates != 0 {
		t.Fatalf("expected token burst before interval to be buffered, got %d updates", handle.updates)
	}

	now = now.Add(testStreamingConfig().UpdateInterval.Duration)
	if err := r.Append(context.Background(), handle, "c"); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if handle.updates != 1 {
		t.Fatalf("expected one throttled final answer update, got %d", handle.updates)
	}
	card := mustDecodeCard(t, gw.Message(handle.MessageID))
	elements := mustCardElements(t, card)
	if got := mustMarkdownContent(t, elements[0]); !contains(got, "abc") {
		t.Fatalf("expected throttled final answer content to contain abc, got %q", got)
	}
}

func mustDecodeCard(t *testing.T, raw string) map[string]any {
	t.Helper()
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("decode card json failed: %v, raw=%q", err, raw)
	}
	return card
}

func mustCardElements(t *testing.T, card map[string]any) []map[string]any {
	t.Helper()
	body, ok := card["body"].(map[string]any)
	if !ok {
		t.Fatalf("card body missing: %#v", card)
	}
	rawElements, ok := body["elements"].([]any)
	if !ok {
		t.Fatalf("card body elements missing: %#v", body)
	}
	elements := make([]map[string]any, 0, len(rawElements))
	for _, item := range rawElements {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("unexpected element shape: %#v", item)
		}
		elements = append(elements, m)
	}
	return elements
}

func mustMarkdownContent(t *testing.T, node any) string {
	t.Helper()
	m := mustMap(t, node)
	content, ok := m["content"].(string)
	if !ok {
		t.Fatalf("markdown content missing: %#v", m)
	}
	return content
}

func mustFirstInnerElement(t *testing.T, element map[string]any) any {
	t.Helper()
	inner, ok := element["elements"].([]any)
	if !ok || len(inner) == 0 {
		t.Fatalf("element has no inner elements: %#v", element)
	}
	return inner[0]
}

func mustMap(t *testing.T, node any) map[string]any {
	t.Helper()
	m, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("unexpected map node: %#v", node)
	}
	return m
}

func contains(s, substr string) bool { return strings.Contains(s, substr) }

func testStreamingConfig() config.StreamingConfig {
	return config.StreamingConfig{
		Enabled:              true,
		Mode:                 config.StreamingModeCardStreaming,
		UpdateInterval:       config.Duration{Duration: 500 * time.Millisecond},
		MinUpdateChars:       1,
		MaxUpdateChars:       20000,
		MaxUpdatesPerMessage: 100,
		MaxStreamDuration:    config.Duration{Duration: time.Minute},
		MaxFinalChars:        20000,
		TruncateNotice:       "\n...[truncated]",
	}
}
