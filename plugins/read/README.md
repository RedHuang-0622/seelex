# Read Plugin

`read` 提供项目内只读检索、文件读取和 Git 分析形态，用于审查、调研和定位问题。

允许的主要能力由 `plugin.md` 声明：grep、read_file、glob、git status/log/diff、时间和 plugin 切换。

## 数据流与依赖

`plugin.Loader` 解析 manifest，`plugin.Manager` 激活 Tool filter；实际文件和 Git 工具由 `seelebridge` scoped wrappers 执行。Read Plugin 自身不保存状态。

## 边界

- 不应暴露 write/edit/bash 等可修改项目状态的工具。
- “只读”仍必须经过当前 project root 的 `ProjectScope`/`PathGate`。
- Git 命令只用于观察，不执行 checkout、reset、commit 或 push。

## Review

修改 include 时确认 wildcard 不会意外覆盖写工具；使用实际 `VisibleTools` 测试验证，而不是只看 manifest 文本。

## 验证

```text
go test ./plugin ./seelebridge . -run 'Plugin|Scoped|Layout' -count=1
```
