# Plan 活动图、会话隔离、工作区持久化 — 代码变更

## 变更概览

本轮修复 Seelex 核心层的三个连锁问题：Plan 活动图边信息丢失导致 GUI/TUI 无法渲染连线图；会话切换时 `Runtime.Plan` 残留导致上下文污染；工作区纯内存无持久化且会话存储不按工作区组织。

## 文件与职责

| 文件 | 变更 | 设计职责 |
|------|------|----------|
| `application/state.go` | 修改 | `PlanState` 新增 `Edges []Edge`；`InputMap` 补充 `Inputs` 字段 |
| `application/chat.go` | 修改 | `updatePlanFromLoad` 解析 `edges` 构建 `PlanState.Edges` 和 `Children` 树；节点拓扑排序 |
| `application/command.go` | 修改 | `/new` 清理 `Runtime.Plan`、`CurrentWorkspace`、`Interaction` |
| `application/app.go` | 修改 | `resumeSession` 清理 `Runtime.Plan`、`Interaction` |
| `application/ports.go` | 不改 | 已有 `WorkspacePort` 接口 |
| `workspace/workspace.go` | 修改 | 新增 `InjectSaveLoad`、`Save`、`Load`、`Close`，接入文件持久化 |
| `seelebridge/session_store.go` | 修改 | 新增 `NestedSessionStore` 支持工作区嵌套路径 |
| `session/manager.go` | 修改 | 支持工作区维度的会话列表和 CRUD |
| `main.go` | 修改 | Wiring：Workspace 持久化路径、NestedSessionStore 初始化 |
| `application/ports.go` | 修改 | `SessionPort` 补充 `ListByWorkspace` 等接口 |
| `gui/bridge.go` | 修改 | 暴露工作区会话相关方法 |
| `tui/tui.go` / `tui/plan.go` | 修改 | 支持 `PlanState.Edges` 的树形/连线渲染 |
| `application/*_test.go` | 新增/修改 | 单元、集成、边界、持久化和回归测试 |

---

## 核心实现

### 1. PlanState 补充 DAG 边信息

**问题代码** — `application/state.go` 当前 PlanState：

```go
// 当前代码：PlanState 只有平铺 Nodes，没有边
type PlanState struct {
    Name     string     `json:"name"`
    Status   PlanStatus `json:"status"`
    Nodes    []PlanNode `json:"nodes,omitempty"`
    Progress float64    `json:"progress"`
    Elapsed  string     `json:"elapsed,omitempty"`
}
```

**改动后** — 新增 `Edge` 类型和 `Edges` 字段：

```go
// 新增：有向边
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

`cloneRuntimeState` 需要同步复制 `Edges`：

```go
func cloneRuntimeState(runtime RuntimeState) RuntimeState {
    copyRuntime := runtime
    // ...
    if runtime.Plan != nil {
        planCopy := *runtime.Plan
        planCopy.Nodes = clonePlanNodes(runtime.Plan.Nodes)
        planCopy.Edges = append([]Edge(nil), runtime.Plan.Edges...)  // 新增
        copyRuntime.Plan = &planCopy
    }
    return copyRuntime
}
```

### 2. updatePlanFromLoad 解析 edges 构建完整活动图

**问题代码** — `application/chat.go` 当前 `updatePlanFromLoad`：

```go
// 当前代码：edges 被解析但从未使用，map 迭代顺序随机
func (service *Service) updatePlanFromLoad(argsJSON string) {
    type planNodeSpec struct {
        Input string `json:"input"`
    }
    var input struct {
        Entry string                  `json:"entry"`
        Nodes map[string]planNodeSpec `json:"nodes"`
        Edges map[string][]string     `json:"edges"`   // ← 解析了，但没用到
    }
    if err := json.Unmarshal([]byte(argsJSON), &input); err != nil || len(input.Nodes) == 0 {
        return
    }
    nodes := make([]PlanNode, 0, len(input.Nodes))
    for id := range input.Nodes {    // ← map 遍历，顺序随机
        label := id
        nodes = append(nodes, PlanNode{ID: id, Label: label, Status: NodePending})
    }
    service.snapshot.Runtime.Plan = &PlanState{
        Name:   input.Entry,
        Status: PlanPending,
        Nodes:  nodes,               // ← 平铺列表，无 edges
    }
}
```

**改动后** — 解析 edges，拓扑排序，构建 Children 树：

```go
func (service *Service) updatePlanFromLoad(argsJSON string) {
    type planNodeSpec struct {
        Input string `json:"input"`
    }
    var input struct {
        Entry string                  `json:"entry"`
        Nodes map[string]planNodeSpec `json:"nodes"`
        Edges map[string][]string     `json:"edges"`
    }
    if err := json.Unmarshal([]byte(argsJSON), &input); err != nil || len(input.Nodes) == 0 {
        return
    }

    // 1. 构建边列表（用于 JSON 输出到 GUI 画连线图）
    edges := make([]Edge, 0)
    for from, targets := range input.Edges {
        for _, to := range targets {
            edges = append(edges, Edge{From: from, To: to})
        }
    }

    // 2. 拓扑排序节点（消除 map 迭代随机性）
    sortedIDs := topologicalSort(input.Entry, input.Edges, input.Nodes)

    // 3. 构建 Children 树（用于 TUI 树形渲染）
    childrenMap := buildChildrenMap(input.Edges)

    // 4. 按拓扑序构造节点列表
    nodes := make([]PlanNode, 0, len(sortedIDs))
    for _, id := range sortedIDs {
        nodes = append(nodes, PlanNode{
            ID:       id,
            Label:    id,
            Status:   NodePending,
            Children: childrenMap[id],
        })
    }

    service.snapshot.Runtime.Plan = &PlanState{
        Name:   input.Entry,
        Status: PlanPending,
        Nodes:  nodes,
        Edges:  edges,    // ← 新增：JSON 序列化后 GUI 可渲染连线
    }
}

// topologicalSort 从 Entry 开始 BFS 拓扑排序
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
        result = append(result, id)  // 后序 → 逆拓扑序
    }
    dfs(entry)
    // 反转得到拓扑序
    for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
        result[i], result[j] = result[j], result[i]
    }
    // 处理不在 edges 图中的孤立节点
    for id := range nodes {
        if !visited[id] {
            result = append(result, id)
        }
    }
    return result
}

// buildChildrenMap 从 edges 邻接表构建 Children 映射
func buildChildrenMap(edges map[string][]string) map[string][]PlanNode {
    children := make(map[string][]PlanNode)
    for from, targets := range edges {
        for _, to := range targets {
            children[from] = append(children[from], PlanNode{
                ID:     to,
                Label:  to,
                Status: NodePending,
            })
        }
    }
    return children
}
```

`clonePlanNodes` 已经递归复制 `Children`，无需修改。

### 3. 状态实时同步验证

**已有代码** — `HandlePlanNodeComplete`（已有的，验证链路正确）：

```
framework scheduler.OnNodeDone(nodeID, kind, status, elapsed)
  → WorkPlanTool.ProgressCallback
    → Service.HandlePlanNodeComplete(nodeID, kind, status, elapsed)
      → plan.Nodes[i].Status = PlanNodeStatus(status)
      → plan.Nodes[i].Elapsed = elapsed.String()
      → recalculate Progress
      → events.Publish(EventSnapshotChanged)
        → TUI: model.snapshot = app.Snapshot() → PlanPanel 重绘
        → GUI: bridge 收到事件 → 前端 re-render
```

需要添加的测试：手动触发 `ProgressCallback` 验证 `EventSnapshotChanged` 是否携带更新后的 PlanState。

### 4. /new 清理 Plan/Workspace/Interaction

**问题代码** — `application/command.go` 当前 `/new`：

```go
// 当前代码：只清 HistoryOffset/TotalMessages，不清理 Runtime
register("new", "新建会话（当前会话自动保存）", func(context.Context, []string) (CommandResult, error) {
    id := service.deps.Engine.SessionID()
    if err := service.deps.Sessions.SaveCurrent(id); err != nil {
        return CommandResult{}, fmt.Errorf("保存会话失败: %w", err)
    }
    newID := service.deps.Engine.StartSession()
    service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
    service.mu.Lock()
    service.snapshot.Session.ID = newID
    service.snapshot.HistoryOffset = 0
    service.snapshot.TotalMessages = 0
    service.snapshot.HasMoreHistory = false
    // ← 遗漏：Runtime.Plan、CurrentWorkspace、Interaction
    service.mu.Unlock()
    service.resetConversation(...)
    return CommandResult{}, nil
})
```

**改动后**：

```go
register("new", ... , func(context.Context, []string) (CommandResult, error) {
    id := service.deps.Engine.SessionID()
    if err := service.deps.Sessions.SaveCurrent(id); err != nil {
        return CommandResult{}, fmt.Errorf("保存会话失败: %w", err)
    }
    newID := service.deps.Engine.StartSession()
    service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
    service.mu.Lock()
    service.snapshot.Session.ID = newID
    service.snapshot.HistoryOffset = 0
    service.snapshot.TotalMessages = 0
    service.snapshot.HasMoreHistory = false
    // ★ 新增：清除旧会话残留状态
    service.snapshot.Runtime.Plan = nil
    service.snapshot.CurrentWorkspace = nil
    service.snapshot.Interaction = nil
    service.mu.Unlock()
    service.resetConversation(...)
    return CommandResult{}, nil
})
```

### 5. resumeSession 清理 Plan/Interaction

**问题代码** — `application/app.go` 当前 `resumeSession`：

```go
func (service *Service) resumeSession(sessionID string) error {
    // ...
    service.mu.Lock()
    service.snapshot.Session.ID = sessionID
    service.snapshot.Conversation = nil
    service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
    service.appendHistoryLocked(visibleHistory)
    // ← 遗漏：Runtime.Plan、Interaction
    service.mu.Unlock()
}
```

**改动后**：

```go
func (service *Service) resumeSession(sessionID string) error {
    // ...
    service.mu.Lock()
    service.snapshot.Session.ID = sessionID
    service.snapshot.Conversation = nil
    // ★ 新增：清除旧会话 Plan 和交互状态
    service.snapshot.Runtime.Plan = nil
    service.snapshot.Interaction = nil
    service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
    service.appendHistoryLocked(visibleHistory)
    // workspace 绑定已在下面恢复
    service.mu.Unlock()
}
```

### 6. Workspace 持久化

**问题代码** — `workspace/workspace.go` 当前 `Repo`：

```go
type Repo struct {
    mu         sync.RWMutex
    workspaces map[string]Info        // 纯内存
    bindings   map[string]string      // 纯内存，重启丢失
}
```

**改动后**：

```go
type Repo struct {
    mu         sync.RWMutex
    workspaces map[string]Info
    bindings   map[string]string
    storePath  string
    saveFn     func(repo *Repo) error
    loadFn     func(repo *Repo) error
}

// NewRepoWithStore 创建带持久化的 Repo
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

// Save 序列化到 JSON 文件
func (r *Repo) Save() error {
    if r.saveFn == nil {
        return nil
    }
    return r.saveFn(r)
}

// Load 从 JSON 文件恢复
func (r *Repo) Load() error {
    if r.loadFn == nil {
        return nil
    }
    return r.loadFn(r)
}

type repoSnapshot struct {
    Workspaces map[string]Info            `json:"workspaces"`
    Bindings   map[string]string          `json:"bindings"`
}

func defaultSave(r *Repo) error {
    r.mu.RLock()
    snap := repoSnapshot{
        Workspaces: r.workspaces,
        Bindings:   r.bindings,
    }
    r.mu.RUnlock()
    data, err := json.MarshalIndent(snap, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(r.storePath, "workspace_index.json"), data, 0644)
}

func defaultLoad(r *Repo) error {
    path := filepath.Join(r.storePath, "workspace_index.json")
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil  // 首次运行，没有文件也算成功
        }
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

`Create` 和 `BindSession` 等修改操作的末尾增加 `r.Save()`：

```go
func (r *Repo) Create(name, rootPath, gitRemote string) (Info, error) {
    // ... 原有代码 ...
    r.workspaces[id] = w
    r.mu.Unlock()
    r.Save()  // 新增：持久化
    return w, nil
}
```

### 7. 会话存储分层

**问题代码** — 当前会话直接存 `sessions/` 目录：

```
.seelex/sessions/
  sess_123456/    ← 平铺，不区分工作区
  sess_123457/
```

**改动后** — 新增 `NestedSessionStore`：

```go
// seelebridge/session_store.go 新增
type NestedSessionStore struct {
    basePath    string                // .seelex/
    stores      map[string]*SessionStore  // workspaceID → SessionStore
    defaultPath string                // 无工作区时用 .seelex/sessions/
}

func NewNestedSessionStore(basePath string) *NestedSessionStore {
    return &NestedSessionStore{
        basePath:    basePath,
        stores:      make(map[string]*SessionStore),
        defaultPath: filepath.Join(basePath, "sessions"),
    }
}

// StoreForWorkspace 获取或创建工作区专属 SessionStore
func (n *NestedSessionStore) StoreForWorkspace(workspaceID string) *SessionStore {
    if workspaceID == "" {
        path := n.defaultPath
        store, _ := NewSessionStore(path)
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
```

`main.go` 初始化调整：

```go
func initStore() *seelebridge.NestedSessionStore {
    return seelebridge.NewNestedSessionStore(filepath.Dir(*storePath))
}

func initSessionManager(store *seelebridge.NestedSessionStore, eng *enginePort) *session.Manager {
    manager := session.NewManager(store)
    manager.InjectSaveLoad(
        func(sessionID, workspaceID string) error {
            s := store.StoreForWorkspace(workspaceID)
            return s.Save(sessionID, eng.rawHistory())
        },
        func(sessionID, workspaceID string) error {
            s := store.StoreForWorkspace(workspaceID)
            history, err := s.Load(sessionID)
            if err != nil {
                return err
            }
            return eng.replaceRawHistory(sessionID, history)
        },
    )
    return manager
}
```

### 8. 会话列表按工作区过滤

`session/manager.go` 补充接口：

```go
// Manager 新增方法
func (m *Manager) ListByWorkspace(workspaceID string) []seelebridge.SessionMeta {
    store := m.store.StoreForWorkspace(workspaceID)
    return store.List()
}
```

`application/ports.go` `SessionPort` 补充：

```go
type SessionPort interface {
    SaveCurrent(string) error
    Delete(string) error
    List() []SessionInfo
    ListByWorkspace(workspaceID string) []SessionInfo  // 新增
    LoadHistory(string) ([]EngineMessage, error)
    LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)
}
```

---

## 兼容性

- `PlanState.Edges` 用 `omitempty`，旧 Plan（无 edges 字段的 JSON）反序列化后 `nil`，不影响渲染
- Workspace 持久化文件 `workspace_index.json` 和现有 `sessions/` 目录共存，互不冲突
- `NestedSessionStore` 优先读工作区路径，无工作区时回退 `sessions/` 目录
- Snapshot/Event DTO、Wails Bridge、TUI Controller 公共签名不变
- 现有 Session schema 不变，只是存储路径分层

## 测试计划

### 单元测试

| 测试 | 文件 | 覆盖 |
|------|------|------|
| `TestTopologicalSort` | `application/chat_test.go` | 线性 DAG、fork、孤立节点、空图 |
| `TestBuildChildrenMap` | `application/chat_test.go` | 多层嵌套、交叉引用 |
| `TestPlanEdgesClone` | `application/application_test.go` | `cloneRuntimeState` 的 Edges 深拷贝 |
| `TestWorkspacePersistence` | `workspace/workspace_test.go` | Create/Delete roundtrip；进程模拟 save→load |
| `TestWorkspaceBindPersistence` | `workspace/workspace_test.go` | BindSession → Save → Load → SessionWorkspace |
| `TestNestedSessionStore` | `seelebridge/session_store_test.go` | 工作区路径、回退路径、并发 |

### 集成测试

| 测试 | 覆盖 |
|------|------|
| `TestPlanLoadFullDAG` | 完整 `plan_load` → Snapshot 含 edges；`plan_run` → 状态打点 |
| `TestNewSessionCleansPlan` | `/new` → Snapshot.Runtime.Plan 为 nil |
| `TestResumeSessionCleansPlan` | `/resume` → 旧 Plan 消失 |
| `TestWorkspaceRestartRecovery` | 创建 workspace → 模拟重启 → List 恢复 |

### 边界测试

| 测试 | 覆盖 |
|------|------|
| 空 Plan (`plan_clear` 后) | PlanState nil → TUI/GUI 不显示 |
| 无 edges 的 plan_load | 单节点、零边 → Edges 为 nil |
| `/new` 时 `Interaction` 活跃 | 审批框消失 |
| 空 Workspace 存储文件 | 首次启动无 `workspace_index.json` |

### 回归测试

```bash
go build ./...
go vet ./...
go test -race -count=1 ./application/...
go test -race -count=1 ./workspace/...
go test -race -count=1 ./seelebridge/...
```
