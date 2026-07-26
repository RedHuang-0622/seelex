# Plan 活动图、会话隔离、工作区持久化 — 详细方案

## 文件级变更预估

| 文件 | 操作 | 职责 |
|------|------|------|
| `seelebridge/plan.go` | **新增** | 桥接层：`type PlanEdge = serialize.PlanEdgeSpec`；`AdjacencyToEdges`；`TopoSort` |
| `application/state.go` | 修改 | `PlanState.Edges []PlanEdge` |
| `application/chat.go` | 修改 | `updatePlanFromLoad` 接入桥接类型+拓扑排序；`HandlePlanNodeComplete` 签名修正为 `func(*types.NodeResult)`；`updatePlanFromRunResult` 对齐框架 NodeBase JSON 平铺格式 |
| `seelebridge/runtime.go` | 修改 | `SetPlanNodeCallback` 类型修正为 `func(*workplanTypes.NodeResult)` |
| `application/command.go` | 修改 | `/new` 清理 Plan/Workspace/Interaction |
| `application/app.go` | 修改 | `resumeSession` 清理 Plan/Interaction |
| `workspace/workspace.go` | 修改 | 新增 Save/Load；Create/Delete/Bind/Unbind 自动持久化 |
| `main.go` | 修改 | 初始化 NestedSessionStore 和 Workspace 持久化路径 |
| `seelebridge/session_store.go` | 新增/修改 | `NestedSessionStore` 封装 |
| `session/manager.go` | 修改 | `ListByWorkspace` 接口 |
| `tui/plan.go` | 修改 | High/Max 模式利用 Edges 渲染连线关系 |
| `gui/bridge.go` | 修改 | 暴露工作区会话接口 |

## 类型复用路径

### 框架→seelebridge→application 链路

```
github.com/RedHuang-0622/Seele/workplan/runtime/serialize.PlanEdgeSpec
  │
  ▼ 类型别名
seelebridge.PlanEdge  (= serialize.PlanEdgeSpec)
  │
  ▼ 引用
application.PlanState.Edges  []seelebridge.PlanEdge
```

### ProgressCallback 链路（修正后）

```
Seele 框架 Scheduler
  │ OnNodeDone(nr *types.NodeResult)
  ▼ WorkPlan.NodeHook = func(*types.NodeResult)
seelebridge.WorkPlanTool.ProgressCallback = func(*types.NodeResult)
  │ SetPlanNodeCallback(cb) 直接赋值，不做签名变换
  ▼
application.Service.HandlePlanNodeComplete(nr *types.NodeResult)
  │ 从 nr.NodeBase 取 NodeID, Kind, Status, Elapsed(), Output
  ▼
更新 service.snapshot.Runtime.Plan.Nodes[i]
```

**与前版方案差异**：不再拆成 4 个扁平参数 `(nodeID, kind, status string, elapsed time.Duration)`，改为直接接收 `*types.NodeResult`。优势：
- 零 wrapper 代码，类型天然兼容
- 可访问 `Output`（节点输出内容）、`Skipped`/`Aborted`（跳过/中止状态）、`StartedAt`/`EndedAt`（精确计时）

### plan_run 返回 JSON 解析（修正后）

框架 `planRunHandler.Execute()` 返回的 `nodes` 字段是 `[]*types.NodeResult`，其中 `NodeBase` 字段平铺在 JSON 中：

```json
{
  "status": "completed",
  "node_count": 3,
  "final_output": "...",
  "nodes": [
    {
      "node_id": "start",
      "kind": "auto",
      "status": "completed",
      "output": "分析完成",
      "skipped": false,
      "aborted": false,
      "started_at": "2026-07-26T10:00:00Z",
      "ended_at": "2026-07-26T10:00:05Z"
    }
  ]
}
```

seelex 端解析结构需对齐（`node_id` 而非 `nodeID`，`skipped`/`aborted` 而非 `Skipped`/`Aborted` 等 snake_case）。

### 不需要的新类型

| 原先设计 | 现方案 | 理由 |
|----------|--------|------|
| `application.PlanEdge` 新 struct | `seelebridge.PlanEdge = serialize.PlanEdgeSpec` | 框架已有，JSON 格式完全匹配 |
| `progressCallback(nodeID, kind, status, elapsed)` | `progressCallback(nr *types.NodeResult)` | 框架原生类型，零适配 |
| `PlanNodeStatus(s string)` 转换函数 | 直接用 `nr.Status`（已是标准字符串） | 框架已统一"completed/failed/skipped/aborted" |

## 核心设计

### PlanState 补 Edges

`PlanState.Edges` 直接使用框架的 `serialize.PlanEdgeSpec` 格式。邻接表 → `[]PlanEdge` 的转换在 `seelebridge` 中完成。

### ProgressCallback — 框架类型直通

`seelebridge/runtime.go` 中 `SetPlanNodeCallback` 的类型从：

```go
// 【旧】与框架不兼容
func (r *Runtime) SetPlanNodeCallback(cb func(nodeID, kind, status string, elapsed time.Duration))
```

改为：

```go
// 【新】直接匹配框架类型
import workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"

func (r *Runtime) SetPlanNodeCallback(cb func(nr *workplanTypes.NodeResult)) {
    if r.planTool != nil {
        r.planTool.ProgressCallback = cb
    }
}
```

`application/chat.go` 中 `HandlePlanNodeComplete` 相应修正：

```go
func (service *Service) HandlePlanNodeComplete(nr *workplanTypes.NodeResult) {
    service.mu.Lock()
    plan := service.snapshot.Runtime.Plan
    if plan == nil {
        service.mu.Unlock()
        return
    }
    for i := range plan.Nodes {
        if plan.Nodes[i].ID == nr.NodeID {
            plan.Nodes[i].Status = PlanNodeStatus(nr.Status)
            plan.Nodes[i].Elapsed = nr.Elapsed().String()
            plan.Nodes[i].Kind = nr.Kind
            break
        }
    }
    // 重新计算进度...
}
```

`updatePlanFromRunResult` 解析结构对齐框架 JSON（snake_case field names）：

```go
// plan_run 返回的 per-node 结果（对齐 NodeBase 的 JSON 标签）
Nodes []struct {
    NodeID    string `json:"node_id"`
    Kind      string `json:"kind"`
    Status    string `json:"status"`
    Output    string `json:"output,omitempty"`
    Skipped   bool   `json:"skipped"`
    Aborted   bool   `json:"aborted"`
    StartedAt string `json:"started_at,omitempty"`
    EndedAt   string `json:"ended_at,omitempty"`
} `json:"nodes,omitempty"`
```

### 会话隔离

两个入口点加清理：
- `/new` command handler
- `resumeSession` method

需要清理的字段：`Runtime.Plan`、`Snapshot.Interaction`、`CurrentWorkspace`

### 工作区持久化

`workspace.Repo` 通过 JSON 文件 `workspace_index.json` 持久化 `workspaces` 和 `bindings` 两个 map。每次变更（Create/Delete/BindSession/UnbindSession）后调用 `Save()`。

### 会话按工作区组织

`NestedSessionStore` 封装：
- 无工作区 → `sessions/` 回退路径
- 有工作区 → `workspace_{id}/sessions/`
- 内部创建 `SessionStore` 实例，复用现有分片存储逻辑

## 增量验证

1. `go build ./...` 编译
2. `go vet ./...` 静态检查
3. `go test -count=1 ./seelebridge/...` — seelebridge 新类型
4. `go test -count=1 ./application/...` — PlanState 变更
5. `go test -count=1 -race ./application/... ./workspace/...` — 并发安全
6. 手动 TUI/GUI 验证 Plan 活动图 + 实时打点
