# core/agentteam（根包分卷）

## 生态位

AgentTeam 装配适配与群聊角色会话透传（端口形状与 A2A 元数据口径）

覆盖：`agentteam*.go`、`role_session*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### agentteam_fixture_test.go

- `func goalTeamFixture() dto.TeamSpec` — goalTeamFixture 是一支 goal 形态的团队：user → main → tl（tl 是 techlead，角色回合
- `func newLibrarySessions() *librarySessions`
- `func (s *librarySessions) ReadTeamLibrary(string) (dto.TeamLibrary, error)`
- `func (s *librarySessions) WriteTeamLibrary(_ string, library dto.TeamLibrary) error`
- `func (s *librarySessions) ReadEmployeeLibrary(string) (dto.EmployeeLibrary, error)`
- `func (s *librarySessions) WriteEmployeeLibrary(_ string, library dto.EmployeeLibrary) error`
- `func (s *librarySessions) ReadDefaultOrder(string) (dto.DefaultOrder, error)`
- `func (s *librarySessions) WriteDefaultOrder(_ string, order dto.DefaultOrder) error`
- `func (s *librarySessions) globalWriteCount() int` — globalWriteCount 读当前全局母本写次数（断言"读不写盘"用）。
- `func (s *librarySessions) writeCount() int` — writeCount 读团队库写次数（加锁：直接读 sessions.writes 与写侧没有 happens-before
- `func (s *librarySessions) setLibrary(library dto.TeamLibrary)` — setLibrary 覆盖夹具的团队库（加锁）。
- `func summonService(t *testing.T, sessions SessionPort) *Service` — summonService 造一个带会话端口的服务，并把视图会话钉成固定 ID

### role_session.go

- `func (service *Service) rolePorts() (rolePorts, error)`
- `func (service *Service) roleSessionPort() (contract.RoleSessionPort, error)`
- `func (service *Service) schedulePort() (contract.SchedulePort, error)`
- `func (service *Service) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)`
- `func (service *Service) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error`
- `func (service *Service) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)`
- `func (service *Service) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)`
- `func (service *Service) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error`
- `func (service *Service) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)`
- `func (service *Service) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)`
- `func (service *Service) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)`
- `func (service *Service) SetLifecycleOrder(sessionID, policy string, roles []string) error`
- `func (service *Service) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error`
- `func (service *Service) ListRoleSessions(mainSessionID string) ([]string, error)`
- `func (service *Service) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (service *Service) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (service *Service) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error`
