# Shell Plugin

`shell` 提供构建、诊断和 DevOps 命令执行形态。当前核心能力是 `bash`/shell tool 与时间、plugin 切换。

## 依赖与状态

Shell Plugin 是纯声明模块，不持有进程表。命令执行、timeout 和取消由 Seele tool/runtime 负责，工作目录限制由 `seelebridge.ProjectScope` 提供。

## 边界

- 命令工作目录必须落在绑定 project scope，或由用户明确指定的安全范围。
- Shell 可见不代表允许破坏性操作；删除、覆盖、发布和外部系统写入仍遵循审批与用户授权。
- 跨平台命令应区分 PowerShell、POSIX shell 和 build tags。

## Review

检查参数转义、环境变量、超时、后台进程和退出清理，避免命令注入与进程泄漏。

## 验证

```text
go test ./plugin ./seelebridge . -run 'Plugin|Runtime|Layout' -count=1
```
