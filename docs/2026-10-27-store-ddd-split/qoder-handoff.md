# Store DDD M6 截止交接包

生成时间：2026-10-27 23:22 Asia/Shanghai

## Qoder 任务说明

继续实施 Seelex `store` 领域驱动拆分，目标是在已完成的 M0 基线上按
[`plan.md`](plan.md) 与 [`milestones.md`](milestones.md) 推进到 M11。接手时先运行：

```text
git worktree list
git status --short --branch
```

优先处理分支 `codex/store-ddd-M7-g` 的隔离 worktree
`G:\seelex-worktrees\store-ddd-M6`。旧的 C: worktree 仅保留历史证据，C: 盘已满；
后续不得在旧 worktree 写文件。若该 worktree 已推进到后续
里程碑，以 [`progress.md`](progress.md) 和该分支最新提交为事实源，不回到主工作区的旧状态。

## 已完成

- M0：物理布局 golden、Commit 写序/半失败、锁身份、God 类型 ratchet、
  `./store/...` import 门禁、冷读 fixture。checkpoint 提交 `6c0545fd`。
- M1：`store/board` 已承接项目看板追加、折叠、压实、路径校验、按路径锁、回收与
  `BOARD.md` 渲染；根包只保留 DTO alias 与 Router 单层转发。领域测试、
  `kernel/board`、`wiring/registry`、`test -run ProjectBoard` 和全仓 build/vet 已通过。
  外部批次 checkpoint `22b60880`，里程碑提交 `ba243f67`。
- M2：`store/internal/layout` 已承接会话路径、guide、module head 信封/checksum、
  原子发布与按 `(Scope, ModuleID)` 的动态锁身份；根 18 个模块锁字段和中央
  `mutexFor` switch 已删除。定向 Head/Lock/Atomic/Pending/Guide、完整 store/e2e、
  全仓 build/vet/test 与 tagged build 已通过。
- M3：`store/artifact` 已承接内容寻址 tool result、超大文本 blob、media
  配额/去重/GC 与派生搜索索引；`storeEngine` 删除 3 个媒体实现方法。
  里程碑提交 `b76ccdc2`。
- M4：application `TurnStoreCommit` 已编排 artifact → message → compaction；
  store 只暴露三个单聚合写口，M0 顺序/失败矩阵和 fork snapshot 链路通过。
- M5：`store/session` 已承接 message/event 分片命名和
  pending-tail 分类，随后迁入 message/event head JSON、seq 分配、EVENT
  kind/row/commit identity，以及 JSONL 共享读、残尾修复、追加与解码。
  提交：`9f745772`、`5d7a8374`、`266a9d5f`、`c3e60865`、`6083dd33`、
  `bcb522d0`、`711bfc67`、`a162e565`、`c393c418`。head 自愈、typed
  publish、JSONL storage、draft-tail 与 committed-head 状态转换已下沉。
- M6：`store/session` 已承接 lifecycle/queue、retention、compact、fork 的
  共享类型、状态转移与校验；首片提交 `0fbb82d6`。
- M7（进行中）：已迁移 context/checkpoint 机制、Context/Stack wire 类型、
  SystemPrompt/checkpoint payload。提交 `be49335d`、`12325efd`、`bd889120`。

## 构建环境

`C:` 盘空间可能不足。Go 命令失败于 linker 临时文件时，设置：

```text
GOCACHE=G:\go-build-cache\seelex
GOTMPDIR=G:\go-build-tmp\seelex
```

后重跑。禁止通过清理 Go cache、`dist/`、`.seelex/` 或用户数据解决空间问题。

## 必须先读

1. `MEMORY.md`
2. `AGENTS.md` §0、§5、§8–§10
3. `docs/2026-10-27-store-ddd-split/plan.md`
4. `docs/2026-10-27-store-ddd-split/milestones.md`
5. `docs/2026-10-27-store-ddd-split/external-change-protocol.md`
6. `docs/2026-10-27-store-ddd-split/progress.md`
7. 当前里程碑涉及模块的 README 与测试

## 下一步

进入 M7 Context：

- 迁移 prompt、四类上下文栈、checkpoint 与 compact bridge；
- `wire_assembler` 归 engine context 读侧；
- 保持冷恢复与 provider 前缀稳定；
- 首选验证：

```text
go test ./store/... ./backend/engine/context/... ./backend/engine/assemble/... ./test/ -count=1
```

## 纪律

- 每个里程碑先红后绿，提交“原位置 → 新唯一位置”删除清单。
- God 类型方法数只降不升；禁止新增兼容 facade 换编译。
- 零路径、零 JSON 格式、零 schema/head/watermark 变更、零数据迁移。
- 不执行 `git reset --hard`、`git clean`、`make clean/release`、目录删除或用户数据清理。
- 外部依赖批次必须走 `external-change-protocol.md` 的 worktree、manifest、checkpoint、
  dry-run、apply 和 rollback 流程。
- 不读取或输出 `config/accounts.yaml`、`*.local.yaml` 等真实配置。

## 截止规则

若 2026-10-28 02:00 时 M6 尚未验收，Qoder 从当前最新 worktree 继续，不要求 Codex
重复已完成工作。交接必须在 `progress.md` 记录接手点、当前失败门禁和 next command。
