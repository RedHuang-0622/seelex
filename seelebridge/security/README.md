# Security

`seelebridge/security` 承载项目作用域、路径门禁与命令执行隔离的安全边界：

- `project_scope.go`：`ProjectScope` 项目根 containment（fail-closed，无 fallback root）。
- `pathgate.go`：`PathGate` allow/ask/deny 权限规则（读取 `seele.yaml` permission 段）。
- `sandbox.go`：`CommandSandbox` shell 执行隔离端口（项目 cwd 门禁 + 凭据环境清洗 +
  超时，非 OS 级隔离）；`ScrubEnvironment`/`FileExists` 供根包命令路径复用。
- `command_windows.go`/`command_other.go`：`ConfigureHiddenCommand`（平台构建标签）。

被根包 `scoped_tools` / `runtime` / `scheduler` / `docker` / `worktree_manager` 消费；
不反向依赖 `seelebridge` 根包。根包 `security_aliases.go` 重导出公开类型保持 API 兼容。

## 验证

```text
go test ./seelebridge/security -count=1
```
