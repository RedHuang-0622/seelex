# core/command（根包分卷）

## 生态位

内置命令注册与执行

覆盖：`command*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### command.go

- `func (service *Service) registerBuiltinCommands() error`

### command_registry_test.go

- `func TestCommandRegistry_RegisterAndGet(t *testing.T)`
- `func TestCommandRegistry_RegisterDuplicate(t *testing.T)`
- `func TestCommandRegistry_RegisterEmptyName(t *testing.T)`
- `func TestCommandRegistry_GetNotFound(t *testing.T)`
- `func TestCommandRegistry_All(t *testing.T)`
- `func TestCommandRegistry_AllEmpty(t *testing.T)`
- `func lastNotice(t *testing.T, svc *Service) string`

### permission_command_test.go

- `func permissionCommandFor(t *testing.T, service *Service) interface`
- `func TestPermissionCommandRegisteredInHelp(t *testing.T)` — TestPermissionCommandRegisteredInHelp：命令在册，且 /help 能列出来。
- `func TestPermissionCommandWithoutArgsShowsCurrentAndCatalog(t *testing.T)` — TestPermissionCommandWithoutArgsShowsCurrentAndCatalog：无参回显当前档位 + 档位目录
- `func TestPermissionCommandSwitchesTier(t *testing.T)` — TestPermissionCommandSwitchesTier：带参切档走 SetPermissionTier（会话槽 + 快照），
