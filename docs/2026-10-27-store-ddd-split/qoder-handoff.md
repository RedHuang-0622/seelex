# Store DDD Qoder 交接包

状态快照：M0–M7 已完成；wire 纯装配、压缩/prompt 窄投影与 context fork
重写均已下沉并分片提交。

截至 2026-10-28 01:55，M7 正式验收测试、四条 ownership gate、普通/tagged build
与全仓 vet 均 PASS。外部 consumer 批次已从 `8fbae4e8` 完成 dry-run、apply、
replay 测试、tree parity 和 rollback 演练，M7 已闭环。

工作区：`G:\seelex-worktrees\store-ddd-M6`

分支：`codex/store-ddd-M7-g`

接手顺序：

1. 先读仓库根 `MEMORY.md`、`AGENTS.md` §0/§5/§8–§10。
2. 读本目录 `plan.md`、`milestones.md`、`external-change-protocol.md`、
   `progress.md` 和 `impact/M1-external-deps.md`。
3. 运行 `git status --short --branch` 与 `git worktree list`，以 `progress.md`
   和最新提交为事实源。
4. M7 Context 已完成：M5/M6 已迁 message/event 分片、head、draft 与状态机；
   head JSON、seq 分配、EVENT identity 与 JSONL 读写/残尾修复；提交
   `9f745772`、`5d7a8374`、`266a9d5f`、`c3e60865`、`6083dd33`、`bcb522d0`。
   `711bfc67`、`a162e565`、`c393c418`、`0fbb82d6`。M6 已完成
   queue/lifecycle、retention、compact、fork 的共享类型、状态转移与校验。
  M7 已迁 `context.json`、checkpoint key、Context/Stack wire 类型、
  SystemPrompt/checkpoint payload、compact 栈校验和各栈纯操作；提交
  `be49335d`、`12325efd`、`bd889120`、`4e4867b4`、`81c5d4ed`、`0592b22a`、
  `314fdaec`。最新切片把 provider wire 纯装配迁到
  `store/context/wire_engine.go`，根 `store/wire_assembler.go` 只映射
  `Event`/attempt cache；压缩检索、相关记忆与装配控制器改用
  `CompactStackSnapshot() []CompactFrame`，不再通过万能 record 读压缩栈。
  Prompt 装配、local compaction、gap coverage 已改用
  `PromptStackSnapshot`；GoalStack/SystemPrompt/schema 等存储字段不再进入该
  consumer 面。session fork 的 context JSON 重写已迁入
  `store/context.ForkState`，外部生产代码不再直接解码/改写真 record。
  接手前先检查 `git status --short --branch` 和 `git diff`；该 checkpoint 已提交为
  `cf119696`，随后 `PromptStackSnapshot` 内层投影提交为 `3c23ebf4`；每个后续
  里程碑都必须先全量门禁、再创建只含该里程碑的本地 commit（不 push）。
  C: 盘空间不足时设置 `GOCACHE=G:\go-build-cache\seelex`、
  `GOTMPDIR=G:\go-build-tmp\seelex`；禁止清理缓存或用户数据。
5. 首次验证：

```text
go test ./store/context -count=1
go test ./store -run 'Context|Checkpoint|Wire' -count=1
go test ./e2e -run 'TestStoreDDDMContextOwnershipGate|TestStoreDDDMCompactStackReadFaceGate|TestStoreDDDMPromptStackReadFaceGate|TestFacadeDependencyClosureRatchet' -count=1
go test ./store/... ./backend/engine/context/... ./backend/engine/assemble/... ./test/ -count=1
```

`go test ./store/...` 曾有一次 `TestCommitNotBlockedByFullHistoryRead`
环境负载型超时，单测重跑通过；不要通过改阈值掩盖，最终完整重跑后再判定。

外部批次证据见 `impact/M7-external-deps.{json,md}`、
`impact/M7-*.patch` 与 `scripts/store-ddd/M7-external-migrate.py`。以后每个
里程碑完成测试后都必须创建本地 commit（不 push）。

严禁 `git reset --hard`、`git clean`、`make clean/release`、目录删除或用户数据清理；
外部依赖批次必须先在隔离 worktree 生成 manifest 和 checkpoint。

M6 截止条件已满足，不触发开发交接。下一步从 M8 Teamwork 开始；Qoder 100
积分只能在界面实际显示领取入口且点击成功后记为已领取。
