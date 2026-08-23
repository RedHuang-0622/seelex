# Workspace Repository

## 模块定位

`workspace` 管理 Seelex 项目定义及 session-to-workspace binding。项目的作用是限制 session 的文件读写范围；conversation history 仍由 sessionstore 独立保存。

## 数据模型

- `Info`：opaque unique `ID`、持久化名称、absolute `RootPath`、Git remote 和创建时间。
- `SessionBinding`：session ID 到 workspace ID 的一对一绑定。
- `repoSnapshot`：`workspace_index.json` 中的 workspaces + bindings。
- `dto.TreeEntry`/`TreeListing`/`TreeCount`：工作树只读元数据 DTO（名称/相对
  路径/类型/大小/直接文件计数；绝不携带文件内容），定义在
  `application/contract/dto/tree.go`，由 `workspace/tree.go` 产出。

对外适配器会把 `RootPath` basename 作为显示名称；持久化 `Name` 只作为兼容 fallback。名称允许重复，ID 才是唯一索引。

## 生命周期

- `NewRepo`：内存模式。
- `NewRepoWithStore`：加载 `<store>/workspace_index.json`。
- Create 校验目录、转 absolute path、按 root 去重并生成唯一 ID。
- mutation 自动保存 index；持久化使用同目录临时文件、flush 和 rename 原子发布。Create/Delete/UpdateGitRemote 在保存失败时回滚内存状态；Delete 同时清理指向该 workspace 的 bindings。
- `DetectGitRemote` 只读取 `git remote -v` 的 origin。

## 工作树查询（tree.go）

- `ListTree(root, relPath, depth)`：列出根内某目录的子条目（dir-first 排序、
  单目录条目 ≤500、总读取预算 200k，超限置 `Truncated`）。客户端只允许相对
  路径，绝对路径与 `..` 逃逸直接拒绝（Windows 大小写不敏感 containment）。
- `CountFiles(root)`：递归统计文件/目录数（同一预算与忽略规则）。
- 默认忽略目录：`.git`、`.seelex`、`dist`、`node_modules`、`.venv`、`tmp`、
  `.idea`、`__pycache__`；敏感文件名 `accounts.yaml` 与 `*.local.yaml` 只
  元数据也不展示/统计（防真实账号配置暴露）。
- 遍历不跟随符号链接（防环、防逃逸）；权限/IO 错误目录按 best-effort 跳过。
- Repo 实现 `contract.WorkspaceTreePort`（optional 端口），Application 经
  类型断言启用；GUI Bridge 暴露 `WorkspaceTree`/`WorkspaceFileCount`。

## 生态位与边界

Repo 保存项目目录和关系，但不执行 PathGate、文件工具或 session history IO。Runtime 的 ProjectScope 由 Application 在 bind/resume 时同步。

## Review 指南

- 不以显示名称查找或覆盖项目。
- root dedup 应处理 clean/absolute/case/volume 语义，跨平台时特别注意 Windows 大小写。
- binding mutation 的公开接口目前没有 error 返回值，因此 Bind/Unbind 的持久化失败仍是 best-effort；如果这条契约升级，必须同步修改 Application port。
- Git remote 命令必须只读且有退出边界。

## 测试

```text
go test . -run Workspace -count=1
go test ./application/core -run Workspace -count=1
```

`tree_test.go` 覆盖排序/计数、忽略与敏感过滤、逃逸拒绝、预算截断与嵌套深度。
