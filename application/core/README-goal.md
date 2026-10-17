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

### goal_board_archive_read_test.go

- `func boardRouterForArchive(t *testing.T) *store.Router` — boardRouterForArchive 建一个临时会话存储（看板存档落在其 metadata 目录）。
- `func boardStoreFor(router *store.Router) func(string) *store.SessionContextStore` — boardStoreFor 返回协调器的按会话存储入口（每次给一个新的会话存储实例：与生产
- `func writeBoardArchiveFor(t *testing.T, router *store.Router, sessionID string, meta store.GoalBoardMeta)` — writeBoardArchiveFor 直接发布一份看板存档（构造"只有存档、没有活体栈"的现场）。
- `func TestGoalGovernanceViewRecoversBoardFromArchive(t *testing.T)` — TestGoalGovernanceViewRecoversBoardFromArchive 验证兜底读：本次进程还没碰过该
- `func TestGoalGovernanceViewRetiresBoardOnClosedArchive(t *testing.T)` — TestGoalGovernanceViewRetiresBoardOnClosedArchive 验证"看板退场"：存档 state=closed
- `func TestGoalGovernanceViewCarriesHistoryLedgerWhileLive(t *testing.T)` — TestGoalGovernanceViewCarriesHistoryLedgerWhileLive 验证账本与活动栈**正交**：
- `func TestGoalGovernanceViewWithoutArchiveStaysHidden(t *testing.T)` — TestGoalGovernanceViewWithoutArchiveStaysHidden 验证未装配/无存档时的降级：

### goal_coordinator.go

- `func newGoalCoordinator(deps goalCoordinatorDeps) *goalCoordinator`
- `func (g *goalCoordinator) ForgetSession(sessionID string)` — ForgetSession 释放一个会话的 goal 运行时（会话释放面调用）：丢掉按会话缓存
- `func (g *goalCoordinator) bundleFor(sessionID string) *goalSessionRuntime` — bundleFor 返回（需要时创建）指定会话的 goal bundle。
- `func (g *goalCoordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — Begin 注册并压栈（会话路由）。
- `func (g *goalCoordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — Update 更新栈顶（会话路由）。
- `func (g *goalCoordinator) FinishDirect(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)` — FinishDirect 直接落终态收口（goal_done 的领域动作）：把栈顶 goal 标
- `func (g *goalCoordinator) StatusFor(sessionID string) goaldomain.StatusView` — StatusFor 返回会话 goal 栈全量视图（无 bundle 时返回空视图）。
- `func goalStackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView` — goalStackFrames 把 Controller 的活动栈投影成逐帧只读视图（栈底→栈顶，末元素
- `func (g *goalCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal/无存档 → nil）。
- `func (g *goalCoordinator) boardFor(sessionID string) (store.SessionBoards, bool)` — boardFor 返回指定会话的看板存档取用面（未装配 → false）。
- `func (g *goalCoordinator) readGoalBoardArchive(sessionID string) (store.GoalBoardMeta, bool)` — readGoalBoardArchive 读取该会话的看板存档（缺失/损坏 → false）。
- `func recoveredGoalView(archive store.GoalBoardMeta, hasArchive bool) *dto.GoalGovernanceView` — recoveredGoalView 用存档里的 active 帧重建看板（Recovered=true）。
- `func goalHistoryViews(archive store.GoalBoardMeta, hasArchive bool) []dto.GoalHistoryView` — goalHistoryViews 把看板存档的收口账本投影成只读 DTO（空 → nil）。
- `func goalProgressViews(items []store.GoalProgress) []dto.GoalProgressView` — goalProgressViews 把看板存档里的打点投影成只读 DTO（空 → nil）。
- `func tailProgressViews(items []dto.GoalProgressView, limit int) []dto.GoalProgressView` — tailProgressViews 取末尾 limit 条（卡片一行摘要用；详情面用全量）。

### goal_done_test.go

- `func TestGoalDoneFinishesDirectly(t *testing.T)`

### goal_read_session.go

- `func goalReadSessionID(ctx context.Context) string` — goalReadSessionID 是「读 goal 看板」该用的会话号。

### goal_service.go

- `func (service *Service) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 返回指定会话的 goal 治理只读视图（view_state 装配
- `func (service *Service) goalCoordinatorFor(sessionID string) (*goalCoordinator, error)`
- `func (service *Service) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — GoalBeginFor 按显式会话注册 goal（多会话路由）。
- `func (service *Service) GoalBegin(ctx context.Context, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — GoalBegin 按执行 ctx 会话注册 goal（main agent 工具调用路径）。
- `func (service *Service) GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — GoalUpdateFor 按显式会话更新栈顶 goal。
- `func (service *Service) GoalUpdate(ctx context.Context, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — GoalUpdate 按执行 ctx 会话更新（main agent 工具调用路径）。
- `func (service *Service) GoalDoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)` — GoalDoneFor 按显式会话**直接收口**栈顶 goal（goal_done 工具路径）。
- `func (service *Service) GoalDone(ctx context.Context, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)` — GoalDone 按执行 ctx 会话直接收口（main agent 工具调用路径）。
- `func (service *Service) GoalStatusFor(sessionID string) (goaldomain.StatusView, error)` — GoalStatusFor 按显式会话返回 goal 栈全量视图。
- `func (service *Service) refreshGoalRuntimeProjection(sessionID string)` — refreshGoalRuntimeProjection 在 goal 状态迁移后刷新目标会话的 runtime
- `func (service *Service) goalBeginHandler(ctx context.Context, argsJSON string) (string, error)` — goalBeginHandler 是 goal_begin 工具 handler（wiring/registry 注册）。
- `func (service *Service) goalUpdateHandler(ctx context.Context, argsJSON string) (string, error)` — goalUpdateHandler 是 goal_update 工具 handler（agent 工具面：权限收口见
- `func (service *Service) goalDoneHandler(ctx context.Context, argsJSON string) (string, error)` — goalDoneHandler 是 goal_done 工具 handler（main agent 的**真收口**面，口径见 GoalDoneFor）。
- `func (service *Service) goalStatusHandler(ctx context.Context, _ string) (string, error)` — goalStatusHandler 是 goal_status 工具 handler。
- `func marshalGoalResult(value any) (string, error)`
- `func (service *Service) GoalBeginHandler(ctx context.Context, argsJSON string) (string, error)` — GoalBeginHandler / GoalUpdateHandler / GoalStatusHandler
- `func (service *Service) GoalUpdateHandler(ctx context.Context, argsJSON string) (string, error)`
- `func (service *Service) GoalStatusHandler(ctx context.Context, argsJSON string) (string, error)`
- `func (service *Service) GoalDoneHandler(ctx context.Context, argsJSON string) (string, error)` — GoalDoneHandler 是 goal_done 工具 handler（main agent 的真收口面；teammate 看不到这个

### goal_stack_view_test.go

- `func TestGoalStackFramesProjectsEveryFrame(t *testing.T)`
- `func TestGoalStackFramesCarriesDetailFields(t *testing.T)` — TestGoalStackFramesCarriesDetailFields 钉住"点开看详情"需要的字段不缺：
- `func TestGoalStackFramesEmptyAndNilRecords(t *testing.T)`

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
- `func (s *teamRecordingSessions) AssembleRoleWire(string, string, string, int, int) (dto.RoleWireSnapshot, error)`
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
- `func (s *teamRecordingSessions) joinSeqsSnapshot() []string` — joinSeqsSnapshot 只读角色装配的 join_seq_id 记录（加锁 + 拷贝）。
