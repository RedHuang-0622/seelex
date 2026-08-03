# Plan 活动图、会话隔离、工作区持久化 — 修复方案

## 目标

1. **Plan 活动图 JSON 输出** — `PlanState` 携带框架 `serialize.PlanEdgeSpec` 格式的边数据，GUI/TUI 据此渲染有向连线图。
2. **活动状态实时打点** — `plan_run` 执行中每节点状态通过 `ProgressCallback` 实时同步到节点图标与耗时。
3. **会话切换上下文隔离** — `/new` 和 `/resume` 清除 `Runtime.Plan`、`CurrentWorkspace`、`Interaction`，杜绝旧 Plan 跨会话残留。
4. **工作区持久化** — `workspace.Repo` 接入文件存储 Save/Load，进程重启后恢复。
5. **会话按工作区组织** — 存储路径从 `sessions/` → `workspace_{id}/sessions/`。

## 根因

| 问题 | 根因 | 文件位置 |
|------|------|----------|
| 活动图无 JSON | `updatePlanFromLoad` 解析了 `edges` 但从未使用；Go map 迭代顺序随机 | `application/chat.go:185-207` |
| 状态无法打点 | 活动图（边/层次）不存在，节点是平铺列表，无从"打点" | `application/chat.go:updatePlanFromLoad` |
| 编译错误 | `SetPlanNodeCallback` 签名为 `func(string,string,string,time.Duration)` 但框架 `ProgressCallback` 是 `func(*types.NodeResult)` | `seelebridge/runtime.go:130-134` |
| 会话 Plan 污染 | `/new` 和 `resumeSession` 不清理 `Runtime.Plan` | `application/command.go:108-123` / `application/app.go:461-513` |
| 工作区无持久 | `workspace.Repo` 纯内存 `map`，无 Save/Load | `workspace/workspace.go:37-41` / `main.go:90` |
| 会话不平铺 | `seelebridge.SessionStore` 只有 `sessions/` 目录，无工作区嵌套 | `seelebridge/runtime.go` / `storage.NewFileStore` |

## 框架现状 (Seele @ 8e3df49)

### ProgressCallback 链路

```
seelebridge.Runtime.SetPlanNodeCallback(fn)
  → WorkPlanTool.ProgressCallback = fn           // 类型: func(*types.NodeResult)
    → planRunHandler: wp.NodeHook = tool.ProgressCallback
      → WorkPlan.Run(): runner.SetNodeHook(wp.NodeHook)
        → Scheduler.OnNodeDone(nr *types.NodeResult)   ← 最终调用签名
```

### 关键框架类型

| 类型 | 位置 | 关键字段 |
|------|------|----------|
| `types.NodeBase` | `workplan/core/types/context.go:31` | `NodeID, Kind, Status, Output, Skipped, Aborted, StartedAt, EndedAt` |
| `types.NodeResult` | `workplan/core/types/context.go:53` | 嵌入 `NodeBase` + `Err error`（`json:"-"`） |
| `serialize.PlanEdgeSpec` | `workplan/runtime/serialize/serialize.go:23` | `From, To, Label, Condition` |
| `serialize.Plan` | `workplan/runtime/serialize/serialize.go:31` | `Name, EntryNodeID, Nodes[], Edges[]` |
| `graph.Graph` | `workplan/runtime/graph/graph.go` | `AllNodes()`, `AllEdges()`, `GetEdgesFrom()` |

### plan_run 返回的 JSON 格式 (框架实际)

```json
{
  "status": "completed",
  "node_count": 3,
  "final_output": "...",
  "abort_reason": "...",
  "nodes": [
    {
      "node_id": "start",
      "kind": "auto",
      "status": "completed",
      "output": "...",
      "skipped": false,
      "aborted": false,
      "started_at": "2026-07-26T10:00:00Z",
      "ended_at": "2026-07-26T10:00:05Z"
    }
  ]
}
```

注意：框架 `NodeResult` 嵌入 `NodeBase`，JSON 序列化后 `NodeBase` 的所有 JSON-tagged 字段**平铺**（非嵌套）。`Err` 字段有 `json:"-"` 标签，不参与序列化。

## 设计原则

**框架优先** — 所有 Plan 相关数据结构复用 Seele 框架的现有类型，通过 `seelebridge` 做类型别名桥接。

**ProgressCallback 直传** — seelex 的 `HandlePlanNodeComplete` 直接接收 `*types.NodeResult`，由 bridge 层做类型适配（而非拆成 4 个扁平参数再重组），可访问更丰富字段（Output、Skipped、Aborted、StartedAt/EndedAt）。

**不用新类型** — `PlanEdge` 直接用框架 `serialize.PlanEdgeSpec`，Graph 操作复用现有 graph API。

## 接口与数据结构

### 1. 复用框架类型 — seelebridge 桥接

**框架 `serialize.PlanEdgeSpec`** — 标准 JSON 序列化边格式 (`workplan/runtime/serialize/serialize.go:23-29`)：

```go
type PlanEdgeSpec struct {
    From      string `json:"from"`
    To        string `json:"to"`
    Label     string `json:"label,omitempty"`
    Condition string `json:"condition,omitempty"`
}
```

**`seelebridge/plan.go`** — 类型别名桥接：

```go
package seelebridge

import (
    "github.com/RedHuang-0622/Seele/workplan/runtime/serialize"
)

// PlanEdge 直接复用框架序列化边类型。
type PlanEdge = serialize.PlanEdgeSpec
```

`application/state.go` 使用桥接后的类型：

```go
type PlanState struct {
    Name     string       `json:"name"`
    Status   PlanStatus   `json:"status"`
    Nodes    []PlanNode   `json:"nodes,omitempty"`
    Edges    []PlanEdge   `json:"edges,omitempty"`     // ← seelebridge.PlanEdge
    Progress float64      `json:"progress"`
    Elapsed  string       `json:"elapsed,omitempty"`
}
```

### 2. 邻接表 → []PlanEdge 的转换

`plan_load` 的输入用 `map[string][]string` 邻接表表示边。转换为 `[]PlanEdge` 的逻辑放 `seelebridge/plan.go`：

```go
// AdjacencyToEdges 将邻接表转为 PlanEdge 列表。
func AdjacencyToEdges(adj map[string][]string) []PlanEdge {
    edges := make([]PlanEdge, 0)
    for from, targets := range adj {
        for _, to := range targets {
            edges = append(edges, PlanEdge{From: from, To: to})
        }
    }
    return edges
}
```

### 3. 节点拓扑排序

框架 `graph.Graph` 提供 `AllNodes()`（无保证顺序）和 `GetEdgesFrom()`（查询出边），但不暴露拓扑排序方法。在 `seelebridge/plan.go` 中实现：

```go
// TopoSort 从入口节点开始 BFS/DFS 产生稳定节点顺序。
func TopoSort(entry string, edges map[string][]string, allNodes map[string]struct{}) []string { ... }
```

### 4. ProgressCallback 适配（关键变更）

**旧方案**（已废弃）：拆成 4 个扁平参数 `func(nodeID, kind, status string, elapsed time.Duration)`，与框架类型 `func(*NodeResult)` 不兼容。

**新方案**：seelex 回调直接使用框架类型。在 `SetPlanNodeCallback` 中不做签名变换，只负责转发。

```go
// seelebridge/runtime.go
func (r *Runtime) SetPlanNodeCallback(cb func(nr *workplanTypes.NodeResult)) {
    if r.planTool != nil {
        r.planTool.ProgressCallback = cb
    }
}

// application/chat.go
func (service *Service) HandlePlanNodeComplete(nr *workplanTypes.NodeResult) {
    // 直接从 nr.NodeBase 取 NodeID, Kind, Status, Elapsed() 等
}
```

`workplanTypes` 即 `github.com/RedHuang-0622/Seele/workplan/core/types`，已在 `seelebridge/runtime.go` import 中使用。

### 5. 数据流

```
plan_load args (邻接表 map[string][]string)
  → seelebridge.AdjacencyToEdges()
    → PlanState.Edges (框架 serialize.PlanEdgeSpec 格式)
      → Snapshot JSON 给 GUI → 直接渲染有向连线图

plan_run 执行中（框架 Scheduler）
  → OnNodeDone(nr *types.NodeResult)
    → HandlePlanNodeComplete(nr)
      → 实时更新 PlanNode 的 Status/Elapsed/Output

plan_run 结果 JSON（框架 planRunHandler 返回）
  → 解析 nodes: []NodeResult (NodeBase 平铺字段)
    → 更新 PlanState 所有节点最终状态
```

### 6. Workspace 持久化

```go
// workspace/workspace.go
type repoSnapshot struct {
    Workspaces map[string]Info   `json:"workspaces"`
    Bindings   map[string]string `json:"bindings"`
}

func NewRepoWithStore(storePath string) (*Repo, error)
func (r *Repo) Save() error    // 写 workspace_index.json
func (r *Repo) Load() error    // 读 workspace_index.json
```

### 7. 会话存储分层

```
.seelex/
  workspace_index.json              ← 工作区列表 + 绑定关系
  sessions/                         ← 回退路径（无工作区会话）
  workspace_{id}/
    sessions/
      sess_123456/                  ← 原 shard 布局不变
```

---

## 实现步骤

| # | 步骤 | 影响文件 | 验收 |
|---|------|----------|------|
| 1 | `seelebridge/plan.go`：类型别名 `PlanEdge`、转换函数 `AdjacencyToEdges`、`TopoSort` | `seelebridge/plan.go` (新增) | 编译通过，类型即 `serialize.PlanEdgeSpec` |
| 2 | `application/state.go`：`PlanState.Edges []PlanEdge` | `application/state.go` | Snapshot JSON 含 `plan.edges`，格式同 `PlanEdgeSpec` |
| 3 | `application/chat.go`：`updatePlanFromLoad` 调用桥接函数 | `application/chat.go` | 邻接表 → `[]PlanEdge` + 拓扑序 Nodes |
| 4 | `seelebridge/runtime.go`：`SetPlanNodeCallback` 类型修正为 `func(*types.NodeResult)` | `seelebridge/runtime.go` | 编译通过，回调链路完整 |
| 5 | `application/chat.go`：`HandlePlanNodeComplete` 签名改为 `func(*types.NodeResult)`，`updatePlanFromRunResult` 对齐框架 JSON 格式 | `application/chat.go` | 实时打点 + 最终状态正确更新 |
| 6 | `/new` 清理 Plan/Workspace/Interaction | `application/command.go` | 新建会话后 `Runtime.Plan` 为 nil |
| 7 | `resumeSession` 清理 Plan/Interaction | `application/app.go` | 恢复会话后不显示旧 Plan |
| 8 | Workspace 持久化 Save/Load + `Create`/`Delete`/`BindSession` 自动 Save | `workspace/workspace.go`, `main.go` | 重启后 List 恢复 |
| 9 | NestedSessionStore：工作区嵌套路径 | `seelebridge/session_store.go`, `session/manager.go`, `main.go` | session 文件在工作区子目录 |
| 10 | 会话列表按工作区过滤 | `application/ports.go`, `gui/bridge.go` | `/sessions` 显示当前工作区会话 |

## 测试策略

- **单元**：`seelebridge.PlanEdge` 类型即 `serialize.PlanEdgeSpec`；`AdjacencyToEdges` 转换正确；`TopoSort` 稳定有序
- **集成**：`plan_load` 完整 DAG → Snapshot 含 `edges：[{from,to}]`； `/new`/`resume` → Plan 残余消失
- **持久化**：Workspace 创建 → 重启 → List 恢复；BindSession → 重启 → SessionWorkspace
- **回归**：`go test -race ./application/... ./workspace/... ./seelebridge/...`

## 回滚

无新类型引入，仅添加桥接文件和字段。`PlanState.Edges` 用 `omitempty`，旧客户端忽略。Workspace 新文件不与旧 `sessions/` 目录冲突。
