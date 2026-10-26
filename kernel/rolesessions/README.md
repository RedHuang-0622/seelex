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

- `type RoleSessions struct` — 聚合角色会话与调度端口的窄门面。
- `func (r RoleSessions) RolePort() (contract.RoleSessionPort, error)` — 解析角色会话端口。
- `func (r RoleSessions) SchedulePort() (contract.SchedulePort, error)` — 解析调度端口。
- `func (r RoleSessions) CreateRoleSession(...)` — 转发创建角色会话。
- `func (r RoleSessions) ScheduleRegister(...)` — 转发注册定时事件。
