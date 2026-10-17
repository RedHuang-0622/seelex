# 任务书：worktree 收尾判据归位

> **状态：已完成并提交 `2c043125`。请勿重复认领。** 本文件保留为验收记录；
> 如后续要改，先读该提交与 `backend/workunit/merge_retry.go`。

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
