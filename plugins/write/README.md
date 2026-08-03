# Write Plugin

`write` 是代码与文件编辑形态，组合 read、write/edit、必要 shell 和 Git diff/status，用于在已绑定项目范围内完成实现。

## 数据流与依赖

manifest 只决定工具可见性；具体路径解析由 `seelebridge.ProjectScope`，业务提交由 Application，文件副作用由 Seele tool handler 完成。Write Plugin 不维护会话或项目状态。

## 边界

- 所有路径仍由 project scope 限制；Plugin 可见性不能替代路径安全。
- bash 只作为实现和验证手段，不自动扩大到发布、push 或外部系统写操作。
- 写入后应检查 diff，并运行与风险匹配的测试。

## Review

重点检查 include glob 是否过宽、写工具是否统一经过 scoped wrapper、失败时是否留下部分文件状态。

## 验证

```text
go test ./plugin ./seelebridge . -run 'Plugin|Scoped|Layout' -count=1
```
