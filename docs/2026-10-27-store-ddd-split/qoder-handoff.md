# Store DDD Qoder 交接包

状态快照：M0–M6 已完成，M7 进行中。

工作区：`G:\seelex-worktrees\store-ddd-M6`

分支：`codex/store-ddd-M7-g`

接手顺序：

1. 先读仓库根 `MEMORY.md`、`AGENTS.md` §0/§5/§8–§10。
2. 读本目录 `plan.md`、`milestones.md`、`external-change-protocol.md`、
   `progress.md` 和 `impact/M1-external-deps.md`。
3. 运行 `git status --short --branch` 与 `git worktree list`，以 `progress.md`
   和最新提交为事实源。
4. 继续 M7 Context：M5/M6 已迁 message/event 分片、head、draft 与状态机；
   head JSON、seq 分配、EVENT identity 与 JSONL 读写/残尾修复；提交
   `9f745772`、`5d7a8374`、`266a9d5f`、`c3e60865`、`6083dd33`、`bcb522d0`。
   `711bfc67`、`a162e565`、`c393c418`、`0fbb82d6`。M6 已完成
   queue/lifecycle、retention、compact、fork 的共享类型、状态转移与校验。
  M7 已迁 `context.json`、checkpoint key、Context/Stack wire 类型与
  SystemPrompt/checkpoint payload；提交 `be49335d`、`12325efd`、`bd889120`。
  C: 盘空间不足时设置 `GOCACHE=G:\go-build-cache\seelex`、
  `GOTMPDIR=G:\go-build-tmp\seelex`；禁止清理缓存或用户数据。
5. 首次验证：

```text
go test ./store/... ./backend/engine/context/... ./backend/engine/assemble/... ./test/ -count=1
```

严禁 `git reset --hard`、`git clean`、`make clean/release`、目录删除或用户数据清理；
外部依赖批次必须先在隔离 worktree 生成 manifest 和 checkpoint。

若在 2026-10-28 02:00 前已到 M6，则停止截止交接；否则 Qoder 从本 worktree 最新状态
继续。Qoder 100 积分只能在界面实际显示领取入口且点击成功后记为已领取。
