# Security

`seelebridge/security` 承载项目作用域与路径门禁的安全边界：

- `project_scope.go`：`ProjectScope` 项目根 containment（fail-closed，无 fallback root）。
- `pathgate.go`：`PathGate` allow/ask/deny 权限规则（读取 `seele.yaml` permission 段）。

被根包 `scoped_tools` / `runtime` 消费；不反向依赖 `seelebridge` 根包。

## 验证

```text
go test ./seelebridge/security -count=1
```
