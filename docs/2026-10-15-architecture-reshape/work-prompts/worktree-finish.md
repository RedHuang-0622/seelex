# 任务书：worktree 收尾判据归位

> **状态：已完成并提交 `2c043125`。请勿重复认领。** 本文件保留为验收记录；
> 如后续要改，先读该提交与 `backend/workunit/merge_retry.go`。

## 完成记录（2026-10-17 认领会话）

### 完成项

- [x] `WorktreeManager.Finish` 只执行**一次**机械收尾：rebase → 提交/脏判定 → 一次 merge →
  cleanup；不再持有重试预算。
- [x] 挡路等待与重试上移到 `workunit.Host`：`MergeRetry`（默认 2 分钟 / 5 秒）+ 可选
  `awaiting_merge` 观察者；只有 `IsMergeBlockedByMain` 认的那一族才重试。
- [x] 现场适配器只交回事实与类型化错误：`ErrUncommittedChanges` /
  `ErrMergeBlockedByMain` / 真实 git 失败；收尾分类仍只有 `ClassifyFinish` 一份。
- [x] `WorktreeManagerDeps` 不再有审批门；`Phase` 保留为可选的机械阶段诊断观察者，
  生产装配不注入。
- [x] worktree 测试里的 `approveGateStub`（已无注入点的 plan 遗留替身）已删除。
- [x] 两份 README（`backend/workunit`、`backend/tool/worktree`）已按新边界更新；
  workunit 索引已用生成器刷新。

### 任务书更正

1. **「当前事实」是认领前快照，部分已过期**：`WorktreeManager.Finish` 里的
   `mergeRetryBudget` / `mergeRetryInterval`、`phaseAwaitingMerge` 与重试循环都已删除；
   现在读第 1 节应理解为“改造前的事实”。
2. **提交切分未按原计划**：核心实现被并发会话连同 `NodeScope → SceneScope` 改名一起
   提交为 `2c043125`（同一 commit 含两个主题）；本轮没有改写共享分支历史，只补了
   独立清理提交 `fecd891d`。后续若要求严格一行为一 commit，需要单独处理历史，不能
   在本分支上静默 rebase。

### 判据副本删除清单（旧位置 → 新唯一位置）

| 旧位置（`3d2a9ab7`） | 新唯一位置 |
|---|---|
| `backend/tool/worktree/worktree_manager.go:213-216` 重试默认常量 | `backend/workunit/merge_retry.go` 的 `defaultMergeRetry*` |
| `backend/tool/worktree/worktree_manager.go:217-218` `phaseAwaitingMerge` | `backend/workunit/merge_retry.go` 的 `MergePhaseAwaiting` |
| `backend/tool/worktree/worktree_manager.go:236-238`、`:254-255` 重试字段与初始化 | `workunit.Host.mergeRetry` + `WithMergeRetry` |
| `backend/tool/worktree/worktree_manager.go:519-541` `Finish` 重试循环 | `backend/workunit/merge_retry.go` 的 `Host.mergeScene` |
| `backend/tool/worktree/worktree_manager.go` 的 `finishExclusive` | 同一文件的 `finishOnce`（单次，不重试） |

### 钉住新边界的测试

- `backend/workunit/merge_retry_test.go`
  - 挡路两次后在预算内合上（3 次调用 + `awaiting_merge`）；
  - 预算耗尽仍返回 `ErrMergeBlockedByMain` 且带处置办法；
  - 等待期间取消立即收口；
  - 确定性冲突只尝试一次；
  - `Host.Finish` 与 `ClassifyFinish` 对同一合并事实给出同一分类。
- `backend/tool/worktree/worktree_merge_serial_test.go`
  - 现场适配器挡路时 `mergeCalls == 1`、现场保留；
  - 把真实 `WorktreeManager.Finish` 接成 `workunit.SceneFace`，验证同一个脚本化 git
    输入经 Host 重试后合上；
  - 单写者在挡路事实交回后立刻释放。

### 验收结果

| 命令 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./backend/workunit/ ./backend/tool/worktree/ ./backend/engine/assemble/ -count=1 -timeout 900s` | 通过（workunit 3.2s / worktree 93–135s / assemble 129s） |
| `python scripts/check_readme_refs.py --strict` | 通过（180 个 README，0 未解析引用） |
| `python scripts/gen_core_readme_index.py --only backend/workunit --check` | 通过 |
| `python scripts/gen_core_readme_index.py --check` | **全仓未通过**：`backend/limit/policy/README.md`、`contract/dto/README.md` 所属其他会话的漂移；本次未代改 |

worktree 侧 plan 专用字段：生产 `WorktreeManagerDeps` 已无审批门；仅保留可选的
`Phase` 机械阶段观察者（生产不注入）。

## 你的任务

在 `refactor/architecture-reshape` 分支上，把 `backend/tool/worktree` 从“第二份生命周期”
收成**只做 git 机械动作的现场适配器**。生命周期判据（是否算落定、未提交/挡路怎么分类、
是否重试、是否回收）继续只留在 `backend/workunit/lifecycle.go` 的 `Host`。

## 当前事实（先核对，不要凭记忆）

- 最近提交：`3d2a9ab7`（内部 Plan 引擎已删除）。
- `WorktreeManager.Finish` 仍在 `backend/tool/worktree/worktree_manager.go`，内部含：
  - 合并重试循环（`mergeRetryBudget` / `mergeRetryInterval`）；
  - rebase、commit count、dirty 判定、merge blocked 判定、冲突文件读取、cleanup；
  - 返回 `workunit.ErrUncommittedChanges` / `workunit.ErrMergeBlockedByMain`。
- `workunit.Host.Finish` 已经调用 `SceneFace.MergeScene`，并用 `ClassifyFinish` 做分类。
- `workunit.Host` 是 `Begin/Finish/Reclaim/Recover` 的唯一判据处；`SceneFace` 端口在
  `backend/workunit/ports.go`。
- 审批门已删除；不要再把审批加回来。

## 目标形态

1. `WorktreeManager` 只保留机械原语：
   - 建现场 / 认领 / 在册 / 拆现场；
   - rebase 到目标分支；
   - 判断“有没有提交”“工作区是否脏”“merge 是否被主工作区挡住”“冲突文件”；
   - 执行一次 merge / cleanup。
2. `WorktreeManager.Finish` 不再自己决定“重试几次”“未提交算成功还是失败”“挡路算不算失败”。
   它只返回**事实 + 类型化错误**：
   - `ErrUncommittedChanges`：现场有未提交改动；
   - `ErrMergeBlockedByMain`：主工作区挡路；
   - 其他错误：真实 git 失败。
3. 重试与分类在 `workunit.Host`（或其调用的策略）里完成；如果当前 `Host` 没有重试预算，
   先保留现有行为，把重试循环上移，不要在 worktree 里留第二份。
4. `WorktreeManagerDeps` 里不再有“阶段观察者 / 审批门”这类 plan 专用面；`Phase` 如仍被
   测试使用，可保留为可选观察者，但生产装配不再注入。

## 必须遵守

- 先读根 `MEMORY.md`、`AGENTS.md` 与 `backend/workunit/README.md`、
  `backend/tool/worktree/README.md`。
- 先写红灯用例：同一份输入在旧路径与新路径产生同一分类；再改实现。
- 不许改测试去迁就实现；不许把判据复制到 worktree 和 Host 两处。
- 不要动 NodeScope/SceneScope 的改名工作（另一个会话负责）。
- 一个可解释的行为一个 commit；提交到 `refactor/architecture-reshape`，不要动主分支。

## 验证

```text
go build ./...
go vet ./...
go test ./backend/workunit/ ./backend/tool/worktree/ ./backend/engine/assemble/ -count=1 -timeout 900s
python scripts/gen_core_readme_index.py --check
python scripts/check_readme_refs.py --strict
```

交付时列出：删掉了哪些判据副本（文件:行 → 新唯一位置）、哪些测试钉住了新边界、是否还有
worktree 侧的 plan 专用字段。
