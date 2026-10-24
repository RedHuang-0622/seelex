# application/core 权限分卷

## 生态位

本卷覆盖角色默认权限、会话覆盖、角色继承与 SSH 用户空间路径范围。调用方是 GUI
角色权限板、会话装配与工具路径门；它不承载存储实现，持久化落在 `store`，执行判定
落在 `backend/tool/registry`。

## 核心约束

- 账户没有独立权限向量，只继承所属 agent role。
- subagent / teammate 的默认继承只作用于角色默认层；当前会话仍可为任一角色登记覆盖。
- 开启继承时，`ssh.userspace` 路径范围随 mainagent 一起继承；关闭后才读自身路径。
- 写入侧显式校验权限标签、远端绝对路径、路径数量并去重，错误不回滚成“没设置”。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。

### role_permissions.go

- `func (service *Service) RestoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error)` — 在会话加载点为多个角色恢复权限登记。
- `func (service *Service) RolePermissionVector(sessionID, role string) ([]string, error)` — 读取角色生效权限向量，登记优先，缺失走五级链路。
- `func (service *Service) SetRolePermissionVector(sessionID, role string, vector []string) error` — 写入角色在指定会话的覆盖向量。
- `func (service *Service) RoleDefaultVector(sessionID, role string) ([]string, bool, error)` — 读取角色默认向量并报告是否显式设置。
- `func (service *Service) SetRoleDefaultVector(sessionID, role string, vector []string) error` — 写入跨会话的角色默认向量。
- `func (service *Service) RoleAccount(sessionID, role string) (string, bool, error)` — 读取角色的优先账号。
- `func (service *Service) SetRoleAccount(sessionID, role, account string) error` — 校验账号归属后写入角色优先账号并热推运行时。
- `func (service *Service) RoleInheritMainAgent(sessionID, role string) (bool, error)` — 读取继承开关；未设置时 subagent / teammate 默认继承。
- `func (service *Service) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error` — 只允许设置 subagent / teammate 的继承开关。
- `func (service *Service) RoleSSHUserSpaces(sessionID, role string) ([]string, error)` — 读取继承后生效的 SSH 用户空间路径范围。
- `func (service *Service) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error` — 校验并写入角色的 SSH 用户空间路径。

### role_permissions_test.go

- `func TestRolePermissionVectorSingleRole(t *testing.T)` — 单角色读取和写入登记的一致性。
- `func TestRoleInheritanceDefaultsAndUserSpaceNormalization(t *testing.T)` — 继承默认与路径规范化。
- `func TestInheritedRoleUsesMainAgentSSHUserSpaces(t *testing.T)` — 继承态读取 mainagent 路径，关闭后读自身覆盖。
