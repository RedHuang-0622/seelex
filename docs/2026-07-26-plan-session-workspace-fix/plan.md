# Plan 活动图、会话隔离、工作区持久化 — 修复方案

## 目标

1. **Plan 活动图 JSON 输出** — `PlanState` 携带 DAG 边信息，GUI/TUI 据此渲染有向连线图。
2. **活动状态实时打点** — `plan_run` 执行中每节点状态通过 `ProgressCallback` 实时同步到节点图标与耗时。
3. **会话切换上下文隔离** — `/new` 和 `/resume` 清除 `Runtime.Plan`、`CurrentWorkspace`、`Interaction`，杜绝旧 Plan 跨会话残留。
4. **工作区持久化** — `workspace.Repo` 接入文件存储 Save/Load，进程重启后恢复。
5. **会话按工作区组织** — 存储路径从 `sessions/` → `workspace/{workspace_id}/sessions/{session_id}`。

## 根因

| 问题 | 根因 | 文件位置 |
|------|------|----------|
| 活动图无 JSON | `updatePlanFromLoad` 解析了 `edges` 但从未使用；Go map 迭代顺序随机 | `application/chat.go:185-207` |
| 状态无法打点 | 活动图（边/层次）不存在，节点是平铺列表，无从"打点" | `application/chat.go:updatePlanFromLoad` / `state.go:PlanState` |
| 会话 Plan 污染 | `/new` 和 `resumeSession` 不清理 `Runtime.Plan` | `application/command.go:108-123` / `application/app.go:461-513` |
| 工作区无持久 | `workspace.Repo` 纯内存 `map`，无 Save/Load | `workspace/workspace.go:37-41` / `main.go:90` |
| 会话不平铺 | `seelebridge.SessionStore` 只有 `sessions/` 目录，无工作区嵌套 | `seelebridge/runtime.go` / `storage.NewFileStore` |

## 接口与数据结构

### 1. PlanEdge — 复用 Seele 框架的 edge 序列化格式

`PlanEdge` 的设计直接镜像框架 `serialize.PlanEdgeSpec` 的 JSON 结构，使 GUI 收到的边格式与框架的 `plan_export` 输出一致。

**框架参考** — `G:\Program\go\seele\workplan\runtime\serialize\serialize.go:23-29`:

```go
// 框架的序列化边格式
type PlanEdgeSpec struct {
    From      string `json:"from"`
    To        string `json:"to"`
    Label     string `json:"label,omitempty"`
    Condition string `json:"condition,omitempty"` // label reference to ConditionRegistry
}
```

**Seelex 镜像类型** — `application/state.go` 新增：

```go
// PlanEdge 镜像框架 serialize.PlanEdgeSpec，保持 JSON 格式兼容。
type PlanEdge struct {
    From      string `json:"from"`
    To        string `json:"to"`
    Label     string `json:"label,omitempty"`
    Condition string `json:"condition,omitempty"` // 条件标签（框架 ConditionRegistry 中的名称）
}
```

### 2. PlanState 补充 Edges 字段

```go
type PlanState struct {
    Name     string     `json:"name"`
    Status   PlanStatus `json:"status"`
    Nodes    []PlanNode `json:"nodes,omitempty"`
    Edges    []PlanEdge `json:"edges,omitempty"`     // 新增：DAG 有向边（兼容框架 PlanEdgeSpec 格式）
    Progress float64    `json:"progress"`
    Elapsed  string     `json:"elapsed,omitempty"`
}
```

### 3. 节点排序策略

按 `Edges` 拓扑序排序节点列表，消除 map 迭代随机性。从 `Entry` 节点出发 DFS 遍历 DAG 得到稳定顺序。

### 4. 数据流

```
plan_load args (邻接表 map[string][]string)
  → updatePlanFromLoad 转换为 []PlanEdge{From, To}
    → PlanState.Edges JSON 输出

plan_export result ([]PlanEdgeSpec{From, To, Label, Condition})
  → 直接匹配 PlanEdge 格式，可复用
```

### 5. Workspace 持久化接口

```go
type Repo struct {
    ...
    saveFn func(repo *Repo) error
    loadFn func(repo *Repo) error
}

func (r *Repo) InjectSaveLoad(saveFn, loadFn func(repo *Repo) error)
func (r *Repo) Save() error
func (r *Repo) Load() error
```

### 6. 会话存储分层

```
.seelex/
  workspace_index.json              ← 工作区列表 + 绑定关系
  workspace_A/
    sessions/
      sess_123456/                  ← 原 sessions/ 目录布局不变
      sess_123457/
  workspace_B/
    sessions/
```

## 实现步骤

| # | 步骤 | 影响文件 | 验收 |
|---|------|----------|------|
| 1 | `PlanEdge` 类型 + `PlanState.Edges` 字段 | `application/state.go` | Snapshot JSON 包含 `plan.edges` 且格式兼容 `PlanEdgeSpec` |
| 2 | `updatePlanFromLoad` 解析 `edges` 邻接表为 `[]PlanEdge`；拓扑排序消除随机顺序 | `application/chat.go` | 节点顺序稳定；GUI 收到 `{from, to}` 边列表可画连线 |
| 3 | `HandlePlanNodeComplete` 实时同步验证 | `application/chat.go`（已有逻辑） | 确认 `ProgressCallback` → `EventSnapshotChanged` 链路包含更新后的 Edges |
| 4 | `/new` 清理 Plan/Workspace/Interaction | `application/command.go` | 新建会话后 `Runtime.Plan` 为 nil |
| 5 | `resumeSession` 清理 Plan/Interaction | `application/app.go` | 恢复会话后不显示旧 Plan |
| 6 | Workspace 持久化 Save/Load | `workspace/workspace.go`, `main.go` | 重启后 `List()` 恢复；`Create`/`BindSession` 自动保存 |
| 7 | 会话存储分层：NestedSessionStore | `seelebridge/session_store.go`, `session/manager.go` | session 文件在 `workspace/{id}/sessions/` 下 |
| 8 | 会话列表按工作区过滤 | `application/ports.go`, `gui/bridge.go`, `tui/tui.go` | `/sessions` 显示当前工作区会话 |

## 测试策略

- **单元**：PlanEdge 序列化 roundtrip；拓扑排序稳定；Workspace Save/Load roundtrip
- **集成**：`plan_load` → Snapshot 含 edges 且格式 JSON `[{from, to}]`；`/new` → Plan 为 nil；`/resume` → 旧 Plan 消失
- **持久化**：Create workspace → 进程重启 → List 恢复；BindSession → 重启 → SessionWorkspace 恢复
- **边界**：空 Plan、无 edges 的 plan_load、跨 workspace 切换
- **回归**：现有 application race tests、GUI bridge tests

## 回滚

`PlanEdge` 使用 `omitempty`，旧客户端忽略 `edges` 字段向后兼容。Workspace 新文件 `workspace_index.json` 不影响旧 `.seelex/sessions/`。NestedSessionStore 优先读新路径，回退旧路径。
