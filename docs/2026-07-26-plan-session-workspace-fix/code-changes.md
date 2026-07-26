# Plan 活动图、会话隔离、工作区持久化 — 代码变更

## 变更概览

本轮修复 Seelex 核心层的三个连锁问题：Plan 活动图边信息丢失导致 GUI/TUI 无法渲染连线图；会话切换时 `Runtime.Plan` 残留导致上下文污染；工作区纯内存无持久化且会话存储不按工作区组织。

## 文件与职责

| 文件 | 变更 | 设计职责 |
|------|------|----------|
| `application/state.go` | 修改 | `PlanEdge` 类型（镜像框架 `serialize.PlanEdgeSpec`）；`PlanState.Edges []PlanEdge` |
| `application/chat.go` | 修改 | `updatePlanFromLoad` 解析 `edges` 邻接表 → `[]PlanEdge` + 拓扑排序 |
| `application/command.go` | 修改 | `/new` 清理 `Runtime.Plan`、`CurrentWorkspace`、`Interaction` |
| `application/app.go` | 修改 | `resumeSession` 清理 `Runtime.Plan`、`Interaction` |
| `workspace/workspace.go` | 修改 | 新增 `InjectSaveLoad`、`Save`、`Load`，JSON 文件持久化 |
| `seelebridge/session_store.go` | 新增/修改 | `NestedSessionStore` 支持 `workspace/{id}/sessions/` 嵌套路径 |
| `session/manager.go` | 修改 | `InjectSaveLoad` 增加 `workspaceID` 参数；`ListByWorkspace` |
| `main.go` | 修改 | Wiring：Workspace 带持久化路径、NestedSessionStore 初始化 |
| `application/ports.go` | 修改 | `SessionPort` 补充 `ListByWorkspace` |
| `gui/bridge.go` | 修改 | 暴露工作区会话方法 |
| `tui/tui.go` / `tui/plan.go` | 修改 | 支持 `PlanEdge` 的连线/树形渲染 |
| `application/*_test.go` | 新增/修改 | 单元、集成、边界、持久化和回归 |
| `workspace/workspace_test.go` | 新增 | Workspace Save/Load、bind 持久化 roundtrip |

---

## 核心实现

### 1. PlanEdge — 复用框架 serialize.PlanEdgeSpec 设计

**框架参考** — `G:\Program\go\seele\workplan\runtime\serialize\serialize.go:23-29`:

```go
// 框架序列化边 — 被 plan_export 使用
type PlanEdgeSpec struct {
    From      string `json:"from"`
    To        string `json:"to"`
    Label     string `json:"label,omitempty"`
    Condition string `json:"condition,omitempty"` // ConditionRegistry 标签引用
}
```

**问题代码** — `application/state.go` 当前 PlanState 无边信息：

```go
type PlanState struct {
    Name     string     `json:"name"`
    Status   PlanStatus `json:"status"`
    Nodes    []PlanNode `json:"nodes,omitempty"`
    Progress float64    `json:"progress"`
    Elapsed  string     `json:"elapsed,omitempty"`
    // ← 没有 edges
}
```

**改动后** — `application/state.go` 新增 `PlanEdge`，与框架 `PlanEdgeSpec` JSON 格式兼容：

```go
// PlanEdge 镜像框架 serialize.PlanEdgeSpec，JSON 格式完全兼容。
// 使 GUI 侧可用同一 consumer 处理 plan_load 和 plan_export 的边数据。
type PlanEdge struct {
    From      string `json:"from"`
    To        string `json:"to"`
    Label     string `json:"label,omitempty"`
    Condition string `json:"condition,omitempty"` // 条件标签（框架 ConditionRegistry 名称）
}

type PlanState struct {
    Name     string     `json:"name"`
    Status   PlanStatus `json:"status"`
    Nodes    []PlanNode `json:"nodes,omitempty"`
    Edges    []PlanEdge `json:"edges,omitempty"`   // 新增：DAG 有向边列表
    Progress float64    `json:"progress"`
    Elapsed  string     `json:"elapsed,omitempty"`
}
```

`cloneRuntimeState` 同步深拷贝 Edges：

```go
func cloneRuntimeState(runtime RuntimeState) RuntimeState {
    copyRuntime := runtime
    // ...
    if runtime.Plan != nil {
        planCopy := *runtime.Plan
        planCopy.Nodes = clonePlanNodes(runtime.Plan.Nodes)
        planCopy.Edges = append([]PlanEdge(nil), runtime.Plan.Edges...)  // 新增
        copyRuntime.Plan = &planCopy
    }
    return copyRuntime
}
```

### 2. updatePlanFromLoad — 邻接表 → []PlanEdge + 拓扑排序

**问题代码** — `application/chat.go` 当前：

```go
func (service *Service) updatePlanFromLoad(argsJSON string) {
    type planNodeSpec struct {
        Input string `json:"input"`
    }
    var input struct {
        Entry string                  `json:"entry"`
        Nodes map[string]planNodeSpec `json:"nodes"`
        Edges map[string][]string     `json:"edges"`   // ← 解析了但未使用
    }
    if err := json.Unmarshal([]byte(argsJSON), &input); err != nil || len(input.Nodes) == 0 {
        return
    }
    nodes := make([]PlanNode, 0, len(input.Nodes))
    for id := range input.Nodes {    // ← map 遍历，随机顺序
        nodes = append(nodes, PlanNode{ID: id, Label: id, Status: NodePending})
    }
    service.snapshot.Runtime.Plan = &PlanState{
        Name:   input.Entry,
        Status: PlanPending,
        Nodes:  nodes,               // ← 只有平铺列表，无 edges
    }
}
```

**改动后** — 邻接表 → `[]PlanEdge` + 拓扑排序 + Children 树：

```go
func (service *Service) updatePlanFromLoad(argsJSON string) {
    type planNodeSpec struct{ Input string `json:"input"` }
    var input struct {
        Entry string                  `json:"entry"`
        Nodes map[string]planNodeSpec `json:"nodes"`
        Edges map[string][]string     `json:"edges"`
    }
    if err := json.Unmarshal([]byte(argsJSON), &input); err != nil || len(input.Nodes) == 0 {
        return
    }

    // 1. 邻接表 → []PlanEdge（复用框架 PlanEdgeSpec 格式）
    edges := make([]PlanEdge, 0)
    for from, targets := range input.Edges {
        for _, to := range targets {
            edges = append(edges, PlanEdge{From: from, To: to})
        }
    }

    // 2. 拓扑排序节点列表（消除 map 迭代随机性）
    sortedIDs := topologicalSort(input.Entry, input.Edges, input.Nodes)

    // 3. 构建 Children 树（TUI 树形渲染用）
    childrenMap := buildChildrenMap(input.Edges)

    // 4. 按拓扑序构造节点列表
    nodes := make([]PlanNode, 0, len(sortedIDs))
    for _, id := range sortedIDs {
        nodes = append(nodes, PlanNode{
            ID: id, Label: id, Status: NodePending,
            Children: childrenMap[id],
        })
    }

    plan := &PlanState{
        Name:   input.Entry,
        Status: PlanPending,
        Nodes:  nodes,
        Edges:  edges,            // ← GUI 据此渲染有向连线图
    }
    service.snapshot.Runtime.Plan = plan
}

// topologicalSort 从 Entry 开始 DFS 后序 → 反转得到拓扑序
func topologicalSort(entry string, edges map[string][]string, nodes map[string]planNodeSpec) []string {
    visited := make(map[string]bool)
    result := make([]string, 0, len(nodes))
    var dfs func(id string)
    dfs = func(id string) {
        if visited[id] {
            return
        }
        visited[id] = true
        for _, next := range edges[id] {
            dfs(next)
        }
        result = append(result, id)
    }
    dfs(entry)
    // 反转 → 拓扑序
    for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
        result[i], result[j] = result[j], result[i]
    }
    // 孤立节点追加到末尾
    for id := range nodes {
        if !visited[id] {
            result = append(result, id)
        }
    }
    return result
}

// buildChildrenMap 从邻接表构建 PlanNode 子节点树
func buildChildrenMap(edges map[string][]string) map[string][]PlanNode {
    children := make(map[string][]PlanNode)
    for from, targets := range edges {
        for _, to := range targets {
            children[from] = append(children[from], PlanNode{
                ID: to, Label: to, Status: NodePending,
            })
        }
    }
    return children
}
```

### 3. 状态实时打点（验证已有链路）

已有逻辑可以复用，无需改动：

```
framework scheduler.OnNodeDone(nodeID, kind, status, elapsed)
  → WorkPlanTool.ProgressCallback
    → Service.HandlePlanNodeComplete(nodeID, kind, status, elapsed)
      → plan.Nodes[i].Status = PlanNodeStatus(status)
      → plan.Nodes[i].Elapsed = elapsed.String()
      → recalculate Progress
      → events.Publish(EventSnapshotChanged)
        → TUI: model.snapshot = app.Snapshot() → PlanPanel 重绘（含节点图标）
        → GUI: 前端收到事件 → 活动图节点上更新状态色/图标/耗时
```

`PlanEdge` 无状态字段（边本身没有运行状态，状态在节点上），但 GUI 可根据节点状态动态改变连线颜色（如已完成节点之间的线变绿）。

### 4. /new 清理 Plan/Workspace/Interaction

**问题代码** — `application/command.go:108-123`：

```go
register("new", ... , func(...) {
    newID := service.deps.Engine.StartSession()
    service.mu.Lock()
    service.snapshot.Session.ID = newID
    service.snapshot.HistoryOffset = 0
    service.snapshot.TotalMessages = 0
    service.snapshot.HasMoreHistory = false
    // ← 未清理：Runtime.Plan、CurrentWorkspace、Interaction
    service.mu.Unlock()
})
```

**改动后**：

```go
    service.mu.Lock()
    service.snapshot.Session.ID = newID
    service.snapshot.HistoryOffset = 0
    service.snapshot.TotalMessages = 0
    service.snapshot.HasMoreHistory = false
    // ★ 清除旧会话残留状态
    service.snapshot.Runtime.Plan = nil
    service.snapshot.CurrentWorkspace = nil
    service.snapshot.Interaction = nil
    service.mu.Unlock()
```

### 5. resumeSession 清理 Plan/Interaction

**问题代码** — `application/app.go:461-513`：

```go
func (service *Service) resumeSession(sessionID string) error {
    // ...
    service.mu.Lock()
    service.snapshot.Session.ID = sessionID
    service.snapshot.Conversation = nil
    service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
    service.appendHistoryLocked(visibleHistory)
    // ← 未清理：Runtime.Plan、Interaction
    service.mu.Unlock()
}
```

**改动后**：

```go
    service.mu.Lock()
    service.snapshot.Session.ID = sessionID
    service.snapshot.Conversation = nil
    service.snapshot.Runtime.Plan = nil     // ★ 清除旧 Plan
    service.snapshot.Interaction = nil      // ★ 清除旧交互
    service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
    service.appendHistoryLocked(visibleHistory)
```

### 6. Workspace 持久化

**问题代码** — `workspace/workspace.go`：

```go
type Repo struct {
    mu         sync.RWMutex
    workspaces map[string]Info    // 纯内存，重启丢
    bindings   map[string]string  // 纯内存，重启丢
}
```

**改动后** — JSON 文件 Save/Load：

```go
type Repo struct {
    mu         sync.RWMutex
    workspaces map[string]Info
    bindings   map[string]string
    storePath  string
    saveFn     func(repo *Repo) error
    loadFn     func(repo *Repo) error
}

type repoSnapshot struct {
    Workspaces map[string]Info   `json:"workspaces"`
    Bindings   map[string]string `json:"bindings"`
}

func NewRepoWithStore(storePath string) (*Repo, error) {
    r := &Repo{
        workspaces: make(map[string]Info),
        bindings:   make(map[string]string),
        storePath:  storePath,
    }
    r.saveFn = defaultSave
    r.loadFn = defaultLoad
    if err := r.Load(); err != nil {
        return nil, err
    }
    return r, nil
}

func defaultSave(r *Repo) error {
    r.mu.RLock()
    snap := repoSnapshot{Workspaces: r.workspaces, Bindings: r.bindings}
    r.mu.RUnlock()
    data, _ := json.MarshalIndent(snap, "", "  ")
    return os.WriteFile(filepath.Join(r.storePath, "workspace_index.json"), data, 0644)
}

func defaultLoad(r *Repo) error {
    path := filepath.Join(r.storePath, "workspace_index.json")
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) { return nil }  // 首次启动
        return err
    }
    var snap repoSnapshot
    if err := json.Unmarshal(data, &snap); err != nil {
        return err
    }
    r.mu.Lock()
    r.workspaces = snap.Workspaces
    r.bindings = snap.Bindings
    r.mu.Unlock()
    return nil
}
```

`Create`、`Delete`、`BindSession` 等修改操作末尾调用 `r.Save()`：

```go
func (r *Repo) Create(name, rootPath, gitRemote string) (Info, error) {
    // ... 原有逻辑 ...
    r.workspaces[id] = w
    r.mu.Unlock()
    r.Save()    // 新增
    return w, nil
}
```

### 7. 会话按工作区组织 — NestedSessionStore

**问题代码** — 当前 `sessions/` 目录平铺：

```
.seelex/sessions/ ← 所有会话平铺
```

**改动后** — `seelebridge/session_store.go` 新增：

```go
type NestedSessionStore struct {
    mu          sync.RWMutex
    basePath    string
    stores      map[string]*SessionStore  // workspaceID → store
    defaultPath string
}

func NewNestedSessionStore(basePath string) *NestedSessionStore {
    return &NestedSessionStore{
        basePath:    basePath,
        stores:      make(map[string]*SessionStore),
        defaultPath: filepath.Join(basePath, "sessions"),
    }
}

// StoreFor 获取/创建工作区专属 SessionStore
func (n *NestedSessionStore) StoreFor(workspaceID string) *SessionStore {
    if workspaceID == "" {
        store, _ := NewSessionStore(n.defaultPath)
        return store
    }
    n.mu.Lock()
    defer n.mu.Unlock()
    if store, ok := n.stores[workspaceID]; ok {
        return store
    }
    path := filepath.Join(n.basePath, "workspace", workspaceID, "sessions")
    store, err := NewSessionStore(path)
    if err != nil {
        return nil
    }
    n.stores[workspaceID] = store
    return store
}

func (n *NestedSessionStore) ListByWorkspace(workspaceID string) []SessionMeta {
    store := n.StoreFor(workspaceID)
    if store == nil {
        return nil
    }
    return store.List()
}
```

`main.go` 初始化调整：

```go
func initStore() *seelebridge.NestedSessionStore {
    return seelebridge.NewNestedSessionStore(filepath.Dir(*storePath))
}

// SessionPort 适配 NestedSessionStore
type sessionPort struct {
    store *seelebridge.NestedSessionStore
    // ...
}

func (p *sessionPort) SaveCurrent(workspaceID, sessionID string) error {
    s := p.store.StoreFor(workspaceID)
    return s.Save(sessionID, p.eng.rawHistory())
}

func (p *sessionPort) LoadHistory(workspaceID, sessionID string) ([]seelebridge.Message, error) {
    s := p.store.StoreFor(workspaceID)
    return s.Load(sessionID)
}

func (p *sessionPort) ListByWorkspace(workspaceID string) []SessionInfo {
    return p.store.ListByWorkspace(workspaceID)
}
```

### 8. 会话列表按工作区过滤

`application/ports.go` 补充接口：

```go
type SessionPort interface {
    SaveCurrent(string) error
    Delete(string) error
    List() []SessionInfo
    ListByWorkspace(workspaceID string) []SessionInfo  // 新增
    LoadHistory(string) ([]EngineMessage, error)
    LoadHistoryRange(id string, offset, limit int) ([]EngineMessage, int, error)
}
```

---

## 兼容性

- `PlanEdge` JSON 格式 （`{from, to, label, condition}`）与框架 `serialize.PlanEdgeSpec` 一致，GUI 侧可用同一 consumer
- `PlanState.Edges` 用 `omitempty`，旧 Plan JSON 不包含 edges 字段时反序列化为 nil
- Workspace 持久化 `workspace_index.json` 文件与现有 `.seelex/sessions/` 目录共存，互不影响
- `NestedSessionStore` 优先读 `workspace/{id}/sessions/`，无工作区时回退 `sessions/`
- Snapshot/Event DTO、Wails Bridge、TUI Controller 公共签名不变
- 现有 Session schema 不变，仅组织路径分层

## 测试计划

### 单元测试

| 测试 | 文件 | 覆盖 |
|------|------|------|
| `TestPlanEdgeJSON` | `application/state_test.go` | PlanEdge JSON 序列化 roundtrip，兼容 `PlanEdgeSpec` 格式 |
| `TestTopologicalSort` | `application/chat_test.go` | 线性 DAG、fork、孤立节点、空图、循环检测 |
| `TestBuildChildrenMap` | `application/chat_test.go` | 多层嵌套、交叉引用 |
| `TestPlanStateCloneEdges` | `application/application_test.go` | `cloneRuntimeState` 的 Edges 深拷贝隔离 |
| `TestWorkspacePersistence` | `workspace/workspace_test.go` | Create roundtrip；进程模拟 save→load→verify |
| `TestWorkspaceBindPersistence` | `workspace/workspace_test.go` | BindSession → Save → Load → SessionWorkspace |
| `TestNestedSessionStore` | `seelebridge/session_store_test.go` | 工作区路径创建、回退路径、并发安全 |

### 集成测试

| 测试 | 文件 | 覆盖 |
|------|------|------|
| `TestPlanLoadFullDAG` | `application/application_test.go` | `plan_load` 完整 DAG → Snapshot 含 Edges 且格式正确 |
| `TestNewSessionCleansPlan` | `application/command_test.go` | `/new` 后 Snapshot.Runtime.Plan == nil |
| `TestResumeSessionCleansPlan` | `application/application_test.go` | `/resume` 后旧 Plan 消失 |
| `TestWorkspaceRestartRecovery` | `workspace/workspace_test.go` | 创建 workspace → 模拟重启 → List 恢复 |

### 边界测试

| 场景 | 覆盖 |
|------|------|
| 空 Plan（`plan_clear` 后） | `Runtime.Plan` nil → TUI 不显示、GUI 无 plan 块 |
| 单节点无 edges | `Edges` 为 nil/空，拓扑序返回 [entry] |
| `/new` 时 Interaction 活跃 | Interaction 关闭，Plan 清空 |
| 首次启动无 workspace_index.json | Load 返回 nil 而非错误 |

### 回归测试

```bash
go build ./...                       # 编译
go vet ./...                         # 静态检查
go test -race -count=1 ./application/...    # 应用层 race
go test -race -count=1 ./workspace/...     # 工作区
go test -race -count=1 ./seelebridge/...   # SessionStore
```
