# 任务书：`workspace_usecase` 迁入 `middle/switch/workspace`

> **状态：已完成并提交 `2eec5f1f`。请勿重复认领。** 本文件保留为验收记录；
> 工作区用例现位于 `middle/switch/workspace/`，应用层公开方法只做一行转发。

## 你的任务

在 `refactor/architecture-reshape` 分支上，把
`application/core/workspace_usecase.go` 的工作区用例搬到
`middle/switch/workspace/`，应用层只保留薄转发；`middle` 不反向依赖 `application`。

## 当前事实

- 最近提交：`3d2a9ab7`。
- `application/core/workspace_usecase.go` 目前同时含：
  - 会话删除/释放：`DeleteSession`、`releaseSessionAsync`；
  - 工作区用例：`CreateWorkspace`、`BindWorkspace`、`bindWorkspaceInfo`、
    `UnbindWorkspace`、`WorkspaceTree`、`WorkspaceFileCount`、`WorkspaceGitLog`、
    `WorkspaceChanges`、`WorkspaceGitCommitDetail`、`WorkspaceFileContent`、
    `WorkspaceGitCommitFileContent`、`WorkspaceWriteFile`；
  - 端口适配：`workspaceTreePort` / `workspaceFilePort` / `workspaceRootPort`；
  - 投影：`workspaceStateProjection` + `collectWorkspaceProjection` +
    `applyWorkspaceProjectionLocked`。
- `session/` 已迁入 `middle/switch/sessionusecase/`；`session.Manager` 已是
  `wiring/adapters.SessionManager`。
- `middle/switch/workspace` 目录尚不存在。

## 目标形态

1. 新建 `middle/switch/workspace/`，配 `README.md`（按 AGENTS.md 十项 + `## 生态位`）。
2. 只把**工作区用例**搬进去：
   - `UseCase` 持有 `Deps`：会话目录/绑定、工作区存储、运行时端口、视图投影端口；
   - 所有跨层依赖用窄接口/函数值，不 import `application/core`、不 import `store.Router`
     具体类型、不 import `wiring`。
3. `DeleteSession` / `releaseSessionAsync` 留在 `application/core`（会话域），不要带进
   workspace 包；如果它们与工作区绑定有共享判据，抽成端口，不要复制。
4. `application/core` 的公开方法名与签名保持不变，内部改为一行转发。
5. 工作区投影的 owner 仍是应用视图；`middle` 只产出/更新它被注入的窄投影面。

## 必须遵守

- 先读 `MEMORY.md`、`application/core/README*.md`、`middle/switch/README.md`、
  `store/workspace/README.md`。
- 先写/迁移测试，再搬实现；测试随实现走，仓库级集成用例留在 `test/`。
- 不改变持久化格式、路径根、绑定键与用户可见文案。
- 不要动 NodeScope/SceneScope 的改名工作。
- 一个可解释的切片一个 commit；不要 push、不要动主分支。

## 验证

```text
go build ./...
go vet ./...
go test ./application/core/ ./middle/switch/... ./store/workspace/ ./test/ -count=1 -timeout 900s
python scripts/gen_core_readme_index.py --check
python scripts/check_readme_refs.py --strict
```

交付时列出：搬走的文件/函数、保留在 application 的会话面、端口清单、删除的重复实现。

> **状态：已完成并提交 `2eec5f1f`（记录 `213f9517`）。请勿重复认领。**
