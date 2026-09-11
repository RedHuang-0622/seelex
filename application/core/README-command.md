# core/command（根包分卷）

## 生态位

内置命令注册与执行

覆盖：`command*.go`；未归属文件由覆盖自检拦下。

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
