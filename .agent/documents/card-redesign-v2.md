# Card Streaming Architecture v2

## Lark API Constraints (from official docs)

| API | 作用 | 限制 |
|-----|------|------|
| `UpdateStreamingElement` | typewriter 效果 | **仅 `plain_text` / `markdown` 元素** |
| `InsertStreamingElementsBefore` | 插入元素 | 任意元素类型，但不是 typewriter |
| `UpdateCard Configuration` | 开启/关闭 streaming_mode | — |
| `UpdateCard` (PatchMessage) | 替换整张卡片 | — |

核心原则：**typewriter 只用于 final answer，action timeline 通过 Insert + 组件级更新实现**。

## Card Structure

卡片分为三个区域：

```
┌────────────────────────────┐
│  Action Timeline           │  ← 每行一个 action
│  ┌──────────────────────┐  │
│  │ 🔧 Tool: search_files │  │  ← markdown：加粗的 action 名 + 内容
│  │ > Found 5 files...   │  │
│  └──────────────────────┘  │
│  ┌──────────────────────┐  │
│  │ 💭 Thinking          │  │
│  │ > Analyzing...       │  │
│  └──────────────────────┘  │
├────────────────────────────┤
│  Final Answer              │  ← markdown，有 typewriter 效果
│  1 + 1 = 2                 │
└────────────────────────────┘
```

即：actions 和 final answer 都是 **`markdown` 元素**。Actions 用 markdown 的加粗 + 引用语法模拟分组效果，不需要 `collapsible_panel`。

### JSON 模板

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
        "tag": "markdown",
        "element_id": "action_0",
        "content": ""
      },
      {
        "tag": "markdown",
        "element_id": "action_1",
        "content": ""
      },
      {
        "tag": "markdown",
        "element_id": "answer_final",
        "content": "_Waiting..._"
      }
    ]
  }
}
```

每个 action 的 `content` 格式：

```markdown
**{icon} {title}**
> {detail}
```

示例：
```markdown
**🔧 Tool: search_files**
> Found 5 files matching `*.go`
```

```markdown
**💭 Thinking**
> Looking at search results, the main entry point is in `cmd/acp2larkbot/main.go`...
```

## 状态模型

不使用 Virtual DOM，只用简单的状态列表 + 渲染函数。

```go
type ActionState string
const (
    ActionRunning  ActionState = "running"
    ActionDone     ActionState = "done"
    ActionFailed   ActionState = "failed"
)

type Action struct {
    ID     string       // element_id: "action_0"
    Type   string       // "tool_call", "thinking", "process"
    Title  string       // "Tool: search_files"
    Detail string       // detail text
    State  ActionState
}

type CardState struct {
    Actions     []Action
    FinalAnswer string  // current streaming text
    Finished    bool    // true after finalize
}
```

### Action 图标

| Type | Running | Done | Failed |
|------|---------|------|--------|
| `tool_call` | 🔧 | ✅ | ❌ |
| `thinking` | 💭 | ✅ | — |
| `process` | ⚙️ | ✅ | ❌ |

## 渲染函数

将 `CardState` 渲染为完整的卡片 JSON：

```go
func (s *CardState) RenderCardJSON() map[string]any {
    var elements []map[string]any
    
    // Action elements
    for _, a := range s.Actions {
        icon := actionIcon(a.Type, a.State)
        title := a.Title
        if title == "" {
            title = strings.Title(a.Type)
        }
        
        content := fmt.Sprintf("**%s %s**", icon, title)
        if a.Detail != "" {
            lines := strings.Split(a.Detail, "\n")
            for _, line := range lines {
                content += "\n> " + line
            }
        }
        
        elements = append(elements, map[string]any{
            "tag":        "markdown",
            "element_id": a.ID,
            "content":    content,
        })
    }
    
    // Final answer
    answer := s.FinalAnswer
    if answer == "" {
        answer = "_Waiting..._"
    }
    elements = append(elements, map[string]any{
        "tag":        "markdown",
        "element_id": "answer_final",
        "content":    answer,
    })
    
    return map[string]any{
        "schema": "2.0",
        "config": map[string]any{
            "streaming_mode": !s.Finished,
            "update_multi":   true,
            "width_mode":     "fill",
        },
        "body": map[string]any{
            "elements": elements,
        },
    }
}
```

## 生命周期

### Phase 1: Initialize

```
1. CreateStreamingCard(RenderCardJSON(state))
   state = {Actions: [], FinalAnswer: "", Finished: false}
2. SendCardByID(cardID, chatID)
   → store messageID, cardID
```

### Phase 2: Streaming

```
On action start (e.g., tool_call begins):
  1. Append Action{ID: "action_N", Type: "tool_call", State: Running}
  2. InsertStreamingElementsBefore(cardID, "answer_final", [rendered action markdown])

On action content update (e.g., tool output arrives):
  1. Update action.Detail = newText
  2. UpdateStreamingElement(cardID, "action_N", renderedContent)

On action finish (tool_call complete):
  1. Update action.State = Done
  2. UpdateStreamingElement(cardID, "action_N", renderedContent)

On final answer text (model outputs):
  1. Update finalAnswer = newText
  2. UpdateStreamingElement(cardID, "answer_final", newText)
```

### Phase 3: Finalize

```
1. state.Finished = true
2. FinalizeStreamingCard(cardID, sequence)
   → Sets streaming_mode: false
3. UpdateCard(messageID, RenderCardJSON(state))
   → Sends static snapshot with correct icons
```

注意：Phase 2 中 `UpdateStreamingElement` 更新 action 时，不是 typewriter 效果——因为 action 的 icon 会变（🔧 → ✅），prefix 不同，typewriter 不会触发，直接替换。只有 `answer_final` 的逐字追加才有 typewriter 效果。

## Diff / Sync 逻辑

不需要完整的 diff 树，只需要跟踪"上一次发送给 Lark 的状态"。

```go
type CardSyncState struct {
    state     *CardState
    lastSent  map[string]string  // elementID → content
}

func (s *CardSyncState) Sync(ctx context.Context, gw Gateway, cardID string) error {
    card := s.state.RenderCardJSON()
    elements := card["body"].(map[string]any)["elements"].([]map[string]any)
    
    for _, el := range elements {
        id := el["element_id"].(string)
        content := el["content"].(string)
        
        if s.lastSent[id] == "" {
            // New element: insert before answer_final
            gw.InsertStreamingElementsBefore(ctx, cardID, "answer_final", []map[string]any{el}, seq)
        } else if s.lastSent[id] != content {
            // Updated: update element
            gw.UpdateStreamingElement(ctx, cardID, id, content, seq)
        }
        s.lastSent[id] = content
    }
}
```

## 简化方案总结

| | 旧方案 | 新方案 |
|------|--------|--------|
| 抽象层 | Virtual DOM + diff | State list + render |
| Action 展示 | collapsible_panel | markdown（加粗 + 引用） |
| Typewriter | 不区分 | 仅 answer_final |
| 代码量 | ~300 lines | ~100 lines |
| 文件变更 | 2 新文件 | 1 新文件 `card_state.go` + 改 `card_renderer.go` |

## 文件变更

1. **新增 `lark/card_state.go`** — CardState, Action, RenderCardJSON, CardSyncState
2. **修改 `lark/card_renderer.go`** — CardStreamingRenderer 使用 CardState，去掉现有 renderer 逻辑
3. **修改 `lark/live.go`** — 无变化

## Action 历史限制

- `cardProcessHistoryMax = 8`（可配置）
- 当 actions 超过上限时，合并最早的几个 action 为一个摘要行
