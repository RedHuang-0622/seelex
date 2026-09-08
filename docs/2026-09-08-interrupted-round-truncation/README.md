# 中断轮截断策略更改：UI 可见轮次不得因单元切分消失

> 性质：一次性修复工作包（2026-09-08）。截断策略更改 + 装配层补齐，使重启后
> continue 具备续跑上下文。描述以字符画表达，风格参考 [tmp/draft-charts.md](../../tmp/draft-charts.md)。
> 状态：实现 + 测试已落地（工作树），受影响包回归绿。关联：
> [context-restore-review](../2026-09-08-context-restore-review/README.md)（同域评审与其它修复）、
> [session-order-log](../2026-09-07-session-order-log/README.md)（有序存储恢复设计）。
> 代码与测试是最终事实来源。

## 0. 结论速览

1. **两处截断缺陷**（本会话探针复现）：残缺（中断）工具链轮被整体作废并连坐
   其后缀文本轮/下一 user；会话以残缺工具链收尾时，整轮（含未回复 user 请求）
   不构成单元 → 冷加载 provider 上下文里**UI 有、模型无** → 重启后 continue 失忆。
2. **根因**：三处同构的“完整协议单元”切分器（sessionstore / task_context /
   seelexctx）对残缺链一律作废并 `nextUserIndex` 跳到下一 user。
3. **修复（两层）**：
   - 切分层：单元 = **可见轮次**；残缺链保留为**开放单元**，断点续扫、不跳
     next user；孤儿 tool 结果与控制块仍不构成单元。
   - 装配层：请求前把缺失 tool 结果**补齐为合成占位**（provider-only、幂等、
     后文已有同 ID 结果时不注入），保证 assistant/tool 配对合法。
4. **continue 续跑**：冷加载保留中断轮 + 装配补齐缺失结果后，用户输入 continue
   时模型可见“原任务 + 已完成结果 + 未知结果显式信号”，可续作而非失忆。

## 1. 探针场景（消息流）

```text
Case A —— 链后带同轮后缀文本与下一轮（修复前会连坐丢弃）：

  user: 任务1：重构模块并验证
  asst: tool_calls t1=read_file  t2=grep_search
  tool: t1 read_file  "file body"
  asst: 已完成第一步，继续          ← 同一轮的后缀文本
  user: 任务2：继续
  asst: 任务2完成

  ┌─ 修复前单元切分 ────────────────────────────────────────────┐
  │ user 轮扫描到 t2 结果缺失 → 整轮作废(empty)                │
  │ → nextUserIndex 跳到下一 user：残缺链与后缀文本一并消失     │
  └────────────────────────────────────────────────────────────┘

Case B —— 会话以残缺工具链收尾（修复前整轮不进冷加载上下文）：

  user: 任务1：重构并验证
  asst: tool_calls t1=read_file  t2=grep_search
  tool: t1 read_file  "file body"
                                ← 进程在此中断，无文本终止点、无下一 user
  ┌─ 修复前 ───────────────────────────────────────────────────┐
  │ transcriptUserUnit 判“不完整” → 不产出单元                │
  │ → 冷加载尾窗(TranscriptTailHistory/selectEventTail)没有该轮 │
  │ → UI 显示任务1，重启后 provider 看不到 → continue 失忆      │
  └────────────────────────────────────────────────────────────┘
```

## 2. 根因：三处同构的切分器

```text
同一套“完整协议单元”语义在三个包各写了一份，行为完全同构：

  ① sessionstore.CompleteEventUnits / userEventUnit      → selectEventTail 冷加载尾窗
  ② task_context.transcriptProtocolUnits / transcriptUserUnit → TranscriptTailHistory
     （resumeSessionCold 冷加载 / coordinator 窗口装配）
  ③ seelexctx.chatUnits / userMessageUnit                → 窗口/溢出统计与压缩投影

  ┌─ 旧语义：残缺链 = 不完整 = 作废 ──────────────┐
  │ user 轮内遇到 tool_calls 链缺结果            │
  │   → 返回 empty，并把扫描跳到下一个 user       │
  │   → 该轮已记录内容 + 后缀文本 + 相邻轮次丢失  │
  │ 会话尾无文本终止点 → 不产出单元 → 冷加载缺失  │
  └───────────────────────────────────────────────┘
```

差异只在消息模型（Event / TranscriptEvent / types.Message）与消费方，判定逻辑
雷同——因此修复也在三处各做一次，保持语义同构（见 §3.1）。

## 3. 修复语义

### 3.1 切分层：开放单元 + 断点续扫（三处同构）

```text
新规则：
  · 单元 = UI 可见协议轮次：user 轮 / assistant 文本轮 / assistant 工具链轮
  · 残缺（中断）工具链轮 = 开放单元：保留已记录部分，缺失 tool 结果由装配层补齐
  · 扫描到链断裂点后“续扫”而非跳 next user —— 同轮后缀文本、下一 user 都不丢
  · 孤儿 tool / 未知角色 / 上下文控制块 = 仍不构成单元（外层 default 跳过）

Case A 修复后：  [user:任务1 | asst:tc | tool:t1 | asst:文本] 一个单元
                 [user:任务2 | asst:完成]                      下一个单元
Case B 修复后：  [user:任务1 | asst:tc | tool:t1]              开放单元 → 进入冷加载尾窗
```

消费点更新（均为同一语义）：
- `sessionstore/sessionstore.go`：`CompleteEventUnits`/`userEventUnit`（
  [sessionstore.go:2667](../../sessionstore/sessionstore.go:2667)），尾部
  `selectEventTail`（:2636）随之保留开放单元。
- `application/core/task_context/plan_transcript.go`：
  `transcriptProtocolUnits`/`transcriptUserUnit`（:146/:197），
  `TranscriptTailHistory`（:100）保留开放单元。
- `seelexctx/controller.go`：`chatUnits`/`userMessageUnit`/`toolChainUnit`
  （:632/:668/:698）。

### 3.2 装配层：请求前补齐缺失 tool 结果（两套同构实现）

```text
发送给 provider 前（PrepareProviderHistoryFor / PrepareReplaceHistory）：

  [asst:tc(t1,t2)] [tool:t1]        ← t2 结果缺失（链断裂点）
  RepairInterruptedToolChains 在“最后一个已配对结果之后、第一个非 tool 消息
  之前”插入合成占位：

  [asst:tc(t1,t2)] [tool:t1] [tool:t2: "[Seelex recovery note: interrupted
  tool call "grep_search" may not have executed; ... Verify side effects or
  re-issue the call before continuing.]"] [后续消息…]
```

实现与不变式：
- `application/core/context_runtime/history.go`：`RepairInterruptedToolChains`
  （EngineMessage 版，:107）+ `PrepareProviderHistoryFor`（:49）在装配后、发请求
  前调用（coordinator.go:218）。
- `seelexctx/history_safety.go`：`repairInterruptedToolChains`（types.Message 版，
  :43）+ `PrepareReplaceHistory`（:34）在 ReplaceHistory 投影前调用
  （controller.go:424）。
- 合成正文以 `InterruptedToolResultPrefix` 开头，`IsProviderOnlyHistoryContent`
  识别为 provider-only（UI 不渲染、不当作真实成功证据）。
- **幂等**：已补齐的链重复执行不重复插入。
- **保守**：调用 ID 为空/重复的链不动；后文已有该 ID 结果（乱序历史）不注入，
  避免破坏既有配对（`toolResultExistsLater`）。

### 3.3 活跃 ReAct 中间态防误判

```text
OnIterationComplete（tool_hooks.go，活跃迭代中）：
  只补空正文（PrepareNewHistoryContentFor, history.go:71）
  不做链配对修复 —— 新 append 的 assistant/tool 记录可能仍在执行，
  此时注入占位会与“即将到达的真结果”重复。

定稿链配对修复 → 留到装配/请求前 PrepareProviderHistoryFor 一次性执行。
```

## 4. 重启 continue 端到端（测试语义）

```text
中断/重启前 transcript 以残缺链收尾：
  [user:任务1] [asst:tc(t1,t2)] [tool:t1]

重启冷恢复 resumeSessionCold → TranscriptTailHistory（开放单元保留）：
  engine history = 同 3 条

chat 前装配 seam PrepareProviderHistory：
  + [tool:t2 合成占位]  → 4 条（assistant/tool 配对合法）

用户输入 continue 追加：
  + [user:continue]     → 5 条

模型上下文 = 原任务 + t1 已完成结果 + “t2 状态未知”显式信号
  → 继续未完成步骤，而非失忆重启
（固化于 interrupted_continue_test.go，无 LLM、确定性断言）
```

## 5. 代码锚点

| 层 | 位置 | 符号/行 |
|---|---|---|
| 切分 ① | sessionstore/sessionstore.go | `CompleteEventUnits`:2667 `userEventUnit`:2700 `toolEventUnit`:2726 `selectEventTail`:2636 |
| 切分 ② | application/core/task_context/plan_transcript.go | `transcriptProtocolUnits`:146 `transcriptUserUnit`:197 `transcriptToolUnit`:224 `TranscriptTailHistory`:100 |
| 切分 ③ | seelexctx/controller.go | `chatUnits`:632 `userMessageUnit`:668 `toolChainUnit`:698 |
| 装配(engine) | application/core/context_runtime/history.go | `RepairInterruptedToolChains`:107 `PrepareProviderHistoryFor`:49 `PrepareNewHistoryContentFor`:71 `toolResultExistsLater`:179 |
| 装配(types) | seelexctx/history_safety.go | `PrepareReplaceHistory`:34 `repairInterruptedToolChains`:43 `toolResultExistsLater`:111 |
| seam 调用 | application/core/context_runtime/coordinator.go | `PrepareProviderHistoryFor`:218 |
| seam 调用 | application/core/tool_hooks.go | `PrepareNewHistoryContentFor`（OnIterationComplete，约 :366-375） |
| seam 调用 | seelexctx/controller.go | `PrepareReplaceHistory`:424 |

## 6. 测试与验证

新增测试（每包回归用例）：

| 文件 | 用例 |
|---|---|
| sessionstore/complete_units_recovery_test.go | `TestCompleteEventUnitsKeepsSuffixTextOfInterruptedChain`、`TestCompleteEventUnitsKeepsTailInterruptedChainAsOpenUnit` |
| seelexctx/interrupted_units_test.go | `TestChatUnitsKeepsSuffixTextOfInterruptedChain`、`TestChatUnitsKeepsTailInterruptedChainAsOpenUnit`、`TestPrepareReplaceHistoryRepairsInterruptedChain` |
| application/core/task_context/plan_transcript_recovery_test.go | `TestTranscriptProtocolUnitsKeepsSuffixTextOfInterruptedChain`、`TestTranscriptProtocolUnitsKeepsTailInterruptedChainAsOpenUnit` |
| application/core/context_runtime/history_recovery_test.go | `TestRepairInterruptedToolChainsFillsMissingResultBeforeSuffixText`、`...FillsAllMissingAtTail`、`...SkipsCompleteChainsAndIsIdempotent`、`...SkipsWhenResultExistsLater` |
| application/core/interrupted_continue_test.go | `TestColdResumeContinueAfterInterruptedToolChain`（重启 continue 端到端） |

同步更新的既有断言（原“丢弃残缺/孤儿”语义 → “保留开放轮次”）：
`sessionstore/state_test.go`、`seelexctx/controller_test.go`、
`application/core/context_controller_test.go`。

验证命令（本工作包相关包，-count=1）：

```text
go test ./sessionstore ./seelexctx ./application/core/... -count=1   → 绿
```

> 注：仓库根目录/e2e 存在与本工作包无关的既有红测（会话切换 repro、
> GUI 构建契约），不影响本包结论。
