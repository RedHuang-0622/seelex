# Store DDD Qoder 交接包

状态快照：M0、M1、M2、M3 已完成，下一步 M4。

工作区：`C:\Users\redre\.codex\worktrees\store-ddd-m1\seelex`

分支：`codex/store-ddd-M1`

接手顺序：

1. 先读仓库根 `MEMORY.md`、`AGENTS.md` §0/§5/§8–§10。
2. 读本目录 `plan.md`、`milestones.md`、`external-change-protocol.md`、
   `progress.md` 和 `impact/M1-external-deps.md`。
3. 运行 `git status --short --branch` 与 `git worktree list`，以 `progress.md`
   和最新提交为事实源。
4. 下一里程碑是 M4 Commit/Turn UoW：按 M0 写序/半失败矩阵拆 Session、
   Artifact、Compaction 提交，并在 application 层保留 artifact → message →
   compaction 的现有可见性。
5. 首次验证：

```text
go test ./application/core/... ./store/... ./wiring/... ./test/ -count=1
go test ./store/... ./e2e/... -count=1
```

严禁 `git reset --hard`、`git clean`、`make clean/release`、目录删除或用户数据清理；
外部依赖批次必须先在隔离 worktree 生成 manifest 和 checkpoint。

若在 2026-10-28 02:00 前已到 M6，则停止截止交接；否则 Qoder 从本 worktree 最新状态
继续。Qoder 100 积分只能在界面实际显示领取入口且点击成功后记为已领取。
