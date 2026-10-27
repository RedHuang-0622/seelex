# assembly/goal（根包分卷）

## 生态位

goal 域协调器/门面用例与「goal 上线不覆盖会话团队」接线回归

覆盖：`goal*.go`；未归属文件由覆盖自检拦下。

## Goal Application 用例（AP-015 首个切片）

`Goal application` 集中会话目标用例：协调器查找、Begin/Update/Done/Status、
治理视图和运行投影刷新。`goalCoordinator` 继续持有按会话 bundle，
`kernel/goal.Controller` 继续唯一拥有目标状态机。

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

### goal_application_test.go

- `func TestServiceComponentsAssembleGoalApplication(t *testing.T)`

### goal_service.go

- `func (service *Service) refreshGoalRuntimeProjection(sessionID string)` — refreshGoalRuntimeProjection 在 goal 状态迁移后刷新目标会话的 runtime
- `func (port goalGovernanceViewPort) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView`

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
