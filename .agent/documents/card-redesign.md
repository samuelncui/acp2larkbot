# Card Streaming Architecture Redesign

## Problem Statement

The current card streaming implementation has two issues:

1. **Blank card on finalize** — In network ACP mode, the card sometimes shows blank after finalization. The root cause is the `patchFinalCardLocked` → `finalizeLocked` sequence where `patchFinalCardLocked` patches the message content with a snapshot card (which may interfere with the streaming card rendering), and `finalizeLocked` only sets `streaming_mode: false` without ensuring the final content is visible.

2. **No structured action timeline** — The current card is flat: it shows a single "final answer" area with streaming text. There is no structured timeline of actions (tool calls, thinking steps, intermediate results) that are collapsible and independent.

## Design Goals

1. **Virtual DOM model** — Build an internal card tree representation. When the card needs to update, compute a diff between old and new state, and send minimal API calls to sync.

2. **Structured action timeline** — Card contains a sequence of collapsible panels (`action 1 → action 2 → ... → action n → final answer`). Each action is a `collapsible_panel` component with:
   - Header: icon + title showing the action type and status
   - Body: content (thinking process, tool call output, etc.)
   - Status indicator: running/completed/failed

3. **Streaming lifecycle** — Streaming starts with a snapshot card (streaming_mode: true), then actions are incrementally added and updated, and finally streaming is closed and the card is replaced with a static snapshot.

4. **Minimal API calls** — Only send API calls when the virtual DOM differs from the last known Lark state.

## Card Structure

### JSON Card Template

```json
{
  "schema": "2.0",
  "config": {
    "streaming_mode": true,
    "update_multi": true,
    "width_mode": "fill"
  },
  "body": {
    "elements": [
      {
        "tag": "collapsible_panel",
        "element_id": "action_001",
        "expanded": false,
        "header": {
          "title": {
            "tag": "plain_text",
            "content": "🔧 Tool: search_files"
          }
        },
        "elements": [
          {
            "tag": "markdown",
            "element_id": "action_001_content",
            "content": "Found 5 files..."
          }
        ]
      },
      {
        "tag": "collapsible_panel",
        "element_id": "action_002",
        "expanded": true,
        "header": {
          "title": {
            "tag": "plain_text",
            "content": "💭 Thinking"
          }
        },
        "elements": [
          {
            "tag": "markdown",
            "element_id": "action_002_content",
            "content": "Analyzing the results..."
          }
        ]
      },
      {
        "tag": "markdown",
        "element_id": "answer_final",
        "content": "_Waiting for final answer..._"
      }
    ]
  }
}
```

### Virtual DOM Tree

```
Card
├── ActionBlock (action_001)
│   ├── Header: "🔧 Tool: search_files" [collapsed]
│   └── Content: "Found 5 files..."
├── ActionBlock (action_002)
│   ├── Header: "💭 Thinking" [expanded]
│   └── Content: "Analyzing the results..."
└── FinalAnswer (answer_final)
    └── Content: "The answer is 42."
```

## Action Types

| Type | Icon | Header Format | Content |
|------|------|---------------|---------|
| `tool_call` | 🔧 | `Tool: {tool_name}` | Tool output (runs, results) |
| `thinking` | 💭 | `Thinking` | Thinking process text |
| `process` | ⚙️ | `{process_title}` | Process output |
| `error` | ❌ | `Error: {brief}` | Error details |

### Action States

```
running → completed
running → failed
```

When an action is `running`, its header shows a spinner animation (via streaming updates). When completed, spinner is replaced with final icon. The last action is auto-expanded.

## Component Tree & Diff Algorithm

### Internal Representation

```go
type CardNode struct {
    ElementID string
    Tag       string // "collapsible_panel" | "markdown"
    Props     map[string]any
    Children  []*CardNode
    Version   int  // incremented on every change
}

type CardTree struct {
    Root    *CardNode
    Version int
    // Last known Lark state (for diff)
    LastSent map[string]string // elementID -> content hash
}
```

### Diff Strategy

Instead of a full tree diff, use element-level diffing:

1. Each element has a unique `element_id`
2. When content changes, compute diff for that element only
3. Use `UpdateStreamingElement` to update changed elements
4. Use `InsertStreamingElementsBefore` to add new actions
5. Track `LastSent` state to avoid redundant API calls

### Diff Algorithm

```
func (t *CardTree) Diff() []CardOp {
    var ops []CardOp
    for _, node := range t.Root.Children {
        hash := hashContent(node)
        if t.LastSent[node.ElementID] != hash {
            ops = append(ops, CardOp{
                Type:      OpUpdateElement,
                ElementID: node.ElementID,
                Content:   node.Content(),
            })
            t.LastSent[node.ElementID] = hash
        }
    }
    return ops
}
```

## Streaming Lifecycle

### Phase 1: Initialize

```
1. Create card via CreateStreamingCard:
   - streaming_mode: true
   - update_multi: true
   - Empty body with only answer_final placeholder

2. Send card via SendCardByID
3. Store message_id and card_id
```

### Phase 2: Actions

```
For each action:
  1. Build action node in CardTree
  2. Insert element before answer_final via InsertStreamingElementsBefore
  3. Update action content via UpdateStreamingElement
  4. Update final answer via UpdateStreamingElement
```

### Phase 3: Finalize

```
1. Update final answer with final content
2. Call FinalizeStreamingCard (sets streaming_mode: false)
3. UpdateCard with static snapshot (no streaming config)
   — This replaces the streaming card with a static rendering
```

Important: In Phase 3, FinalizeStreamingCard MUST be called BEFORE UpdateCard. The current code does the opposite, which causes the blank card issue.

## API Operations

| Operation | API | When |
|-----------|-----|------|
| Create card | `CreateStreamingCard` | Phase 1: first card |
| Send card | `SendCardByID` | Phase 1: first card |
| Insert action | `InsertStreamingElementsBefore` | Phase 2: new action |
| Update action | `UpdateStreamingElement` | Phase 2: action content change |
| Update answer | `UpdateStreamingElement` | Phase 2: streaming answer |
| Finalize | `FinalizeStreamingCard` | Phase 3: stop streaming |
| Replace card | `UpdateCard` | Phase 3: static snapshot |

## Implementation Plan

### File Changes

1. **New file: `lark/card_vdom.go`** — Virtual DOM tree implementation
   - `CardNode` struct
   - `CardTree` struct with diff/patch
   - `CardTreeBuilder` for incremental construction

2. **New file: `lark/card_actions.go`** — Action block types
   - `ActionBlock` struct (type, title, content, status)
   - Action rendering helpers

3. **Modify: `lark/card_renderer.go`** — Replace CardStreamingRenderer
   - Use CardTree internally
   - Phase 1/2/3 lifecycle
   - Proper finalize ordering: flush → finalize → patch

4. **Modify: `lark/live.go`** — Gateway methods (no changes needed, already have all APIs)

### Timeline

- Step 1: Implement CardTree + diff algorithm
- Step 2: Implement action blocks
- Step 3: Rewrite CardStreamingRenderer
- Step 4: E2E test with both local and network modes

## Key Design Decisions

1. **Element-level diff, not tree diff** — Simpler, faster, and sufficient for this use case. Each action panel is independent.

2. **Finalize before patch** — `FinalizeStreamingCard` first, then `UpdateCard` for the static snapshot. This ensures the streaming card is properly closed before replacing with static content.

3. **No incremental streaming for actions** — Actions are inserted as whole blocks, not streamed character-by-character. Only the final answer is streamed.

4. **Last action auto-expanded** — The most recent action is expanded by default, older ones are collapsed. This keeps the card readable.