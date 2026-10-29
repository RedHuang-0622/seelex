# 上下文前缀稳定性 — 基线冻结与交接（M0/M1 完成，M2 起未完成）

本轮目标：按 `docs/arch/context-prefix-stability-l1.md` 根治上下文前缀不稳定（每轮重算历史导致
deepseek `prompt_cache` 失效）。

## 1. 冻结基线

| 项 | 值 |
|---|---|
| main HEAD | `da373609` feat(goal): 自动续跑生产接线 |
| 工作树 | 干净（`git status --porcelain` 空） |
| 回归口径 | `go test ./application/... ./kernel/... -count=1` → 58 包 ok，0 失败 |
| 装配层预算 | production 49 files / **6452 行**（上限 6467）、components 65/65、assertions 23/23，`TestApplicationArchitectureBudget` PASS |
| vendor | 全程未改 |

### 半成品（已按 WIP 提交保存，未测试、**编译可能坏**）

| 分支 | 提交 | 现场 |
|---|---|---|
| `seelex/l1-wi-m2-l1` | `9353dc99` | `kernel/context/prefix_key.go`、`kernel/task/context_l1.go` 新增；改动 `assemble/runtime_context.go`、`context/coordinator.go`、`prefixprobe/probe.go`、`task/coordinator.go`。**已知破损**：`prefixprobe/probe.go:417` 语法错（non-declaration statement）、未 gofmt |
| `seelex/freeze-wi-m7-stack` | `4a77e3e4` | 新增探针 `backend/tool/node/zz_probe_tail_frame_test.go`、改 `coordinator.go`。**已知破损**：`c.deps.InheritedTailBlocks undefined`（`backend/tool/node/coordinator.go:415`） |

这两个分支是「接着干」的起点，不是可直接合并的成果；下轮开工先 `go build ./...` 修坏点。

## 2. 已完成（可当既定事实）

### M0 观测（全绿）

`kernel/context/prefixprobe`：字符级前缀探针（`LCPBytes` + `BackoffToRuneBoundary` + 段表 +
`FirstDivergence` + cum/last/byTurn 三口径 + 与 `prefix_key` 对撞），11 用例绿。
integration 侧三个变体用例：工作打点表、env 表、非空 compact 栈；外加 plan 帧变体。
**实测原始缺陷**：跨轮断在 byte 16766 / char 16688、`msg#0 role=user`，复用仅 80.5%，整段历史重算。

### M1 字节冻结（全绿，§8 已标 ✅）

| 帧 | 落点 |
|---|---|
| user 帧 | `TurnInputApplication.Prepare` → `tasks.RecordUserWireContentLocked`，最终送引擎的正文冻结进 `ProviderContent`（`32cd1897`） |
| plan 帧 | 改为「计划状态写入那一刻」落 transcript 的 internal 事件（`kernel/task/plan_frame.go`），装配侧删掉 `tryFitExecutionHistory` 的每轮插入（`c4d8d903`）。实测翻转前 BREAK char=16767 / 95.7% / unexpected=1 → 翻转后 turn3 96.3%、turn4 99.4%、unexpected=0 |
| task 尾帧 | **不漂，未改代码**，只钉机制（`6bf04e3a`）：(a) 框架把 Blocks 全排在 `WorkingHistory` 之前 ⇒ 帧恒为请求最末格；(b) `ReActLoop.callLLM` 只传历史快照、装配结果不写回历史。判据：相邻请求 LCP ≥「上一条减尾部帧」，损坏面上界 = 帧自身字节。实测 `LCP=68/100/126`、`loss=227/227/230`、`unexpected=0`，用例 `backend/engine/context/seelectx/tail_frame_position_test.go` |

### 目标未收口自动续跑（引擎 + 接线，§2.8 大部完成）

- 设计：`docs/arch/goal-auto-continue.md`（323 行，五条结论全部指到真实符号）
- 引擎：`application/execution/continuation.go` + `contract/dto/goal_run_state.go`（两层状态唯一判定点）；`Resume`（人工，立即发一次）与 `Continue`（pull，只清位）是两个入口，**UI 误用 Continue 会表现为「点了继续没反应」**
- 接线：`da373609`。休止点信号只发一次（`TestContinuationSignalFiresOncePerTurnNotPerToolRound`：3 个工具轮 → 信号 1/判定 1/起回合 1）；push 真实取消面 `InterruptChildSessions ∩ CancelRunningTurns`；状态经 `ContinuationStatus.View()` → `Snapshot.Runtime.GoalGovernance`；续跑装配整层搬到 `application/assembly/continuation` 子包，预算反降 12 行

## 3. 剩余项（按依赖排序）

| # | 项 | 角色建议 | 状态 |
|---|---|---|---|
| 1 | `wi-m2-l1` L1 快照 + 真 `prefix_key`（六格）+ 命中打点 | l1 | 半成品在 `seelex/l1-wi-m2-l1`，先修编译 |
| 2 | `wi-m2-slice` 装配改 `truth[retained_from:]` 切片 + 删每轮 `ReleaseWorkingHistory` + **压缩边界重建** | l1 | 未开始 |
| 3 | `wi-m2-coldload` 冷加载一致性（逐字节）+ 装配耗时不线性量测 + M2 收口 | l1 | 未开始 |
| 4 | `wi-m7-stack` 子代理栈块帧位核查（`InheritedBlocks` + `ScopeAssembler` 把 Blocks 排在 `WorkingHistory` **之前**，`backend/tool/node/coordinator.go:428-447`，是 plan 旧缺陷的同形状接缝） | freeze | 半成品在 `seelex/freeze-wi-m7-stack` |
| 5 | `wi-spur-persist` 续跑状态落盘 + 审计接账 | spur | 未开始，**优先级高** |
| 6 | `wi-spur-surface` 暂停/继续控制面 + stalled 时长（依赖 5） | spur | 未开始 |
| 7 | M3 draft + actor + ack（轮尾一次提交、并发不丢序、崩溃按水位重放） | 新开 | 未排 |
| 8 | M4 memory 纪元表（`memory_update` + 跨压缩字节一致） | epoch | 未排 |
| 9 | M5 工具集纪元 + `⟦toolset⟧` 哨兵 | epoch | 未排 |

### 第 5 项为什么不能拖

`SessionRecord.Execution` 的 `Paused` / `PausedGoalID` / `Continuations` 至今**无人填** ⇒ 同进程内正确，
**重启丢位**：用户暂停后重启，goal 会自己恢复续跑；且续跑账不落盘会让同一中断点重启后再续一次（重复扣费）。
另 `ContinuationDeps.Audit` 仍为 `nil`，`goal.pause/continue/resume` 三种打点未落账。

## 4. 施工纪律（本轮踩过的坑，务必沿用）

1. **每个里程碑边界先提交再派活**。worktree 从 `git HEAD` 切出，未提交的实现对 teammate 等于不存在
   （本轮因此让一个变体用例重试 23 次、合并在未提交现场上失败）。
2. **用例必须可失败**。禁止恒真断言、禁止只打日志不断言；钉缺陷的用例要能说出「翻转哪一行会变红」。
3. **不许改断言去迁就实现**，尤其 `TestApplicationArchitectureBudget` 的预算数值。
4. **装配层只有减法**：当前 6452/6467。要加代码先抽走（本轮做法：把续跑装配整层搬进子包）。
5. **不改 vendor**：Seele 侧 `cached_tokens` 等能力只做观测不做修改（`go.mod` 无 replace）。
6. **压缩边界是预期失效不是缺陷**：`epoch` 变导致的 miss 记 `ExpectedMiss`；有真 key 之后不许再留零值键写法。

## 5. 开发文档地址

| 文档 | 用途 |
|---|---|
| `docs/arch/context-prefix-stability-l1.md` | **主设计**。I1–I5 不变量、L1/draft/actor 三层、字符级探针三口径、§6.3.2 plan 帧已修、§6.3.3 task 尾帧不漂 + 子代理接缝、§8 里程碑进度（M0/M1 ✅）、§9 D1、§11 索引 |
| `docs/arch/goal-auto-continue.md` | 自动续跑设计：触发三条件、防呆四条、暂停/继续两条传播路径（pull 轮边界 / push 瞬时中断）、两层状态模型 |
| `docs/arch/teamwork-leader-worker-architecture.md` | 编排口径（里程碑屏障 + Work Item DAG） |
| `backend/engine/context/seelectx/README.md` | 装配器侧口径：尾块位置与「Blocks 先于 WorkingHistory」的事实 |
| `application/assembly/README.md` | 装配层结构与预算约束 |
