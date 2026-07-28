# Workspace Repository

## 模块定位

`workspace` 管理 Seelex 项目定义及 session-to-workspace binding。项目的作用是限制 session 的文件读写范围；conversation history 仍由 sessionstore 独立保存。

## 数据模型

- `Info`：opaque unique `ID`、持久化名称、absolute `RootPath`、Git remote 和创建时间。
- `SessionBinding`：session ID 到 workspace ID 的一对一绑定。
- `repoSnapshot`：`workspace_index.json` 中的 workspaces + bindings。

对外适配器会把 `RootPath` basename 作为显示名称；持久化 `Name` 只作为兼容 fallback。名称允许重复，ID 才是唯一索引。

## 生命周期

- `NewRepo`：内存模式。
- `NewRepoWithStore`：加载 `<store>/workspace_index.json`。
- Create 校验目录、转 absolute path、按 root 去重并生成唯一 ID。
- mutation 自动保存 index；Delete 同时清理指向该 workspace 的 bindings。
- `DetectGitRemote` 只读取 `git remote -v` 的 origin。

## 生态位与边界

Repo 保存项目目录和关系，但不执行 PathGate、文件工具或 session history IO。Runtime 的 ProjectScope 由 Application 在 bind/resume 时同步。

## Review 指南

- 不以显示名称查找或覆盖项目。
- root dedup 应处理 clean/absolute/case/volume 语义，跨平台时特别注意 Windows 大小写。
- index 写入目前是直接文件写；若提升可靠性，应改为原子 temp+rename 并补崩溃测试。
- binding mutation 与持久化错误当前部分为 best-effort，调用方可见性需明确。
- Git remote 命令必须只读且有退出边界。

## 测试

```text
go test . -run Workspace -count=1
go test ./application/core -run Workspace -count=1
```
