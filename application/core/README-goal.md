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
- `func (g *goalCoordinator) bumpHeartbeat(sessionID string)`
- `func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — Begin 注册并压栈（会话路由）。
- `func (g *goalCoordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — Update 更新栈顶（会话路由）。
- `func (g *goalCoordinator) ProposeFinish(ctx context.Context, sessionID string, request goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)` — ProposeFinish 送终态 gate（TL 缺席时 OutcomeNoTL 直连收口；B4）。
- `func (g *goalCoordinator) Notify(ctx context.Context, sessionID string, signal goaldomain.TLEvalSignal) error` — Notify 登记 a 事件（exec 账本；触发策略见 Supervisor）。
- `func (g *goalCoordinator) Next(ctx context.Context, sessionID string) (bool, error)` — Next 推进治理循环一轮（惰性装配 EXEC+ADVISOR 双座位；返回 false = 收束）。
- `func (g *goalCoordinator) AdvanceAfterChat(ctx context.Context, sessionID string) error` — AdvanceAfterChat 在 ChatStream 返回后的锁外安全点推进一次治理：登记
- `func (g *goalCoordinator) newGovernor(runtime *goalSessionRuntime) govern.Governor` — newGovernor 装配 EXEC+ADVISOR 双座位（EXEC 由外部 ChatStream 驱动，
- `func (g *goalCoordinator) Break(_ context.Context, sessionID, reason string) error` — Break 外部中断治理循环（无 Governor 时报错，对齐 headless 未装配语义）。
- `func (g *goalCoordinator) setEvaluator(evaluator goaldomain.TLEvaluator)` — setEvaluator 装配/替换 TL 评估器：更新后续会话 bundle 构造输入，并为已
- `func (g *goalCoordinator) StatusFor(sessionID string) goaldomain.StatusView` — StatusFor 返回会话 goal 栈全量视图（无 bundle 时返回空视图）。
- `func (g *goalCoordinator) DrainDirectives(sessionID string) []goaldomain.TLDirective` — DrainDirectives 排空该会话 b→a 指令队列（ChatStream 回合边界注入）。
- `func (g *goalCoordinator) NoteInjected(sessionID string, texts []string)` — NoteInjected 记录一次已注入引擎的 TL 指令文本（回合尾可见区回放）。
- `func (g *goalCoordinator) TakeInjected(sessionID string) []string` — TakeInjected 取走（并清空）该会话已注入的 TL 指令文本。
- `func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal → nil，前端隐藏）。

### goal_coordinator_test.go

- `func TestGoalCoordinatorSessionIsolation(t *testing.T)` — TestGoalCoordinatorSessionIsolation 验证 P1 会话级协调器：两会话各自
- `func TestGoalCoordinatorHeartbeatMonotonic(t *testing.T)` — TestGoalCoordinatorHeartbeatMonotonic 验证治理推进即心跳：Begin/Update
- `func TestSessionRuntimeCarriesGoalGovernance(t *testing.T)` — TestSessionRuntimeCarriesGoalGovernance 验证 GoalGovernanceView 进入
- `func (e *stubTLEvaluator) Evaluate(context.Context, goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error)`
- `func TestGoalCoordinatorAdvanceAfterChatRunsTLRound(t *testing.T)` — TestGoalCoordinatorAdvanceAfterChatRunsTLRound 验证 A2A 在真实会话边界
- `func TestGoalCoordinatorAdvanceAfterChatTLDisabledNoError(t *testing.T)` — TestGoalCoordinatorAdvanceAfterChatTLDisabledNoError 验证 TL 未启用时

### goal_service.go

- `func (service *Service) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 返回指定会话的 goal 治理只读视图（view_state 装配
- `func (service *Service) goalCoordinatorFor(sessionID string) (*goalCoordinator, error)`
- `func (service *Service) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — GoalBeginFor 按显式会话注册 goal（多会话路由）。
- `func (service *Service) ensureGoalAgentTeam(sessionID string)` — ensureGoalAgentTeam 确保当前会话的 goal-a2a 团队已装配（幂等）。
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
- `func (service *Service) injectGoalDirectivesForStart(sessionID string)` — injectGoalDirectivesForStart 在 ChatStream 开始前把 TL 回合产生的指令
- `func (service *Service) goalAdvanceAfterChat(ctx context.Context)` — goalAdvanceAfterChat 在 ChatStream 返回后的锁外安全点推进 goal 治理
- `func (service *Service) injectGoalDirectivesFor(sessionID string)` — injectGoalDirectivesFor 在 ChatStream 结束后的锁外安全点，把本回合已注入
- `func (service *Service) goalBeginHandler(ctx context.Context, argsJSON string) (string, error)` — goalBeginHandler 是 goal_begin 工具 handler（main.go 注册）。
- `func (service *Service) goalUpdateHandler(ctx context.Context, argsJSON string) (string, error)` — goalUpdateHandler 是 goal_update 工具 handler。
- `func (service *Service) goalProposeFinishHandler(ctx context.Context, argsJSON string) (string, error)` — goalProposeFinishHandler 是 goal_propose_finish 工具 handler。
- `func (service *Service) goalStatusHandler(ctx context.Context, _ string) (string, error)` — goalStatusHandler 是 goal_status 工具 handler。
- `func marshalGoalResult(value any) (string, error)`
- `func (service *Service) GoalBeginHandler(ctx context.Context, argsJSON string) (string, error)` — GoalBeginHandler / GoalUpdateHandler / GoalStatusHandler /
- `func (service *Service) GoalUpdateHandler(ctx context.Context, argsJSON string) (string, error)`
- `func (service *Service) GoalStatusHandler(ctx context.Context, argsJSON string) (string, error)`
- `func (service *Service) GoalProposeFinishHandler(ctx context.Context, argsJSON string) (string, error)`

### goal_team_wiring_test.go

- `func (s *teamRecordingSessions) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)`
- `func (s *teamRecordingSessions) ReadLifecycleOrder(string) (string, []string, error)`
- `func (s *teamRecordingSessions) SetLifecycleOrder(_ string, policy string, roles []string) error`
- `func (s *teamRecordingSessions) ReadTeamRegistry(string) (dto.TeamRegistry, error)`
- `func (s *teamRecordingSessions) WriteTeamRegistry(_ string, registry dto.TeamRegistry) error`
- `func (s *teamRecordingSessions) CreateRoleSession(string, string, string, uint64) (dto.RoleSessionInfo, error)`
- `func (s *teamRecordingSessions) AppendRoleDraft(string, string, string, []dto.RoleDraftRow) error`
- `func (s *teamRecordingSessions) ReadRoleDraft(string, string, string) ([]dto.RoleDraftRow, error)`
- `func (s *teamRecordingSessions) SyncRoleDraft(string, string, string, []string) (dto.RoleDraftSyncResult, error)`
- `func (s *teamRecordingSessions) AppendRoleSessionRows(string, string, string, []dto.RoleRow) error`
- `func (s *teamRecordingSessions) ReadRoleSessionRows(string, string, string) ([]dto.RoleRow, error)`
- `func (s *teamRecordingSessions) RoleSnapshot(string, string, string) (dto.RoleSnapshot, error)`
- `func (s *teamRecordingSessions) AssembleRoleWire(string, string, string, int, int) (dto.RoleWireSnapshot, error)`
- `func (s *teamRecordingSessions) SetRoleLifecycle(string, string, string, uint64, *dto.CompactFrameRef) error`
- `func (s *teamRecordingSessions) ListRoleSessions(string) ([]string, error)`
- `func TestGoalBeginMaterializesGoalAgentTeam(t *testing.T)` — TestGoalBeginMaterializesGoalAgentTeam 钉住 goal → AgentTeam 接线：创建 goal
- `func TestGoalBeginWithoutTeamStorageIsBestEffort(t *testing.T)` — TestGoalBeginWithoutTeamStorageIsBestEffort 钉住降级语义：宿主没有团队存储
