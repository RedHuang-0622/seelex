# Runtime 整体重构详细执行设计（R1–R5）

日期：2026-08-14
状态：**已批准，按此执行**（本文档为跨会话执行依据，上下文压缩后先读本文档再动手）
目标：把 `seelebridge` 根包从"巨型组合根单文件"重构为
"骨架组合根 + 纯端口 + 按层装配文件 + 状态下沉 + 生命周期链"的整体形态。
行为约束：公开 API（`application/contract`）零变更；每阶段全绿后提交；全程顺序执行，
禁止并行子代理做紧耦合迁移。

---

## 0. 新会话阅读顺序（先读这些再动手）

1. 本文档（`docs/2026-08-14-runtime-refactor/plan.md`）——执行依据。
2. `seelebridge/runtime.go`：`NewRuntime`（L204-381）→ `Runtime` 结构体（L78-169）→
   `Shutdown`（L492-517）→ 其余方法（见 §2 归属表）。
3. `seelebridge/ports.go`：公开端口 + 内部 actor 方法 + `mcpRegistryAdapter`（R2 要纯化）。
4. 域 README（按需）：`node/` `session/` `plan/` `fork/` `worktree/` `task/`
   `tools/` `account/` `mcp/` `plugin/` `scheduler/`（均有 README，先读 README 再读源码）。
5. `application/contract/ports.go`：RuntimePort 接口（ports.go 编译期断言目标）。
6. 测试基线：`seelebridge/runtime_test.go`、`subagent_audit_test.go`、
   `fork_concurrency_repro_test.go`（理解并发/关停不变量）。

## 1. 目标与验收

- `runtime.go` 骨架 < 15KB；`Runtime` 结构体字段 ≤ 25 且全部为 Manager / 只读快照；
  裸可变状态（`branchMu/selectedAccountID/providerFilter` 等）清零。
- `Shutdown` = 逆装配序统一关停，覆盖全部资源（补 mcp/plan/node/fork/pool）。
- `ports.go` 只含公开端口 + 兼容别名，每方法 ≤ 3 行，加编译期断言。
- 无 forward closure（装配拓扑序）；根包 → 子包单向依赖。
- 每阶段验证门全绿：
  `gofmt -l .` 空、`go build ./...`、`go vet ./...`、
  `go test ./... -count=1 -timeout=120s`、`go test ./e2e/... -run 'Docs|ModuleReadme'`。

## 2. 现状快照（改动基线）

### runtime.go（57KB，111 个顶层声明）

方法按目标文件归属（R1 搬移依据；行号以 2026-08-14 HEAD 为准）：

| 归属文件（目标） | 方法/类型 |
|---|---|
| `runtime.go`（骨架） | `RuntimeConfig`、`Runtime` 结构体、`Tool`/`Account` 类型、`NewRuntime`、`Shutdown`、`MainSessionID`、`SetTurnArchiver`、`Agent`、`Session`、`NewMainSession`、`NewMainSessionWithID`、`AttachHistoryRouter`、`durableHistoryRouter`、`PrepareMainSessionHistory`、`newMainSession`、`SetBashDiagnosticObserver`、`Model`、`Tracer`、`CurrentSession`、`ContextWindow`、`MaxOutputTokens`、`currentAccountLimits`、`BindProjectRoot`、`UnbindProjectRoot`、`summarizeTools` |
| `runtime_plan.go` | `SetPlanNodeCallback`、`PlanNodeEventChannel`、`SetEventPersister`、`SetEventErrorHandler`、`SetPlanApprovalGate`、`currentApprovalGate`、`currentAgentFactory`、`SetPlanBranchBinding`、`SetPlanPolicy`、`RestorePlan`、`agentDispatch`、`currentPlanPolicy`、`ReplanMetrics`、`nodeFactoryDeps`、`nodeFactory`、`currentPlanBranchBinding`、`resolvePlanBranchAccount`、`roleForPlanBranch`、`branchTraceID`、`nodeSessionComponents`、`nodeSessionID`、`stableHash`、`forkDeps`、`registerForkTool`、`forkSubagentsHandler`、`nodeDeps`、`beginNodeWorktree`、`finishNodeWorktree`、`releaseNodeWorktree` |
| `runtime_context.go` | `runtimeBudgetProvider`、`runtimeCompactStacks`、`mainContextComponents`、`nodeContextComponents`、`seelexAssembler`、`relatedMemoryBlocks`、`coverHistoryGap`、`seelexCompressor`、`seelexController`、`windowPolicy`、`windowTailBudget`、`stackBlocks`、`projectBlock`、`resolvePlaceholder`、`sessionContextStore`、`AttachSessionContextStore`、`SetProjectKnowledgeProvider`、`compressionSnapshot`、`compactorInstance` |
| `runtime_account.go` | `accountSelector`、`nodeAccountRequest`、`bridgeAccountCompleter`、`assembleCompleters`、`Accounts`、`SelectAccount`、`accountSpecList`、`Provider`、`SetProvider`、`setSelectedAccount` |
| `runtime_tools.go` | `RegisterTool`、`isProjectScopedTool`、`AllTools`、`VisibleTools`、`SetPermissionConfig`、`SetFullAccess`、`FullAccess`、`RegisterBuiltins`、`registerTodoTools`、`registerTaskTools`、`bashDiagnosticMiddleware`、`seelexVisibilityPolicy`、`isPlanTool`、`nodeScopeExcludedTool`、`BashDiagnosticEvent`/`BashDiagnosticObserver` 别名、`registerProjectScopedTools`、`observeBash`、`taskToolsDeps`、`scopedToolsDeps` |
| `runtime_session.go` | 主会话绑定状态归组（见 R3.3）+ `DrainSubagentContexts` 等内部 actor 方法（R2 从 ports.go 移入） |
| `runtime_deps.go` | `ensureDockerForRuntime`、`dockerProbe` 别名（R1 并入 runtime_tools.go 亦可，二选一，见 §3） |

### ports.go（21KB）

公开端口：task 8 个、PrepareReplan、子代理树 4 个、节点 6 个、MCP 11 个、plugin 5 个、
调度器 8 个、SearchHistory、Todo 2 个。
**待移出（R2）**：`enqueueSubagentContext`、`DrainSubagentContexts`、
`subagentContextDropped`、`mergeBackIntoParent`、`mcpRegistryAdapter`。
保留兼容别名：`MCPServer`/`ScheduledTaskKind`/`ScheduledCommand*`/`ScheduledTaskSpec`/
`ScheduledTaskStatus`/`ScheduledPromptExecutor`/`NodeWorktreeInfo`/
`RuntimeVisibilityProjection`/`ParentEvidenceProjection`/`mainAgentNodeID`。

### Runtime 结构体字段（61 个，L78-169）

- Manager（约 20，不动）：pool/completer/streamer/agt/tracer/hook/mcpManager/
  planExecutor/node/worktreeMgr/forkTool/tasks/scheduler/toolEvents/
  subagentSessions/subagentTree/subagentContext/plugins/permission/filesystem/
  sandbox/projectScope/dockerProbe/registry。
- 只读快照（约 6，不动）：model/defaultAccountID/limits/window/
  toolCallTimeout/approvalTimeout（+ heartbeat/planDecisionTimeout 等）。
- **裸可变状态（R3 处理）**：`branchMu`/`selectedAccountID`/`providerFilter`
  （→ account.Manager）；`visibilityProjection`（atomic，保留但只经 Set 写入）；
  `windowMu/window`（只读快照，可并入 config 快照）；`ctxStoreMu/ctxStore`/
  `historyRouterMu/historyRouter`/`mainHistoryMu/mainHistory`/`projectMu/
  projectKnowledge`/`turnArchiverMu/turnArchiver`/`mainSessionMu/mainSessionID`
  （→ session 绑定归组，R3.3）。

### Shutdown 缺口（L492-517）

现关停：tasks → subagentContext → subagentSessions → worktreeMgr → scheduler → agt。
缺：mcpManager（breaker listener goroutine + MCPStack flush）、planExecutor、
node、forkTool、pool、telemetry。R3.4 补齐。

## 3. 分阶段执行

### R1 物理拆分（纯搬移 + NewRuntime 内联块提取）

1. 按 §2 归属表把方法搬入 7 个 `runtime_*.go`（import 各自收敛）；`runtime.go`
   保留骨架 + `NewRuntime` + `Shutdown`。
2. `NewRuntime` 内联块提取为 stage 函数（放对应 runtime_*.go）：
   - `assembleTelemetry(cfg)`（L228 块）、`assembleMCP(r, cfg)`（L279-280）、
     `assemblePlanExecutor(r, cfg, …)`（L281-303）、`assembleNode(r)`（L304-329）、
     `assembleWorktree(r)`（L330-336）、`assembleFork(r)`（L337）、
     `assembleRegistry(r, cfg, …)`（L343-345）、`assembleAgent(r)`（L346-366）、
     `assemblePlanAgentFactory(r)`（L368-379）。
   - `NewRuntime` 按拓扑序显式调用（此时允许保留 forward closure，R3.5 消除）。
3. 验证门全绿；一个 commit（`refactor(runtime): R1 按层物理拆分`）。

### R2 ports.go 纯化

1. 把 `enqueueSubagentContext`/`DrainSubagentContexts`/`subagentContextDropped`/
   `mergeBackIntoParent` 移入 `runtime_session.go`；`mcpRegistryAdapter` 移入
   `runtime_tools.go`。
2. 加编译期断言：**注意 ports.go 不能 import `application/adapters`（环：adapters
   依赖 seelebridge）**。正确落点二选一：
   - `seelebridge/ports_contract_test.go`（`package seelebridge_test` 外部测试包）：
     `var _ contract.RuntimePort = (*seelebridge.Runtime)(nil)`（外部测试包可同时
     import seelebridge 与 application/contract，测试边无环）；
   - 或 `application/adapters/adapters.go`：
     `var _ contract.RuntimePort = RuntimePort{}`（证明适配器自身满足端口）。
   两者都做亦可；接口名 `application/contract.RuntimePort`（ports.go L62）。
3. 验证门全绿；一个 commit。

### R3 状态下沉 + 生命周期链（改动最深，逐项提交）

#### R3.1 account.Manager（必做）

新增 `seelebridge/account/manager.go`：

```go
type Manager struct {
    mu            sync.RWMutex
    specs         map[string]model.AccountSpec
    limits        map[string]config.AccountLimits
    defaultID     string
    selectedID    string
    providerFilter string
    pool          *accountpool.P2CPool[agent.Completer]
}
func NewManager(specs []model.AccountSpec, limits map[string]config.AccountLimits,
    defaultID string, pool *accountpool.P2CPool[agent.Completer]) *Manager
func (m *Manager) Select(name string) bool
func (m *Manager) SetProvider(provider string)
func (m *Manager) Provider() string
func (m *Manager) Accounts() []Account  // 返回 dto/domain 账号摘要
func (m *Manager) Specs() []model.AccountSpec
func (m *Manager) Limits() config.AccountLimits        // 当前账号限额
func (m *Manager) Selector() func(ctx context.Context, msgs []types.Message, tools []types.Tool) accountpool.AcquireRequest
```

`Selector()` 内含现 `accountSelector`/`nodeAccountRequest` 逻辑（node 解析经闭包
注入：`BranchBinding func() dto.PlanBranchBinding`、`Resolve func(scope) (string, error)`）。
Runtime 字段：删除 `branchMu/selectedAccountID/providerFilter/accountSpecs/
accountLimits`，新增 `accounts *account.Manager`；
`runtime_account.go` 全部方法改为委托（`Provider/SetProvider/SelectAccount/Accounts/...`）。
同步改测试：`runtime_test.go` 中直接读写 `runtime.selectedAccountID` 的用例
（`TestRuntimeAccountsToolsAndPlugins`、`TestSelectAccount` 等）改走公开方法或
`runtime.accounts`。

#### R3.2 tools.Policy（必做）

新增 `seelebridge/tools/policy.go`：

```go
type PolicyDeps struct {
    GoalSkillActive func() bool
    PluginFilter    func([]types.Tool) []types.Tool
}
type Policy struct{ deps PolicyDeps }
func NewPolicy(deps PolicyDeps) *Policy
func (p *Policy) Filter(ctx context.Context, tools []types.Tool) []types.Tool
```

移入 `isPlanTool`/`nodeScopeExcludedTool`（tools 域私有）；`seelexVisibilityPolicy`
改为 `r.visibilityPolicy.Filter`；删除 runtime.go 中对应实现。NodeScope 读取经
`internal/model.NodeScopeFromContextOrEmpty`（tools 已依赖 internal/model）。

#### R3.3 session 绑定状态归组（必做，物理归组）

新建 `runtime_session.go` 内部类型：

```go
type sessionBindings struct {
    mu             sync.RWMutex // ctxStore/historyRouter/mainHistory/project/turnArchiver
    ctxStore       *sessionstore.SessionContextStore
    historyRouter  *sessionstore.Router
    mainHistory    *sessionstore.DurableHistory
    project        func() *sessionstore.ProjectRecord
    turnArchiver   seelexctx.TurnArchiver
    mainSessionMu  sync.RWMutex
    mainSessionID  string
}
```

Runtime 字段收敛为 `bindings sessionBindings`（原 10 字段 + 6 把锁 → 2 字段）。
`runtime_context.go` 的 `sessionContextStore`/`AttachSessionContextStore`/
`SetProjectKnowledgeProvider`/`projectBlock`/`coverHistoryGap`/`seelexController`
与 runtime.go 的 `MainSessionID`/`SetTurnArchiver`/`durableHistoryRouter`/
`AttachHistoryRouter`/`PrepareMainSessionHistory` 改读写 `r.bindings`。
不做语义迁移（是否提升为域包 `session.Manager` 另行评估，改动面超阈值则保持）。

#### R3.4 生命周期链（必做）

1. `mcp.Manager` 新增 `Close()`：记录 Attach 启动的 `ListenBreaker` goroutine
   （`sync.WaitGroup` + `stopCh`/close(ch)），Close 停止监听并等待；若 MCPStack
   带 autosave，Close 时 flush。
2. `node.Coordinator` 新增 `Close()`（清理 `started` map，幂等）。
3. `plan.Executor`/`fork.Tool` 评估后新增 `Close()`（当前无 goroutine → no-op，
   但登记进生命周期，防未来引入资源）。
4. runtime.go 新增：

```go
type closer struct{ close func() }
func (c closer) Close() { c.close() }
```

   或者直接 `lifecycle []func()`；`NewRuntime` 按装配序登记
   （tasks/subagentContext/subagentSessions/scheduler/worktreeMgr/node/forkTool/
   planExecutor/mcpManager/agt…）；`Shutdown` 逆序遍历 + 幂等（每项判 nil）。
5. 删除手写 Shutdown 各分支（或保留 nil 判断，统一走 lifecycle 逆序）。

#### R3.5 NewRuntime 拓扑序 + 消除 forward closure（必做，最深）

1. 调整装配顺序为严格拓扑序：config → telemetry → account → registry →
   completer → agent → plan → node → worktree → fork → session。
2. `plan.ExecutorDeps.LoadPlanDefinition` 从"闭包读 r.registry"改为直接接收
   `*seeltools.RegistryState`（plan 域定义 `LoadPlanDefinition func() (types.Tool, bool)`
   由 runtime_plan.go 用已建 registry 构造，不再依赖 r）。
3. `assembleCompleters` 前移到 registry 之后（依赖 pool/selector，与 registry 无关，
   顺序调整无副作用）。
4. 验证 `runtime.go` 内无 `r.registry` 等"后建字段"前向引用（grep 审计）。

### R4 测试归位

1. `runtime_test.go`（31KB）按域拆为 `runtime_account_test.go`/`runtime_session_test.go`/
   `runtime_plan_test.go`/`runtime_tools_test.go`/`runtime_context_test.go`（纯搬移）。
2. R3 新增单测：`account/manager_test.go`（Select/SetProvider/Limits/Selector）、
   `tools/policy_test.go`（可见性过滤）、`mcp` Close 幂等测试、
   `runtime` Shutdown 逆序 + 幂等测试（重复 Shutdown 不 panic）。
3. 根 README 文件结构表同步（runtime_*.go 行 + 职责），`docs_contract_test` 通过。

### R5 验收

对照 §1 验收清单逐项核对 + 全量验证门 + 更新 README/plan 状态为"已实施"。

## 4. 波及面总表

| 文件 | 阶段 | 改动 |
|---|---|---|
| `seelebridge/runtime.go` | R1/R3 | 拆分搬移；字段收敛；Shutdown 重写 |
| `seelebridge/ports.go` | R2 | 移出 5 个内部方法；加断言 |
| `seelebridge/runtime_{plan,context,account,tools,session,deps}.go` | R1/R3 | 新增文件（方法搬入 + stage 函数） |
| `seelebridge/account/manager.go` + `manager_test.go` | R3.1 | 新增 Manager 类型 |
| `seelebridge/tools/policy.go` + `policy_test.go` | R3.2 | 新增 Policy 类型 |
| `seelebridge/mcp/mcp.go` | R3.4 | 新增 Close（listener 追踪） |
| `seelebridge/node/coordinator.go` | R3.4 | 新增 Close（幂等清理） |
| `seelebridge/plan/executor.go`、`fork/tool.go` | R3.4 | 新增 Close（no-op 登记，视评估） |
| `seelebridge/runtime_test.go` 等 27 个根测试 | R3/R4 | 适配 Manager 化 + 按域拆分 |
| `seelebridge/README.md` | R4 | 文件结构表同步 |

## 5. 风险与禁忌

- R3.5 是唯一高破坏阶段：先做 R3.1-R3.4 绿点，再单独提交 R3.5；若触碰
  plan 测试（`plan_kernel_test`/`plan_executor_test`）适配面过大，拆分小步提交。
- 白盒测试直接读内部字段：Manager 化后同步改测试，禁止留下 `runtime.xxx` 直接
  写裸状态的用例。
- 禁止并行子代理；禁止 `git add -A`（用显式路径，个人文档
  `docs/product/pmstory.md`、`docs/self_judgement.md`、`opt/` 保持 untracked）。
- 文件编辑一律 apply_patch；PowerShell 读写源码必须显式 UTF-8。

## 6. 提交规划

每阶段一个 commit（R3 每小节一个）：

```text
refactor(runtime): R1 按层物理拆分 runtime_*.go
refactor(runtime): R2 ports.go 纯化 + 编译期接口断言
refactor(account): R3.1 account.Manager 收拢账号路由状态
refactor(tools): R3.2 tools.Policy 收拢可见性策略
refactor(runtime): R3.3 session 绑定状态归组
refactor(runtime): R3.4 生命周期链（Closer 登记 + Shutdown 逆序）
refactor(runtime): R3.5 NewRuntime 拓扑序 + 消除 forward closure
refactor(runtime): R4 测试按域归位 + 新增单测
docs(runtime): R5 验收与 README 同步
```
