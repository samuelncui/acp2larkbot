package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeProcessUpdateToolCall(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"sessionUpdate": "tool_call",
		"title":         "bash",
		"rawInput": map[string]any{
			"Command": "pwd",
		},
	})
	if err != nil {
		t.Fatalf("marshal raw failed: %v", err)
	}
	got := summarizeProcessUpdate("tool_call", raw)
	if !strings.Contains(got, "Starting bash: pwd") {
		t.Fatalf("unexpected summary %q", got)
	}
}

func TestSummarizeProcessUpdateToolCallUpdate(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"sessionUpdate": "tool_call_update",
		"title":         "bash",
		"rawOutput": map[string]any{
			"Output": map[string]any{
				"stdout": "/tmp/workspace\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal raw failed: %v", err)
	}
	got := summarizeProcessUpdate("tool_call_update", raw)
	if !strings.Contains(got, "bash output") || !strings.Contains(got, "/tmp/workspace") {
		t.Fatalf("unexpected summary %q", got)
	}
}
