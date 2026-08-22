# Seelex application/core 模块拆分详设

日期：2026-08-22
状态：设计稿（实现进度见 `plan.md` 执行记录）
范围：`application/core` 内部按域拆分子包 + 共享状态内核 + 接口装配；对外
`application` 门面 API 不变；行为零变化。
前置：`docs/2026-08-22-application-split/plan.md`（P0–P3 已完成）。

## 1. 目标与边界

1. **共享内核 + 域包 + 装配根**：锁、权威 Snapshot、外部端口依赖集中到
   `core/internal/state`；session/task/subagent/context/prompt/view 等域包
   持有各自子状态与逻辑；`serviceAssembler` 是唯一装配根。
2. **接口定义在消费方**：域包声明自己需要的窄接口，实现方在别处；
   装配根注入，形如 `a := newA(b)`，域包之间不互相 import。
3. **依赖单向无环**：`core` 根 → 域包 → `core/internal/state`；
   域包禁止反向依赖 `core` 根包；`core/internal/state` 不依赖任何域包。
4. **行为零变化**：锁语义、Snapshot/Event 协议、持久化格式均不改变；
   拆包是物理归位，不是重写。

## 2. 现状与耦合证据

### 2.1 规模

| 项 | 数值 |
|---|---|
| `application/core` 非测试文件 | 46（拆分 chat/adapters/service_test 前） |
| 行数 | 16,453（拆分前）；`chat.go` 1339、`task_context_state.go` 555、`work_table.go` 512 |
| 测试文件 | 28（`service_test.go` 2220 已拆为 6 文件，52 个测试不变） |

### 2.2 耦合点

- **session ↔ task/plan 状态互读**：`sessionCoordinator.persistCurrentSession` 直接读
  `service.transcript`/`pendingToolResults`/`taskCheckpoints`/`toolResultRefs`/`activePlanID`/
  `planStack`/`taskExecution`，并调用 `taskProjectionLocked`。会话持久化必须看到任务执行的
  权威状态。
- **chat ↔ context/task/session**：`runChat` 依次调用 `prompts.applyActiveTaskSystemPrompt`、
  `context.prepareExecutionContext`、`tasks.ensureFinalAssistantTranscript`、
  `context.takeContextControlFailure`、`sessions.persistCurrentSession`。
- **跨域事务集中在 Service 方法**：`BeginNewSession`/`materializeDraftSession`/
  `bindWorkspaceInfo` 一次复位 session+plan+task+input 状态；`replanFailedWork` 跨
  runtime/session/task。
- **锁与快照是唯一共享事实**：`serviceState.mu` 保护 `snapshot`；所有域方法都通过
  嵌入 `*serviceState` 访问（当前单包内合法，拆包后必须走内核）。

结论：不能按文件直接搬；必须先建立共享内核，再把域状态下沉，跨域读写全部走
消费方接口。

## 3. 目标包结构

```text
application/core/
├─ core.go / service*.go / chat.go / plan_tools.go / tool_hooks.go / work_table.go
│    # 根门面：Service 公共 API + 跨域事务编排 + serviceAssembler
├─ internal/state/            # 共享状态内核（锁 + Snapshot + Deps + Events + Approval）
├─ chat/                      # ✅ 已落地：流式批次 + 可见输出
├─ worktable/                 # ✅ 已落地：工作表格 CSP 汇聚发布器
├─ input_router/              # ✅ 已落地：命令注册表 + 输入路由
├─ context_control/           # ✅ 已落地：窗口策略配置加载
├─ session_runtime/           # 目标：会话持久化/目录/项目绑定协调器
├─ task_context/              # 目标：任务执行/checkpoint/transcript 协调器
├─ subagent_view/             # 目标：子代理 detail/live/tree 投影
├─ context_runtime/           # 目标：上下文控制 + 历史安全协调器
├─ prompt_layer/              # 目标：system prompt 组装协调器
└─ view_state/                # 目标：Snapshot 读/写/发布协调器
```

命名约定：新域包统一 `xxxx_yyyy` 下划线风格；已落地的 `chat`/`worktable` 保留
（避免无谓 churn，后续可选统一）。

## 4. 共享状态内核（core/internal/state）

```go
package state

type Core struct {
    Mu       sync.RWMutex          // 唯一共享锁：保护 Snapshot 与域间一致发布
    Snapshot model.Snapshot        // 权威前端快照（事实源）
    Deps     contract.Dependencies // 外部端口（Engine/Runtime/Sessions/...）
    Events   event.Hub             // 事件发布窄接口
    Approval contract.ApprovalBroker // 异步审批窄接口
}
```

语义约束：

- `Mu` 是**唯一**共享锁。根门面与各域协调器都嵌入 `*state.Core`，任何持锁修改
  Snapshot 的操作必须经同一把锁。
- 持锁时禁止调用外部端口（Engine/Runtime/Sessions/Workspace 的 I/O、LLM、数据库），
  与现有 Review 指南一致；锁外收集投影、锁内应用。
- `Snapshot.Revision` 的 bump 与事件发布保持"锁内 bump → 锁外 Publish"顺序。
- `core/internal/state` 不 import 任何域包；域包都 import 它（叶子）。

### 4.1 字段归属迁移（量化）

| 字段 | 现位置 | 目标 | 非测试引用 | 测试引用 |
|---|---|---:|---:|---:|
| `mu` | `serviceState` | `Core.Mu` | 532 | 286 |
| `snapshot` | `conversationRuntimeState` | `Core.Snapshot` | 330 | 74 |
| `deps` | `infrastructureState` | `Core.Deps` | 235 | 3 |
| `events` | `infrastructureState` | `Core.Events` | 53 | 2 |
| `approval` | `infrastructureState` | `Core.Approval` | 10 | 1 |

改名接收者白名单（只改这些接收者上的字段访问）：

- 改：`service.`（*Service 与各 Coordinator 方法，嵌入 serviceState）、
  `state.`（work_table 的 `*serviceState` 接收者）、`svc.`（tool_hooks 闭包内的 *Service）。
- **不改**：`bridge.mu`（ToolHookBridge 自持锁）、`s.mu`/`s.snapshot`（TaskService 自持状态）、
  `c.mu`（calibratedTokenCounter）、`r.snapshot`（planProjectionReader）、
  `assembler.deps`（serviceAssembler 装配输入）。

编译器是完整检查表：任何误改/漏改都会以 undefined 报出，逐个修正即可。

## 5. 叶子包详设

### 5.1 core/chat（已落地）

- 职能：聊天流式侧的工具层：chunk 批次聚合/背压/落库、`<think>` 推理块剥离。
- 关键类型与属性：
  - `StreamBatcher{pipe *lifecycle.BatchPipeline[string]; store *streamBatchStorage}`
  - `StreamBatcherOptions{FlushSize, Interval, BufferSize, BatchSize, Store}`
  - `VisibleOutputStream{requestID string; inThink bool; pending string}`
- 行为：`NewStreamBatcher`/`OnChunk`/`Flush`/`FlushPending`/`Stats`；
  `NewVisibleOutputStream`/`Consume`/`RequestID`；`StripThoughtBlocks`。
- 依赖：`seelexctx/lifecycle`、标准库。禁止依赖 `core` 根包。
- 非职责：聊天状态机、输入队列、工具事件投影、Plan 打点（仍在根包）。
- 验证：`go test ./application/core/chat -count=1`（节流/并发/背压/幂等）。

### 5.2 core/worktable（已落地）

- 职能：`worktable.changed` 增量事件的中枢投递（CSP latest-wins、有界背压、关闭排空尾态）。
- 关键类型与属性：`WorkTableUpdate{Revision, RequestID, Items []model.WorkItem}`；
  `WorkTablePublisher{updates chan WorkTableUpdate; publish func(WorkTableUpdate); done; once}`。
- 行为：`NewWorkTablePublisher`/`Send`/`Close`。
- 依赖：`application/model`、标准库。
- 非职责：`WorkItem` 行投影构建（根包 `work_table.go`）、todo 三态、task 注册表同步。

### 5.3 core/input_router（已落地）

- 职能：输入分派（command/skill/plugin/conversation）与命令注册表。
- 关键类型与属性：
  - `Command` 接口 / `CommandResult{Notice, Exit, Interaction}` / `CommandFunc{name, description, execute}`
  - `CommandRegistry{commands map[string]Command}`（Register/Get/All）
  - `Router{routes []Route}` + `RouteHandlers{Command, Skill, Plugin, Conversation}`
- 依赖：`application/model`、标准库。路由只持闭包，不持有任何 Service 状态。
- 非职责：内置命令注册（`registerBuiltinCommands` 是根包 Service 方法）、Skill 上下文编解码。

### 5.4 core/context_control（已落地）

- 职能：`seele.yaml` window 配置段加载与窗口策略类型别名。
- 关键类型：`WindowConfig`/`WindowPolicy`/`ProviderContextInfo`/`DefaultWindowPolicy`；
  行为：`LoadWindowConfig`/`DefaultWindowConfig`/`NewDefaultWindowPolicy`。
- 依赖：`seelexctx`。决策公式归属 seelexctx，本包不做实现。

### 5.5 core/internal/state（已建，见 §4）

### 5.6 core/session_runtime（目标）

- 职能：会话域权威逻辑——持久化（SessionRecord v3/transcript/tool-result 原子提交）、
  会话目录与标题恢复、项目 binding、storage 设置、会话三读（record/history/transcript）。
- 关键类型与属性：
  - `Coordinator` 持有 `sessionRuntimeState`：
    `sessionNameMu/sessionTransitionMu`、`sessionNames map[string]sessionNameCacheEntry`、
    `sessionTitle model.SessionTitle`、`sessionCatalogWake/Stop/Done chan struct{}`、`once`。
  - 数据形状：`sessionLocation{workspaceID, workspace *model.WorkspaceInfo, meta model.SessionInfo}`、
    `sessionNameCacheEntry{updatedAt, name}`。
- 行为（自 `session_archive.go`/`session_scope.go`/`session_storage.go`/
  `session_history.go` 协调器方法迁入）：
  `PersistCurrentSession`、`SessionRecordLocked`、`ArchivedConversationMessageLocked`、
  `LoadSessionRecord`、`LoadSessionTranscript`、`SessionCatalog`、`SessionName`、
  `LocateSession`、`LoadSessionHistory`/`Range`、`InvalidateSessionName`、`ClearSessionNames`、
  `SessionStorageConfig`/`TestSessionStorage`/`ConfigureSessionStorage`、
  `PushLoadedPlanLocked`、`RecordReadFileLocked`、`StartCatalogRefresh`/`StopCatalogRefresh` 等。
- 依赖：`state.Core` + `TaskPersistencePort`（§6.1）+ 端口接口
  （`sessionRecordPort`/`scopedSessionPort`/`sessionSnapshotPort`/`sessionTranscriptPort`/
  `sessionContextPort`/`sessionStoragePort`，均为对 `contract.SessionPort` 的可选能力断言）。
- 非职责：跨域事务（BeginNewSession/ResumeSession/BindWorkspace 的编排）仍在根包
  Service；本包只提供原子域操作。
- 验证：根包 `service_test.go`（session 组 23 测试）+ `session_archive_test.go` 迁入本包测试。

### 5.7 core/task_context（目标）

- 职能：任务执行域——`taskExecutionState`、`TaskService`（Plan 打点/终态）、
  transcript（append-only 协议事件）、checkpoint、result-ref、token 审计、
  `calibratedTokenCounter`、`planProjectionReader`。
- 关键类型与属性：
  - `Coordinator` 持有 `taskRuntimeState`：`taskExecution *taskExecutionState`、
    `taskService *TaskService`、`transcript []model.TranscriptEvent`、`transcriptSeq`、
    `pendingProviderCalls`/`pendingToolResults`、`toolResultRefs`、`resultRefsByToolCallID`、
    `taskCheckpoints`、`goalSkillActive atomic.Bool`、`contextControlFailure`、`tokenCounter`。
  - `taskExecutionState{requestID, status, activeSkills, planArguments, ...}`；
    `TaskService` 自持 `mu` + 只读 `snapshot` 引用（`s.mu`/`s.snapshot` 保留小写，是域内状态）。
- 行为：`ActivateTaskSkillsLocked`、`AppendTranscriptEventLocked`、
  `EnsureToolCallTranscriptLocked`、`RecordToolTranscriptLocked`、`EnsureFinalAssistantTranscript`、
  `RecordLLMComplete`、`BuildTaskCheckpointLocked`、`ImportEngineHistoryAsTranscriptLocked`、
  `SyncGoalSkillActiveLocked`、`TaskProjectionLocked`、`ResetForNewSessionLocked` 等。
- 依赖：`state.Core`；对 session/context 的协作经消费方端口（§6.2）。
- 非职责：会话持久化的跨域事务；chat 流式编排。

### 5.8 core/subagent_view（目标）

- 职能：子代理详情（会话记录/上下文/工具事件/worktree 现场）、live 流、树投影。
- 行为：`SubagentDetail`（截断会话 + 上下文快照 + worktree）、
  `SubscribeSubagentLive`、`SubagentTree` 投影与 `HandleSubagentToolEvent` 有界增量。
- 依赖：`state.Core` + `contract.ChatEngine` 的 Node 查询面（只读 actor，安全）。
- 非职责：fork/merge-back 的执行（seelebridge 负责），本包只做投影。

### 5.9 core/context_runtime（目标）

- 职能：上下文装配与控制——`ContextController`（token 预算/压缩/result-ref）、
  `historySafetyCoordinator`（provider 空内容/504 恢复）。
- 关键类型：`contextCoordinator`（嵌入内核 + `collaborators` 窄端口图）与
  `historySafetyCoordinator`；`contextRuntimeState` 与压缩/恢复专用状态。
- 协作端口（沿用现有 `contextCollaborators`，物理搬出）：
  `prompts`（system prompt）、`sessions`（persist）、`view`（bump）、
  `tasks`（checkpoint/compaction/result-ref）、`history`（prepareProviderHistory）。
- 非职责：chat 主循环；本包只做 provider 上下文装配与可恢复中断处理。

### 5.10 core/prompt_layer（目标）

- 职能：system prompt 层组装与引擎同步、Plan/Skill 栈可见性投影。
- 关键类型：`promptCoordinator` + `promptRuntimeState{promptStack, effortManager}`。
- 依赖：`application/prompt`（策略纯逻辑）、`state.Core`。

### 5.11 core/view_state（目标）

- 职能：Snapshot 读/写/事件发布协调器——`viewCoordinator` 的
  `SnapshotView`/`Subscribe`/`AppendMessageLocked`/`BumpLocked`/`ApplyRuntimeProjectionLocked`/
  `CollectRuntimeProjection`/`AddNotice`/`ResetConversation`。
- 依赖：`state.Core`；runtime 投影经 `contract.RuntimePort` 锁外收集。

## 6. 接口与装配设计（核心）

规则：**接口定义在消费方；实现方结构满足即实现（Go 结构化接口）；装配根
`serviceAssembler` 负责注入**。示例：

```go
// package a（消费方）：声明需要的能力
package a
type B interface { Do() }
type A struct { b B }
func NewA(b B) *A { return &A{b: b} }

// package b（实现方）：结构满足即可，无需声明依赖
package b
func (x *Impl) Do() {}

// package c（装配根）：a := NewA(bImpl)
package c
impl := &b.Impl{}
a := a.NewA(impl)
```

### 6.1 session_runtime 的消费方接口

```go
// core/session_runtime/ports.go
package session_runtime

// TaskPersistencePort 是会话持久化对 task/plan 权威状态的读写面。
// Locked 后缀方法要求调用方已持有 state.Core.Mu。
type TaskPersistencePort interface {
    TaskProjectionLocked(sessionID string) *model.TaskContextProjection
    Transcript() []model.TranscriptEvent
    PendingToolResults() []model.StoredToolResult
    TaskCheckpoints() []model.TaskCheckpoint
    ToolResultRefs() []model.ToolResultRef
    ToolResultRefByCallID(callID string) string
    ContinuationSummary(requestID string) string
    ActivePlanID() string
    PlanStack() []model.SessionPlanFrame
    SyncActivePlanFrameLocked(now time.Time)
    PushLoadedPlanLocked(arguments string, now time.Time)
    RemoveCommittedToolResultsLocked(committed []model.StoredToolResult)
}

type Deps struct {
    Core  *state.Core
    Tasks TaskPersistencePort
}

func NewCoordinator(deps Deps) *Coordinator
```

实现方：`task_context.Coordinator`（迁移后）满足该接口；装配根注入。

### 6.2 task_context / context_runtime 的消费方接口

沿用现有窄端口并物理搬出：

```go
// core/context_runtime/ports.go
package context_runtime

type TaskPort interface {
    ActivePlanProjectionLocked() *model.ActivePlanProjection
    BuildTaskCheckpointLocked(*task.TaskExecutionState) model.TaskCheckpoint
    RecordContextCompactionLocked(string, model.ContextCompaction) bool
    StoreToolResultLocked(string, string) model.StoredToolResult
    CountTranscriptEvent(model.TranscriptEvent) int
}
type SessionPort interface {
    PersistCurrentSession(string) error
}
type PromptPort interface {
    SystemPromptForActiveTaskLocked() string
}
type ViewPort interface {
    BumpLocked() uint64
}
```

`task_context` 对 session/prompt/view 的依赖同样用消费方接口表达；chat 编排
（根包）经 `components.tasks`/`components.context`/`components.sessions` 访问，
这些组件的具体类型由装配根注入。

### 6.3 装配根（serviceAssembler）顺序

```go
func (assembler serviceAssembler) assemble() (*Service, error) {
    validateDependencies(assembler.deps)
    kernel := state.New(assembler.deps)          // 1. 内核（锁+Snapshot+Deps+Events+Approval）
    prompts := prompt_layer.NewCoordinator(prompt_layer.Deps{Core: kernel})     // 2. 叶子优先
    tasks   := task_context.NewCoordinator(task_context.Deps{Core: kernel, ...})
    sessions := session_runtime.NewCoordinator(session_runtime.Deps{Core: kernel, Tasks: tasks})
    view    := view_state.NewCoordinator(view_state.Deps{Core: kernel, Sessions: sessions})
    context := context_runtime.NewCoordinator(context_runtime.Deps{
        Core: kernel, Prompts: prompts, Sessions: sessions, View: view, Tasks: tasks,
    })
    chat     := chat_flow.NewCoordinator(chat_flow.Deps{Core: kernel, Tasks: tasks,
        Context: context, Sessions: sessions, View: view, Prompts: prompts})
    input    := input_router.NewRouter(input_router.RouteHandlers{
        Command: service.submitCommand, Skill: service.submitSkill,
        Plugin: service.SwitchPlugin, Conversation: service.submitConversation,
    })
    service := &Service{Core: kernel, components: serviceComponents{
        prompts: prompts, tasks: tasks, sessions: sessions, view: view,
        context: context, input: input,
    }}
    // 装配后处理：注册内置命令、runtime 投影、初始 workspace、目录刷新
    ...
    return service, nil
}
```

依赖方向校验（编译期断言，放各实现方包内）：

```go
var _ session_runtime.TaskPersistencePort = (*task_context.Coordinator)(nil)
var _ context_runtime.TaskPort = (*task_context.Coordinator)(nil)
```

## 7. 状态模型与锁语义

- `kernel.Mu` 是唯一共享锁；各域子状态（`taskRuntimeState`/`sessionRuntimeState`/
  `promptRuntimeState`/`planRuntimeState` 等）从 `serviceState` 迁到各域
  Coordinator 自持，`serviceState` 退化为"内核嵌入 + 生命周期 + 组件图"。
- 锁内只做内存态变更与 Snapshot 一致发布；锁外调用外部端口收集投影
  （`collectRuntimeProjection`/`collectWorkspaceProjection` 模式）。
- `Snapshot.Revision` bump 与事件 Publish 的先后顺序不变
  （锁内 bump → 锁外 Publish）。
- `TaskService` 自持 `s.mu` + 只读 snapshot 引用（`s.mu`/`s.snapshot` 保留小写），
  与内核锁分层：TaskService 是纯状态机，不直接持内核锁。

## 8. 迁移步骤

每步结束必须 `go build ./...` + `go test ./application/... ./internal/adapters/... -count=1`
全绿后再进入下一步；步骤之间不叠加未验证改动。

| Step | 内容 | 风险 | 验证重点 |
|---|---|---|---|
| S0 | 内核落地：`serviceState` 嵌入 `*state.Core`，按 §4.1 白名单改名；`serviceAssembler` 构造内核 | 中（机械改名） | `go test ./application/core/ -count=1` + `-race` |
| S1 | `core/session_runtime`：session 协调器/状态/纯 helper 迁出；`TaskPersistencePort` 定义；task 侧实现导出方法；根包调用点改 `components.sessions.X` | 高（跨域端口语义） | session 组 23 测试 + `session_archive_test.go` 随迁 |
| S2 | `core/task_context`：task 协调器/状态/`TaskService`/`calibratedTokenCounter` 迁出；实现 S1 端口；chat 编排改走端口 | 高 | task/chat 相关测试 + race |
| S3 | `core/subagent_view`/`core/context_runtime`/`core/prompt_layer`/`core/view_state` 逐个下沉 | 中-高 | 对应测试 + race |
| S4 | 收尾：README/`e2e/layout_test.go` 模块清单/AGENTS 模块表同步；`make rebuild-gui` 冒烟（按 MEMORY.md 预警流程） | 低 | 全量 + GUI 构建 |

### S1 详细动作

1. 新建 `core/session_runtime`；从 `session_archive.go`/`session_scope.go`/
   `session_storage.go`/`session_history.go` 搬出 **Coordinator 方法**（Service 方法留在根包）；
   纯 helper（`sessionTitle`/`sessionTitleFromHistory`/`shortSessionID`/
   `preferSessionLocation`/`workspaceID`/`enrichTranscriptMessageIDs`/
   `mergeConversationMessages`/`recordConversation*`/`cloneSessionPlanStack`/
   `recordResumeHistory`/`isInternalConversationMessage`）随包迁出。
2. 端口接口（`sessionRecordPort`/`scopedSessionPort`/`sessionSnapshotPort`/
   `sessionTranscriptPort`/`sessionContextPort`/`sessionStoragePort`）随包迁出；
   根包引用处改 `session_runtime.`。
3. `sessionRuntimeState` 从 `serviceState` 移除；`chat.go` 的
   `service.sessionTitle` 读写改 `components.sessions` 访问器。
4. task 侧新增导出方法满足 `TaskPersistencePort`（对现有逻辑零改动，仅改名导出）。
5. 根包 Service 方法（`BeginNewSession`/`materializeDraftSession`/`resumeSession`/
   `DeleteSession`/`BindWorkspace`/`UnbindWorkspace`/`SessionStorageConfig` 等）
   改经 `components.sessions` 装配调用。
6. `serviceAssembler` 按 §6.3 顺序装配。

### S2 详细动作

1. 新建 `core/task_context`；搬出 `task_context_state.go`/`task_execution.go`/
   `task_service.go`/`token_counter.go` 的协调器与纯类型（`taskExecutionState`/
   `TaskService`/`calibratedTokenCounter`/`planProjectionReader`）。
2. `taskRuntimeState` 从 `serviceState` 移除；`chat.go`/`work_table.go`/
   `session_draft.go` 等直接字段访问改走 `components.tasks` 导出方法。
3. 实现 §6.1/§6.2 端口；根包编译期断言。
4. 测试随迁：`task_service_test.go`/`task_execution_test.go` 白盒部分随包，
   跨域集成测试留在根包。

## 9. 风险与对策

| 风险 | 对策 |
|---|---|
| 机械改名误伤（`bridge.mu`/`s.mu`/`assembler.deps` 等） | §4.1 接收者白名单 + 编译器 undefined 兜底 + 每步全量测试 |
| 跨域端口语义（Locked 方法调用时机） | 接口注释明确锁约定；race 测试覆盖；持锁不得调用外部端口 |
| 拆包后测试失去白盒访问 | 白盒测试随包迁移；黑盒/跨域集成测试留在根包（见 plan.md §4.4 约定） |
| 用户未提交改动（worktable traceboard 相关） | S0/S1 涉及 `work_table.go`/`service_test.go` 时先 diff 确认，保留改动语义 |
| 依赖环 | 域包只依赖 `state.Core` + 消费方接口；装配根在根包；`go list` 检查 |

## 10. 验证矩阵

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./application/... ./internal/adapters/...
go test ./application/... ./internal/adapters/... ./e2e/... -count=1
go test ./application/core/ -race -count=1        # CGO 可用时
git diff --check
```

验收标准：`application` 门面导出符号不变；`serviceState` 不再持有
mu/snapshot/deps/events/approval；session/task 协调器物理位于子包；
`TaskPersistencePort` 编译期断言存在；全量测试全绿。
