# Plan 可视化系统设计

> **状态**: 设计稿
> **日期**: 2026-07-22
> **领域**: TUI / Application 层 / Snapshot 数据流
> **涉及改动**: `application/state.go` → `application/app.go` → `tui/view.go` → `tui/plan.go` (新增)

---

## 1. 设计目标

### 1.1 核心需求

- 在 TUI 中实时展示 WorkPlan DAG 的执行进度
- 四级 Effort（lite/medium/high/max）对应不同的展示粒度
- 支持 Fork 子 Agent 树的递归展开
- 不阻塞主对话流的正常渲染

### 1.2 非功能性目标

- 渲染开销低：DAG 节点数 ≤ 200 时单帧渲染 < 5ms
- 数据源单向依赖：TUI 只读 snapshot，不直接查询 WorkPlan
- 向后兼容：不修改 Seele 框架代码，通过 Hook 机制捕获计划事件

---

## 2. 市面 CLI 模式调研

### 2.1 打点表（Status Board）

```
Task                     Status    Duration
───────────────────────────────────────────
  analyze-deps           ✓ done     2.3s
  build-modules          ▶ running  5.1s
  run-tests              ○ pending    —
  deploy                 ○ pending    —
```

**适用场景**: medium effort，节点数 5-30 的线性/简单并行任务。
**优点**: 紧凑，一目了然，适合窄终端。
**缺点**: 不适合 DAG 分支展示，子 Agent 展示需缩进。

### 2.2 子 Agent 树（Sub-Agent Tree）

```
plan: refactor-auth [max]
├─ ✓ 1. analyze (2.3s)
│  └─ ✓ agent: code-audit
│     ├─ ✓ read-sources (0.5s)
│     └─ ✓ generate-report (1.2s)
├─ ▶ 2. implement (ongoing)
│  ├─ ✓ agent: auth-rewrite
│  │  └─ ✓ create-service (0.8s)
│  ├─ ▶ agent: test-writer  [fork]
│  │  ├─ ✓ write-unit-tests (1.1s)
│  │  └─ ▶ write-integration-tests
│  └─ ○ agent: migration-script
└─ ○ 3. verify
```

**适用场景**: high/max effort，Fork 子 Agent 深度嵌套。
**优点**: 层级关系清晰，Fork/Join 分支直观。
**缺点**: 宽终端需求，深嵌套时水平空间不足。

### 2.3 活动图流（Activity Flow）

```
[analyze-deps] ─→ [build-modules] ─→ [run-tests] ─→ deploy
       │                                    │
       ├→ [lint] ──────────────────────────→┤
       │                                    │
       └→ [type-check] ────────────────────→┤
```

**适用场景**: high effort 流程概览，CI/CD 流水线。
**优点**: 流程拓扑一目了然。
**缺点**: 渲染复杂，终端空间利用率低，不适合 > 20 节点。

### 2.4 简易轮播（Compact Carousel）

```
◉ analyze-deps  →  ◎ build-modules  →  ◉ run-tests  →  ○ deploy
[  ████████░░░░░░░░  3/7 nodes completed  ]
```

**适用场景**: lite effort，仅需知道"在跑、进度多少"。
**优点**: 极简，仅占 1-2 行。
**缺点**: 无细节，无子 Agent。

---

## 3. 推荐方案：复合式展示

根据 Effort 等级组合以上模式：

| Effort | 展示模式 | 行数 | 展示内容 |
|--------|----------|------|----------|
| lite   | 简易轮播 + 进度条 | 1-2 | 当前节点名 + 进度条 + 耗时 |
| medium | 打点表（紧凑） | 3-8 | 节点列表（状态·名·耗时），Fork 仅缩进一级 |
| high   | 打点表 + 子 Agent 树 | 动态 | 完整 DAG 列表 + 子 Agent 展开 + 时序 |
| max    | 打点表 + 子 Agent 树 + 统计 | 动态 | 同 high + 每节点 tokens + 重试次数 + 输出预览 |

### 3.1 Lite 模式

```
◉ plan: analyze-deps  [  ████░░░░  40%  ]  5.2s
```

### 3.2 Medium 模式

```
 Plan: refactor-auth (3/7 nodes)                     
 ─────────────────────────────────────────────────
  ✓  analyze-deps       2.1s                         
  ✓  build-modules      3.4s                         
  ▶  run-tests          1.2s   (fork: lint + type-c… 
  ○  deploy              —                           
  ○  verify              —                           
 ─────────────────────────────────────────────────
```

### 3.3 High 模式

```
 Plan: refactor-auth (5/9 nodes)                  E:high
 ───────────────────────────────────────────────────────
  ✓  1. analysis        2.1s  ─→  ✓  2. design    1.5s
  │                                   │
  │              ┌────────────────────┤
  │              ▼                    ▼
  │  ✓  3. impl-core  4.2s  ○  4. impl-ext    —
  │              │
  │    ┌─────────┼──────────┐
  │    ▼         ▼          ▼
  │  ✓ lint     ▶ test   ○  docs
  │  0.8s       1.2s       —
  │                          
  ▶  5. review              —
 ───────────────────────────────────────────────────────
 [ fork: test-runner ] 2 子 Agent 运行中
```

### 3.4 Max 模式

```
 Plan: refactor-auth (7/12 nodes)                  t:28.5s
 ╔════════════════════════════════════════════════════════╗
 ║  Step          Status     Duration  Tokens  Retries   ║
 ║ ────────────────────────────────────────────────────  ║
 ║  1. analysis   ✓ done     2.1s      1,245   0        ║
 ║  2. design     ✓ done     1.5s      892     0        ║
 ║  3. impl-core  ✓ done     4.2s      3,456   1        ║
 ║  │  agent: auth-mod                                  ║
 ║  │  ├─ ✓ read-sources   0.3s      120     0          ║
 ║  │  ├─ ✓ rewrite        3.1s      2,890   1          ║
 ║  │  └─ ✓ compile        0.8s      446     0          ║
 ║  4. impl-ext   ▶ running 1.2s      892     0         ║
 ║  5. lint       ✓ done    0.5s      234     0         ║
 ║  6. test       ▶ running 2.1s      1,892   0  [fork] ║
 ║  │  agent: test-writer                                ║
 ║  │  ├─ ▶ unit          1.1s      934     0           ║
 ║  │  └─ ○ integration   —         —       —           ║
 ║  7. review      ○ pending  —       —       —          ║
 ╚════════════════════════════════════════════════════════╝
```

---

## 4. 数据层设计

### 4.1 Snapshot 扩展

在 `application/state.go` 的 `RuntimeState` 中新增 `Plan` 字段：

```go
type RuntimeState struct {
    // 现有字段...
    Plan *PlanState `json:"plan,omitempty"` // 当前活跃的 Plan（nil = 无活跃 Plan）
}

// PlanState 描述当前 WorkPlan 的执行状态。
type PlanState struct {
    ID        string      `json:"id"`                 // 计划实例 ID
    Name      string      `json:"name"`               // 计划名称 / 目标
    Status    PlanStatus  `json:"status"`              // running | completed | failed | aborted
    Nodes     []PlanNode  `json:"nodes"`               // 扁平节点列表（含层级信息）
    Edges     []PlanEdge  `json:"edges,omitempty"`     // DAG 边（仅 high/max 使用）
    Progress  float64     `json:"progress"`            // 0.0 - 1.0
    Elapsed   string      `json:"elapsed,omitempty"`   // 已耗时
    NodeCount int         `json:"node_count"`
}

type PlanNode struct {
    ID         string        `json:"id"`                   // 节点 ID
    Label      string        `json:"label"`                // 显示名
    Kind       string        `json:"kind"`                 // agent | fork | llm | method | approve | loop | ...
    Status     NodeStatus    `json:"status"`               // pending | running | completed | failed | aborted | skipped
    Depth      int           `json:"depth"`                // 缩进层级（0 = 根）
    ParentID   string        `json:"parent_id,omitempty"`  // Fork/子 Agent 的父节点
    Elapsed    string        `json:"elapsed,omitempty"`    // 已耗时 / 总耗时
    Tokens     int           `json:"tokens,omitempty"`     // token 消耗（max 模式）
    Retries    int           `json:"retries,omitempty"`    // 重试次数（max 模式）
    Output     string        `json:"output,omitempty"`     // 输出预览（max 模式，截断）
    Children   []PlanNode    `json:"children,omitempty"`   // Fork 子节点 / 子 Agent
    ForkCount  int           `json:"fork_count,omitempty"` // 并行分支数（fork 节点）
}

type PlanEdge struct {
    From string `json:"from"`
    To   string `json:"to"`
    Label string `json:"label,omitempty"` // 条件标签（if/switch 分支）
}

type PlanStatus string
const (
    PlanRunning   PlanStatus = "running"
    PlanCompleted PlanStatus = "completed"
    PlanFailed    PlanStatus = "failed"
    PlanAborted   PlanStatus = "aborted"
)

type NodeStatus string
const (
    NodePending   NodeStatus = "pending"
    NodeRunning   NodeStatus = "running"
    NodeCompleted NodeStatus = "completed"
    NodeFailed    NodeStatus = "failed"
    NodeAborted   NodeStatus = "aborted"
    NodeSkipped   NodeStatus = "skipped"
)
```

### 4.2 数据流：事件捕获 → Snapshot

```
Seele WorkPlan Runner (goroutine)
  │  节点 start / end / error
  ▼
PlanEventEmitter (新增)
  │  toolhook: plan tool 调用时注入监听
  ▼
application.Service.handlePlanEvent()
  │  更新锁住的 PlanState
  │  发布 EventSnapshotChanged
  ▼
TUI Model.waitApplicationEvent()
  │  app.Snapshot() 重新读取
  ▼
TUI Model.renderPlanPanel()   ← 新增
  根据 effort 等级选择展示模式
```

#### 关键设计决策：事件捕获方式

**方案 A：ToolHookBridge 扩展**（推荐）

在 `application/tool_hook.go` 中新增 Plan 工具调用拦截：

```go
func (bridge *ToolHookBridge) OnToolStart(ctx context.Context, toolName string, args string) {
    if toolName == "plan_run" {
        bridge.planState = PlanState{ID: genID(), Status: PlanRunning, ...}
    }
}

func (bridge *ToolHookBridge) OnToolComplete(ctx context.Context, toolName string, result string, err error) {
    if toolName == "plan_run" && err == nil {
        // 解析 plan_run 返回的 JSON（含 NodeResults、TotalElapsed、Vars 等）
        // 映射到 bridge.planState.Nodes
        bridge.emitSnapshotChanged()
    }
    if toolName == "plan_status" {
        // 轮询状态更新
        // 更新 bridge.planState 中的节点状态
    }
}
```

**方案 B：PlanRunner Hook（未来）**

在 Seele 框架的 `WorkPlan.Run()` 关键路径植入回调接口，但此方案需要修改框架代码，暂不采用。

**当前选择方案 A** 的理由：
- 零框架侵入，全在 seelex 应用层实现
- `plan_run` 返回结果已包含 `NodeResults`（见 Seele 源码 `workplan/core/types/context.go`）
- `plan_status` 可在 LLM 调用间隙主动轮询获取最新 DAG 状态

### 4.3 轮询 vs 推送

由于 WorkPlan 执行在 LLM 调用之间进行，TUI 获取 plan 进度的方式：

```
LLM 调用 ─→ plan_run ─→ LLM 调用 ─→ plan_status ─→ LLM 调用
                │                        │
                ▼                        ▼
          plan_run 返回结果        plan_status 返回当前 DAG
          一次性解析               含每个 Node 的最新 status
```

- **推模式** (plan_run) → 执行结束后一次性获得完整结果
- **拉模式** (plan_status) → 在每次 LLM 回复前/之后主动查询

对于 medium/high/max，采用 **混合策略**：
1. `plan_run` 钩子捕获完整的 `WorkPlanResult`（执行完毕后）
2. 在运行中通过 `plan_status` 钩子刷新节点状态（需 LLM 主动调用 `plan_status`）
3. 如果 LLM 未主动调 `plan_status`，可通过前端自定时的快照刷新（已有机制）展示最后已知状态

---

## 5. TUI 渲染设计

### 5.1 新增文件 `tui/plan.go`

```go
// Package tui 新增 plan 渲染组件
package tui

// planPanel 根据 effort 等级渲染 Plan 展示面板。
// 返回渲染后的字符串（空 = 不显示）。
func planPanel(plan *PlanState, effort string) string {
    if plan == nil {
        return ""
    }
    switch effort {
    case "lite":
        return renderPlanLite(plan)
    case "medium":
        return renderPlanMedium(plan)
    case "high":
        return renderPlanHigh(plan)
    case "max":
        return renderPlanMax(plan)
    default:
        return renderPlanMedium(plan)
    }
}
```

### 5.2 View() 集成

在 `tui/view.go` 的 `View()` 中，在状态栏下方、对话视口上方插入 plan 面板：

```
// View() 新布局:
func (model Model) View() string {
    var builder strings.Builder

    // 1. 状态栏 (2行)
    builder.WriteString(model.renderStatusBar())
    builder.WriteRune('\n')

    // 2. Plan 面板 (新增，仅 plan != nil 时渲染)
    if plan := model.snapshot.Runtime.Plan; plan != nil {
        panel := planPanel(plan, model.snapshot.Runtime.Effort)
        if panel != "" {
            builder.WriteString(panel)
            builder.WriteRune('\n')
        }
    }

    // 3. 对话视口
    builder.WriteString(model.viewport.View())
    // ... 后续不变
}
```

### 5.3 各等级渲染实现

#### Lite（rendPlanLite）

```go
func renderPlanLite(p *application.PlanState) string {
    // 查找当前运行节点
    var running string
    for _, n := range p.Nodes {
        if n.Status == application.NodeRunning {
            running = n.Label
            break
        }
    }
    if running == "" && p.Status == application.PlanRunning {
        running = p.Nodes[0].Label // fallback
    }
    bar := progressBar(p.Progress, 16) // ████░░░░
    status := planIcon(p.Status)

    // ◉ plan: run-tests  [████░░░░  40%]  5.2s
    return fmt.Sprintf(" %s  plan: %s  [%s  %d%%]  %s",
        status, running, bar, int(p.Progress*100), p.Elapsed)
}
```

#### Medium（renderPlanMedium）

```go
func renderPlanMedium(p *application.PlanState) string {
    var b strings.Builder
    // 标题行
    b.WriteString(fmt.Sprintf(" Plan: %s (%d/%d nodes)\n",
        p.Name, completedCount(p.Nodes), len(p.Nodes)))
    b.WriteString(" ─────────────────────────────────\n")
    // 节点列表
    for _, n := range p.Nodes {
        if n.Depth > 1 {
            continue // medium 仅展示第一级 + 父 Fork
        }
        indent := strings.Repeat("  ", n.Depth)
        icon := nodeIcon(n.Status)
        elapsed := n.Elapsed
        if elapsed == "" { elapsed = "—" }
        label := n.Label
        if n.ForkCount > 0 {
            label += fmt.Sprintf(" (fork: %d)", n.ForkCount)
        }
        b.WriteString(fmt.Sprintf("  %s%s %s  %s\n", indent, icon, label, elapsed))
    }
    b.WriteString(" ─────────────────────────────────")
    return b.String()
}
```

#### High（renderPlanHigh）

```go
func renderPlanHigh(p *application.PlanState) string {
    var b strings.Builder
    // 标题行 + 统计
    done := completedCount(p.Nodes)
    total := len(p.Nodes)
    b.WriteString(fmt.Sprintf(" Plan: %s (%d/%d)                         E:%s\n",
        p.Name, done, total, "high"))
    b.WriteString(" ─────────────────────────────────────────────\n")
    // 递归渲染节点树
    for _, n := range p.Nodes {
        if n.Depth == 0 {
            renderNodeTree(&b, n, "")
        }
    }
    // 运行中的 fork 汇总
    forks := runningForks(p.Nodes)
    if len(forks) > 0 {
        b.WriteString(" ─────────────────────────────────────────────\n")
        for _, f := range forks {
            b.WriteString(fmt.Sprintf(" [fork: %s] %d 子 Agent 运行中\n", f.Label, f.ForkCount))
        }
    }
    b.WriteString(" ─────────────────────────────────────────────")
    return b.String()
}

func renderNodeTree(b *strings.Builder, n application.PlanNode, prefix string) {
    icon := nodeIcon(n.Status)
    elapsed := n.Elapsed
    if elapsed == "" { elapsed = "—" }
    b.WriteString(fmt.Sprintf("  %s%s %s  %s\n", prefix, icon, n.Label, elapsed))
    for i, child := range n.Children {
        childPrefix := prefix + "│  "
        if i == len(n.Children)-1 {
            childPrefix = prefix + "   "
        }
        renderNodeTree(b, child, childPrefix)
    }
}
```

#### Max（renderPlanMax）

```go
func renderPlanMax(p *application.PlanState) string {
    var b strings.Builder
    // 表格风格渲染，带边框
    b.WriteString(fmt.Sprintf(" ╔══════════════════════════════════════════════╗\n"))
    b.WriteString(fmt.Sprintf(" ║ Plan: %-40s ║\n", p.Name))
    b.WriteString(fmt.Sprintf(" ║ Elapsed: %-35s ║\n", p.Elapsed))
    b.WriteString(fmt.Sprintf(" ╠══════════════════════════════════════════════╣\n"))
    // 列标题
    b.WriteString(fmt.Sprintf(" ║  %-20s %-8s %-7s %-7s %-5s║\n",
        "Step", "Status", "Time", "Tokens", "Retry"))
    b.WriteString(fmt.Sprintf(" ║  %s ║\n", strings.Repeat("─", 48)))
    // 节点
    for _, n := range p.Nodes {
        renderMaxRow(&b, n, "")
    }
    b.WriteString(fmt.Sprintf(" ╚══════════════════════════════════════════════╝"))
    return b.String()
}
```

### 5.4 Icon/Color 系统

```go
// 状态图标映射
func nodeIcon(status NodeStatus) string {
    switch status {
    case NodePending:   return "○ " // 灰
    case NodeRunning:   return "▶ " // 黄（闪烁）
    case NodeCompleted: return "✓ " // 绿
    case NodeFailed:    return "✗ " // 红
    case NodeAborted:   return "⊘ " // 红
    case NodeSkipped:   return "– " // 灰
    default:            return "? "
    }
}

func planIcon(status PlanStatus) string {
    switch status {
    case PlanRunning:   return "◉" // 黄
    case PlanCompleted: return "●" // 绿
    case PlanFailed:    return "●" // 红
    case PlanAborted:   return "●" // 灰
    default:            return "○"
    }
}

// 颜色（lipgloss）
var nodeColors = map[NodeStatus]lipgloss.Color{
    NodePending:   lipgloss.Color("240"), // 灰
    NodeRunning:   lipgloss.Color("220"), // 黄
    NodeCompleted: lipgloss.Color("76"),  // 绿
    NodeFailed:    lipgloss.Color("196"), // 红
    NodeAborted:   lipgloss.Color("124"), // 暗红
    NodeSkipped:   lipgloss.Color("242"), // 浅灰
}
```

### 5.5 高度计算适配

因为 plan 面板会占用额外垂直空间，`topPanelH()` / `midPanelH()` 需要扩展：

```go
func (model Model) planPanelH() int {
    plan := model.snapshot.Runtime.Plan
    if plan == nil {
        return 0
    }
    switch model.snapshot.Runtime.Effort {
    case "lite":
        return 1                  // 单行
    case "medium":
        return 3 + min(len(plan.Nodes), 8) // 标题 + 分隔 + 节点 + 分隔
    case "high", "max":
        return estimatePlanHeight(plan)    // 动态估计
    default:
        return 0
    }
}
```

---

## 6. 状态显示优先级

同一 row 中多个状态同时存在时的显示规则：

```
对话消息（ToolCall）     ← 工具调用结果，始终显示
Plan 面板               ← 有活跃 Plan 时在对话上方插入
审批交互                ← 有审批时覆盖对话视口（现有机制）
```

**优先级**：Plan 面板 < 对话消息 < 审批交互

即：
1. 无审批时：Plan 面板 + 对话正常显示
2. 有审批时：Plan 面板折叠为最小（仅进度条），对话区域被审批面板覆盖
3. Plan 完成后：面板保留 5 秒后渐隐，或立即收起

---

## 7. 实现阶段

### Phase 1：数据层（application/）
1. 在 `state.go` 添加 `PlanState`、`PlanNode` 等结构体
2. 在 `RuntimeState` 添加 `Plan *PlanState` 字段
3. 在 `tool_hook.go` 扩展 `ToolHookBridge`，新增 Plan 事件拦截
4. 实现 `plan_run` 结果解析 → 填充 `PlanState`
5. 实现 `plan_status` 轮询钩子 → 更新节点状态

### Phase 2：TUI 渲染（tui/plan.go）
1. 创建 `tui/plan.go`，实现四个 effort 等级的渲染函数
2. 集成到 `view.go` 的 `View()` 布局
3. 实现节点图标、颜色系统
4. 适配高度计算 `planPanelH()`
5. 添加渐变/收起动画

### Phase 3：高级交互
1. Plan 面板的键盘导航（选中节点 → 查看详情）
2. 节点输出预览弹出层
3. Fork 子 Agent 树的展开/折叠
4. 历史 Plan 的回溯查看

---

## 8. 性能考虑

| 场景 | 节点数 | 渲染频率 | 预估开销 |
|------|--------|----------|----------|
| 空状态（无 Plan） | 0 | — | 0 (early return) |
| Lite | 1-20 | 每次事件 | < 0.5ms |
| Medium | 5-50 | 每次事件 | < 1ms |
| High | 5-100 | 每次事件 | < 3ms |
| Max | 5-200 | 每次事件 | < 5ms |

Plan 面板仅在 **`Runtime.Plan != nil`** 时参与渲染，无 Plan 时零开销。

---

## 9. 未解决问题

1. **plan_status 轮询时机**：当前 LLM 不一定会主动调用 `plan_status`，如何在 LLM 回复间隙自动刷新？
   - 方案：在 `ReActLoop` 的每次 LLM 调用前/后植入自动 `plan_status` 查询
   - 但这需要修改 Seele 框架的 LoopHooks 接口

2. **Fork 子 Agent 的实时状态**：`plan_run` 返回的是快照而非流。对于长时间运行的 Fork，无法获取中间进度。
   - 方案：通过 `workplan/sugar/loop/loop.go` 的 `Signal.OnUpdate(cb)` 机制注册实时回调

3. **嵌套深度截断**：深度 > 4 的子 Agent 树在窄终端中可能无法完整显示。
   - 方案：深度 > 3 时折叠为 `[+N more levels]`，可展开查看
