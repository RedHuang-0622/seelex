# application/policy

## 生态位

`application/policy` 承载角色账号、权限向量、继承规则、SSH 路径白名单以及
permission/effort/plugin 切换用例。主要调用方是 `application/core` 的兼容
Service、GUI Bridge 和会话装配路径。

本包不拥有配置事实：会话级登记、项目级角色默认、Runtime 账号路由和五层权限
来源分别由注入端口实现持有。

## 职责

- 集中角色设置输入规整、缺省继承规则和登记优先判据。
- 在持久化失败时回滚 Runtime 角色账号。
- 保证会话登记即使为空集也压过五层来源。
- 为旧 Service 提供可逐方法迁移的窄应用组件。
- 固定 permission tier、Effort 与 Plugin 切换的应用顺序。

## 非职责

- 不缓存第二份权限向量或角色账号。
- 不解析五层优先级；该判据只属于 `contract/dto`。
- 不直接访问 SessionStore、Runtime、配置文件或 GUI。
- 不导入 `application/core`，也不持有完整 `Service`。

## 文件结构

- `application.go`：窄端口、`Deps`、`RoleApplication` 与 14 个角色策略用例。
- `switch.go`：`SwitchDeps`、`SwitchApplication` 与策略切换。
- `skill_activation.go`：`SkillActivationPort` 消费方契约。
- `application_test.go`：登记优先、显式空集、账号回滚、继承和路径归一回归。

## 核心实现

`RoleApplication` 通过四个调用时访问器取得：

- `RoleDefaultsPort`：项目作用域角色默认、账号、继承和 SSH 路径；
- `PermissionRegistryPort`：会话级显式权限登记；
- `PermissionLayerPort`：登记缺失时使用的五层输入；
- `contract.AccountRoleSelectionPort`：角色账号的 Runtime 热路由。

`RestoreRolePermissions` 与 `RolePermissionVector` 共用
`resolveRoleSettings`，保证“登记优先、缺失才解析”只有一份实现。

Skill 激活判据仍由 `backend/engine/skill.Activator` 唯一实现；Application
只持有 `SkillActivationPort`，由 Wiring adapter 注入。

## 数据流

```text
Service compat -> application/policy.RoleApplication -> injected ports
                                                        ├─ Session adapter
                                                        ├─ Runtime account router
                                                        └─ contract/dto resolver
```

## 依赖方向

允许依赖 `contract`、`contract/dto`、`middle/protocol/model` 与
`middle/view/state` 的只读投影形状。禁止依赖 `application/core`、具体
`backend/**` 实现、`gui/**` 或 `tui/**`。

## 并发与错误

- 组件自身无共享可变状态，不新增锁。
- 端口由调用时解析，支持装配后替换和测试 fake。
- 登记缺失显式返回 `ErrRolePermissionsUnavailable`，不静默授予空权限。
- SSH 路径在写入前完成长度、绝对 POSIX 路径、根路径与重复项校验。

## 扩展方式

新增角色策略用例时扩展 `Deps` 中真实需要的端口和方法。若底层规则已存在于
`contract/dto` 或宿主端口，不得在包内复制。

## Review 指南

- 检查显式空登记是否仍压过低层来源。
- 检查 Runtime 账号更新失败或落盘失败时是否保持旧路由。
- 检查继承角色是否读取 mainagent 的 SSH 路径范围。
- 检查本包未反向导入 core 或具体基础设施。

## 测试与验证

```text
go test ./application/policy/... -count=1
go test ./application/core -run Role -count=1
go test ./application/... -count=1
go test -race ./application/... -count=1
```
