# 保留窗口一条规则：压缩保留前缀 + 读尾一并换到 min(token1, token2)（2026-09-20）

> 日期: 2026-09-20 | 范围: `seelexctx/window.go`、`seelexctx/dag.go`、
> `seelexctx/gap.go`、`application/core/window.go`、`application/core/session_history.go`、
> `application/core/service_assembler.go`、`application/core/session_runtime/{ports,archive}.go`、
> `application/core/context_runtime/coordinator.go`、`application/core/task_context/plan_transcript.go`、
> `application/model/state.go`、`config/seelex.yaml`、相关 README 与测试；
> 复核轮（§六）追加 `seelebridge/runtime_context.go`、`seelebridge/README.md`、
> `config/seelex.yaml` 的注释与默认值收编。

## 一、动机：两处"窗口"各说各话

用户给的形状（原始需求）：

```text
|-compact context-|-context_window-|
|--------------all_context---------------|
```

即：`all_context`（本次要携带的上下文）里，`context_window`（保留前缀）是两个值取较小者，
剩余部分尽数交 `compact_context` 折叠：

- `token1` = 硬编码在配置里的保留窗口 token 数（`window.retain_tokens`）；
- `token2` = `window.ratio` × `all_context`（占比窗口）。

改动前，这条规则在代码里根本不存在：

| 位置 | 旧口径 |
|---|---|
| 请求装配（压缩分支） | `budget.TargetAfterCompaction` = 预算的 60%（硬编码比例） |
| 冷恢复读尾 | `budget.TargetAfterCompaction` + 固定单元上限（4 / 3 / 4） |
| 可见 transcript 尾部 | 同上，经 `Deps.TranscriptTailBudget` 注入 |

配置里的 `retain_tokens` / `ratio` 对两处都不生效，`all_context ≥ 硬阈值必须压`
也无处可配。

## 二、已落地（本轮一并完成）

### 2.1 一条实现：`RetainedContextTokens`（seelexctx/window.go）

```go
retained = min(token1, token2)
  token1 = RetainTokens（未配置 → 调用方给的回退值，通常是账号上下文窗口）
  token2 = Ratio × allContextTokens
```

- 两个候选任一不可用（<=0）则不参与比较；都不可用 → 全量保留；
  返回值夹在 `[1, allContextTokens]`。
- `MustCompact(allContext)`：`window.force_compact_tokens`（硬压缩阈值）判定，
  未配置恒 false（压缩只由软策略驱动）。
- 默认值仍只在 `DefaultWindowConfig()` 一处定义（决策代码不硬编码）。

### 2.2 请求装配（context_runtime/coordinator.go）

- `all_context` = `CountRequestTokens(fullContext)`（全量累积 context 的实际 token）。
- `compacting = rawTokens ≥ 软阈值(75%) || hardCompact`；压缩时保留窗口 `target` =
  `min(token1, token2)`，窗口外部分交 `compact_context`。
- 压缩后不再叠加 `limits.context_max_units` 做"第二次截断"（单元不可拆分，token
  窗口已是约束）；单元上限只留在降级扫描路径。
- 硬压缩不被 `progress epoch` 节流挡下。

### 2.3 读尾（本轮"一并替换"的落点）

冷恢复发生在请求装配之前，**没有"本次要携带的上下文"可估**，因此
`all_context` 取账号上下文窗口（会话能携带的上限），经同一实现算预算：

```go
// application/core/window.go
func RetainedReadTailBudget(runtime any) int {
    budget := task_context.ContextBudgetFor(runtime)
    return CurrentWindowConfig().RetainedContextTokens(budget.Window, budget.Window)
}
```

替换点（口径从 `TargetAfterCompaction` 换成同一 min 规则）：

- `session_history.go`：冷恢复的 transcript 尾窗、wire 装配（compact 摘要 + 尾窗 +
  最近 K 条尝试）、durable-record 回退历史——三处共用同一个 `tailBudget`；
- `service_assembler.go`：`Deps.TranscriptTailBudget` 直接注入
  `RetainedReadTailBudget`（`session_runtime.LoadSessionTranscript` 消费）。

**为什么读尾的 `all_context` 取账号窗口而不是"当前 transcript 大小"**：读尾是
**重建既有历史**，不是新做压缩决策。若取当前内容量，`ratio × 内容量` 会把一个
远未达上限的会话也裁掉 30%——那是丢历史，不是压缩。取窗口上限后：未达上限的
会话不被占比窗口误裁，贴上限的会话与压缩侧保留同一窗口。

**单元上限保持不变**（transcript 4 / wire K=3 / record 4）：它不是 token 规则的一部分，
而是存储层"读哪些分片"的选择边界——`selectEventTail` / `readTailRowsForSelection`
对 `maxUnits <= 0` 的语义是"不读"（返回空），传 0 会把冷恢复读空。

### 2.4 区间记录而不是推算

- 应用侧：`ContextCompaction` 新增 `MessageFrom/MessageTo`（UI 消息号）与
  `EventFrom/EventTo`（transcript 事件序号），边界取窗口决策本身返回的保留窗口
  起始下标（`TranscriptTailWindow`），`compact_context` 结果面同步暴露。
- 框架侧：`CompactFrame.From/To` 改取被压单元**自带区号**（基准 = 已记录栈顶
  `To+1`），删掉 `len(overflow)-1` / `prevTop.To+len(overflow)` 三处推算公式。

## 三、有意**未**替换的一处（边界说明）

`seelebridge/runtime_context.go windowTailBudget()` 仍是"轮数维度"——它是 D1 的
**读分片宽度**（框架 provider 历史缓存装载），不是压缩保留前缀规则，因此本轮
不动它。复核（§六）修正了这里的原始理由：先前写成"换掉会与
`rounds/min_rounds/max_rounds` 相互覆盖"是不准确的——两侧不在同一个决策上，
`tokenBudget` 取账号上下文窗口、恒 ≥ `RetainedContextTokens` 的返回值（该函数已把
结果夹在 `[1, all_context]`），token 维永远不比新规则更紧，谈不上互踩。不换它的
真实理由是：**职责不同**（读分片 vs 保留口径），且它另有一个不可替换的副作用
——尾窗分支是真空区覆盖的唯一触发点（`sessionstore/durable_history.go` Load 的
尾窗分支 → `SetGapCoverer`）。

## 四、验证

```text
go build ./...                                  # 通过
go vet ./application/core/...                    # 通过
go test ./application/core/... -count=1          # 全绿（16 包）
go test ./... -count=1                           # 全绿
python scripts/gen_core_readme_index.py          # 索引已刷新（window.go 新函数入册）
python scripts/check_readme_refs.py --strict     # 检查 123 个 README，0 个未解析引用
python scripts/check_mermaid.py                  # 196 个代码块，0 问题
```

新增/更新的回归：

- `application/core/context_window_rule_test.go`：
  `TestCompactRetainedPrefixIsMinOfTwoWindows`（token1 紧 / token2 紧 / 都宽）、
  `TestCompactHardThresholdForcesCompression`、`TestCompactHardThresholdBypassesEpochThrottle`、
  `TestCompactContextHandlerReportsRecordedRange`、
  `TestReadTailBudgetFollowsRetainedWindowRule`（本轮：读尾走同一 min 规则，且不再是
  旧的 60% 预算口径）；
- `seelexctx/window_retain_test.go`：规则表（token1 紧 / token2 紧 / 回退 / 退化）；
- `application/core/session_archive_test.go`：冷恢复尾部预算的期望改为 window 段规则。

复核轮（§六）验证——只动注释、配置说明与默认值来源（`rounds = 4` →
`seelexctx.DefaultWindowConfig().MinRounds`，两者同值），行为等价：

```text
gofmt -l seelebridge/                             # 无输出
go build ./...                                    # 通过
go vet ./seelebridge/...                          # 通过
go test ./seelebridge/... ./sessionstore/... -count=1   # 通过
python scripts/check_readme_refs.py --strict      # 123 个 README，0 个未解析引用
python scripts/check_mermaid.py                   # 196 个代码块，0 问题
go test ./... -count=1 -timeout=300s              # 第一次：seelebridge 包 FAIL（38.5s）
go test ./... -count=1 -timeout=300s              # 重跑：exit 0，无 FAIL
go test ./seelebridge -count=2 -timeout=300s      # 两轮：exit 0，无 FAIL
```

两次都没再出现的 `seelebridge` 失败**未被定位**：第一次全仓跑把输出接在
`tail -40` 后面，包内的失败详情被切掉了，只留下 `FAIL github.com/.../seelebridge`
一行。因此这里记为**未复现的 flaky**，不记为"全仓全绿"。教训：长输出验证必须
落文件承接（`> "$TEMP/xxx.log" 2>&1`）再 grep，禁止管道接 tail——否则失败样本
不可用，等于没跑。

## 五、残留不确定性

- 读尾的 `all_context` 取账号上下文窗口是一次**产品口径选择**（见 §2.3）：若希望
  冷恢复也按"当前会话内容量"的占比窗口裁，改 `RetainedReadTailBudget` 一处即可，
  但会引入"未达上限的会话也被裁"的行为。
- `maxUnits` 仍是 4 / 3 / 4（存储层读分片边界）；若希望读尾只受 token 窗口约束，
  需先给存储层补一个"按 token 上限读分片"的选择路径（现在的 0 = 不读）。
- `budget.TargetAfterCompaction`（`ContextBudget` 的 60% 静态值）已不再有读尾消费者：
  剩下的读者是 `context_runtime` 的 `TokenAudit.TargetAfterCompaction`（只写不读，全仓
  无 GUI/TUI 消费）与 `protectOversizedCurrentInputLocked` 的单输入门限
  （`TargetAfterCompaction/2`，另一语义）。审计字段现在报的仍是静态 60%，与实际生效的
  保留窗口（压缩时）不是同一个数——要不要把它改成"本次装配实际目标"属独立口径变更。
  删字段 / 改口径都不影响本轮行为。

## 六、复核（同日）：三条窗口路径的分工与 D1 读尾的真实状态

针对"§三 的边界判断对新策略是否够用"做的复核，逐条给证据。

### 6.1 三条路径谁说了算

| 路径 | 代码位置 | 决定什么 |
|---|---|---|
| A 请求装配 | `application/core/chat.go:178` → `context_runtime/coordinator.go:294-300` → `fitExecutionHistory(target)` | 每次请求模型真正携带什么（min(token1, token2) 的家） |
| B 冷恢复读尾 | `application/core/session_history.go:295-312`（`RetainedReadTailBudget`） | 恢复会话时重建哪段历史 |
| C D1 读分片 | `seelebridge/runtime.go:597-606` → `sessionstore/durable_history.go:145-160` | 框架从磁盘读哪些分片 + 真空区覆盖触发 |

关键接线：A 每次装配都经 `replaceEngineHistory` → `ReplaceHistoryFor` →
`prepareHistory` → `PrepareNextLoad`（`internal/adapters/engine_port.go:438-440`、
`494-496`、`625-627`；`sessionstore/durable_history.go:112-123`），框架侧下一次
`Load` 直接返回装配结果；会话忙时的延迟安装也在下一个 Run 前补装
（`engine_port.go:168-172`、`199-203`）。框架每个 Run 只 `restoreHistory` 一次
（Seele `session/loop.go:221`、`482-491`），Run 内迭代不再 Load。
→ **主会话请求路径上 C 的尾窗分支通常不执行**；三个 Chat 入口
（`chat.go:183`、`chat.go:544`、`history_safety.go:117`）都在其前面装配。

C 只挂在主会话：子代理会话的 `DurableHistory` 不调 `SetTailBudget`
（`seelebridge/runtime_subagent_session.go:39-43` → Load 走全量旧语义），角色回合
会话根本不挂 `DurableHistory`（`seelebridge/runtime_role_turn.go:292` → 框架
`restoreFromCache`）。

### 6.2 结论：够用，但两处口径不实

- **够用成立**：C 的 `tokenBudget` = 账号上下文窗口，恒 ≥ A/B 的保留前缀；units 上限
  与 B 同为 4；读少的部分由真空区覆盖折进 `CompactStack`，原始轮次仍在会话存储。
  即 C 不会把新规则想保留的轮次读没，换掉它也不会改善保留行为——§三 的"不动它"
  结论保留，理由换成 6.1 的分工。
- **不实之一（已在本次修正）**：注释/config 说 C 的轮数是"策略推导"，实际
  `runtime_context.go` 只给 `ProviderContextInfo` 填 `ContextTokens`，
  `AvgRoundTokens`/`Reserved` 缺省 → `seelexctx/window.go:165` 恒走输入缺失回退，
  `maxUnits` 实际 = `window.rounds`（显式配置）或 `window.min_rounds`，clamp 推导
  在这条路径从未生效。`config/seelex.yaml` 里 `rounds: 0 = 使用 provider 推导`、
  `min_rounds = 窗口下限` 两句注释因此按真实作用面重写。
- **不实之二（已在本次收编）**：回退分支 `rounds = 4` 是不可达分支（`WindowRounds`
  恒返回 ≥ minRounds > 0），且在 `DefaultWindowConfig` 之外重复了默认值。改为引用
  `seelexctx.DefaultWindowConfig().MinRounds`：默认值仍只有一处定义，同时保住
  `selectEventTail` 把 `maxUnits<=0` 判为"不读"（会读空历史）这条护栏。
- **覆盖盲区（未修，需知悉）**：`retain_tokens` 这个新旋钮对 C 路径不生效。若某次
  Run 没有被 A 抢先装载，模型可见原文就只有 `min_rounds` 个单元 + 摘要，即使
  `retain_tokens` 想留更多。

### 6.3 待决策

- 让 C 自适应（补 `AvgRoundTokens` + `ReservedTokens`，与 `seelexctx/controller.go:330`
  同款估算）：6.1 显示它不在主会话请求路径上，故非必须；若确认存在绕过装配的
  Chat 入口，则优先级上升。
- C 是否"从不执行"需要运行时证据（帧标记/日志），静态读码只能证明"找不到入口"。
- §五 的审计字段：复核判定为**无读者的死字段但落盘**（`model/context.go:58` 随
  `TaskContextProjection` 持久化），倾向"删字段 + 给 `coordinator.go:612` 的
  `/2` 门限按其真实语义改名 + 删 `seelexctx/controller.go:76` 零调用方法"，
  使"压缩目标 60%"这一口径在代码中彻底消失；仅当计划给审计加读方（GUI token
  面板 / 缓存命中观测）时才改为"写实际 `target` 并同步换字段名"。本轮未动。
