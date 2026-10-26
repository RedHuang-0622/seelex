# application/core 权限分卷

## 生态位

本卷覆盖角色默认权限、会话覆盖、角色继承与 SSH 路径白名单。调用方是 GUI
角色权限板、会话装配与工具路径门；它不承载存储实现，持久化落在 `store`，执行判定
落在 `backend/tool/registry`。

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

### role_permissions.go

- `func newRoleAccountApplication(deps roleAccountApplicationDeps) *roleAccountApplication`
- `func (application *roleAccountApplication) RoleAccount(sessionID, role string) (string, bool, error)`
- `func (application *roleAccountApplication) SetRoleAccount(sessionID, role, account string) error`
- `func (application *roleAccountApplication) RestoreRoleAccounts(sessionID string) error`
- `func (application *roleAccountApplication) roleDefaults() roleDefaultPort`
- `func (application *roleAccountApplication) runtime() contract.AccountRoleSelectionPort`
- `func (service *Service) roleAccountApplicationComponent() *roleAccountApplication`
- `func newServiceRoleAccountApplication(service *Service) *roleAccountApplication`
- `func newRolePermissionApplication(deps rolePermissionApplicationDeps) *rolePermissionApplication`
- `func (application *rolePermissionApplication) RoleDefaultVector(sessionID, role string) ([]string, bool, error)`
- `func (application *rolePermissionApplication) SetRoleDefaultVector(sessionID, role string, vector []string) error`
- `func (application *rolePermissionApplication) RestoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error)`
- `func (application *rolePermissionApplication) resolveRoleSettings(sessionID, role string, storedVector []string, stored bool, resolveLayerMetadata bool) dto.RoleSettings`
- `func (application *rolePermissionApplication) RolePermissionVector(sessionID, role string) ([]string, error)`
- `func (application *rolePermissionApplication) SetRolePermissionVector(sessionID, role string, vector []string) error`
- `func (service *Service) rolePermissionApplicationComponent() *rolePermissionApplication`
- `func newServiceRolePermissionApplication(service *Service) *rolePermissionApplication`
- `func newRoleInheritanceApplication(deps roleInheritanceApplicationDeps) *roleInheritanceApplication`
- `func (application *roleInheritanceApplication) RoleInheritMainAgent(sessionID, role string) (bool, error)`
- `func (application *roleInheritanceApplication) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error`
- `func (application *roleInheritanceApplication) RoleSSHUserSpaces(sessionID, role string) ([]string, error)`
- `func (application *roleInheritanceApplication) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)`
- `func (application *roleInheritanceApplication) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error`
- `func (application *roleInheritanceApplication) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error`
- `func (service *Service) roleInheritanceApplicationComponent() *roleInheritanceApplication`
- `func newServiceRoleInheritanceApplication(service *Service) *roleInheritanceApplication`
- `func (service *Service) RestoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error)` — 在会话加载点为多个角色恢复权限登记。
- `func (service *Service) RolePermissionVector(sessionID, role string) ([]string, error)` — 读取角色生效权限向量，登记优先，缺失走五级链路。
- `func (service *Service) SetRolePermissionVector(sessionID, role string, vector []string) error` — 写入角色在指定会话的覆盖向量。
- `func (service *Service) RoleDefaultVector(sessionID, role string) ([]string, bool, error)` — 读取角色默认向量并报告是否显式设置。
- `func (service *Service) SetRoleDefaultVector(sessionID, role string, vector []string) error` — 写入跨会话的角色默认向量。
- `func (service *Service) RoleAccount(sessionID, role string) (string, bool, error)` — 读取角色的优先账号。
- `func (service *Service) SetRoleAccount(sessionID, role, account string) error` — 校验账号归属后写入角色优先账号并热推运行时。
- `func (service *Service) RoleInheritMainAgent(sessionID, role string) (bool, error)` — 读取继承开关；未设置时 subagent / teammate 默认继承。
- `func (service *Service) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error` — 只允许设置 subagent / teammate 的继承开关。
- `func (service *Service) RoleSSHUserSpaces(sessionID, role string) ([]string, error)` — 读取继承后生效的 SSH 路径白名单。
- `func (service *Service) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)` — 读取角色跨会话默认的 SSH 路径白名单。
- `func (service *Service) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error` — 校验并写入 `ssh` 能力下的路径白名单。
- `func (service *Service) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error` — 校验并写入当前会话中该 agent role 的 SSH 路径覆盖。

### role_permissions_test.go

- `func TestRoleAccountApplicationIsAssembled(t *testing.T)`
- `func TestRolePermissionApplicationIsAssembled(t *testing.T)`
- `func TestRoleInheritanceApplicationIsAssembled(t *testing.T)`
- `func TestRoleAccountApplicationSetAccountRollsBackRuntime(t *testing.T)`
- `func TestRoleAccountApplicationRestoreSkipsExplicitlyMissingAccount(t *testing.T)`
- `func TestRoleAccountFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestRolePermissionFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestRoleInheritanceFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestRolePermissionVectorSingleRole(t *testing.T)` — 单角色读取和写入登记的一致性。
- `func TestRoleInheritanceDefaultsAndUserSpaceNormalization(t *testing.T)` — 继承默认与路径规范化。
- `func TestInheritedRoleUsesMainAgentSSHUserSpaces(t *testing.T)` — 继承态读取 mainagent 路径，关闭后读自身覆盖。
