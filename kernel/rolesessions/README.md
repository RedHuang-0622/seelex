# kernel/rolesessions

## 生态位

`kernel/rolesessions` 是角色会话与定时插话可选端口的窄组合门面，供 Application
Compat 与 Wiring 在未装配宿主能力时返回稳定错误，在装配后原样转发到
`contract.RoleSessionPort` 和 `contract.SchedulePort`。

## 职责与非职责

- 做：聚合两个 Contract 端口，按 `Assembled` 状态返回固定错误，并逐项转发。
- 不做：不持久化、不创建会话、不调度任务，也不持有应用状态。

## 文件结构

- `roles.go`：`RoleSessions` 组合端口与全部转发方法。
- `roles_test.go`：未装配错误和成功转发行为。

## 核心实现

`RoleSessions` 只在两个可选端口之上做状态检查与方法转发；没有第二份业务判据。

## 数据流

Wiring 注入具体角色会话与调度端口 → `RoleSessions` → Application Frontend。

## 依赖方向

只依赖 `contract` 与 `contract/dto`。禁止依赖 Application、Backend 实现、Store 或前端。

## 并发与错误

自身无锁、无状态写入。未装配与缺端口使用固定错误文案，成功路径直接转发。

## 扩展方式

扩展角色会话能力时先扩 Contract 端口，再在本组合门面补同签名转发与 parity 测试。

## Review 指南

- 是否仍只转发且没有本地状态机；
- 错误文案是否与既有调用方保持一致；
- 是否错误地引入 Backend/Application 具体包。

## 测试与验证

```text
go test ./kernel/rolesessions -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### roles.go

- `func (r RoleSessions) RolePort() (contract.RoleSessionPort, error)` — RolePort 返回角色会话端口。
- `func (r RoleSessions) SchedulePort() (contract.SchedulePort, error)` — SchedulePort 返回定时插话端口。
- `func (r RoleSessions) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)`
- `func (r RoleSessions) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error`
- `func (r RoleSessions) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)`
- `func (r RoleSessions) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)`
- `func (r RoleSessions) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error`
- `func (r RoleSessions) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)`
- `func (r RoleSessions) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)`
- `func (r RoleSessions) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)`
- `func (r RoleSessions) SetLifecycleOrder(sessionID, policy string, roles []string) error`
- `func (r RoleSessions) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error`
- `func (r RoleSessions) ListRoleSessions(mainSessionID string) ([]string, error)`
- `func (r RoleSessions) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (r RoleSessions) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error`
- `func (r RoleSessions) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error`

### roles_test.go

- `func (port *fakeRolePort) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)`
- `func (port *fakeRolePort) AppendRoleDraft(string, string, string, []dto.RoleDraftRow) error`
- `func (port *fakeRolePort) ReadRoleDraft(string, string, string) ([]dto.RoleDraftRow, error)`
- `func (port *fakeRolePort) SyncRoleDraft(string, string, string, []string) (dto.RoleDraftSyncResult, error)`
- `func (port *fakeRolePort) AppendRoleSessionRows(string, string, string, []dto.RoleRow) error`
- `func (port *fakeRolePort) ReadRoleSessionRows(string, string, string) ([]dto.RoleRow, error)`
- `func (port *fakeRolePort) RoleSnapshot(string, string, string) (dto.RoleSnapshot, error)`
- `func (port *fakeRolePort) AssembleRoleWire(string, string, string, int, int) (dto.RoleWireSnapshot, error)`
- `func (port *fakeRolePort) SetLifecycleOrder(string, string, []string) error`
- `func (port *fakeRolePort) SetRoleLifecycle(string, string, string, uint64, *dto.CompactFrameRef) error`
- `func (port *fakeRolePort) ListRoleSessions(string) ([]string, error)`
- `func (port *fakeSchedulePort) ScheduleRegister(string, dto.ScheduleEventPayload) error`
- `func (port *fakeSchedulePort) ScheduleCancel(string, dto.ScheduleEventPayload) error`
- `func (port *fakeSchedulePort) ScheduleFire(sessionID string, _ dto.ScheduleEventPayload) error`
- `func TestRoleSessionsPortErrors(t *testing.T)` — TestRoleSessionsPortErrors：三种"没有可用端口"的情形各自给固定文案（判据只此一份）。
- `func TestRoleSessionsForwards(t *testing.T)` — TestRoleSessionsForwards：解析成功后原样转发（角色面与定时面各一条）。
