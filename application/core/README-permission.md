# core/permission（根包分卷）

## 生态位

角色默认/会话覆盖的权限向量、账号选择、继承与 SSH 路径白名单

覆盖：`role_permissions*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 核心约束

- 账户没有独立权限向量，只继承所属 agent role。
- 角色账号的读取、运行时热推、失败回滚和会话加载恢复由
  `roleAccountApplication` 集中；存储与账号归属校验仍由既有端口负责。
- 角色默认向量、会话登记和五层来源解析由 `rolePermissionApplication` 集中；
  `resolveRoleSettings` 是“登记优先、显式空集不回退、缺失才解析”的唯一判据。
- 继承开关与 SSH 路径范围由 `roleInheritanceApplication` 集中；Role Permissions
  文件的 14 个公开入口都只做一行组件转发。
- subagent / teammate 的默认继承只作用于角色默认层；当前会话仍可为任一角色登记覆盖。
- SSH 只有 `ssh` 一枚能力；其路径白名单随 agent role 的继承关系一起继承，关闭继承后
  才读自身路径。
- 角色默认路径按项目作用域保存；当前会话可为每个 agent role 写独立覆盖，未覆盖时
  回落角色默认。运行期按会话 + agent role 读取生效路径。
- 写入侧显式校验权限标签、远端绝对路径、路径数量并去重，错误不回滚成“没设置”。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### policy_test_aliases_test.go

- `func newPolicyApplication(deps policyApplicationDeps) *policyApplication`

### role_permissions_test.go

- `func newRolePermissionFakeSessions() *rolePermissionFakeSessions`
- `func (fake *rolePermissionFakeSessions) RolePermissionVectors(sessionID string) (map[string][]string, error)`
- `func (fake *rolePermissionFakeSessions) RolePermissionVector(sessionID, role string) ([]string, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRolePermissionVector(sessionID, role string, vector []string) error`
- `func (fake *rolePermissionFakeSessions) RolePermissionLayers(sessionID, role string) dto.RoleSettingsLayers`
- `func (fake *rolePermissionFakeSessions) RoleDefaultVectors(sessionID string) (map[string][]string, error)`
- `func (fake *rolePermissionFakeSessions) RoleDefault(sessionID, role string) ([]string, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleDefault(sessionID, role string, vector []string) error`
- `func (fake *rolePermissionFakeSessions) RoleAccount(sessionID, role string) (string, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleAccount(sessionID, role, account string) error`
- `func (fake *rolePermissionFakeSessions) RoleInheritance(sessionID, role string) (bool, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleInheritance(sessionID, role string, inherit bool) error`
- `func (fake *rolePermissionFakeSessions) RoleSSHUserSpaces(sessionID, role string) ([]string, error)`
- `func (fake *rolePermissionFakeSessions) RoleDefaultSSHUserSpaces(_ string, role string) ([]string, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error`
- `func (fake *rolePermissionFakeSessions) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error`
- `func fourLayerFixture() dto.RoleSettingsLayers` — fourLayerFixture 是五层中的四层配了一遍、且**互相冲突**的一组来源：
- `func TestRestoreRolePermissionsPrefersStoredVector(t *testing.T)`
- `func TestRestoreRolePermissionsExplicitEmptyVectorWins(t *testing.T)` — TestRestoreRolePermissionsExplicitEmptyVectorWins 是三态纪律的核心用例：
- `func TestRestoreRolePermissionsWithoutRegistryFailsLoudly(t *testing.T)`
- `func TestRolePermissionVectorSingleRole(t *testing.T)`
- `func TestRoleDefaultVectorIsExplicitNotEffective(t *testing.T)`
- `func TestRoleAccountRoundTrip(t *testing.T)`
- `func (spy *roleAccountRuntimeSpy) SetRoleAccount(role, account string) error`
- `func TestRoleAccountPushesToRuntimeAndRestores(t *testing.T)`
- `func TestRoleInheritanceDefaultsAndUserSpaceNormalization(t *testing.T)`
- `func TestInheritedRoleUsesMainAgentSSHUserSpaces(t *testing.T)`
- `func TestSessionSSHUserSpacesOverrideDefaultAndFollowInheritance(t *testing.T)`
- `func TestSetRolePermissionVectorRejectsMalformedEntries(t *testing.T)`
- `func TestRestoreRolePermissionsConcurrentRoles(t *testing.T)` — TestRestoreRolePermissionsConcurrentRoles 让多个角色同时恢复：判据无共享可变
- `func TestRoleApplicationIsAssembled(t *testing.T)`
- `func TestRoleAccountApplicationSetAccountRollsBackRuntime(t *testing.T)`
- `func TestRoleAccountApplicationRestoreSkipsExplicitlyMissingAccount(t *testing.T)`
- `func TestRoleAccountFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestRolePermissionFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestRoleInheritanceFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func (store *roleAccountApplicationFakeStore) RoleAccount(sessionID, role string) (string, bool, error)`
- `func (store *roleAccountApplicationFakeStore) SetRoleAccount(sessionID, role, account string) error`
- `func (runtime *roleAccountApplicationFakeRuntime) SetRoleAccount(role, account string) error`
