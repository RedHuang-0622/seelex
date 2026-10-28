# application/policy

## 生态位

`application/policy` 承载角色账号、权限向量、继承规则、SSH 路径白名单以及
permission/effort/plugin 切换用例。主要调用方是 `application/assembly` 的兼容
Service、GUI Bridge 和会话装配路径。

本包不拥有配置事实：会话级登记、项目级角色默认、Runtime 账号路由和五层权限
来源分别由注入端口实现持有。

## 职责

- 集中角色设置输入规整、缺省继承规则和登记优先判据。
- 在持久化失败时回滚 Runtime 角色账号。
- 保证会话登记即使为空集也压过五层来源。
- 为旧 Service 提供可逐方法迁移的窄应用组件。
- 固定 permission tier、Effort 与 Plugin 切换的应用顺序。
- 以 identity alias 暴露 Backend tier 的 `Deps/Gate`，供 core permission-tier host 装配。
- 以 identity alias/forwarder 暴露 Backend limits、Knobs 与 release-window 入口。

## 非职责

- 不缓存第二份权限向量或角色账号。
- 不解析五层优先级；该判据只属于 `contract/dto`。
- 不直接访问 SessionStore、Runtime、配置文件或 GUI。
- 不导入 `application/assembly`，也不持有完整 `Service`。

## 文件结构

- `application.go`：窄端口、`Deps`、`RoleApplication` 与 14 个角色策略用例。
- `switch.go`：`SwitchDeps`、`SwitchApplication` 与策略切换。
- `skill_activation.go`：`SkillActivationPort` 消费方契约。
- `tier_facade.go`：`TierDeps` / `TierGate` identity alias。
- `limits_facade.go`：Limits/Knobs/KnobsHost alias 与基座函数转发。
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
Tier facade 不实现档位解析或持久化；这些判据仍只在 `kernel/limit/tier`。
Limits facade 不复制默认表、Knobs 状态或窗口边界，所有语义仍只在 `kernel/limit`。

## 数据流

```text
Service compat -> application/policy.RoleApplication -> injected ports
                                                        ├─ Session adapter
                                                        ├─ Runtime account router
                                                        └─ contract/dto resolver
```

## 依赖方向

允许依赖 `contract`、`contract/dto`、`contract/protocol` 与
`application/session/viewstate` 的只读投影形状。禁止依赖 `application/assembly`、具体
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
go test ./application/assembly -run Role -count=1
go test ./application/... -count=1
go test -race ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### application.go

- `func NewRoleApplication(deps Deps) *RoleApplication` — NewRoleApplication constructs the role policy application.
- `func (application *RoleApplication) RoleDefaultVector(sessionID, role string) ([]string, bool, error)` — RoleDefaultVector reads a role's explicit default vector.
- `func (application *RoleApplication) SetRoleDefaultVector(sessionID, role string, vector []string) error` — SetRoleDefaultVector validates and writes a role's default vector without
- `func (application *RoleApplication) RoleAccount(sessionID, role string) (string, bool, error)` — RoleAccount reads a role's explicit default account.
- `func (application *RoleApplication) SetRoleAccount(sessionID, role, account string) error` — SetRoleAccount applies a role account to the runtime before persisting it
- `func (application *RoleApplication) RestoreRoleAccounts(sessionID string) error` — RestoreRoleAccounts applies all explicitly stored role accounts to the
- `func (application *RoleApplication) RoleInheritMainAgent(sessionID, role string) (bool, error)` — RoleInheritMainAgent reports whether a role inherits mainagent settings.
- `func (application *RoleApplication) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error` — SetRoleInheritMainAgent updates inheritance for subagent or teammate only.
- `func (application *RoleApplication) RoleSSHUserSpaces(sessionID, role string) ([]string, error)` — RoleSSHUserSpaces reads the effective SSH path scope for a role. Inherited
- `func (application *RoleApplication) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)` — RoleDefaultSSHUserSpaces reads a role's cross-session default SSH paths.
- `func (application *RoleApplication) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error` — SetRoleSSHUserSpaces writes normalized default SSH paths for a role.
- `func (application *RoleApplication) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error` — SetRoleSessionSSHUserSpaces writes a normalized session-local SSH path
- `func (application *RoleApplication) RestoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error)` — RestoreRolePermissions restores effective settings for the requested roles:
- `func (application *RoleApplication) RolePermissionVector(sessionID, role string) ([]string, error)` — RolePermissionVector reads one effective permission vector.
- `func (application *RoleApplication) SetRolePermissionVector(sessionID, role string, vector []string) error` — SetRolePermissionVector validates and writes a session-local role vector.
- `func (application *RoleApplication) resolveRoleSettings( sessionID, role string, storedVector []string, stored bool, resolveLayerMetadata bool, ) dto.RoleSettings`
- `func (application *RoleApplication) roleDefaults() RoleDefaultsPort`
- `func (application *RoleApplication) runtime() contract.AccountRoleSelectionPort`
- `func (application *RoleApplication) registry() PermissionRegistryPort`
- `func (application *RoleApplication) layers() PermissionLayerPort`
- `func normalizeRemoteUserSpaces(paths []string) ([]string, error)`

### application_test.go

- `func TestRoleApplicationPrefersExplicitStoredVector(t *testing.T)`
- `func TestRoleApplicationFallsBackOnlyWhenUnregistered(t *testing.T)`
- `func TestRoleApplicationRollsBackRuntimeAccountOnPersistenceFailure(t *testing.T)`
- `func TestRoleApplicationInheritanceAndRemoteUserSpaces(t *testing.T)`
- `func TestRoleApplicationFailsLoudlyWithoutRegistry(t *testing.T)`
- `func (fake *roleDefaultsFake) RoleDefault(string, string) ([]string, bool, error)`
- `func (fake *roleDefaultsFake) SetRoleDefault(string, string, []string) error`
- `func (fake *roleDefaultsFake) RoleAccount(sessionID, role string) (string, bool, error)`
- `func (fake *roleDefaultsFake) SetRoleAccount(sessionID, role, account string) error`
- `func (fake *roleDefaultsFake) RoleInheritance(sessionID, role string) (bool, bool, error)`
- `func (fake *roleDefaultsFake) SetRoleInheritance(string, string, bool) error`
- `func (fake *roleDefaultsFake) RoleSSHUserSpaces(_, role string) ([]string, error)`
- `func (fake *roleDefaultsFake) RoleDefaultSSHUserSpaces(string, string) ([]string, error)`
- `func (fake *roleDefaultsFake) SetRoleSSHUserSpaces(_, _ string, paths []string) error`
- `func (fake *roleDefaultsFake) SetRoleSessionSSHUserSpaces(_, _ string, paths []string) error`
- `func (fake *permissionRegistryFake) RolePermissionVectors(string) (map[string][]string, error)`
- `func (fake *permissionRegistryFake) RolePermissionVector(_ string, role string) ([]string, bool, error)`
- `func (fake *permissionRegistryFake) SetRolePermissionVector(string, string, []string) error`
- `func (fake permissionLayersFake) RolePermissionLayers(string, string) dto.RoleSettingsLayers`
- `func (fake *runtimeAccountFake) SetRoleAccount(role, account string) error`

### limits_facade.go

- `func Apply(l Limits)`
- `func Get() Limits`
- `func ValidateSessionReleaseWindow(window int) error`
- `func SetSessionReleaseWindow(window int)`
- `func ApplySessionReleaseWindow(window int) (int, error)` — ApplySessionReleaseWindow validates first, then changes the process limit.
- `func NewKnobs(host KnobsHost) *Knobs`

### limits_facade_test.go

- `func TestLimitsFacadeTypesAreBackendIdentity(t *testing.T)`
- `func TestApplySessionReleaseWindowValidatesBeforeMutation(t *testing.T)`

### skill_activation_boundary_test.go

- `func TestCoreDoesNotImportSkillActivator(t *testing.T)`
- `func policyRepositoryRoot(t *testing.T) string`

### switch.go

- `func NewSwitch(deps SwitchDeps) *SwitchApplication` — NewSwitch constructs the policy switching application.
- `func (application *SwitchApplication) ActivateSkill(name string) (model.SkillInfo, error)` — ActivateSkill activates one Skill.
- `func (application *SwitchApplication) SetReasoningEffort(effort string) error` — SetReasoningEffort changes the reasoning effort.
- `func (application *SwitchApplication) SetLoopLimit(limit int) error` — SetLoopLimit changes the loop limit.
- `func (application *SwitchApplication) SetSessionReleaseWindow(window int) (int, error)` — SetSessionReleaseWindow changes the session release window.
- `func (application *SwitchApplication) SessionReleaseWindow() int` — SessionReleaseWindow reads the session release window.
- `func (application *SwitchApplication) SetFullAccess(on bool) bool` — SetFullAccess maps full-access state to the permission tier.
- `func (application *SwitchApplication) SetPermissionTier(tier string) (string, error)` — SetPermissionTier persists and applies one permission tier.
- `func (application *SwitchApplication) SwitchEffort(level string) error` — SwitchEffort applies a selected or cycled effort level.
- `func (application *SwitchApplication) SwitchPlugin(ctx context.Context, name string) error` — SwitchPlugin activates/deactivates one process plugin and rebuilds history.

### switch_external_test.go

- `func TestPolicyApplicationDelegatesPolicyOperations(t *testing.T)`
- `func TestPolicyApplicationFullAccessError(t *testing.T)`
- `func TestPolicyApplicationSetPermissionTierFullPath(t *testing.T)`
- `func TestPolicyApplicationSetPermissionTierStopsOnPersistAndRuntimeErrors(t *testing.T)`
- `func TestPolicyApplicationSetPermissionTierDoesNotResolveNonFull(t *testing.T)`
- `func TestPolicyApplicationSwitchEffortRunsFullPath(t *testing.T)`
- `func TestPolicyApplicationSwitchEffortStopsOnIdleCycleAndApplyErrors(t *testing.T)`
- `func TestPolicyApplicationSwitchPluginActivatesAndRebuilds(t *testing.T)`
- `func TestPolicyApplicationSwitchPluginOffAndErrors(t *testing.T)`
- `func boolWord(value bool) string`

### tier_facade_test.go

- `func TestTierFacadeTypesAreBackendIdentity(t *testing.T)`
