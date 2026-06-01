# Card Streaming Architecture v3

## Lark API Constraints

| API | 作用 | 限制 |
|-----|------|------|
| `UpdateStreamingElement` | typewriter / 更新内容 | **仅 `plain_text` / `markdown` 元素** |
| `InsertStreamingElementsBefore` | 插入元素 | 任意元素类型 |
| `UpdateCard Configuration` | 开启/关闭 streaming_mode | — |
| `UpdateCard` (PatchMessage) | 替换整张卡片 | 可更新任意属性 |

关键限制：
- `UpdateStreamingElement` 只能更新 `plain_text` / `markdown` 的内容（content 字段），不能改属性（如 `expanded`、`header`）
- `collapsible_panel` 的 `expanded` 和 `header` 在 streaming 期间不变
- 所有属性变化（图标、折叠状态）只在 finalize 后的 `UpdateCard` 中体现

## Card Structure

```
┌────────────────────────────────┐
│  Action Timeline (上部)        │  ← collapsible_panel 列表
│  ┌ ▶ 🔧 Tool: search_files ─┐ │
│  │   > Found 5 files        │ │
│  └──────────────────────────┘ │
│  ┌ ▼ ✅ Thinking             ┐ │  ← 最下面一个展开
│  │   > Analyzing...          │ │
│  └──────────────────────────┘ │
├────────────────────────────────┤
│  Final Answer (下部)          │  ← markdown + typewriter
│  1 + 1 = 2                    │
└────────────────────────────────┘
```

### JSON 模板（初始卡片）

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
        "element_id": "answer_final",
        "content": "_Waiting..._"
      }
    ]
  }
}
```

注意：初始卡片**不含任何 action panel**。Action 通过 `InsertStreamingElementsBefore("answer_final", ...)` 动态插入到 answer 上方。

### Action Panel 结构

```json
{
  "tag": "collapsible_panel",
  "element_id": "panel_0",
  "expanded": true,
  "header": {
    "title": {
      "tag": "plain_text",
      "content": "🐾 Tool: search_files"
    }
  },
  "elements": [
    {
      "tag": "markdown",
      "element_id": "panel_0_content",
      "content": "_Running..._"
    }
  ]
}
```

命名规则：panel 的 `element_id = "panel_{n}"`，内部 markdown 的 `element_id = "panel_{n}_content"`。这样 `UpdateStreamingElement` 可以更新内部 markdown。

## 状态模型

```go
type ActionState string
const (
    ActRunning ActionState = "running"
    ActDone    ActionState = "done"
    ActFailed  ActionState = "failed"
)

type Action struct {
    ID     string       // "panel_0"
    Seq    int          // 0, 1, 2, ...
    Type   string       // "tool_call", "thinking", "process"
    Title  string       // "Tool: search_files"
    Detail string       // detail text (panel content)
    State  ActionState  // running / done / failed
}

type CardState struct {
    Actions     []Action
    FinalAnswer string  // current streaming text
    Finished    bool    // true after finalize
}
```

### 图标映射

| Type | Running (streaming) | Done (finalize) | Failed (finalize) |
|------|---------------------|-----------------|-------------------|
| `tool_call` | 🐾 | ✅ | ❌ |
| `thinking` | 🐾 | 💡 | — |
| `process` | 🐾 | ✅ | ❌ |

`🐾` 表示"正在执行中"。`UpdateStreamingElement` 不能更新 header，所以 streaming 期间的 icon 固定为 `🐾`。Finalize 后通过 `UpdateCard` 替换为最终图标。

## 生命周期

### Phase 1: Initialize

```
1. CreateStreamingCard(initialCard)   — 只有 answer_final
2. SendCardByID(cardID, chatID)       — 获得 messageID
3. state := CardState{
    Actions:     nil,
    FinalAnswer: "",
    Finished:    false,
   }
4. lastSent := map[string]string{}
```

### Phase 2: Streaming

```
On new action (e.g., tool_call starts):
  1. act := Action{Seq: len(state.Actions), Type: "tool_call", Title: "Tool: search_files", State: ActRunning}
  2. state.Actions = append(state.Actions, act)
  3. panel := buildPanel(act)    // expanded: true, icon: 🐾
  4. InsertStreamingElementsBefore(cardID, "answer_final", [panel])
  5. lastSent["panel_N_content"] = ""

On action content update:
  1. act := &state.Actions[N]
  2. act.Detail = newDetail
  3. content := renderActionContent(act)
  4. if content != lastSent["panel_N_content"]:
       UpdateStreamingElement(cardID, "panel_N_content", content)
       lastSent["panel_N_content"] = content

On action finish:
  1. act.State = ActDone
  2. // header icon stays 🐾 (can't update during streaming)
  3. // detail can be updated:
     content := renderActionContent(act)
     UpdateStreamingElement(cardID, "panel_N_content", content)

On final answer text:
  1. state.FinalAnswer = newText
  2. UpdateStreamingElement(cardID, "answer_final", newText)
```

### Phase 3: Finalize

```
1. state.Finished = true
2. FinalizeStreamingCard(cardID, sequence)
   → streaming_mode: false
3. card := state.RenderFinalCard()  // correct icons, collapse old panels
4. UpdateCard(messageID, card)
```

## 渲染函数

```go
func buildPanel(a Action, final bool, isLast bool) map[string]any {
    icon := "🐾"
    if final {
        icon = finalIcon(a.Type, a.State)
    }
    
    header := fmt.Sprintf("%s %s", icon, a.Title)
    
    content := a.Detail
    if content == "" {
        content = "_Running..._"
    }
    
    return map[string]any{
        "tag":        "collapsible_panel",
        "element_id": a.ID,
        "expanded":   !final || isLast,  // streaming: all expanded; final: only last
        "header": map[string]any{
            "title": map[string]any{
                "tag":     "plain_text",
                "content": header,
            },
        },
        "elements": []map[string]any{
            {
                "tag":        "markdown",
                "element_id": a.ID + "_content",
                "content":    content,
            },
        },
    }
}

func (s *CardState) RenderFinalCard() map[string]any {
    var elements []map[string]any
    
    lastIdx := len(s.Actions) - 1
    for i, a := range s.Actions {
        elements = append(elements, buildPanel(a, true, i == lastIdx))
    }
    
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
            "streaming_mode": false,
            "update_multi":   true,
            "width_mode":     "fill",
        },
        "body": map[string]any{
            "elements": elements,
        },
    }
}
```

## 关键设计决策

1. **Streaming 期间所有 panel expanded** — 因为无法更新 `expanded` 属性，让用户看到所有 action 的实时更新

2. **Streaming 期间 icon 固定为 `🐾`** — 无法更新 header。只有在 finalize 后的 `UpdateCard` 中才替换为 `✅/❌/💡`

3. **Finalize 时 collapse 旧 panel** — `expanded: false` for all except the last one

4. **`UpdateStreamingElement` 更新 panel 内部 markdown** — panel 内部的 `markdown` 元素有独立的 `element_id`（`panel_N_content`），可以独立更新

## 文件变更

1. **新增 `lark/card_state.go`** — CardState, Action, render 函数
2. **修改 `lark/card_renderer.go`** — CardStreamingRenderer 使用 CardState
3. **`lark/live.go`** — 无变化

## Action 历史上限

```
cardProcessHistoryMax = 8
```

超过上限时，将前 N-7 个 action 合并为一个摘要 panel：
```
**📋 Summary**
> search_files → read_file → ...
```
