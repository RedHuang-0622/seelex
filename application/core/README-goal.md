# core/goal（根包分卷）

## 生态位

goal 域协调器/门面用例与「goal 上线不覆盖会话团队」接线回归

覆盖：`goal*.go`；未归属文件由覆盖自检拦下。

> **2026-10-03（阶段三 W3）**：goal 的**席位轮转已整条退场**。回合尾不再自动跑
> "exec 让位 → advisor 评审"的座位循环（`govern` 包、`Adapter`/`NewAdvisorSeat`、
> 座位作业面 `jobs.KindSeat`、headless `goal_gov_*`、视图的 Round/座次/断环 一并删除）。
> goal 的驱动改为**提示词驱动的 leader 派活**（见 `plugins/default/goal/SKILL.md`），
> 终态判定只在显式入口：`goal_propose_finish` 的终态 gate 与审批预筛。保留不变的是
> `Controller` + `Supervisor` + 终态 gate + 逃生。详见
> [`docs/devlog/2026-10-03-seat-rotation-retired.md`](../../docs/devlog/2026-10-03-seat-rotation-retired.md)。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### goal_coordinator.go

- `func newGoalCoordinator(deps goalCoordinatorDeps) *goalCoordinator`
- `func (g *goalCoordinator) bundleFor(sessionID string) *goalSessionRuntime` — bundleFor 返回（需要时创建）指定会话的 goal bundle。创建时若装配了会话
- `func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — Begin 注册并压栈（会话路由）。
- `func (g *goalCoordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — Update 更新栈顶（会话路由）。
- `func (g *goalCoordinator) ProposeFinish(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)` — ProposeFinish 送终态 gate（TL 缺席时 OutcomeNoTL 直连收口；B4）。
- `func (g *goalCoordinator) Notify(ctx context.Context, sessionID string, signal goaldomain.TLEvalSignal) error` — Notify 登记 a 事件（exec 账本；触发策略见 Supervisor）。
- `func (g *goalCoordinator) AdvanceAfterChat(ctx context.Context, sessionID, detail string) error` — AdvanceAfterChat 在 ChatStream 返回后的回合边界安全点推进一次 goal 收尾记账：
- `func (g *goalCoordinator) teamRuntimeFor(sessionID string) *agentteam.Runtime` — teamRuntimeFor 取该会话的团队发言调度运行态（未装配团队环 → nil）。它**只**
- `func (g *goalCoordinator) setEvaluator(evaluator goaldomain.TLEvaluator)` — setEvaluator 装配/替换 TL 评估器：更新后续会话 bundle 构造输入，并为已
- `func (g *goalCoordinator) StatusFor(sessionID string) goaldomain.StatusView` — StatusFor 返回会话 goal 栈全量视图（无 bundle 时返回空视图）。
- `func (g *goalCoordinator) DrainDirectives(sessionID string) []goaldomain.TLDirective` — DrainDirectives 排空该会话 b→a 指令队列（ChatStream 回合边界注入）。
- `func (g *goalCoordinator) PeekDirectives(sessionID string) []goaldomain.TLDirective` — PeekDirectives 读取该会话待注入的 b→a 指令（不消费）：回合结束时把刚产出的
- `func goalStackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView` — goalStackFrames 把 Controller 的活动栈投影成逐帧只读视图（栈底→栈顶，末元素
- `func goalStepViews(steps []goaldomain.TLStep) []dto.GoalStepView` — goalStepViews 把 goal 域的评审过程步骤投影成只读 DTO（nil 进 → nil 出，
- `func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal → nil，前端隐藏）。

### goal_coordinator_test.go

- `func TestGoalCoordinatorSessionIsolation(t *testing.T)` — TestGoalCoordinatorSessionIsolation 验证 P1 会话级协调器：两会话各自
- `func TestSessionRuntimeCarriesGoalGovernance(t *testing.T)` — TestSessionRuntimeCarriesGoalGovernance 验证 GoalGovernanceView 进入
- `func (e *stubTLEvaluator) Evaluate(context.Context, goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`

### goal_directive_not_in_conversation_test.go

- `func TestAdvisorDirectiveNeverEntersVisibleConversation(t *testing.T)`

### goal_directive_session_lock_test.go

- `func newSessionLockEngine() *sessionLockEngine`
- `func (engine *sessionLockEngine) SessionBacked() bool` — SessionBacked 报告"新 Session 装配"：OnIterationComplete 在会话锁内同步执行。
- `func (engine *sessionLockEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *sessionLockEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *sessionLockEngine) History() []EngineMessage`
- `func (engine *sessionLockEngine) HistoryFor(string) []EngineMessage`
- `func (engine *sessionLockEngine) AppendHistory(msg types.Message)`
- `func (engine *sessionLockEngine) AppendHistoryFor(_ string, msg types.Message)`
- `func (engine *sessionLockEngine) ClearHistory()`
- `func (engine *sessionLockEngine) ClearHistoryFor(string)`
- `func (engine *sessionLockEngine) ReplaceHistory(sessionID string, history []EngineMessage) error`
- `func (engine *sessionLockEngine) ReplaceHistoryFor(sessionID string, history []EngineMessage) error`
- `func (engine *sessionLockEngine) SetSystemPrompt(string)`
- `func (engine *sessionLockEngine) SetSystemPromptFor(string, string)`
- `func TestQueuedRoundMustNotReenterSessionLock(t *testing.T)`

### goal_governance_steps_test.go

- `func (e *stubStepEvaluator) Evaluate(ctx context.Context, _ goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`
- `func TestGoalGovernanceViewCarriesRoundSteps(t *testing.T)`
- `func TestGoalGovernanceViewWithoutStepsHasNoEmptyShell(t *testing.T)` — TestGoalGovernanceViewWithoutStepsHasNoEmptyShell：没有步骤时不产生空壳

### goal_permission_test.go

- `func TestAgentGoalUpdateCannotRewriteDefinition(t *testing.T)` — TestAgentGoalUpdateCannotRewriteDefinition 钉住 agent 工具面的收口与人类面的保留。

### goal_ring_escape_test.go

- `func (s *escapeRecordingSessions) AppendRoleDraft(_ string, _ string, _ string, rows []dto.RoleDraftRow) error`
- `func (s *escapeRecordingSessions) draftsOfKind(kind string) []dto.RoleDraftRow`
- `func TestRingEscapeClosesGoalAndArchivesTLHistory(t *testing.T)` — TestRingEscapeClosesGoalAndArchivesTLHistory：逃生后 goal 必须收口，且 tl 角色
- `func TestRingEscapeWithoutGoalIsQuiet(t *testing.T)` — TestRingEscapeWithoutGoalIsQuiet：没有 goal 时环逃生不应报错、不应写归档

### goal_service.go

- `func (service *Service) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 返回指定会话的 goal 治理只读视图（view_state 装配
- `func (service *Service) goalCoordinatorFor(sessionID string) (*goalCoordinator, error)`
- `func (service *Service) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — GoalBeginFor 按显式会话注册 goal（多会话路由）。
- `func (service *Service) teamJoinSeqFor(sessionID string) uint64` — teamJoinSeqFor 返回团队装配的 join 切点 = 主会话当前已提交的 message 尾 seq。
- `func (service *Service) GoalBegin(ctx context.Context, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — GoalBegin 按执行 ctx 会话注册 goal（main agent 工具调用路径）。
- `func (service *Service) GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — GoalUpdateFor 按显式会话更新栈顶 goal。
- `func (service *Service) GoalUpdate(ctx context.Context, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — GoalUpdate 按执行 ctx 会话更新（main agent 工具调用路径）。
- `func (service *Service) GoalProposeFinishFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)` — GoalProposeFinishFor 按显式会话送终态 gate。
- `func (service *Service) GoalProposeFinish(ctx context.Context, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)` — GoalProposeFinish 按执行 ctx 会话提议收口（main agent 工具调用路径）。
- `func (service *Service) GoalStatusFor(sessionID string) (goaldomain.StatusView, error)` — GoalStatusFor 按显式会话返回 goal 栈全量视图。
- `func (service *Service) SetGoalTLEvaluator(evaluator goaldomain.TLEvaluator)` — SetGoalTLEvaluator 注入真实 TL 评估器（组合根：seelebridge 账号面 →
- `func (service *Service) refreshGoalRuntimeProjection(sessionID string)` — refreshGoalRuntimeProjection 在 goal 状态迁移后刷新目标会话的 runtime
- `func (service *Service) GoalIterationCompleted(ctx context.Context) bool` — GoalIterationCompleted 是 ChatStream OnIterationComplete 的 goal 接线：
- `func formatDirectiveText(directive goaldomain.TLDirective) string` — formatDirectiveText 是 b→a 指令的**单行可读形式**：受信注入是它唯一的落地形式
- `func (service *Service) injectGoalDirectives(sessionID string, directives []goaldomain.TLDirective)` — injectGoalDirectives 把 b→a 指令注入引擎受信区——这是 ADVISOR 与 EXEC 之间
- `func (service *Service) injectGoalDirectivesForStart(sessionID string)` — injectGoalDirectivesForStart 在 ChatStream 开始前把 TL 回合产生的指令
- `func (service *Service) goalAdvanceAfterChat(ctx context.Context)` — goalAdvanceAfterChat 在 ChatStream 返回后的锁外安全点做一次 goal 收尾记账
- `func (service *Service) dismissTeamWhenGoalClosed(sessionID string)` — dismissTeamWhenGoalClosed 让"干完就走人"成立：目标收口（栈里没有 active goal）
- `func (service *Service) goalBeginHandler(ctx context.Context, argsJSON string) (string, error)` — goalBeginHandler 是 goal_begin 工具 handler（main.go 注册）。
- `func authorizeAgentGoalMutation(request goaldomain.UpdateRequest) error` — authorizeAgentGoalMutation 判定"agent 工具面（EXEC / 员工 / 子代理）"是否可以做这次
- `func (service *Service) goalUpdateHandler(ctx context.Context, argsJSON string) (string, error)` — goalUpdateHandler 是 goal_update 工具 handler（agent 工具面：权限收口见
- `func (service *Service) goalProposeFinishHandler(ctx context.Context, argsJSON string) (string, error)` — goalProposeFinishHandler 是 goal_propose_finish 工具 handler。
- `func (service *Service) goalStatusHandler(ctx context.Context, _ string) (string, error)` — goalStatusHandler 是 goal_status 工具 handler。
- `func marshalGoalResult(value any) (string, error)`
- `func (service *Service) GoalBeginHandler(ctx context.Context, argsJSON string) (string, error)` — GoalBeginHandler / GoalUpdateHandler / GoalStatusHandler /
- `func (service *Service) GoalUpdateHandler(ctx context.Context, argsJSON string) (string, error)`
- `func (service *Service) GoalStatusHandler(ctx context.Context, argsJSON string) (string, error)`
- `func (service *Service) GoalProposeFinishHandler(ctx context.Context, argsJSON string) (string, error)`

### goal_stack_view_test.go

- `func TestGoalStackFramesProjectsEveryFrame(t *testing.T)`
- `func TestGoalStackFramesCarriesDetailFields(t *testing.T)` — TestGoalStackFramesCarriesDetailFields 钉住"点开看详情"需要的字段不缺：
- `func TestGoalStackFramesEmptyAndNilRecords(t *testing.T)`

### goal_team_recorder.go

- `func (service *Service) goalTLRecorderFor(sessionID string) goaldomain.TLRoundRecorder` — goalTLRecorderFor 是装配根注入的按会话记录器工厂。
- `func (r goalTLRecorder) RecordTLRound(_ context.Context, record goaldomain.TLRoundRecord) error`
- `func (r goalTLRecorder) RecordMainTurn(_ context.Context, record goaldomain.MainTurnRecord) error` — RecordMainTurn 在 b 交还发言权时发布 EXEC 主持标记：main 的过程行照旧实时
- `func (r goalTLRecorder) ArchiveTLHistory(_ context.Context, record goaldomain.TLArchiveRecord) error` — ArchiveTLHistory 把 b 侧会话历史归档进 tl 角色历史（环逃生收口时调用；实现

### goal_team_recorder_test.go

- `func (s *draftRecordingSessions) AppendRoleDraft(_, roleName, roleSessionID string, rows []dto.RoleDraftRow) error`
- `func (s *draftRecordingSessions) SyncRoleDraft(_, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)`
- `func (s *draftRecordingSessions) recordedSteps() []string`
- `func (s *draftRecordingSessions) appendedRows() []dto.RoleDraftRow`
- `func (s *draftRecordingSessions) syncOrder() []string`
- `func (s *draftRecordingSessions) pendingRows() int`
- `func draftRecordingFixture(t *testing.T, sessionID string) (*draftRecordingSessions, goaldomain.TLRoundRecorder, dto.TeamView)` — draftRecordingFixture 装配一个已建 goal（= 已自动装配 goal-a2a 团队）的会话，
- `func TestRecordTLRoundAppendsThenSyncsInTheSameRound(t *testing.T)` — TestRecordTLRoundAppendsThenSyncsInTheSameRound 钉住 b（ADVISOR）回合的记录口径：
- `func TestRecordMainTurnPublishesHostMarkerInTheSameRound(t *testing.T)` — TestRecordMainTurnPublishesHostMarkerInTheSameRound 钉住 EXEC 主持标记：
- `func TestRecordTLRoundKeepsTheOriginalWhenTheRoundIsCancelled(t *testing.T)` — TestRecordTLRoundKeepsTheOriginalWhenTheRoundIsCancelled 钉住「取消不丢内容」：
- `func TestRecordTLRoundWritesNothingWithoutATeam(t *testing.T)` — TestRecordTLRoundWritesNothingWithoutATeam 钉住降级：未装配 team 的会话不落

### goal_team_wiring_test.go

- `func (s *teamRecordingSessions) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)`
- `func (s *teamRecordingSessions) ReadLifecycleOrder(string) (string, []string, error)`
- `func (s *teamRecordingSessions) SetLifecycleOrder(_ string, policy string, roles []string) error`
- `func (s *teamRecordingSessions) ReadTeamRegistry(string) (dto.TeamRegistry, error)`
- `func (s *teamRecordingSessions) WriteTeamRegistry(_ string, registry dto.TeamRegistry) error`
- `func (s *teamRecordingSessions) RemoveTeamRegistry(string) error` — RemoveTeamRegistry 是 agentteam.DismissPort 的桩：团队离场 = 注册表清空 + 顺序复位
- `func (s *teamRecordingSessions) dismissCount() int` — dismissCount 返回团队离场次数。
- `func (s *teamRecordingSessions) CreateRoleSession(string, string, string, uint64) (dto.RoleSessionInfo, error)`
- `func (s *teamRecordingSessions) AppendRoleDraft(string, string, string, []dto.RoleDraftRow) error`
- `func (s *teamRecordingSessions) ReadRoleDraft(string, string, string) ([]dto.RoleDraftRow, error)`
- `func (s *teamRecordingSessions) SyncRoleDraft(string, string, string, []string) (dto.RoleDraftSyncResult, error)`
- `func (s *teamRecordingSessions) AppendRoleSessionRows(string, string, string, []dto.RoleRow) error`
- `func (s *teamRecordingSessions) ReadRoleSessionRows(string, string, string) ([]dto.RoleRow, error)`
- `func (s *teamRecordingSessions) RoleSnapshot(string, string, string) (dto.RoleSnapshot, error)`
- `func (s *teamRecordingSessions) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)`
- `func (s *teamRecordingSessions) SetRoleLifecycle(string, string, string, uint64, *dto.CompactFrameRef) error`
- `func (s *teamRecordingSessions) setLifecycle(policy string, order []string)` — 以下是夹具的**加锁存取入口**：夹具状态只允许通过它们读写。
- `func (s *teamRecordingSessions) setOrder(order []string)` — setOrder 只覆盖顺序（顺序策略不动）：装配前的"清现场"用它。
- `func (s *teamRecordingSessions) setRegistry(registry dto.TeamRegistry)` — setRegistry 覆盖夹具的团队注册表（加锁）。
- `func (s *teamRecordingSessions) lifecycleSnapshot() (string, []string)` — lifecycleSnapshot 读当前顺序策略与顺序（加锁 + 深拷贝，读到的是快照）。
- `func (s *teamRecordingSessions) registrySnapshot() dto.TeamRegistry` — registrySnapshot 读当前注册表（加锁 + 拷贝角色切片）。
- `func (s *teamRecordingSessions) orderSnapshot() []string` — orderSnapshot 只读顺序（加锁 + 拷贝）。
- `func (s *teamRecordingSessions) ensuredRoles() []string` — ensuredRoles 读已装配的角色会话名（加锁 + 拷贝）。
- `func (s *teamRecordingSessions) joinSeqSnapshot() []string` — joinSeqSnapshot 读角色会话装配时的 join 切点（加锁 + 拷贝）：
- `func (s *teamRecordingSessions) ListRoleSessions(string) ([]string, error)`
- `func TestGoalBeginLeavesSessionTeamAlone(t *testing.T)` — TestGoalBeginLeavesSessionTeamAlone 钉住 2026-10-01 的口径：创建 goal **不**自动装配
- `func TestGoalBeginWithoutTeamStorageIsBestEffort(t *testing.T)` — TestGoalBeginWithoutTeamStorageIsBestEffort 钉住降级语义：宿主没有团队存储
- `func (s *teamRecordingSessions) wireAsksSnapshot() []string` — wireAsksSnapshot 只读前缀用例的装配请求记录（加锁 + 拷贝）。
- `func (s *teamRecordingSessions) joinSeqsSnapshot() []string` — joinSeqsSnapshot 只读角色装配的 join_seq_id 记录（加锁 + 拷贝）。
- `func TestMaterializeJoinsTeammatesAtItsTurn(t *testing.T)` — TestMaterializeJoinsTeammatesAtItsTurn 钉住 teammate 记录（它自己那份 team work
- `func TestNoteTeamWorkPrefixReadsMainSessionContext(t *testing.T)` — TestNoteTeamWorkPrefixReadsMainSessionContext 钉住前缀的作者与读取口径：

### goal_work_summary.go

- `func (service *Service) goalTurnWorkSummary(sessionID string) string` — goalTurnWorkSummary 读目标会话的可见投影，返回本轮 EXEC 工作摘要（有界）。
- `func summarizeTurnWork(messages []Message) string` — summarizeTurnWork 抽取本轮（最后一条 user 行之后）的 EXEC 产出摘要。
- `func computerUseEvidence(messages []Message) string` — computerUseEvidence 抽取本轮 computer use 的可审查证据：截图工具结果里的
- `func oneLine(text string) string` — oneLine 压掉换行/连续空白（Detail 是"有界一句话"，不是多行正文）。
- `func truncateRunes(text string, max int) string` — truncateRunes 按 rune 截断（中文不被切半）。

### goal_work_summary_test.go

- `func TestSummarizeTurnWorkKeepsCurrentTurnOnly(t *testing.T)` — TestSummarizeTurnWorkKeepsCurrentTurnOnly 钉住"只取最后一条 user 行之后"：
- `func TestSummarizeTurnWorkIsBounded(t *testing.T)` — TestSummarizeTurnWorkIsBounded 钉住有界：Detail 上限由 goal 域
- `func TestSummarizeTurnWorkCarriesComputerUseEvidence(t *testing.T)` — TestSummarizeTurnWorkCarriesComputerUseEvidence：EXEC 用 computer_screenshot
- `func (e *capturingTLEvaluator) Evaluate(_ context.Context, embed goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`
- `func TestGoalAdvanceAfterChatFeedsEXECWorkToAdvisor(t *testing.T)` — TestGoalAdvanceAfterChatFeedsEXECWorkToAdvisor 是端到端接线用例：
- `func TestGoalAdvanceAfterChatWithoutWorkContentStaysQuiet(t *testing.T)` — TestGoalAdvanceAfterChatWithoutWorkContentStaysQuiet 钉住降级：会话没有可摘要
