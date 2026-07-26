# Plan 活动图、会话隔离、工作区持久化 — 修复方案

## 目标

1. **Plan 活动图 JSON 输出** — `PlanState` 携带 DAG 边信息（`edges`），GU I/TUI 据此渲染有向连线图。
2. **活动状态实时打点** — `plan_run` 执行中每节点状态通过 `ProgressCallback` 实时同步到 `PlanState`，节点状态图标与耗时在活动图上正确显示。
3. **会话切换上下文隔离** — `/new` 和 `/resume` 清除 `Runtime.Plan`、`CurrentWorkspace`、`Interaction`，杜绝旧 Plan 跨会话残留。
4. **工作区持久化** — `workspace.Repo` 接入文件存储 Save/Load，进程重启后恢复。
5. **会话按工作区组织** — 存储路径从 `sessions/` → `workspace/{workspace_id}/sessions/{session_id}`，支持工作区维度的会话管理。

## 根因

| 问题 | 根因 | 文件位置 |
|------|------|----------|
| 活动图无 JSON | `updatePlanFromLoad` 解析了 `edges` 但从未使用；Go map 迭代顺序随机 | `application/chat.go:185-207` |
| 状态无法打点 | 活动图（边/层次）不存在，节点是平铺列表，无从"打点" | `application/chat.go:updatePlanFromLoad` / `state.go:PlanState` |
| 会话 Plan 污染 | `/new` 和 `resumeSession` 不清理 `Runtime.Plan` | `application/command.go:108-123` / `application/app.go:461-513` |
| 工作区无持久 | `workspace.Repo` 纯内存 `map`，无 Save/Load | `workspace/workspace.go:37-41` / `main.go:90` |
| 会话不平铺 | `seelebridge.SessionStore` 只有 `sessions/` 目录，无工作区嵌套 | `seelebridge/runtime.go` / `storage.NewFileStore` |

## 接口与数据结构

### 1. `PlanState` 新增 `Edges` 字段

```go
// application/state.go — PlanState 补充 DAG 边信息
type Edge struct {
    From string `json:"from"`
    To   string `json:"to"`
}

type PlanState struct {
    Name     string     `json:"name"`
    Status   PlanStatus `json:"status"`
    Nodes    []PlanNode `json:"nodes,omitempty"`
    Edges    []Edge     `json:"edges,omitempty"`    // 新增：DAG 有向边
    Progress float64    `json:"progress"`
    Elapsed  string     `json:"elapsed,omitempty"`
}
```

### 2. 节点排序策略

按 `Edges` 拓扑序排序节点列表，消除 map 迭代随机性。从 `Entry` 节点出发 BFS/DFS 遍历 DAG 得到稳定顺序。

### 3. `Workspace` 持久化接口

```go
// workspace/workspace.go — 新增 Save/Load/Close
type Repo struct {
    ...
    saveFn func(repo *Repo) error
    loadFn func(repo *Repo) error
}

func (r *Repo) InjectSaveLoad(saveFn, loadFn func(repo *Repo) error)
func (r *Repo) Save() error
func (r *Repo) Load() error
```

### 4. 会话存储分层

```
存储目录:
  .seelex/
    workspace_index.json          ← 工作区列表
    workspace_A/
      meta.json                   ← 工作区元信息
      sessions/
        sess_123456/              ← 消息文件 (原 sessions/ 目录布局)
        sess_123457/
    workspace_B/
      ...
```

## 实现步骤

| # | 步骤 | 影响文件 | 验收 |
|---|------|----------|------|
| 1 | `PlanState.Edges` 字段 + 拓扑排序 | `application/state.go`, `chat.go` | Snapshot JSON 包含 `plan.edges`；节点顺序稳定 |
| 2 | `updatePlanFromLoad` 解析 `edges` 构建 `PlanState.Edges` 和 `Children` 树 | `application/chat.go` | TUI High/Max 模式渲染缩进树；GUI 收到 edges 可画连线 |
| 3 | `HandlePlanNodeComplete` 实时同步到 `PlanState` | `application/chat.go`（已有逻辑，验证链路） | 确认 `plan_run` 中的 `ProgressCallback` → `EventSnapshotChanged` 链路 |
| 4 | `/new` 清理 Plan/Workspace/Interaction | `application/command.go` | 新建会话后 Snapshot.Runtime.Plan 为 nil |
| 5 | `resumeSession` 清理 Plan/Interaction | `application/app.go` | 恢复会话后不显示旧 Plan |
| 6 | `Workspace` 持久化 Save/Load | `workspace/workspace.go`, `main.go` | 重启后 `Workspace.List()` 恢复 |
| 7 | 会话存储分层：工作区 Nested SessionStore | `seelebridge/session_store.go`, `session/manager.go` | session 文件在 `workspace/{id}/sessions/` 下 |
| 8 | 会话列表按工作区过滤 | `application/ports.go`, `gui/bridge.go`, `tui/tui.go` | `/sessions` 显示当前工作区的会话 |

## 测试策略

- **单元**：PlanState 拓扑排序、Edges 序列化、Workspace Save/Load roundtrip
- **集成**：完整 `plan_load` → `plan_run` → Snapshot JSON 包含 edges；`/new` → Snapshot.Plan 为 nil；`/resume` → 旧 Plan 消失
- **持久化**：Workspace 创建后进程重启 → List 恢复；Session 在 workspace 子目录
- **边界**：空 Plan、空 Workspace、无 edges 的 plan_load、跨 workspace 切换
- **回归**：现有 application race tests、GUI bridge tests、Session save/load

## 回滚

PlanState 新增字段 `edges` 使用 `omitempty`，旧客户端忽略该字段即可向后兼容。Workspace 持久化新文件格式不影响旧 `.seelex/sessions/` 目录。SessionStore 路径变更需要数据迁移或保持向后兼容（优先读取新路径，回退旧路径）。
