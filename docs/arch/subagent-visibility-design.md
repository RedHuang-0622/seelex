# 子代理详情查看系统设计方案

> 参考：Claude Code Agent View + `/workflows` + 社区 claudeview/agent-monitor-hs

---

## 一、Claude Code 的做法

Claude Code 的 sub-agent 可视化工具有三个层次：

```
1. 对话内联 (默认)
   主代理回复中直接嵌入 [Sub-agent: exploring] ✓ completed
   优点: 零额外操作
   缺点: 多 agent 并行时信息过载

2. /workflows (TUI 树)
   激活后展示独立面板，树形结构:
   ├─ ● main (planning)
   │  ├─ ✓ explore (完成)
   │  ├─ ● implement-core (运行中)  tok:4.2k
   │  └─ ○ test (等待)
   └─ ✓ verify (完成)
   优点: 结构化、实时
   缺点: 切换到独立视图

3. claude agents (后台任务列表)
   Agent View 统一屏幕，列出所有后台会话
```

核心交互模式：
- 用 `j/k` 上下导航，`Enter` 展开折叠
- 状态色标：`●` 黄=运行中 `✓` 绿=完成 `✗` 红=失败 `○` 灰=等待
- 每个节点显示：类型 + 状态 + token 消耗 + 耗时
- 展开后显示该 agent 的工具调用列表

---

## 二、Seelex 当前已有的基础设施

### 可以复用的

| 组件 | 位置 | 说明 |
|------|------|------|
| WorkPlan DAG | `workplan/` 图引擎 | 已有 node/edge/runner，能产生活动图 |
| TraceTree | `seelectx/tracer/` | span 树 + LLM call / tool dispatch 追踪 |
| EventHub | `application/event.go` | 已用于 snapshot/tool/chat 事件分发 |
| Snapshot.Runtime | `application/state.go` | 已有显示子项可用 |

### 缺的

| 组件 | 说明 |
|------|------|
| SubAgentTree 数据结构 | 应用层的子代理执行树 |
| plan_run 的执行事件 | WorkPlan runner 完成节点时需发出事件 |
| TUI 树形渲染 | Bubble Tea 的树形列表组件 |

---

## 三、应用层数据结构（先做这步）

```go
// application/subagent.go

// SubAgentStatus 子代理状态
type SubAgentStatus string
const (
    AgentPending   SubAgentStatus = "pending"
    AgentRunning   SubAgentStatus = "running"
    AgentCompleted SubAgentStatus = "completed"
    AgentFailed    SubAgentStatus = "failed"
    AgentSkipped   SubAgentStatus = "skipped"
)

// SubAgentNode 子代理执行树的节点
type SubAgentNode struct {
    ID          string          `json:"id"`
    Name        string          `json:"name"`
    Kind        string          `json:"kind"` // "auto" | "llm" | "method" | "fork" | "loop"
    Status      SubAgentStatus  `json:"status"`
    ParentID    string          `json:"parent_id,omitempty"`
    Depth       int             `json:"depth"`
    Input       string          `json:"input,omitempty"`
    Output      string          `json:"output,omitempty"`      // 简短摘要
    TokenCount  int             `json:"token_count,omitempty"`
    Duration    time.Duration   `json:"duration,omitempty"`
    Error       string          `json:"error,omitempty"`
    ToolCalls   []AgentToolCall `json:"tool_calls,omitempty"`
    Children    []string        `json:"children,omitempty"`    // 子节点 ID 列表
    StartedAt   time.Time       `json:"started_at,omitempty"`
}

// AgentToolCall 子代理内部的工具调用
type AgentToolCall struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments,omitempty"`
    Result    string `json:"result,omitempty"` // 截断 200 字
    Duration  time.Duration `json:"duration,omitempty"`
    Status    string `json:"status"` // "success" | "error"
}

// SubAgentTree 子代理执行树
type SubAgentTree struct {
    RootID  string                   `json:"root_id"`
    Nodes   map[string]*SubAgentNode `json:"nodes"`
    Order   []string                 `json:"order"` // 按执行顺序排列的节点 ID
    UpdatedAt time.Time              `json:"updated_at"`
}

func NewSubAgentTree(rootID, rootName string) *SubAgentTree
func (t *SubAgentTree) AddNode(parentID, id, name, kind string) *SubAgentNode
func (t *SubAgentTree) UpdateStatus(id string, status SubAgentStatus)
func (t *SubAgentTree) Complete(id string, output string, tokens int, duration time.Duration)
func (t *SubAgentTree) Fail(id string, err error)
func (t *SubAgentTree) AddToolCall(id string, call AgentToolCall)
func (t *SubAgentTree) Root() *SubAgentNode
```

### 与 EventHub 集成

```go
// application/event.go 新增事件类型
const (
    EventSubAgentStarted   EventKind = "subagent.started"
    EventSubAgentUpdated   EventKind = "subagent.updated"
    EventSubAgentCompleted EventKind = "subagent.completed"
    EventSubAgentToolCall  EventKind = "subagent.tool_call"
)

// Snapshot 增加子代理树
type Snapshot struct {
    // ... 现有字段 ...
    ActiveSubAgents *SubAgentTree `json:"active_sub_agents,omitempty"`
}
```

---

## 四、TUI 渲染（下一步做）

参考 Claude Code 的 `/workflows` 树形视图：

```
  ┌─ Sub-Agents ─────────────────────────────────────────────┐
  │                                                          │
  │  ● design      方案设计               tok:3.2k  12s      │
  │  │  ✓ explore    竞品调研              tok:1.1k  4s       │
  │  │  ✓ select     技术选型              tok:2.1k  6s       │
  │  │                                                       │
  │  ● implement   核心实现               tok:8.5k  34s      │
  │  │  ● code        实现接口             tok:4.2k  15s      │
  │  │     ├─ grep_search (3 files)              0.3s        │
  │  │     ├─ read_file (2 files)                0.2s        │
  │  │     └─ write_file (1 file)                0.1s        │
  │  │  ○ test        单元测试             —       等待      │
  │  │                                                       │
  │  └ 汇总: 6/8 完成  总 tok: 15.3k  总耗时: 52s           │
  │                                                          │
  │  ↑↓ 导航  Enter展开  Tab切换面板                         │
  └──────────────────────────────────────────────────────────┘
```

交互：
- `j/k` 或 `↑/↓` 在树中导航
- `Enter` / `→` 展开选中节点查看详情（tool calls / output）
- `←` 折叠
- 选中节点时右侧面板显示详情（token、耗时、output）
- 流式执行时自动滚动到最新活跃节点

---

## 五、实施路线

| 阶段 | 内容 | 估算 |
|------|------|------|
| **P1** (现在) | application 层: SubAgentTree 数据结构 + 事件定义 | ~80 行 |
| **P2** (下一步) | WorkPlan runner 钩子: 节点完成时发事件到 EventHub | ~40 行 |
| **P3** (再下一步) | TUI 树形渲染 + 导航交互 | ~120 行 |

先做 P1——纯数据结构和事件，不涉及任何 UI 渲染。
