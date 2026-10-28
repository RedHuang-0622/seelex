# assembly/command（根包分卷）

## 生态位

内置命令注册与执行

覆盖：`command*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

AP-115D1/D2 起，基础命令与 mutation 命令的注册、参数和结果适配由
[`application/input/command`](../input/command/README.md) 唯一拥有；assembly 只保留
`registerBuiltinCommands` host callback 装配。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### command.go

- `func (service *Service) registerBuiltinCommands() error` — registerBuiltinCommands installs the extracted command application with the
