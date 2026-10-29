# Host：Application 端口的宿主适配层

## 生态位

`internal/host` 把 Application 声明的窄端口实现到 Seelex Runtime、Engine、Store、
Workspace、Plugin 与 Skill。它位于组合根与领域实现之间，主要调用方是 `boot`；
Application 只看到 `contract` 与领域包定义的消费方接口。

## 职责与非职责

职责：

- 提供 `EnginePort`、`RuntimePort`、`SessionPort`、`WorkspacePort`、
  `PluginPort`、`SkillPort` 等宿主实现。
- 做具体类型到 contract/protocol DTO 的边界转换。
- 维护会话级 Engine 注册、在飞计数、待安装历史和退出释放协调。
- 把账号、审批、压缩、流式、角色权限、SSH、Goal 等应用端口接到唯一领域实现。

非职责：

- 不拥有 Session、Snapshot、Task、Goal 或 Workunit 业务状态机。
- 不实现存储原子写、工具权限判据或 LLM 循环。
- 不向前端暴露具体 Runtime/Store 类型。

## 文件结构

| 文件组 | 职责 |
|---|---|
| `engine_port.go` | 会话级 Engine 路由、历史安装、取消与内存释放。 |
| `runtime_port.go`、`runtime_narrow_ports.go` | Runtime 读面、任务、调度、子代理与策略端口。 |
| `session_manager.go`、`session_records.go` | Session Manager 与记录分页适配。 |
| `session_workspace_ports.go`、`workspace_environment_port.go` | Workspace、Git、环境表与会话 binding。 |
| `role_permission_port.go`、`role_sessions.go` | 角色权限、SSH 路径与角色会话工厂。 |
| `account_admin.go`、`account_window.go` | 账号 CRUD、热注册与窗口策略。 |
| `compaction_index_port.go`、`compressed_turn_store.go` | 上下文压缩与压缩轮次端口。 |
| `conversation_stream.go`、`residency.go` | 流式输出、驻留与释放适配。 |
| `goal_coordinator.go`、`skill_activation.go` | Goal 与 Skill 宿主适配。 |
| `messages.go`、`error_presentation.go` | DTO 转换与错误呈现。 |

## 核心实现

- `EnginePort` 按 `sessionID` 维护独立 Engine；会话切换不再销毁其他会话。
- 历史替换遵循“锁内决定、锁外交接”的序号闸；在飞会话先登记 pending，
  收尾后安装，避免不同会话互相覆盖历史。
- `RuntimePort` 与 `SessionPort` 只做类型转换和窄转发，具体判据保留在 Runtime、
  Store、Kernel 或 Application owner。
- 所有可选能力通过消费方窄接口或编译期断言识别，不以 `if kind/role` 在实现内
  区分层。

## 数据流与生命周期

```text
boot
  -> NewEnginePort / RuntimePort / SessionPort / WorkspacePort ...
  -> Application assembly
  -> Application owner 经 port 调用
  -> host 转换 DTO
  -> Runtime / Store / Workspace / Plugin 唯一实现
```

Engine 与 Session 状态按会话创建；LRU 驱逐、会话卸载、归档和删除必须同步释放
框架 Session、上下文、历史与宿主映射。关闭路径不得留下订阅、锁等待或后台闭包。

## 依赖方向

- 允许依赖 Application 定义的端口、`contract`、`contract/dto`、
  `middle/protocol`、Kernel、Backend、Store 与 `internal/*` 基础设施。
- Application、Kernel、Store、Backend 不得反向 import `internal/host`。
- 适配器只依赖其直接实现所需的端口；禁止新增万能 Deps 或按调用方复制业务判据。

## 并发、存储与错误语义

- 会话 ID 是路由与存储唯一键；显示名称不参与 binding。
- Engine 历史与在飞计数必须同锁读取；网络/LLM 调用不得持锁。
- 历史替换必须保留最后决定序号，防止过期交接覆盖新会话。
- Store/Workspace 调用携带显式 project scope，不得借用当前 active scope。
- 可选能力未装配时返回明确错误或使用已声明的空能力语义，不静默伪造成功。

## 扩展方式

新增 Application 端口时先在消费方定义最小接口，再在本包找现有类型是否能结构满足；
只有无法结构满足时才增加薄适配器。新适配器必须写编译期断言、局部用例和模块 README，
并说明旧面、新面、删除条件和归零验证。

## Review 指南

- `EnginePort` 是否又新增会话级散字段、裸 map 或重复历史判据。
- 是否把业务状态机搬进 host，而不是只转换类型和转发。
- 并发替换是否会覆盖更晚的历史、锁内是否发生 I/O。
- 释放是否同时覆盖 Engine、Session、Context、历史与 Store 映射。
- DTO 转换是否漏字段、复制指针共享或泄露 Seele/Store 具体类型。
- 可选能力失败是否被静默降级。

## 测试与验证

```text
go test ./internal/host -count=1
go test ./application/assembly/{architecture,integration} -count=1
go test ./e2e/ -run 'Boundary|Store|Facade|Session' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### account_admin.go

- `func (admin AccountAdmin) AddAccount(draft dto.AccountDraft) (dto.AccountAddResult, error)` — AddAccount 写入一条新账号并返回生效回执。
- `func (admin AccountAdmin) AccountDetails(name string) (dto.AccountDetails, error)` — AccountDetails 读取账号编辑所需的非秘密字段；APIKey 只报告是否已配置。
- `func (admin AccountAdmin) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)` — UpdateAccount 先改账号档，再把新规格热替换进运行时。热替换失败时账号档已经
- `func (admin AccountAdmin) DeleteAccount(name string) (dto.AccountDetails, error)` — DeleteAccount 先删账号档，再从运行时退租。账号仍在承流时池会拒绝退租，此时
- `func (admin AccountAdmin) hotReplace(name string) error`
- `func accountDetailsDTO(record config.AccountRecord) dto.AccountDetails`
- `func specNamed(specs []model.AccountSpec, name string) (model.AccountSpec, bool)` — specNamed 按名字取加载结果里的规格（名字由加载器推导，是唯一身份）。

### account_window.go

- `func NewAccountWindowPolicy() applicationadmin.AccountWindowPolicy` — NewAccountWindowPolicy exposes the account-window setter through the
- `func (accountWindowPolicy) Set( runtime contract.AccountWindowPort, store contract.AccountWindowStore, name string, window int, ) (int, error)`

### compaction_index_port.go

- `func (port RuntimePort) PushCompactionFrame( ctx context.Context, sessionID string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)` — PushCompactionFrame 实现 context_runtime.CompactionIndexPort：把装配层压缩产出的
- `func (port RuntimePort) ReadbackCompactionSummary( ctx context.Context, sessionID string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)` — ReadbackCompactionSummary 实现 context_runtime 的窄可选探针

### compressed_turn_store.go

- `func NewCompressedTurnStore(target any) applicationreference.CompressedTurnStore` — NewCompressedTurnStore adapts the session manager's explicit-scope commit
- `func (port compressedTurnStore) SaveCompressedTurn(projectID, sessionID, ref, content string) error`

### conversation_stream.go

- `func NewConversationStreamPort() applicationconversation.StreamOutputPort` — NewConversationStreamPort exposes visible-output and batching primitives
- `func (conversationStreamPort) NewBatcher(render func([]string), options applicationconversation.StreamBatcherOptions) applicationconversation.DeltaBatcher`
- `func (conversationStreamPort) NewVisibleOutput(requestID string) applicationconversation.VisibleOutput`
- `func (conversationStreamPort) StripThoughtBlocks(value string) string`

### engine_port.go

- `func (port *EnginePort) ApplyDeps(deps EnginePortDeps)` — ApplyDeps 一次注入 EnginePort 启动期装配（幂等；覆盖旧值）。
- `func queueSessionHistory(engine ReactorEngine, history []types.Message) (bool, error)` — queueSessionHistory 把一次历史替换交给在飞会话的引擎自己排队。返回 false = 该引擎
- `func (port *EnginePort) armHandoffLocked(sessionID string, history []types.Message) historyHandoff` — armHandoffLocked 登记一次待交接。调用方必须持 port.mu；未注入宿主实现时不登记
- `func (port *EnginePort) runHandoff(handoff historyHandoff)` — runHandoff 在 port.mu **之外**把历史交给宿主。已被后续决定取代的那次直接丢弃。
- `func NewEnginePort(eng ReactorEngine, newEngine ReactorEngineFactory, tracer *telemetry.MemoryTracer) *EnginePort`
- `func (port *EnginePort) SessionBacked() bool` — SessionBacked 报告底层 reactor 是否为 session.Session。
- `func (port *EnginePort) SetRoleUserSpacesProvider(provider func(sessionID, role string) []string)` — SetRoleUserSpacesProvider 注入主会话的 SSH 路径范围读面。
- `func (port *EnginePort) withRoleUserSpaces(ctx context.Context, sessionID string) context.Context`
- `func (port *EnginePort) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (port *EnginePort) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 是会话路由的 ChatStream：多会话并行执行时，runChat 用
- `func (port *EnginePort) engineForSessionLocked(sessionID string) ReactorEngine` — engineForSessionLocked 返回指定会话的引擎；未注册会话返回 nil（阶段 0：
- `func (port *EnginePort) HistoryFor(sessionID string) []contract.EngineMessage` — HistoryFor 返回指定会话引擎的历史（只读拷贝）。
- `func (port *EnginePort) RawHistoryFor(sessionID string) []types.Message` — RawHistoryFor 返回指定会话引擎的原始历史（只读拷贝）。
- `func (port *EnginePort) AppendHistoryFor(sessionID string, msg contract.EngineMessage)` — AppendHistoryFor 追加消息到指定会话引擎历史。
- `func (port *EnginePort) ClearHistoryFor(sessionID string)` — ClearHistoryFor 清空指定会话引擎历史。
- `func (port *EnginePort) SetSystemPromptFor(sessionID, prompt string)` — SetSystemPromptFor 设置指定会话引擎的 system prompt。
- `func (port *EnginePort) HasSession(sessionID string) bool` — HasSession 报告目标会话引擎是否已实例化。
- `func (port *EnginePort) ReleaseSessionBundle(sessionID string) bool` — ReleaseSessionBundle 释放指定会话的**桥侧会话本体**（框架 Session + 会话上下文
- `func (port *EnginePort) ReplaceHistoryFor(sessionID string, history []contract.EngineMessage) error` — ReplaceHistoryFor 是会话内历史替换（contract 版）：替换指定会话引擎
- `func (port *EnginePort) ReplaceRawHistoryFor(sessionID string, history []types.Message) error` — ReplaceRawHistoryFor 是 ReplaceHistoryFor 的原始消息版本。
- `func (port *EnginePort) NodeSessionConversation(nodeID string) ([]contract.EngineMessage, bool)` — NodeSessionConversation 转发子代理会话记录查询（节点详情数据面；
- `func (port *EnginePort) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool)` — NodeContextSnapshot 转发子代理结构化上下文查询（详情弹窗"上下文"标签；
- `func (port *EnginePort) NodeToolResult(nodeID, ref string) (string, bool)` — NodeToolResult 转发子代理工具结果读回（ref 带 node:<nodeID>: 前缀；
- `func (port *EnginePort) NodeWorktreeInfoFor(nodeID string) (assemble.NodeWorktreeInfo, bool)` — NodeWorktreeInfoFor 转发节点 worktree 现场查询（失败现场恢复入口；
- `func (port *EnginePort) SubAgentTree() []dto.SubAgentTreeNode` — SubAgentTree 转发 fork 子代理树投影查询（GUI 树视图数据源；内存态
- `func (port *EnginePort) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)` — SubscribeSubagentLive 订阅 node 第一视角实时流（历史回放 + 即时输出面）。
- `func (port *EnginePort) SubagentLiveHistoryPage(nodeID string, offset, limit int) dto.SubagentLiveHistoryPage` — SubagentLiveHistoryPage 转发同一份实时回放窗口的分页读法（翻页入口；窗口上限
- `func (port *EnginePort) NodeStageLogs(nodeID string) []dto.NodeStageLog` — NodeStageLogs 转发 node 第一视角阶段日志历史查询（详情弹窗"第一视角"
- `func (port *EnginePort) AppendHistory(msg contract.EngineMessage)` — AppendHistory 追加消息到引擎内部对话历史。
- `func (port *EnginePort) ClearHistory()` — ClearHistory 清空活跃会话引擎历史。锁纪律同 ClearHistoryFor：port.mu 只解析
- `func (port *EnginePort) ReplaceHistory(sessionID string, history []contract.EngineMessage) error`
- `func (port *EnginePort) ReplaceRawHistory(sessionID string, history []types.Message) error`
- `func (port *EnginePort) replaceRawHistoryLocked(sessionID string, desired []types.Message) (historyHandoff, error)` — replaceRawHistoryLocked 是 ReplaceRawHistory 的锁内段：只动注册表并登记待交接，
- `func (port *EnginePort) replaceRawHistoryFor(sessionID string, history []types.Message) error` — ReplaceHistoryFor 是会话内历史替换（M2 后台并行）：替换指定会话引擎的
- `func (port *EnginePort) replaceRawHistoryForLocked(sessionID string, desired []types.Message) (historyHandoff, error)` — replaceRawHistoryForLocked 的锁内段：只动注册表并登记待交接，宿主调用在锁外
- `func (port *EnginePort) replaceTargetHistoryLocked(sessionID string, history []types.Message)` — replaceTargetHistoryLocked 就地重建目标会话引擎的历史（工厂不可用时的退路）。
- `func installHistoryInPlace(engine ReactorEngine, history []types.Message)` — installHistoryInPlace 用 history 重建 engine 的 provider 历史，保留该会话的引擎
- `func (port *EnginePort) armPendingLocked(sessionID string, history []types.Message)` — armPendingLocked 登记「该会话这次回合收尾之后要装的历史」。调用方必须持 port.mu
- `func (port *EnginePort) installPendingLocked(sessionID string) historyHandoff` — installPendingLocked 在该会话的最后一个在飞回合收尾时兑现登记的历史。调用方必须
- `func (port *EnginePort) activateLocked(sessionID string) bool` — activateLocked 把活跃别名（port.engine / port.sessionID）成对指向目标会话。
- `func (port *EnginePort) installSessionEngineLocked(sessionID string, history []types.Message) historyHandoff` — installSessionEngineLocked 为目标会话安装权威历史：优先创建全新引擎并
- `func canonicalEngineHistory(history []types.Message) []types.Message`
- `func (port *EnginePort) StartSession() string`
- `func (port *EnginePort) ActivateSession(sessionID string) error` — ActivateSession 把活跃引擎切换到目标会话（首次激活按需经工厂创建并
- `func (port *EnginePort) ResumeSession(sessionID string, history []contract.EngineMessage) error` — ResumeSession 为目标会话恢复引擎历史并切换为活跃引擎：会话已有引擎时
- `func (port *EnginePort) ResumeRawSession(sessionID string, history []types.Message) error` — ResumeRawSession 是 ResumeSession 的原始消息版本（框架消息类型）。
- `func (port *EnginePort) resumeRawSessionLocked(sessionID string, desired []types.Message) (historyHandoff, error)` — resumeRawSessionLocked 是 ResumeRawSession 的锁内段：只动注册表并登记待交接，
- `func (port *EnginePort) UnloadSession(sessionID string) error` — UnloadSession 释放指定会话的引擎实例（阶段 2 生命周期：unload 后重开走
- `func (port *EnginePort) SessionID() string`
- `func (port *EnginePort) SetSystemPrompt(prompt string)`
- `func (port *EnginePort) SetSessionMaxLoopsFor(sessionID string, loops int)` — SetSessionMaxLoopsFor 给指定会话钉一个自己的循环上限（application 的
- `func (port *EnginePort) loopsForLocked(sessionID string) int` — loopsForLocked 是"这个会话该用多少轮"的唯一读法：本会话钉了就用钉住的，否则回退
- `func (port *EnginePort) SetMaxLoops(n int)`
- `func (port *EnginePort) TraceText() string`
- `func (port *EnginePort) TokenCount() string`
- `func (port *EnginePort) TokenCountFor(sessionID string) string` — TokenCountFor 返回指定会话的 token 计数（G1/M8）：只累加带该会话
- `func writeSpanSnapshot(builder *strings.Builder, span telemetry.SpanSnapshot, depth int)` — writeSpanSnapshot 递归渲染遥测 span 树（trace 视图文本）。
- `func attrTelemetryInt(attributes telemetry.Attributes, key string) int`
- `func (port *EnginePort) History() []contract.EngineMessage`
- `func (port *EnginePort) RawHistory() []types.Message` — RawHistory 返回**活跃引擎**的原始历史（只读拷贝）。

### error_presentation.go

- `func NewErrorPresentationPort() contract.ErrorPresentationPort` — NewErrorPresentationPort exposes the Middle-owned error classification and
- `func (errorPresentationPort) Wrap(err error, code string) error`
- `func (errorPresentationPort) PresentToolError(toolName string, err error) string`

### goal_coordinator.go

- `func NewGoalCoordinator(runtime contract.RuntimePort) *applicationgoal.Coordinator` — NewGoalCoordinator adapts the optional runtime session-context store into
- `func newGoalCoordinator(provider goalContextStoreProvider) *applicationgoal.Coordinator`
- `func goalStoreFor(provider goalContextStoreProvider, sessionID string) *goaldomain.ContextStateStore`

### messages.go

- `func adaptMessages(messages []types.Message) []contract.EngineMessage`
- `func restoreMessages(messages []contract.EngineMessage) []types.Message`
- `func ApprovalOption(choice string) model.InteractionOption`
- `func ApprovalAccepted(optionID string) bool`
- `func ConvertPermissionOptions(opts []toolspermission.ApproveOption) []model.InteractionOption` — convertPermissionOptions 将 permission.ApproveOption 转为 model.InteractionOption。

### residency.go

- `func NewResidencyPort() applicationsession.ResidencyPort` — NewResidencyPort exposes the Middle residency decisions through the
- `func (residencyPort) Preview(content string, limit int) (string, bool)`
- `func (residencyPort) Touch(order []string, sessionID string) []string`
- `func (residencyPort) Forget(order []string, sessionID string) []string`
- `func (residencyPort) Prune(order []string, loaded func(string) bool, unitIDs []string) []string`
- `func (residencyPort) Budget(legacyLimit, protectedCount, window int) int`
- `func (residencyPort) InputIndexSummaryLimit() int`
- `func (residencyPort) BuildInputIndex( conversation []model.Message, loaded []string, limit int, userText func(model.Message) string, ) []model.SessionInputIndexRow`
- `func (residencyPort) WaitCatalogRefresh(ctx context.Context, refresher applicationsession.CatalogRefresher) error`
- `func (residencyPort) Evict(deps applicationsession.EvictionDeps)`
- `func (residencyPort) Archive(deps applicationsession.ArchiveUseCaseDeps, sessionID string) error`

### role_permission_port.go

- `func (port SessionPort) rolePermissionStore() *storerole.PermissionStore` — rolePermissionStore 返回本端口的登记存储（存储未装配时 nil）。
- `func (port SessionPort) rolePermissionProject(sessionID string) string` — rolePermissionProject 解析会话归属的项目（登记的键是 (项目, 会话, 角色)）。
- `func (port SessionPort) RoleDefaultVectors(sessionID string) (map[string][]string, error)` — RoleDefaultVectors 实现 application/assembly 的 roleDefaultPort：该数据根下所有显式
- `func (port SessionPort) RoleDefault(sessionID, role string) ([]string, bool, error)` — RoleDefault 实现 roleDefaultPort：读单个角色的默认；第二个返回值报告"是否定过"。
- `func (port SessionPort) SetRoleDefault(sessionID, role string, vector []string) error` — SetRoleDefault 实现 roleDefaultPort：写一个角色的默认向量。
- `func (port SessionPort) RoleAccount(sessionID, role string) (string, bool, error)`
- `func (port SessionPort) SetRoleAccount(sessionID, role, account string) error`
- `func (port SessionPort) RoleInheritance(sessionID, role string) (bool, bool, error)`
- `func (port SessionPort) SetRoleInheritance(sessionID, role string, inherit bool) error`
- `func (port SessionPort) RoleSSHUserSpaces(sessionID, role string) ([]string, error)`
- `func (port SessionPort) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)`
- `func (port SessionPort) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error`
- `func (port SessionPort) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error`
- `func (port SessionPort) RolePermissionVectors(sessionID string) (map[string][]string, error)` — RolePermissionVectors 实现 application/assembly 的 rolePermissionPort：返回该会话下
- `func (port SessionPort) RolePermissionVector(sessionID, role string) ([]string, bool, error)` — RolePermissionVector 实现 rolePermissionPort：读单个角色；第二个返回值报告
- `func (port SessionPort) SetRolePermissionVector(sessionID, role string, vector []string) error` — SetRolePermissionVector 实现 rolePermissionPort：写入一个角色的权限向量
- `func (port SessionPort) RolePermissionLayers(sessionID, role string) dto.RoleSettingsLayers` — RolePermissionLayers 实现 application/assembly 的 rolePermissionLayerPort：给出该角色
- `func (port SessionPort) employeePermission(role string) (toolreg.EmployeePermission, bool)` — employeePermission 取该角色在权责表上的装配期分配（未分配 → ok=false）。
- `func grantBits(grant toolspermission.SubjectGrant) map[string]uint8` — grantBits 把框架授权条目投影成"路由组 → 位值"（只取位，资源限定留给权限门）。
- `func permissionVectorTags(bits map[string]uint8) []string` — permissionVectorTags 把"路由组 → 位值"投影成权限向量标签（规范组序，位 0 → `!tag`）。

### role_sessions.go

- `func NewRoleSessionsFactory() func(contract.SessionPort) contract.RoleSessionsPort` — NewRoleSessionsFactory discovers the optional role-session and schedule

### runtime_narrow_ports.go

- `func (port RuntimePort) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView` — TeamworkBoardSnapshot 实现 contract.TeamworkBoardProjection：把某会话的团队看板
- `func (port RuntimePort) SessionContextStoreFor(sessionID string) goaldomain.SessionContextPort` — SessionContextStoreFor 实现 application/assembly 的会话上下文存储取用面：goal 第五栈
- `func (port RuntimePort) SessionBoardsFor(sessionID string) *storeboard.BoundStore`
- `func (port RuntimePort) SetAccountWindow(name string, window int)` — SetAccountWindow 实现 contract.AccountWindowPort：界面「设置 → 账号」设的窗口经这里
- `func (port RuntimePort) SetRoleAccount(role, account string) error` — SetRoleAccount 把角色默认账号推到运行时账号管理器。
- `func (port RuntimePort) AccountWindowFor(name string) int` — AccountWindowFor 返回该账号**生效**的窗口（token；账号级 > 全局 > 账号档）。
- `func (port RuntimePort) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error)` — AccountBalance 实现 contract.AccountBalancePort：界面「设置 → 账户 → 查看余额」的
- `func accountBalanceDTO(name string, balance account.Balance, fetchedAt time.Time) dto.AccountBalance` — accountBalanceDTO 是纯映射（时间由调用方给）：域读数 → 界面契约。
- `func (port RuntimePort) TeammateSessionLive(sessionID string) dto.TeammateSessionLiveView` — TeammateSessionLive 实现 contract.TeammateSessionProjection：**当前 teammate 会话**
- `func (port RuntimePort) TeammateSessionLivePage(sessionID string, offset, limit int) dto.TeammateSessionLiveView` — TeammateSessionLivePage 是同一份读数的**分页读法**（"只有尾巴"改成"有界窗口 +
- `func (port RuntimePort) ViewAgentConversation(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)` — ViewAgentConversation / ChatAgentConversation / RecoverAgentConversation 实现
- `func (port RuntimePort) ChatAgentConversation(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)`
- `func (port RuntimePort) RecoverAgentConversation(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)`
- `func (port RuntimePort) WorkItemExecution(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)` — WorkItemExecution 实现 contract.WorkItemExecutionPort：WorkItem 详情与 teammate
- `func (port RuntimePort) TeammateInspection(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error)` — TeammateInspection 实现 contract.TeammateInspectionPort。
- `func (port RuntimePort) TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord` — TeamworkJobCompletions / TeamworkJobEvents 实现 contract.TeamworkJobCompletion：teammate
- `func (port RuntimePort) TeamworkJobEvents() <-chan struct`
- `func (port RuntimePort) WithPluginAssembly(ctx context.Context, plugins []string) context.Context` — WithPluginAssembly 实现 application/assembly 的「按轮插件装配」探测点（定时任务：
- `func (port RuntimePort) SetSessionReasoningEffort(effort string) int` — SetSessionReasoningEffort 实现 contract.ReasoningEffortPort：把会话档位对应的 wire
- `func (port RuntimePort) TeamworkPlanFor(sessionID string) (storeteamwork.Plan, bool)` — TeamworkPlanFor 实现 application/assembly 的 `TeamworkPlanSource` 窄可选端口

### runtime_port.go

- `func (port RuntimePort) Model() string`
- `func (port RuntimePort) Provider() string`
- `func (port RuntimePort) ContextWindow() int`
- `func (port RuntimePort) MaxOutputTokens() int`
- `func (port RuntimePort) ActivePlugin() string`
- `func (port RuntimePort) FullAccess() bool`
- `func (port RuntimePort) SetFullAccess(on bool)`
- `func (port RuntimePort) SetFullAccessFor(sessionID string, on bool)`
- `func (port RuntimePort) PermissionTier() string`
- `func (port RuntimePort) SetPermissionTierFor(sessionID, tier string) error`
- `func (port RuntimePort) PerSessionExecution() bool`
- `func (port RuntimePort) ForkInFlight(sessionID string) bool`
- `func (port RuntimePort) SetRuntimeVisibilityProjection(projection assemble.RuntimeVisibilityProjection)`
- `func (port RuntimePort) SetParentEvidenceProjection(projection assemble.ParentEvidenceProjection)`
- `func (port RuntimePort) DrainSubagentContexts() []string`
- `func (port RuntimePort) SetSubagentPolicy(policy dto.SubagentPolicy)`
- `func (port RuntimePort) SetSubagentPolicyFor(sessionID string, policy dto.SubagentPolicy)`
- `func (port RuntimePort) BindProjectRoot(rootPath string) error`
- `func (port RuntimePort) UnbindProjectRoot()`
- `func (port RuntimePort) BindProjectRootFor(sessionID, rootPath string) error`
- `func (port RuntimePort) UnbindProjectRootFor(sessionID string)`
- `func (port RuntimePort) SetCurrentTaskBatch(sessionID, batchID string)`
- `func (port RuntimePort) TodoSnapshot() []dto.TodoItem`
- `func (port RuntimePort) SetTodoStatus(index int, status dto.TodoItemStatus) error`
- `func (port RuntimePort) TaskSnapshot() []dto.TaskRecord`
- `func (port RuntimePort) TaskSnapshotFor(sessionID string) []dto.TaskRecord`
- `func (port RuntimePort) ReleaseSessionAsync(sessionID string) int`
- `func (port RuntimePort) AsyncPendingFor(sessionID string) int`
- `func (port RuntimePort) AsyncRunsSnapshot() []dto.AsyncRunRecord`
- `func (port RuntimePort) AsyncOutputTail(handle string, maxBytes int) dto.AsyncOutputTail`
- `func (port RuntimePort) AsyncRunEvents() <-chan struct`
- `func (port RuntimePort) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (port RuntimePort) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (port RuntimePort) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error)`
- `func (port RuntimePort) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error)`
- `func (port RuntimePort) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)`
- `func (port RuntimePort) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)`
- `func (port RuntimePort) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error)`
- `func (port RuntimePort) TaskChangedChannel() <-chan dto.TaskRecord`
- `func (port RuntimePort) SubagentTreeEvents() <-chan struct`
- `func (port RuntimePort) TakeServedRoute(sessionID string) (dto.ServedRoute, bool)`
- `func (port RuntimePort) SwitchSessionTasks(sessionID string, records []dto.TaskRecord)`
- `func (port RuntimePort) SetSessionWorkspace(sessionID, workspaceID string)`
- `func (port RuntimePort) ScheduledCommands() []assemble.ScheduledCommandInfo`
- `func (port RuntimePort) ScheduledTasksSnapshot() []assemble.ScheduledTaskStatus`
- `func (port RuntimePort) ScheduleTask(ctx context.Context, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)`
- `func (port RuntimePort) UpdateScheduledTask(ctx context.Context, id string, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)`
- `func (port RuntimePort) CancelScheduledTask(id string) error`
- `func (port RuntimePort) ClearSubagentTree() error`
- `func (port RuntimePort) RestoreSubagentAnchors(sessionID string) error`
- `func (port RuntimePort) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)`
- `func (port RuntimePort) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)`
- `func (port RuntimePort) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)`
- `func (port RuntimePort) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)`
- `func (port RuntimePort) SetSubagentParentRepairer(fn func(sessionID string) error)`
- `func (port RuntimePort) SearchHistory(ctx context.Context, query string, limit int) (seelexctxsearch.Result, error)`
- `func (port RuntimePort) SelectAccount(name string) bool`
- `func (port RuntimePort) VisibleTools(ctx context.Context) []model.Tool`
- `func (port RuntimePort) Accounts() []model.AccountInfo`

### session_manager.go

- `func NewSessionManager() *SessionManager` — NewSessionManager 构造空装配薄壳（Router 经 WithRouter 装配）。
- `func (m *SessionManager) WithRouter(router *store.Router) *SessionManager` — WithRouter 安装会话存储路由（原子、可配置仓库；生产装配点）。
- `func (m *SessionManager) Router() *store.Router` — Router 返回底层存储路由（context 模块装配等用途；未装配时 nil）。
- `func (m *SessionManager) SetWorkspace(workspaceID string)` — SetWorkspace 设置当前 workspace 写作用域。
- `func (m *SessionManager) Workspace() string` — Workspace 返回当前 workspace 写作用域。
- `func (m *SessionManager) InjectSaveLoad(saveFn, loadFn func(sessionID string) error)` — InjectSaveLoad 注入保存/加载回调（由 boot 装配时传入）。
- `func (m *SessionManager) SaveCommitWorkspace(projectID, sessionID string, commit store.Commit) error` — SaveCommitWorkspace 在显式项目作用域下把一个 commit 追加进会话存储
- `func (m *SessionManager) SaveCurrent(sessionID string) error` — SaveCurrent 持久化当前会话。
- `func (m *SessionManager) Resume(sessionID string) error` — Resume 恢复历史会话。

### session_records.go

- `func NewSessionRecordsPort() applicationconversation.SessionRecordsPort` — NewSessionRecordsPort exposes the shared session-record paginator.
- `func (sessionRecordsPort) PaginateRecords( sessionID string, records []dto.ConversationRecord, request dto.ConversationViewRequest, ) dto.SessionRecordPage`

### session_ssh_state_port.go

- `func sessionSSHBridgeFor(router *store.Router) *sessionSSHBridge` — sessionSSHBridgeFor 取该数据根的共享体（首次调用建实例，其后复用）。
- `func (bridge *sessionSSHBridge) state(projectID, sessionID string) (model.SessionSSHInfo, error)` — state 读会话状态：缓存优先，miss 才回放 JSONL 并回填。
- `func (bridge *sessionSSHBridge) put(projectID string, state model.SessionSSHInfo) error` — put 写会话状态：先落盘（权限状态必须持久），成功后再更新缓存。
- `func (port SessionPort) sessionSSHBridge() *sessionSSHBridge` — sessionSSHBridge 取本端口的共享体（存储未装配时 nil）。
- `func (port SessionPort) SessionSSHState(sessionID string) (model.SessionSSHInfo, error)` — SessionSSHState 实现 application/assembly 的 sessionSSHPort：读会话 SSH 状态。
- `func (port SessionPort) SetSessionSSHState(sessionID string, state model.SessionSSHInfo) error` — SetSessionSSHState 实现 sessionSSHPort：写会话 SSH 状态（整份写入，后写覆盖先写）。
- `func SessionSSHEnvResolver(router *store.Router) func(sessionID, projectID string) string` — SessionSSHEnvResolver 给执行面用的**缺省环境**读面：返回该会话当前限定的环境名

### session_status_record.go

- `func sessionStatusOfRecord(wire dto.SessionRecordStatus) dto.SessionStatus` — session_status_record.go — 会话可见状态在**存储面**上的转换点（全仓唯一一处）。

### session_workspace_ports.go

- `func (port PluginPort) Activate(ctx context.Context, name string) error`
- `func (port PluginPort) Deactivate(ctx context.Context) error`
- `func (port PluginPort) Current() (model.PluginInfo, bool)`
- `func (port PluginPort) All() []model.PluginInfo`
- `func (port PluginPort) pluginInfo(item plugin.Plugin) model.PluginInfo` — pluginInfo 把加载器的 Plugin 投影成应用模型，并补上「从哪来」两个面：
- `func (port WorkspacePort) Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error)`
- `func (port WorkspacePort) Get(id string) (model.WorkspaceInfo, error)`
- `func (port WorkspacePort) List() []model.WorkspaceInfo`
- `func (port WorkspacePort) Delete(id string) error`
- `func (port WorkspacePort) BindSession(sessionID, workspaceID string)`
- `func (port WorkspacePort) UnbindSession(sessionID string)`
- `func (port WorkspacePort) SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool)`
- `func (port WorkspacePort) AllBindings() map[string]string`
- `func (port WorkspacePort) DetectGitRemote(rootPath string) string`
- `func (port WorkspacePort) ListTree(root, relPath string, depth int) (dto.TreeListing, error)` — WorkspaceTreePort 实现：转发给 Repo（工作树只读元数据；root/relPath 的
- `func (port WorkspacePort) CountFiles(root string) (dto.TreeCount, error)`
- `func (port WorkspacePort) GitLog(root string, limit int) (dto.GitLogResult, error)`
- `func (port WorkspacePort) GitChanges(root string, limit int) (dto.WorkspaceChangesResult, error)`
- `func (port WorkspacePort) GitCommitDetail(root, hash string, limit int) (dto.GitCommitDetail, error)`
- `func (port WorkspacePort) GitRepos(root string, limit int) (dto.GitReposResult, error)` — WorkspaceGitReposPort 实现：转发给 Repo（工作区内多仓库发现；BFS 边界与
- `func (port WorkspacePort) ReadFile(root, relPath string, limit int64) (dto.FileContent, error)` — WorkspaceFilePort 实现：转发给 Repo（文件预览读取；root/relPath
- `func (port WorkspacePort) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error)`
- `func (port WorkspacePort) WriteFile(root, relPath string, content []byte) (dto.FileWriteResult, error)` — WorkspaceFileWritePort 实现：转发给 Repo（文件详情编辑保存；可见性边界与
- `func (port WorkspacePort) adapt(item workspace.Info) model.WorkspaceInfo` — adapt 把存储层的工作区记录翻成应用层摘要，并带上**连接配置摘要**。
- `func (port WorkspacePort) WorkspaceConnection(id string) (model.WorkspaceConnection, error)` — WorkspaceConnection 实现 contract.WorkspaceConnectionPort：读连接配置。
- `func (port WorkspacePort) SetWorkspaceConnection(id string, change model.WorkspaceConnectionChange) (model.WorkspaceConnection, error)` — SetWorkspaceConnection 实现 contract.WorkspaceConnectionPort：写连接配置。
- `func adaptConnection(conn workspace.Connection) model.WorkspaceConnection`
- `func workspaceDisplayName(rootPath, fallback string) string`
- `func (port SkillPort) Get(name string) (model.SkillInfo, bool)`
- `func (port SkillPort) All() []model.SkillInfo`
- `func (port SessionPort) SetSessionMeta(sessionID string, meta model.SessionMeta) error` — SetSessionMeta 实现 session.SessionMetaPort：写单个会话的展示元数据到其归属
- `func (port SessionPort) SessionMeta(sessionID string) (model.SessionMeta, error)` — SessionMeta 实现 session.SessionMetaPort：读单个会话的展示元数据。
- `func (port SessionPort) SessionPermissionTier(sessionID string) (string, error)` — SessionPermissionTier 实现 session.SessionSettingPort：读单会话的权限档位设置
- `func (port SessionPort) SetSessionPermissionTier(sessionID, tier string) error` — SetSessionPermissionTier 实现 session.SessionSettingPort：写单会话的权限档位设置
- `func (port SessionPort) applySessionMetas(projectID string, infos []model.SessionInfo) []model.SessionInfo` — applySessionMetas 把项目 blob 里的展示元数据盖到目录枚举结果上。缺 blob 或
- `func adaptSessionMetaToStore(meta model.SessionMeta) store.SessionDisplayMeta`
- `func adaptSessionMetaFromStore(meta store.SessionDisplayMeta) model.SessionMeta`
- `func (port *SessionPort) SetWorkspaceResolver(resolver func(sessionID string) string)` — SetWorkspaceResolver 注入会话绑定项目解析器（boot 装配点）。
- `func (port *SessionPort) SetProjectSource(source func() []string)` — SetProjectSource 注入已知项目列表（boot 装配点）。
- `func (port SessionPort) granular() *store.SessionGranularStore` — granular 返回会话粒度存储入口（Router 为物理布局，暴露层为
- `func (port SessionPort) AttachSessionContext(workspaceID, sessionID string) error` — AttachSessionContext 装配会话 context 模块（system prompt + 四栈）：
- `func (port SessionPort) sessionContextWorkspace(workspaceID, sessionID string) func() string` — sessionContextWorkspace 返回会话 context 落盘用的项目键解析器：
- `func (port SessionPort) DetachSessionContext()` — DetachSessionContext 解绑当前会话的 context 模块（离开会话时调用，
- `func (port SessionPort) SaveCurrent(id string) error`
- `func (port SessionPort) Resume(id string) error`
- `func (port SessionPort) SetWorkspace(workspaceID string)`
- `func (port SessionPort) Workspace() string`
- `func (port SessionPort) Delete(id string) error` — Delete 以会话粒度删除会话（resolve 项目绑定后整键删除）。
- `func (port SessionPort) LoadHistory(id string) ([]contract.EngineMessage, error)`
- `func (port SessionPort) LoadHistoryRange(id string, offset, limit int) ([]contract.EngineMessage, int, error)`
- `func (port SessionPort) MessageCount(id string) (int, error)`
- `func (port SessionPort) SaveSessionTitle(sessionID, title string) error` — SaveSessionTitle / SessionTitle 实现会话标题的持久化读写面
- `func (port SessionPort) SessionTitle(projectID, sessionID string) (string, bool, error)` — SessionTitle 按调用方给出的项目（目录枚举格）只读会话标题；projectID 为空
- `func (port SessionPort) FirstUserInputs(projectID, sessionID string, limit int) ([]string, bool, error)` — FirstUserInputs 实现 session_runtime.FirstUserInputPort：会话最早的若干条
- `func (port SessionPort) List() []model.SessionInfo` — List 返回项目索引下的会话列表（project = 会话集合）。
- `func (port SessionPort) SessionsOf(projectID string) []model.SessionInfo` — SessionsOf 实现 session_runtime.SessionGranularPort：按项目索引枚举会话。
- `func (port SessionPort) SaveSessionRecord(id string, record model.SessionRecord) error`
- `func (port SessionPort) SaveSessionRecordWorkspace(projectID, id string, record model.SessionRecord) error` — SaveSessionRecordWorkspace 在显式项目作用域下写会话 record（物理布局
- `func (port SessionPort) SaveSessionSnapshot( id string, providerHistory []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult, ) error`
- `func (port SessionPort) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]store.Event, error)` — LoadEventRangeWorkspace 按 EventSeq 范围读取事件流（fork 切断点解析用）。
- `func (port SessionPort) LoadToolResultsWorkspace(projectID, sessionID string) ([]store.ToolResult, error)` — LoadToolResultsWorkspace 枚举会话 tool-results 通道全部结果（fork 深拷贝
- `func (port SessionPort) SaveSessionSnapshotWorkspace( projectID, sessionID string, providerHistory []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult, ) error` — SaveSessionSnapshotWorkspace 在显式项目作用域下原子写入会话快照
- `func (port SessionPort) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)` — LoadContextStateWorkspace 读取会话 context 模块（显式项目作用域）。
- `func (port SessionPort) SaveContextStateWorkspace(projectID, sessionID string, state []byte) error` — SaveContextStateWorkspace 保存会话 context 模块（显式项目作用域；fork
- `func (port SessionPort) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)` — CurrentGenerationWorkspace 返回会话当前已发布 generation（fork 血缘
- `func (port SessionPort) LoadTranscriptTailWorkspace(workspaceID, id string, tokenBudget, maxUnits int) ([]model.TranscriptEvent, error)`
- `func (port SessionPort) AssembleWireHistoryWorkspace(projectID, sessionID string, budget, k int) ([]contract.EngineMessage, bool, error)` — AssembleWireHistoryWorkspace 实现 session_runtime.SessionWireAssemblerPort：
- `func (port SessionPort) LifecycleRecoverWorkspace(projectID, sessionID string) (int, bool, error)` — LifecycleRecoverWorkspace 实现 session_runtime.SessionLifecycleRecoverPort。
- `func (port SessionPort) QueueEnqueueWorkspace(projectID, sessionID, requestID, content string) error` — QueueEnqueueWorkspace 把一条排队输入镜像落盘（durable queue 写入侧：
- `func (port SessionPort) QueueMarkConsumedWorkspace(projectID, sessionID, turnID string) error` — QueueMarkConsumedWorkspace 实现 durable queue 的「消费凭据」写入侧：
- `func (port SessionPort) QueueConfirmConsumedWorkspace(projectID, sessionID, turnID string) error` — QueueConfirmConsumedWorkspace 该轮 message 已发布 → 消费项出队。
- `func (port SessionPort) QueueFailConsumedWorkspace(projectID, sessionID, turnID string) error` — QueueFailConsumedWorkspace 该轮未发布（失败）→ 消费项内容回草稿并出队。
- `func (port SessionPort) QueueItemsWorkspace(projectID, sessionID string) ([]dto.QueueItem, bool, error)` — QueueItemsWorkspace 读 lifecycle 队列投影。
- `func (port SessionPort) QueueRecoverItemsWorkspace(projectID, sessionID string) (dto.QueueRecoveryReport, bool, error)` — QueueRecoverItemsWorkspace 重启恢复队列（条目级结果：重发项/出队项/待发项）。
- `func (port SessionPort) PendingMessageTailWorkspace(projectID, sessionID string) (dto.PendingMessageTailReport, bool, error)` — PendingMessageTailWorkspace 探测 message 通道草稿尾部（seq_draft 只读
- `func (port SessionPort) RecoverPendingMessageTailWorkspace(projectID, sessionID string) (dto.PendingMessageTailReport, bool, error)` — RecoverPendingMessageTailWorkspace 显式恢复草稿尾部（基座一致才发布）。
- `func (port SessionPort) DiscardPendingMessageTailWorkspace(projectID, sessionID string) (dto.PendingMessageTailReport, bool, error)` — DiscardPendingMessageTailWorkspace 显式丢弃草稿尾部。
- `func (port SessionPort) AppendDraftSnapshotWorkspace(projectID, sessionID string, events []model.TranscriptEvent) (int, bool, error)` — AppendDraftSnapshotWorkspace 把 transcript 后缀映射成 message 事件行并写入
- `func pendingTailDTO(report store.PendingTailReport) dto.PendingMessageTailReport` — pendingTailDTO 把存储层报告映射成跨层 DTO（正文行本身不跨层：应用侧只需要
- `func (port SessionPort) roleRouter(mainSessionID string) (*store.Router, string, error)`
- `func (port SessionPort) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)`
- `func (port SessionPort) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error`
- `func (port SessionPort) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)`
- `func (port SessionPort) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)`
- `func (port SessionPort) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error`
- `func (port SessionPort) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)`
- `func (port SessionPort) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)`
- `func (port SessionPort) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)`
- `func (port SessionPort) SetLifecycleOrder(sessionID, policy string, roles []string) error`
- `func (port SessionPort) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error`
- `func (port SessionPort) ListRoleSessions(mainSessionID string) ([]string, error)`
- `func (port SessionPort) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (port SessionPort) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (port SessionPort) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error`
- `func storeRoleRows(rows []dto.RoleRow) []store.Event`
- `func roleRows(events []store.Event) []dto.RoleRow`
- `func storeRoleDraftRows(rows []dto.RoleDraftRow) []storerole.DraftRow`
- `func roleDraftRows(rows []storerole.DraftRow) []dto.RoleDraftRow`
- `func storeCompactRef(ref *dto.CompactFrameRef) *store.CompactRef`
- `func compactRef(ref *store.CompactRef) *dto.CompactFrameRef`
- `func roleSnapshot(snapshot store.RoleSnapshot) dto.RoleSnapshot`
- `func roleWireSnapshot(snapshot store.RoleWireSnapshot) dto.RoleWireSnapshot`
- `func (port SessionPort) LoadToolResultWorkspace(workspaceID, id, resultRef string) (model.StoredToolResult, error)`
- `func storeTranscriptEvents(events []model.TranscriptEvent) []store.Event`
- `func adaptTranscriptEvents(events []store.Event) []model.TranscriptEvent`
- `func storeToolResults(results []model.StoredToolResult) []store.ToolResult`
- `func compactionRecordPayloads(record model.SessionRecord) []json.RawMessage` — compactionRecordPayloads 把会话记录里的压缩记录投影成存储通道的原始行。
- `func adaptCompactionRecords(payloads []json.RawMessage) []model.ContextCompaction` — adaptCompactionRecords 把压缩记录通道的原始行还原成应用侧记录。解析不了的行
- `func (port SessionPort) LoadCompactionRecordsWorkspace(workspaceID, id string) ([]model.ContextCompaction, error)` — LoadCompactionRecordsWorkspace 读取会话的持久压缩记录（应用侧可见面事实源）。
- `func (port SessionPort) LoadSessionRecord(id string) (model.SessionRecord, error)`
- `func (port SessionPort) LoadSessionRecordWorkspace(workspaceID, id string) (model.SessionRecord, error)`
- `func (port SessionPort) LoadSessionResumeRecordWorkspace(workspaceID, id string) (model.SessionRecord, int, bool, error)` — LoadSessionResumeRecordWorkspace 是冷加载专用读面：有 compact 指针时只
- `func (port SessionPort) loadProjectedSessionRecord(workspaceID, sessionID string, fromSeq uint64) (model.SessionRecord, int, error)` — loadProjectedSessionRecord 读取原始 message 行，并调用 Kernel Session 的
- `func (port SessionPort) deriveSessionRecord(workspaceID, sessionID string) (model.SessionRecord, error)` — deriveSessionRecord 从该会话自身的事件流构造最小 SessionRecord（S20：
- `func (port SessionPort) LoadConversationRangeWorkspace(workspaceID, id string, offset, limit int) ([]model.Message, int, error)`
- `func decodeSessionRecord(payload []byte, sessionID string) (model.SessionRecord, error)`
- `func migrateSessionArchive(sessionID string, archive model.SessionArchive) model.SessionRecord`
- `func adaptGranularInfos(sessions []store.SessionInfo) []model.SessionInfo`
- `func adaptPlugin(item plugin.Plugin) model.PluginInfo`
- `func adaptSkill(item skill.Skill) model.SkillInfo`
- `func (adapter turnStoreCommitAdapter) WriteArtifacts(_ context.Context, projectID, sessionID string, results []protocol.StoredToolResult) error`
- `func (adapter turnStoreCommitAdapter) WriteMessages(_ context.Context, projectID, sessionID string, events []protocol.TranscriptEvent) error`
- `func (adapter turnStoreCommitAdapter) WriteCompactions(_ context.Context, projectID, sessionID string, records []json.RawMessage) error`
- `func (port SessionPort) saveSessionTurnCommit( projectID, sessionID string, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult, ) error`

### skill_activation.go

- `func NewSkillActivationPort() applicationpolicy.SkillActivationPort` — NewSkillActivationPort exposes the Engine skill activator through the
- `func (skillActivationPort) Activate( name string, skills contract.SkillPort, apply func(model.SkillInfo), ) (model.SkillInfo, error)`

### workspace_environment_port.go

- `func (port WorkspacePort) WorkspaceEnvironments(id string) ([]model.WorkspaceEnvironmentInfo, string, error)` — WorkspaceEnvironments 实现 contract.WorkspaceEnvironmentPort：返回该项目全部环境
- `func (port WorkspacePort) SetWorkspaceEnvironment(id, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error)` — SetWorkspaceEnvironment 实现写面：Delete=true 走删除；否则写入并（可选）设为默认。
- `func (port WorkspacePort) DeleteWorkspaceEnvironment(id, name string) error` — DeleteWorkspaceEnvironment 实现写面：删除环境（连同口令）。
- `func (port WorkspacePort) SetDefaultWorkspaceEnvironment(id, name string) error` — SetDefaultWorkspaceEnvironment 实现写面：指定默认环境（空串 = 取消默认）。
- `func adaptEnvironment(workspaceID string, item workspace.EnvironmentInfo) model.WorkspaceEnvironmentInfo` — adaptEnvironment 把存储层读面翻成跨层读面（字段逐个搬，不做任何推断）。
