# application/session/history

## 生态位

`application/session/history` 拥有 Session resume、冷恢复顶层流程、可见历史窗口
安装和恢复后页面接管。主要调用方是 `application/assembly` 的 Session assembly 与
`application/history` 查询适配。

## 职责

- hot/restoring/cold 恢复分支与 restoring shell；
- 冷恢复的 read/history/restore/workspace/activation/task/view/commit/publish 编排；
- durable conversation 分页、可见窗口安装与正文 LRU 触发；
- provider history 到可见消息的适配与可见性判据。

## 非职责

- 不拥有 SessionUnit、View、Engine 或 Store；
- 不复制 Middle history window 算法；
- 不执行压缩、对话主循环或持久化事务；
- 不 import `application/assembly`。

## 文件结构

- `runtime.go`：依赖图、Session unit/event 回调、message adapter 与 public entry；
- `attach_host.go`：把既有 View/Session/Residency/Task/Prompt 机制实现成
  `AttachPreparationPort`、`AttachCommitPort`、`AttachPublishPort`，不复制
  `AttachApplication` 的顺序或判据；
- `history_runtime.go`：resume/cold restore/history 编排；
- `runtime_test.go`：可见性与消息投影 parity。

## 数据流

Resume 请求 → 过渡锁与视图意图 → hot/restoring/cold 分支 → cold 组件链 →
锁内安装 View/激活 Session → 锁外发布 Snapshot/Runtime/Catalog → 内容 LRU touch。

## 依赖方向

允许依赖 `application/session`、`session/{cold,residency,runtime,state,viewstate}`、
`conversation`、`workspace`、`history`、`contract`、kernel state 与 Middle 纯策略。
禁止依赖 `application/assembly`、GUI/TUI 或具体 Store。

## 并发与错误

过渡锁只覆盖视图意图和提交，磁盘读/Engine 恢复在锁外；发布在锁外完成。
恢复失败保持 notice/close/fallback 语义；查询窗口不使用共享写锁。

## 扩展与 Review

新增恢复分支必须先定义状态所有权和失效条件。Review 重点：锁是否跨 I/O、
epoch 是否仍按会话复判、可见/未过滤行数是否混用、恢复失败是否留下半装状态。

## 测试与验证

```text
go test ./application/session/history -count=1
go test ./application/assembly -run 'Session|History|Cold|Resume|Restore|Fork' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### attach_host.go

- `func (host attachHost) EnsureContent(sessionID string) error`
- `func (host attachHost) TargetRunning(sessionID string) bool`
- `func (host attachHost) Workspace(sessionID string) (*model.WorkspaceInfo, bool)`
- `func (host attachHost) BindProjectRootIfSafe(sessionID, rootPath string) bool`
- `func (host attachHost) SetWorkspaceWriteScope(workspaceID string)`
- `func (host attachHost) SetRuntimeWorkspace(sessionID, workspaceID string)`
- `func (host attachHost) SwitchTasks(sessionID string)`
- `func (host attachHost) RestoreSubagentAnchors(sessionID string)`
- `func (host attachHost) WorkspaceProjection() (switchworkspace.Projection, bool)`
- `func (host attachHost) StoredPermissionTier(sessionID string) string`
- `func (host attachHost) SessionTitle(sessionID string) string`
- `func (host attachHost) SetActive(sessionID string)`
- `func (host attachHost) Unit(sessionID string) *sessionstate.SessionUnit`
- `func (host attachHost) ApplyStoredPermissionTier(sessionID, tier string)`
- `func (host attachHost) SetSessionChat(sessionID string, chat model.ChatState)`
- `func (host attachHost) MirrorActiveView()`
- `func (host attachHost) VisibleTask(sessionID string) *model.TaskState`
- `func (host attachHost) ActivePlan(sessionID string) *model.PlanState`
- `func (host attachHost) MirrorPlan(sessionID string, plan *model.PlanState)`
- `func (host attachHost) MirrorApprovals(sessionID string)`
- `func (host attachHost) ApplyWorkspace(workspace *model.WorkspaceInfo, projection switchworkspace.Projection)`
- `func (host attachHost) SystemPrompt() string`
- `func (host attachHost) PromptRoutesBySession() bool`
- `func (host attachHost) BumpRevision() uint64`
- `func (host attachHost) WriteSessionPrompt(sessionID string)`
- `func (host attachHost) WriteGlobalPrompt(prompt string)`
- `func (host attachHost) RefreshRuntime(sessionID string)`
- `func (host attachHost) PublishSnapshot(sessionID string, revision uint64)`
- `func (host attachHost) PublishRuntimeProjections()`
- `func (host attachHost) RefreshCatalog()`
- `func (host attachHost) TouchResident(sessionID string)`
- `func (host attachHost) TouchContent(sessionID string)`

### history_runtime.go

- `func (service *Runtime) beginAsyncRestore(sessionID string) (uint64, error)` — resumeSession 是会话切换的应用边界：目标已驻留（含运行中）热加载；目标
- `func (service *Runtime) pointViewAtRestoringShell(sessionID string) bool` — pointViewAtRestoringShell 把视图重指到一个"装载在途"会话的空壳上（幂等，
- `func (service *Runtime) resumeSessionColdInBackground(sessionID, previousID string, epoch uint64)` — resumeSessionColdInBackground 后台执行冷加载：完成/失败后按 epoch 判定
- `func (service *Runtime) resumeSessionCold(sessionID string, activateEpoch uint64) error` — handleColdRestoreFailure 后台冷加载失败的降级：用户若仍停留在失败的恢复
- `func (service *Runtime) attachSessionContextFor(workspaceID, sessionID string) error` — attachSessionContextFor 把目标会话的 context 模块（system prompt + 四栈）挂到
- `func (service *Runtime) reloadSessionContent(sessionID string, assembler applicationhistory.WindowAssembler) error` — reloadSessionContent reloads an evicted visible window from durable pages.
- `func (service *Runtime) visibleTailServedFromMemory(sessionID string, assembler applicationhistory.WindowAssembler) bool` — LoadMoreHistory 把更早的一页历史前置到可见会话（GUI 顶部 sentinel 与
- `func (service *Runtime) loadConversationTailPage(workspaceID, sessionID string, window int) (applicationhistory.Page, error)` — loadConversationTailPage 读发布点处的尾部窗口：先探总数（未过滤空间，只用来
- `func (service *Runtime) loadEarlierVisiblePage( workspaceID, sessionID string, offset, limit, window int, seam model.Message, assembler applicationhistory.WindowAssembler, ) (applicationhistory.Page, error)` — loadEarlierVisiblePage 读可见窗口之前的一页历史（「加载更早」的冷读面）。
- `func currentWorkspaceIDLocked(service *Runtime) string` — applicationhistory.Page 是一段冷读结果，**两个坐标空间分开记**：
- `func (service *Runtime) loadConversationPage(workspaceID, sessionID string, offset, limit int) (applicationhistory.Page, error)` — loadConversationPage 读回一段历史：record conversation 模块优先（长会话翻页不
- `func (service *Runtime) installVisibleHistory( sessionID string, page applicationhistory.Page, window int, mode applicationhistory.InstallMode, assembler applicationhistory.WindowAssembler, ) error` — installVisibleHistory 安装一页可见历史：写会话可见投影（事实源）→ 收敛
- `func (service *Runtime) visibleWindowFirstRow(sessionID string, assembler applicationhistory.WindowAssembler) model.Message` — visibleWindowFirstRow 返回目标会话可见窗口的首条 durable 行——前置一页要接在它
- `func (service *Runtime) adaptEngineMessage(msg contract.EngineMessage) model.Message`
- `func adaptEngineMessage(msg contract.EngineMessage, stripThoughtBlocks func(string) string, display func(string) string) model.Message`
- `func isVisibleHistoryMessage(message contract.EngineMessage, display func(string) string) bool`
- `func (service *Runtime) AdaptEngineMessage(msg contract.EngineMessage) model.Message` — AdaptEngineMessage exposes the history message projection to core tests and
- `func (service *Runtime) IsVisibleHistoryMessage(message contract.EngineMessage) bool` — IsVisibleHistoryMessage exposes the history visibility predicate.

### runtime.go

- `func NewRuntime(deps RuntimeDeps) *Runtime` — NewRuntime constructs the session history runtime.
- `func (runtime *Runtime) closed() bool`
- `func (runtime *Runtime) sessionUnitLocked(sessionID string) *sessionstate.SessionUnit`
- `func (runtime *Runtime) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event`
- `func (runtime *Runtime) readStoredPermissionTier(sessionID string) string`
- `func (runtime *Runtime) displayUserInput(input string) string`
- `func (runtime *Runtime) ResetTurnState(unit *sessionstate.SessionUnit)`
- `func (runtime *Runtime) AttachSession(sessionID string, snapshot *protocol.Snapshot) (uint64, error)` — AttachSession points the shared snapshot at one resident session.
- `func (runtime *Runtime) PlanLocked(sessionID string) *protocol.PlanState` — PlanLocked returns the Plan projection selected by the snapshot owner.
- `func (runtime *Runtime) MirrorPlanLocked(sessionID string)` — MirrorPlanLocked refreshes and seeds the active Plan projection.
- `func (runtime *Runtime) MirrorPlanWithFallbackLocked( sessionID string, fallback func() *protocol.PlanState, )` — MirrorPlanWithFallbackLocked refreshes a Plan using a caller-owned baseline.
- `func (runtime *Runtime) BeginAsyncRestore(sessionID string) (uint64, error)`
- `func (runtime *Runtime) PointViewAtRestoringShell(sessionID string) bool`
- `func (runtime *Runtime) ResumeSessionColdInBackground(sessionID, previousID string, epoch uint64)`
- `func (runtime *Runtime) ResumeSessionCold(sessionID string, epoch uint64) error`
- `func (runtime *Runtime) AttachSessionContextFor(workspaceID, sessionID string) error`
- `func (runtime *Runtime) VisibleTailServedFromMemory(sessionID string, assembler applicationhistory.WindowAssembler) bool`
- `func (runtime *Runtime) LoadConversationTailPage(workspaceID, sessionID string, window int) (applicationhistory.Page, error)`
- `func (runtime *Runtime) LoadEarlierVisiblePage(workspaceID, sessionID string, offset, limit, window int, seam protocol.Message, assembler applicationhistory.WindowAssembler) (applicationhistory.Page, error)`
- `func (runtime *Runtime) InstallVisibleHistory(sessionID string, page applicationhistory.Page, window int, mode applicationhistory.InstallMode, assembler applicationhistory.WindowAssembler) error`
- `func (runtime *Runtime) ReloadContent(sessionID string, assembler applicationhistory.WindowAssembler) error` — ReloadContent replaces an evicted visible window with a fresh durable tail.
- `func (runtime *Runtime) VisibleWindowFirstRow(sessionID string, assembler applicationhistory.WindowAssembler) protocol.Message`
- `func (runtime *Runtime) CurrentWorkspaceIDLocked() string`

### runtime_test.go

- `func TestRuntimeHistoryVisibilityAndMessageProjection(t *testing.T)`
- `func hidingDisplay(value string) string` — hidingDisplay 是「用户输入归一化后为空」的显示面：它只负责把内部信封折叠成
- `func TestAdaptEngineMessageAppliesDisplayOnlyToUserRows(t *testing.T)` — TestAdaptEngineMessageAppliesDisplayOnlyToUserRows 钉住 adaptEngineMessage 的
- `func TestAdaptEngineMessageToleratesNilStripThoughtBlocks(t *testing.T)` — TestAdaptEngineMessageToleratesNilStripThoughtBlocks 钉住 stripThoughtBlocks 的
- `func TestAdaptEngineMessageClearsProviderOnlyContent(t *testing.T)` — TestAdaptEngineMessageClearsProviderOnlyContent 钉住 provider-only 修复文本不得
- `func TestAdaptEngineMessageMapsToolCalls(t *testing.T)` — TestAdaptEngineMessageMapsToolCalls 钉住 tool_calls 的投影：引擎历史的每次
- `func TestIsVisibleHistoryMessageJudgesDisplayedUserContent(t *testing.T)` — TestIsVisibleHistoryMessageJudgesDisplayedUserContent 钉住可见性判据：system 恒
- `func TestRuntimeHistorySemanticsReachExportedEntry(t *testing.T)` — TestRuntimeHistorySemanticsReachExportedEntry 是装配面的一致性：Runtime 的导出
