# core/goal（根包分卷）

## 生态位

goal 域协调器/门面用例与「goal 上线即装配 TL 团队」接线回归

覆盖：`goal*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### goal_coordinator.go

- `func newGoalCoordinator(deps goalCoordinatorDeps) *goalCoordinator`
- `func (g *goalCoordinator) bundleFor(sessionID string) *goalSessionRuntime` — bundleFor 返回（需要时创建）指定会话的 goal bundle。创建时若装配了会话
- `func (g *goalCoordinator) noteRoundError(sessionID string, err error)` — noteRoundError 登记（或清除）该会话上一轮治理推进的失败原因。
- `func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — Begin 注册并压栈（会话路由）。
- `func (g *goalCoordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — Update 更新栈顶（会话路由）。
- `func (g *goalCoordinator) ProposeFinish(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)` — ProposeFinish 送终态 gate（TL 缺席时 OutcomeNoTL 直连收口；B4）。
- `func (g *goalCoordinator) Notify(ctx context.Context, sessionID string, signal goaldomain.TLEvalSignal) error` — Notify 登记 a 事件（exec 账本；触发策略见 Supervisor）。
- `func (g *goalCoordinator) Next(ctx context.Context, sessionID string) (bool, error)` — Next 推进治理循环一轮（惰性装配座位；返回 false = 收束）。
- `func (g *goalCoordinator) AdvanceAfterChat(ctx context.Context, sessionID, detail string) error` — AdvanceAfterChat 在 ChatStream 返回后的锁外安全点推进一次治理：登记
- `func (g *goalCoordinator) advanceAfterChat(ctx context.Context, sessionID, detail string) error`
- `func (g *goalCoordinator) teamRuntimeFor(sessionID string) *agentteam.Runtime` — teamRuntimeFor 取该会话的团队发言调度运行态（未装配团队环 → nil）。
- `func goalLoopRoundLimit(configured int) int` — goalLoopRoundLimit 把配置值解析成实际生效的轮次上限。
- `func (g *goalCoordinator) newGovernor(sessionID string, runtime *goalSessionRuntime) govern.Governor` — newGovernor 装配治理循环座位。座位的**存在性**由团队工作顺序（链表）决定：
- `func (g *goalCoordinator) seatsFor(sessionID string, supervisor *goaldomain.Supervisor, execAct func(context.Context) (govern.TurnAction, error)) []govern.Seat` — seatsFor 按团队注册表的**角色 kind** 派生治理座位。
- `func (g *goalCoordinator) roleTurnRunnerFor(sessionID string) RoleTurnRunner` — roleTurnRunnerFor 取该会话的员工执行面（未装配 → nil = 试水形态）。
- `func (plan seatPlan) seats() []govern.Seat` — seats 按角色 kind 派生座位（纯函数，便于单测钉住"改名不丢座位"与
- `func newRoleTurnSeat(seat RoleSeat, orderIndex int, sessionID string, runner RoleTurnRunner) roleTurnSeat`
- `func (seat roleTurnSeat) Name() string`
- `func (seat roleTurnSeat) Kind() govern.AgentKind`
- `func withRoleTurnInput(ctx context.Context, detail string) context.Context`
- `func roleTurnInputFromContext(ctx context.Context) string`
- `func (seat roleTurnSeat) Act(ctx context.Context) (govern.TurnAction, error)` — Act 跑一轮员工回合。错误向上抛（治理循环透传）：执行面出错不能吞成"没产出"，
- `func roleTurnNote(outcome RoleTurnOutcome) string` — roleTurnNote 把一轮员工回合的结论落成面板可见的一句话：没跑起来和跑了没产出
- `func seatsFromOrder(order []string, supervisor *goaldomain.Supervisor, execAct func(context.Context) (govern.TurnAction, error)) []govern.Seat` — seatsFromOrder 是退化路径：只有链表顺序（角色名）时按名字匹配。
- `func orderSeats(seats []govern.Seat) []govern.Seat` — orderSeats 把座位按 EXEC → ADVISOR 归位（同 kind 保持链表次序）。
- `func (g *goalCoordinator) teamOrderFor(sessionID string) []string` — teamOrderFor 读该会话团队环的链表顺序（未装配团队环 → nil）。
- `func (s teamRoleSeat) Name() string`
- `func (s teamRoleSeat) Kind() govern.AgentKind`
- `func (s teamRoleSeat) Act(ctx context.Context) (govern.TurnAction, error)`
- `func (g *goalCoordinator) Break(_ context.Context, sessionID, reason string) error` — Break 外部中断治理循环（无 Governor 时报错，对齐 headless 未装配语义）。
- `func (g *goalCoordinator) setEvaluator(evaluator goaldomain.TLEvaluator)` — setEvaluator 装配/替换 TL 评估器：更新后续会话 bundle 构造输入，并为已
- `func (g *goalCoordinator) StatusFor(sessionID string) goaldomain.StatusView` — StatusFor 返回会话 goal 栈全量视图（无 bundle 时返回空视图）。
- `func (g *goalCoordinator) DrainDirectives(sessionID string) []goaldomain.TLDirective` — DrainDirectives 排空该会话 b→a 指令队列（ChatStream 回合边界注入）。
- `func (g *goalCoordinator) PeekDirectives(sessionID string) []goaldomain.TLDirective` — PeekDirectives 读取该会话待注入的 b→a 指令（不消费）：回合结束时把刚产出的
- `func goalStackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView` — goalStackFrames 把 Controller 的活动栈投影成逐帧只读视图（栈底→栈顶，末元素
- `func (g *goalCoordinator) DirectivePublished(sessionID, corr string) bool` — DirectivePublished 报告该 corr 的指令是否已回放进可见会话（corr 幂等去重）。
- `func (g *goalCoordinator) MarkDirectivePublished(sessionID, corr string)` — MarkDirectivePublished 记录一条指令已回放进可见会话（回合结束时记，下一次
- `func (g *goalCoordinator) NoteInjected(sessionID string, directives []goaldomain.TLDirective)` — NoteInjected 记录一次已注入引擎受信区的 TL 指令（回合尾可见区回放）。
- `func (g *goalCoordinator) TakeInjected(sessionID string) []goaldomain.TLDirective` — TakeInjected 取走（并清空）该会话已注入受信区的 TL 指令。
- `func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal → nil，前端隐藏）。

### goal_coordinator_test.go

- `func TestGoalCoordinatorSessionIsolation(t *testing.T)` — TestGoalCoordinatorSessionIsolation 验证 P1 会话级协调器：两会话各自
- `func (failingTLEvaluator) Evaluate(context.Context, goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`
- `func TestGoalCoordinatorRoundErrorVisible(t *testing.T)` — TestGoalCoordinatorRoundErrorVisible 钉住「本轮治理未完成」的可见面：治理回合
- `func TestSessionRuntimeCarriesGoalGovernance(t *testing.T)` — TestSessionRuntimeCarriesGoalGovernance 验证 GoalGovernanceView 进入
- `func (e *stubTLEvaluator) Evaluate(context.Context, goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`
- `func TestGoalCoordinatorAdvanceAfterChatRunsTLRound(t *testing.T)` — TestGoalCoordinatorAdvanceAfterChatRunsTLRound 验证 A2A 在真实会话边界
- `func TestGoalCoordinatorAdvanceAfterChatTLDisabledNoError(t *testing.T)` — TestGoalCoordinatorAdvanceAfterChatTLDisabledNoError 验证 TL 未启用时
- `func TestGoalCoordinatorRoutineVerdictDoneClosesGoal(t *testing.T)` — TestGoalCoordinatorRoutineVerdictDoneClosesGoal 钉住 2026-09-15 seq-5389 的
- `func TestGoalCoordinatorBeginResetsBrokenGovernor(t *testing.T)` — TestGoalCoordinatorBeginResetsBrokenGovernor 验证新 goal 拿回治理循环：

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
- `func (e *keepGoingEvaluator) Evaluate(_ context.Context, _ goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`
- `func (e *keepGoingEvaluator) count() int`
- `func TestQueuedRoundMustNotReenterSessionLock(t *testing.T)`

### goal_directive_visible_immediately_test.go

- `func advisorDirectiveRows(messages []Message) []Message` — advisorDirectiveRows 取可见会话里的 ADVISOR 裁决行（kind + 归属双条件：
- `func TestAdvisorVerdictVisibleInProducingTurn(t *testing.T)`

### goal_loop_limit_test.go

- `func TestGoalLoopRoundLimitDefaults(t *testing.T)` — TestGoalLoopRoundLimitDefaults：治理循环的轮次上限解析——未配置（0）时落到
- `func TestGoalGovernanceViewCarriesRoundLimit(t *testing.T)` — TestGoalGovernanceViewCarriesRoundLimit：治理视图必须把轮次上限一并下发——
- `func TestTeamRuntimeSharesGovernorRoundLimit(t *testing.T)` — TestTeamRuntimeSharesGovernorRoundLimit：团队环的逃生上限与 Governor 的

### goal_loop_turn_order_test.go

- `func (e *scriptedTLEvaluator) Evaluate(_ context.Context, _ goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`
- `func (e *scriptedTLEvaluator) count() int`
- `func waitUntil(t *testing.T, what string, ready func() bool)` — waitUntil 轮询直到条件成立（回合切换是异步的，用一次有界等待而不是 sleep 猜）。
- `func TestGoalLoopTurnsAlternateExecAdvisorUntilVerdictCloses(t *testing.T)`

### goal_permission_test.go

- `func TestAgentGoalUpdateCannotRewriteDefinition(t *testing.T)` — TestAgentGoalUpdateCannotRewriteDefinition 钉住 agent 工具面的收口与人类面的保留。

### goal_ring_escape_test.go

- `func (s *escapeRecordingSessions) AppendRoleDraft(_ string, _ string, _ string, rows []dto.RoleDraftRow) error`
- `func (s *escapeRecordingSessions) draftsOfKind(kind string) []dto.RoleDraftRow`
- `func TestRingEscapeClosesGoalAndArchivesTLHistory(t *testing.T)` — TestRingEscapeClosesGoalAndArchivesTLHistory：逃生后 goal 必须收口，且 tl 角色
- `func TestRingEscapeWithoutGoalIsQuiet(t *testing.T)` — TestRingEscapeWithoutGoalIsQuiet：没有 goal 时环逃生不应报错、不应写归档

### goal_role_turn_test.go

- `func (runner *stubRoleTurnRunner) RunRoleTurn(_ context.Context, request RoleTurnRequest) (RoleTurnOutcome, error)`
- `func (runner *stubRoleTurnRunner) recorded() []RoleTurnRequest`
- `func TestRoleTurnSeatPassesRoleIdentityToExecutionFace(t *testing.T)` — TestRoleTurnSeatPassesRoleIdentityToExecutionFace：座位必须把"在哪个角色会话上、
- `func TestRoleTurnNoteSurfacesSilentAndEmptyRounds(t *testing.T)` — TestRoleTurnNoteSurfacesSilentAndEmptyRounds：座位一直空着不能被误读成"员工干完了"。
- `func TestRoleTurnSeatPropagatesExecutionError(t *testing.T)` — TestRoleTurnSeatPropagatesExecutionError：执行面出错必须向上抛。吞成"没产出"会让
- `func TestAdvanceAfterChatCompletesRoundWithExecutionSeats(t *testing.T)` — TestAdvanceAfterChatCompletesRoundWithExecutionSeats 是"员工执行面接入后环仍能走完
- `func TestAdvanceAfterChatWithoutExecutionFaceKeepsPilotShape(t *testing.T)` — TestAdvanceAfterChatWithoutExecutionFaceKeepsPilotShape 是试水形态的回归钉：
- `func TestRoleTurnNoteTrimmed(t *testing.T)` — TestRoleTurnNoteTrimmed 保证空 Note 不产生悬空空格（面板拼接用）。

### goal_seats_test.go

- `func seatExecAct(context.Context) (govern.TurnAction, error)`
- `func seatKindsOf(seats []govern.Seat) []govern.AgentKind`
- `func seatNamesOf(seats []govern.Seat) []string`
- `func equalSeatKinds(left, right []govern.AgentKind) bool`
- `func TestSeatPlanFollowsRoleKindNotRoleName(t *testing.T)` — TestSeatPlanFollowsRoleKindNotRoleName：座位由 kind 决定，与角色名无关。
- `func TestSeatPlanGivesExecutionSeatToAgentRoles(t *testing.T)` — TestSeatPlanGivesExecutionSeatToAgentRoles：装配了员工执行面后，agent 角色获得
- `func equalStrings(left, right []string) bool`
- `func TestCoordinatorSeatsFromRegistryKinds(t *testing.T)` — TestCoordinatorSeatsFromRegistryKinds：协调器在有注册表读面时必须走 kind 派生，
- `func TestSeatsFallBackToOrderNamesWithoutRegistry(t *testing.T)` — TestSeatsFallBackToOrderNamesWithoutRegistry：没有注册表读面时退回老路径（按
- `func TestCoordinatorSeatsFallBackWhenRegistryUnavailable(t *testing.T)` — TestCoordinatorSeatsFallBackWhenRegistryUnavailable：没有任何读面时不长座位
- `func TestTeamRoleSeatsFromRegistryView(t *testing.T)` — TestTeamRoleSeatsFromRegistryView 钉住装配层的座位来源：角色按发言链顺序

### goal_service.go

- `func (service *Service) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 返回指定会话的 goal 治理只读视图（view_state 装配
- `func (service *Service) goalCoordinatorFor(sessionID string) (*goalCoordinator, error)`
- `func (service *Service) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — GoalBeginFor 按显式会话注册 goal（多会话路由）。
- `func (service *Service) ensureGoalAgentTeam(sessionID string)` — ensureGoalAgentTeam 确保当前会话的 goal-a2a 团队已装配（幂等）。
- `func (service *Service) teamJoinSeqFor(sessionID string) uint64` — teamJoinSeqFor 返回团队装配的 join 切点 = 主会话当前已提交的 message 尾 seq。
- `func (service *Service) GoalBegin(ctx context.Context, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — GoalBegin 按执行 ctx 会话注册 goal（main agent 工具调用路径）。
- `func (service *Service) GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — GoalUpdateFor 按显式会话更新栈顶 goal。
- `func (service *Service) GoalUpdate(ctx context.Context, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — GoalUpdate 按执行 ctx 会话更新（main agent 工具调用路径）。
- `func (service *Service) GoalProposeFinishFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)` — GoalProposeFinishFor 按显式会话送终态 gate。
- `func (service *Service) GoalProposeFinish(ctx context.Context, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)` — GoalProposeFinish 按执行 ctx 会话提议收口（main agent 工具调用路径）。
- `func (service *Service) GoalStatusFor(sessionID string) (goaldomain.StatusView, error)` — GoalStatusFor 按显式会话返回 goal 栈全量视图。
- `func (service *Service) GoalNextFor(ctx context.Context, sessionID string) (bool, error)` — GoalNextFor 按显式会话推进一轮治理循环。
- `func (service *Service) GoalNext(ctx context.Context) (bool, error)` — GoalNext 按执行 ctx 会话推进治理循环。
- `func (service *Service) SetGoalTLEvaluator(evaluator goaldomain.TLEvaluator)` — SetGoalTLEvaluator 注入真实 TL 评估器（组合根：seelebridge 账号面 →
- `func (service *Service) GoalBreakFor(_ context.Context, sessionID, reason string) error` — GoalBreakFor 按显式会话外部中断治理循环（headless goal_gov_break）。
- `func (service *Service) refreshGoalRuntimeProjection(sessionID string)` — refreshGoalRuntimeProjection 在 goal 状态迁移后刷新目标会话的 runtime
- `func (service *Service) GoalIterationCompleted(ctx context.Context) bool` — GoalIterationCompleted 是 ChatStream OnIterationComplete 的 goal 接线：
- `func formatDirectiveText(directive goaldomain.TLDirective) string` — formatDirectiveText 是 b→a 指令的**单行可读形式**：引擎受信注入与可见回放
- `func (service *Service) injectGoalDirectives(sessionID string, directives []goaldomain.TLDirective)` — injectGoalDirectives 把 b→a 指令注入引擎受信区，并登记"待可见回放"：
- `func (service *Service) injectGoalDirectivesForStart(sessionID string)` — injectGoalDirectivesForStart 在 ChatStream 开始前把 TL 回合产生的指令
- `func (service *Service) goalAdvanceAfterChat(ctx context.Context)` — goalAdvanceAfterChat 在 ChatStream 返回后的锁外安全点推进 goal 治理
- `func (service *Service) dismissTeamWhenGoalClosed(sessionID string)` — dismissTeamWhenGoalClosed 让"干完就走人"成立：目标收口（栈里没有 active goal）
- `func (service *Service) injectGoalDirectivesFor(sessionID string)` — injectGoalDirectivesFor 在 ChatStream 结束后的锁外安全点，把本回合已注入
- `func (service *Service) publishPendingGoalDirectivesFor(sessionID string)` — publishPendingGoalDirectivesFor 把治理回合**刚产出**、仍在待注入队列里的
- `func (service *Service) publishAdvisorDirectiveRows(sessionID string, directives []goaldomain.TLDirective)` — publishAdvisorDirectiveRows 把 b→a 指令以可见 ADVISOR 行写进目标会话：
- `func (service *Service) advisorRoleSessionID(sessionID string) string` — advisorRoleSessionID 解析 ADVISOR（tl）的角色会话号：按工厂口径
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
- `func TestGoalBeginMaterializesGoalAgentTeam(t *testing.T)` — TestGoalBeginMaterializesGoalAgentTeam 钉住 goal → AgentTeam 接线：创建 goal
- `func TestGoalBeginWithoutTeamStorageIsBestEffort(t *testing.T)` — TestGoalBeginWithoutTeamStorageIsBestEffort 钉住降级语义：宿主没有团队存储
- `func (s *teamRecordingSessions) wireAsksSnapshot() []string` — wireAsksSnapshot 只读前缀用例的装配请求记录（加锁 + 拷贝）。
- `func (s *teamRecordingSessions) joinSeqsSnapshot() []string` — joinSeqsSnapshot 只读角色装配的 join_seq_id 记录（加锁 + 拷贝）。
- `func TestGoalBeginJoinsTeammatesAtGoalTurn(t *testing.T)` — TestGoalBeginJoinsTeammatesAtGoalTurn 钉住 teammate 记录（它自己那份 team work
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
