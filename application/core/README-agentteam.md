# core/agentteam（根包分卷）

## 生态位

AgentTeam 装配适配与群聊角色会话透传（端口形状与 A2A 元数据口径）

覆盖：`agentteam*.go`、`role_session*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### agentteam_service.go

- `func (adapter agentTeamAdapter) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)`
- `func (adapter agentTeamAdapter) ReadLifecycleOrder(sessionID string) (string, []string, error)`
- `func (adapter agentTeamAdapter) SetLifecycleOrder(sessionID, policy string, roles []string) error`
- `func (adapter agentTeamAdapter) ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error)`
- `func (adapter agentTeamAdapter) WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error`
- `func (service *Service) agentTeamPorts() (agentTeamPort, contract.RoleSessionPort, error)`
- `func (service *Service) agentTeamFactory() (*agentteam.Factory, error)`
- `func (service *Service) agentTeamRegistry() (*agentteam.Registry, error)`
- `func (service *Service) AgentTeamPresets() []dto.TeamSpec` — AgentTeamPresets 列出内置团队形态（前端角色管理页的可选模板）。
- `func (service *Service) MaterializeAgentTeam(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error)` — MaterializeAgentTeam 按 preset/自定义 TeamSpec 装配一支 AgentTeam。
- `func (service *Service) MaterializeAgentTeamPreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error)` — MaterializeAgentTeamPreset 按内置 preset 名装配（goal-a2a / review-team / research-team）。
- `func (service *Service) AgentTeamView(mainSessionID string) (dto.TeamView, error)` — AgentTeamView 返回成员表（身份/顺序/定时分区/配置状态）。
- `func (service *Service) AgentTeamPutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error)` — AgentTeamPutRole 新增/覆盖一个角色配置。
- `func (service *Service) AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error)` — AgentTeamDeleteRole 删除一个角色配置（并把它从工作顺序里摘除）。
- `func (service *Service) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error)` — AgentTeamSetOrder 写工作顺序（前端拖拽/上下移只提交这个字段）。

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
