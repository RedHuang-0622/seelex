# seelebridge/worktree — 子代理 worktree 生命周期域

## 模块定位

承载子代理节点的独立 worktree 生命周期：创建（Begin）→ 执行期间工作区隔离 → 收尾（Finish：变基 → 提交判定 → 合并审批 → merge → 清理）→ 释放（Release）。主要调用方：根包 `worktree.go` 门面、`node/` 域 `AgentNode.Run` 经 `Deps.Begin/Finish/ReleaseNodeWorktree`。

## 与其它域的关系

```text
node ──► worktree ──► security（项目根校验）
   │         │
   └──► 根包接线（NodeWorktreeInfoFor / begin/finish/release）
```

worktree 为 node 提供隔离工作区；生命周期由 node 编排、根包接线；失败/被拒
路径现场保留供恢复（NodeWorktreeInfo）。

## 职责与非职责

职责：

- git worktree 创建/清理与分支管理；
- 收尾失败时保留"工作区现场"（注册表不释放，供前端展示与人工恢复）；
- 合并审批门注入（`WorktreeManagerDeps.Gate`）、阶段事件（`Phase`）。

非职责：

- 不决定节点何时开/关 worktree（调用方 `node/` 域决定）；
- 不实现 git 命令本身之外的版本控制策略；
- 不落盘会话数据。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `worktree_manager.go` | `WorktreeManager`、`WorktreeManagerDeps`、`NodeWorktree`/`NodeWorktreeInfo`、`GitRunner`/`CleanupWorktree`/`ConflictFilesIn` |
| `worktree_manager_test.go` | fakeGit 驱动的生命周期单元测试 |
| `worktree_failure_smoke_test.go` | B1/B2/B4 收尾失败现场保留冒烟 |

## 核心实现

`WorktreeManager` 内部单锁保护 `nodeID → NodeWorktree` 注册表；git 执行经可注入的 `git` 字段（默认 `GitRunner`，测试替换为 fakeGit）。

`Finish` 流程：`branchBehindBase` → 落后则 rebase（冲突报错保留现场）→ `commitCountSince` 判定 → 有提交则 `approve`（审批门可拒）→ merge → `cleanup`；任一步失败返回可识别错误，调用方据此保留现场。

## 数据流或生命周期

`Begin(scope, nodeID)`（仅 RoleSubAgent；非 git 仓库降级共享工作区）→ `NodeWorktree` 注入 `NodeScope.WorkspaceID` → 节点执行 → `Finish` 成功则 `Release` 移除注册；失败路径注册表保留（`Info` 可查，`NodeWorktreeInfoFor` 暴露恢复入口）。

## 依赖方向

`worktree` → `security`（`ConfigureHiddenCommand`）、`internal/model`（`NodeScope`/角色）、`Seele approve`。**禁止反向依赖 seelebridge 根包**。

## 并发、存储、安全或错误语义

- 单锁 + git 子进程串行；60s git 超时防挂起；
- 失败保留现场是显式语义：`Release` 只由成功路径触发；
- 已知风险：`worktreeDirty` 用裸 `git status --porcelain`，Windows/WSL 下 `.gitattributes` 未覆盖文件的 CRLF 转换可能造成"幻影脏"（待修复方向：CRLF 不敏感判定）。

## 扩展方式

- 注入替代 git 执行器（测试/远程环境）；
- 通过 `WorktreeManagerDeps.Gate` 接入不同审批门；
- 调整收尾策略（rebase 前是否强推、冲突处理提示）改 `Finish`。

## Review 指南

- 失败路径是否必然保留现场（不能误清理）；
- 审批门拒绝/超时是否不会删除 worktree；
- CRLF 幻影脏是否会被误判为"脏未提交"而中断节点。

## 测试与验证

本包内：`worktree_manager_test.go`、`worktree_failure_smoke_test.go`（fakeGit，无真实 git 依赖）；真实 git 集成用例（根包 `worktree_test.go`）需本机 git，沙箱受限时按 skip 名单跳过。验证：

```text
go test ./seelebridge/worktree/ -count=1
go test ./seelebridge/... -count=1
```
