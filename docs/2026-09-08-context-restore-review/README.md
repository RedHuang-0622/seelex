# 会话上下文管理 / 内容存储 / 恢复：现状描述与问题复现

> 性质：一次性评审工作包（2026-09-08）。主体（§0–§7）为只读复现 + 判断，
> 复现以临时红测试完成（红后即移除，用例全文见附录）；§8 为后续追加的
> headless 双进程探针与修复记录（含业务代码改动）。代码与测试是最终事实来源。
> 关联设计/调研：[session-order-log（按顺序存储与恢复设计）](../2026-09-07-session-order-log/README.md)、
> [context-prefix-chain（前缀链路，已实现）](../../docs/arch/context-prefix-chain.md)、
> [context-management-review（上下文管理实现审查）](../research/context-management-review.md)。

## 0. 结论速览

1. **上下文管理（运行期）**：主会话请求按「system → project → memory → compact →
   累积 context → plan/task → 当前输入」装配；累积 context 达峰前 append-only、
   字节稳定，达峰后压缩为「有界新鲜窗口 + CompactStack 摘要」。
2. **内容存储**：按类别多通道（ProviderHistory / Events / State / ToolResults /
   ContextState）各自持久化；JSON 后端已把 transcript 事件改为物理 append-only
   的 `transcript.log`，但 history/state 仍是 generation 快照（派生投影），没有
   一条会话级全序日志作为唯一事实源。
3. **上下文恢复**：`resumeSessionCold` 三读（record / history 尾部 / transcript）
   后，把**尾部窗口（≤4 个完整协议单元）**作为引擎历史安装；SessionContextStore
   四栈经 AttachSessionContext 恢复。
4. **问题一（已复现，严重）**：冷恢复安装的是“尾部窗口”，而恢复后首个请求的
   增量装配仍按“引擎历史 = transcript 头部前缀”的 message 计数裁剪事件。当会话
   轮数超过窗口且总 token 低于软阈值时，恢复后首个请求的上下文变成
   「最新 4 轮 → 第 4..11 轮」：**最新轮重复、第 4..7 轮落到最新轮之后（乱序）、
   第 0..3 轮整体丢失**；且该错误序列会作为新前缀被后续请求字节稳定地复用。
5. **问题二（已复现，严重）**：达峰后的窗口/收缩路径，如果单个已定稿轮次
   估算大于压缩目标（但小于全量预算），`TranscriptTailHistory` 会“装不下就
   break”，把**最新轮也整体裁掉**；最终兜底还可能丢弃整个累积 context 只留
   system 前缀，且不会触发超预算拒绝——请求照发，模型看不到任何历史。
   （**已修复**，见 [§7 修复记录](#7-修复记录2026-09-08)。）

> 更新（2026-09-08 复跑）：问题一已在当前工作树被
> [coordinator.go](../../application/core/context_runtime/coordinator.go:196) 的
> `retainedMatchesTranscriptPrefix` 前缀校验修复（保留段是 transcript 后缀时
> 改为 RetainedSystemOnly + 从完整 transcript 原序重建），配套回归测试
> `application/core/context_restore_prefix_order_test.go` 绿；问题二随后也完成
> 修复（保留最新完整单元 + 超全量预算显式拒绝），回归测试见
> `task_context/budget_last_resort_test.go` 与
> `application/core/context_budget_last_resort_test.go`。
> 复跑与修复过程见 [§6](#6-复跑记录-2026-09-08)、[§7](#7-修复记录2026-09-08)。

## 1. 现状描述

### 1.1 上下文管理（运行期装配与压缩）

见 [context-prefix-chain.md](../../docs/arch/context-prefix-chain.md)（已实现）：

- 装配顺序：system → project → memory → compact 帧（稳定前缀）→ 已定稿轮次
  累积 context → plan/task 尾部 → 当前输入；checkpoint 正常路径不进模型上下文。
- 达峰判定与切换：应用侧
  [coordinator.go](../../application/core/context_runtime/coordinator.go:169) 用
  全量累积 context 估 token；达到软阈值后把目标预算降为压缩目标、切换为有界
  新鲜窗口（`ContextMaxUnits`，默认 4），并发布 ContextCompactions。
- 保留/追加治理：`RetainedSystemHistory` 保留「稳定前缀 + 已定稿轮次」、剔除
  每轮重建的动态尾部（plan/checkpoint/恢复信封/预算终局标记）；
  `retainedContextEventCount` 用 message 计数跳过已保留事件。
- 框架侧控制器：窗口外轮次折叠进 CompactStack 摘要帧（栈顶自足、带链锚点与
  request 索引），真空区由 `CoverHistoryGap` 在尾窗 Load 时补压。

### 1.2 上下文内容存储

[sessionstore/README.md](../../sessionstore/README.md) 描述的多通道模型：

- `ProviderHistory`（有界 provider 缓存）、`Events`（transcript 事件）、
  `State`（SessionRecord 全量 JSON）、`ToolResults`、`ContextState`。
- JSON 后端事件为物理 append-only `transcript.log`（显式 `kind`，按 Seq 增量、
  幂等、崩溃残尾跳过）；history/state 仍为 generation 目录 + manifest 原子切换。
- 会话上下文记录（`SessionContextRecord`，schema v2）持久化 SystemPrompt +
  Plan/Task/Skill/Compact 四栈 + GoalStack/GoalAudit；CompactFrame 携带
  From/To/Event 范围/request 首尾/链锚点（PrevSegmentID）等坐标字段，只追加、
  PushCompact 校验链不变量。

### 1.3 上下文恢复

恢复入口 [session_history.go](../../application/core/session_history.go:241)
`resumeSessionCold`：

1. 三路并发读 record / history 尾部 / transcript；
2. 有 record 时以 transcript（或 record 派生 transcript）重建引擎历史：
   `TranscriptTailHistory(transcript, budget.TargetAfterCompaction, 4)` ——
   只取**尾部 ≤4 个完整协议单元**（token 双限）；空或缺最新 user 时回退
   record 派生历史 / resume 信封；
3. `enginePort.ResumeSession` 安装该历史，同时经 `PrepareMainSessionHistory`
   把同款历史交给框架 DurableHistory 一次性消费
   （[engine_port.go](../../internal/adapters/engine_port.go:577)）；
4. workspace 绑定、task/plan 恢复、`AttachSessionContext` 挂接会话四栈；
5. 下一轮 Chat 前应用侧 `PrepareExecutionContextFor` 从内存 transcript 重新装配。

## 2. 问题一：恢复前后会话上下文顺序不一致（已复现）

### 2.1 触发条件与代码链路

- 恢复安装的历史 = transcript **尾部窗口**（[session_history.go:298](../../application/core/session_history.go:298)）。
- 恢复后首个请求：`PrepareExecutionContextFor` 读取的 transcript 是**全量**
  （含第 0..N 轮）；`rawTokens < 软阈值` 时走“累积模式”
  （[coordinator.go:195-197](../../application/core/context_runtime/coordinator.go:195)）：

  ```go
  if covered := retainedContextEventCount(systems); covered > 0 {
      if covered < len(events) { events = events[covered:] } else { events = nil }
  }
  ```

- `systems` 来自引擎历史 = 恢复尾窗（最新若干轮），而 `retainedContextEventCount`
  只数非 system 消息条数，语义假设“引擎历史 = transcript 头部前缀”（运行期成立：
  引擎历史逐轮全量累积）。尾窗恢复破坏该假设：把“尾部已保留条数”当作“头部条数”
  去裁剪，于是完整事件被从头部错切，再接在尾窗之后。
- 触发前提：已定稿轮数 > 恢复窗口（>4 轮），且整段累积 token 仍低于软阈值
  （否则走“丢弃保留段、按事件重建窗口”的压缩路径，顺序不乱）。

### 2.2 复现结果（红测试，见附录 A）

12 轮已定稿轮次（各 user+assistant），模拟冷恢复尾窗 4 轮后首个请求，引擎历史为：

```text
user-08 answer-08 user-09 answer-09 user-10 answer-10 user-11 answer-11
user-04 answer-04 user-05 answer-05 user-06 answer-06 user-07 answer-07
user-08 answer-08 ... user-11 answer-11        ← 重复最新 4 轮
```

对照“恢复前”（空引擎 + 全量累积）为 `user-00..answer-11` 原序。判断：

- **顺序破坏**：第 4..7 轮出现在第 8..11 轮之后，违反 transcript 时间序；
- **前缀丢失**：第 0..3 轮从未进入请求（若压缩栈无帧，模型也没有摘要兜底）；
- **最新轮重复**：第 8..11 轮出现两次，浪费预算并放大注意力噪声；
- **错误自持**：下一请求会把这份乱序+重复序列当作保留前缀（`covered == len(events)`
  → 全量复用），不触发压缩就一直维持，直到压缩事件或再次冷恢复才可能重排。

恢复前后顺序不一致的直接原因是：**恢复产物是“尾部窗口近似”，与请求装配器的
“头部前缀”增量模型不同构**（与 [session-order-log §2.4/§3](../../docs/2026-09-07-session-order-log/README.md)
的现状结论一致，本次给出可执行复现）。

## 3. 问题二：前缀/累积段截断导致最新上下文整体丢失（已复现，已修复）

### 3.1 代码路径

达峰（`rawTokens >= 软阈值`）后：

1. 压缩目标 `TargetAfterCompaction`（Budget 的 60%），窗口上限 `ContextMaxUnits`；
2. `TranscriptTailHistory(events, target, maxUnits)` 自最新单元向旧单元累加，
   首个“放不下”的单元直接 break（[plan_transcript.go:100-108](../../application/core/task_context/plan_transcript.go:100)）
   ——若**最新一个完整轮次本身就大于压缩目标**，`selected` 为空，历史整体为空；
3. `fitExecutionHistory` 收缩仍放不下时，最终兜底丢弃整个累积 context、只保留
   system + plan（[coordinator.go:278](../../application/core/context_runtime/coordinator.go:278)）；
4. 超预算拒绝只发生在“最终装配估算 > 全量 Budget”
   （[coordinator.go:204](../../application/core/context_runtime/coordinator.go:204)）；
   历史被裁空后估算很小，**不会拒绝**，请求带着空历史照发。

### 3.2 复现结果（红测试，见附录 B）

预算 window=200000 / output=8192（Budget=166808、软阈值=125106、压缩目标=100084），
3 个已定稿轮次，assistant 正文约 60 万 ASCII 字符（≈150k tokens，小于全量预算、
大于压缩目标）。`PrepareExecutionContext` 后引擎历史为空：

```text
engine history is empty after context assembly
```

判断：**“前缀截断”的最坏形态**——不是截掉旧轮留最新，而是把累积段（含最新轮）
整段裁掉、只留 system 前缀，模型对新请求没有任何过往上下文。相比直接拒绝发送
（`ErrProviderContextBudgetExceeded`），这种静默裁剪更难被发现，且对长任务
（刚问完上一轮、正要继续）质量伤害最大。超大工具结果和当前超长输入有归档
保护，但单轮由多段中等结果/长正文构成、单轮估算超压缩目标时仍可触发。

## 4. 关联观察（非阻塞）

1. **恢复“prepared”路径跳过真空区补压**：DurableHistory.Load 命中一次性
   `prepared` 时直接返回（[durable_history.go:134-139](../../sessionstore/durable_history.go:134)），
   不会触发 GapCoverer；真空区覆盖要等后续非 prepared Load 才可能执行。应用侧
   若在同一轮已从全量 transcript 重建窗口，影响较小，但该时序与
   “Load 即补压”的文档语义有间隙，宜补测试明确。
2. **计数裁剪是 message 数而非协议单元坐标**：`retainedContextEventCount` 与
   CompactFrame 的 From/To（单元累计索引）、EventSeq（事件序）不是同一坐标系，
   恢复/压缩后复用任何一侧做“前缀跳过”都可能错位。
3. **无单一会话全序事实源**：record / provider history / transcript / context
   各自权威，冷恢复只能三读拼装近似（session-order-log 已记录），本次复现给出
   了该近似在请求装配层的具体失效形态。

## 5. 建议修复方向（未实施）

- **恢复后首轮装配对齐**：恢复安装尾窗时同步记录“已保留的事件/单元尾坐标”
  （EventSeq 或单元累计索引），请求装配按坐标跳过而不是按 message 条数；
  或恢复时把引擎历史重建为「栈帧摘要 + 满足预算的完整前缀」使头部前缀假设成立。
  —— 当前工作树已用“保留段与 transcript 前缀比对”方式落地（见 §6），比对
  以 Role+Content 为准，尚未用事件坐标。
- **装配超预算语义**：单轮装不下时宁可保留该轮并拒绝（或归档该轮原文后以
  引用继续），不做“静默空历史”的最终兜底；把“只留 system+plan”改为显式
  恢复/降级路径并发布可见事件。
- **存储与恢复治理**：继续 session-order-log P2/P3 —— 以每会话有序日志为事实源，
  恢复 = 最新压缩检查点后正序重放，使恢复前后顺序语义一致；装配层统一
  “基线 + 追加”。
- **回归测试**：把附录红用例固化为“恢复前/后上下文逐字节一致”的契约测试；
  覆盖 12 轮小轮次（累积模式）与单轮超压缩目标（收缩模式）两个分支。

## 6. 复跑记录（2026-09-08）

评审后工作树出现并发修复（`context_runtime/coordinator.go` 增加
`retainedMatchesTranscriptPrefix`、`service_input.go` 过滤继承上下文信封、
`context_restore_prefix_order_test.go` 回归测试），按要求重跑全部用例：

```text
=== RUN   TestRunningSessionAccumulatesAllRoundsInOrderControl
--- PASS: TestRunningSessionAccumulatesAllRoundsInOrderControl
=== RUN   TestColdRestoreTailFirstRequestKeepsTranscriptOrder
--- PASS: TestColdRestoreTailFirstRequestKeepsTranscriptOrder
=== RUN   TestReproRunningSessionAccumulatesAllRoundsInOrder        （对照）
--- PASS
=== RUN   TestReproColdRestoreTailThenFirstRequestReordersContext   （问题一）
--- PASS：已随工作树修复转绿
=== RUN   TestReproBudgetOvershootDropsNewestSettledRoundEntirely   （问题二）
    repro_restore_order_test.go:101: newest settled round silently dropped:
    engine history is empty after context assembly
--- FAIL：问题二仍未修复
```

修复行为：`PrepareExecutionContextFor` 在累积模式检测到保留段与 transcript
前缀不一一对应（`retainedMatchesTranscriptPrefix=false`，即冷恢复尾窗后缀）
时，保留段只留 system，事件不裁剪，从完整 transcript 按会话原序重建
（[coordinator.go:196-201](../../application/core/context_runtime/coordinator.go:196)）。
判定：问题一（恢复后顺序/重复/丢前缀）已解决；问题二（超预算整段裁掉
最新轮/只留前缀）当时仍存在，随后按 [§7](#7-修复记录2026-09-08) 完成修复；
红用例内容见附录 B，已固化为正式回归测试。

## 7. 修复记录（2026-09-08）

修复目标：**协议单元不可拆分，最新已定稿轮次不得因预算装不下而静默丢失**。

1. `TranscriptTailHistory`（[plan_transcript.go](../../application/core/task_context/plan_transcript.go:118)）：
   自最新向旧扫描后若一个单元都放不下（最新完整单元单条超预算），不再返回
   空历史，降级保留最新 1 个完整单元。该兜底同时修正了“全量预算估算
   （fullContext）为空 → 不触发压缩”的连锁问题。
2. `fitExecutionHistory`（[coordinator.go](../../application/core/context_runtime/coordinator.go:288)）：
   删除 `events=nil` 只留 system+plan 的静默兜底；最终分支改为
   `RetainedSystemOnly(systems)` + 最新 1 个完整单元。估算仍超出全量预算时，
   由 `PrepareExecutionContextFor` 的全量门禁返回
   `ErrProviderContextBudgetExceeded`（显式拒绝，不再“带空历史照发”）。
3. 回归测试（正式保留）：
   - `application/core/task_context/budget_last_resort_test.go`：最新轮单条超
     预算时保留最新完整单元；预算能容纳最新轮时裁旧保新的窗口语义不变。
   - `application/core/context_budget_last_resort_test.go`：单轮估算介于压缩
     目标与全量预算之间 → 装配保留最新轮并继续；单轮超出全量预算 → 显式
     返回 `ErrProviderContextBudgetExceeded`。

验证（红 → 绿）：

```text
# 修复前（红）
go test ./application/core/task_context -run "TestTranscriptTail(KeepsNewestUnitWhenItExceedsBudget|DropsOlderUnitsButKeepsNewestWithinBudget)$" -v
go test ./application/core -run "TestContextBudgetOvershoot(KeepsNewestSettledRound|RefusesWhenNewestExceedsFullBudget)$" -v

# 修复后（绿）
go test ./application/core/task_context -count=1 -timeout=120s
go test ./application/core/context_runtime -count=1 -timeout=120s
go test ./application/core -run "TestContextBudgetOvershoot(KeepsNewestSettledRound|RefusesWhenNewestExceedsFullBudget)$" -count=1 -v
```

## 8. pprof 复查（2026-09-08）

方法：带 `pprof` tag 重建二进制（`go build -tags pprof`），在真实 headless
控制面 + 真实 API 上做**跨进程冷恢复复查**：

1. 进程 1：`BeginNewSession` 后连续 5 轮短问答（total_messages=10，超过恢复
   窗口 4 轮，逼近尾窗恢复 + 恢复后首轮装配路径），正常收尾落盘；
2. 进程 2（同一临时 store）：`ResumeSession` 冷恢复（三读 + 尾窗 + 首轮
   续聊），抓 goroutine（恢复后基线 / 续聊后）、heap、活跃期 CPU profile。

探针：`tmp/headless-smoke/pprof_context_review_test.go`
（`PPROF_CONTEXT_REVIEW=1` 启用，真实 API + pprof 二进制，产物落
`tmp/headless-smoke/reports/`）。

结果（全部通过）：

| 检查 | 结果 |
|---|---|
| 冷恢复 + 恢复后首轮 | 收敛正常（10 → 12 messages），无 chat error、无超时 |
| goroutine | 恢复后 19 → 续聊后 22（+3，I/O/轮询瞬态），无堆积；快照仅 chan receive/select/IO wait/syscall 等待态，无 sync.Mutex/semacquire/WaitGroup 等锁等待栈 |
| CPU（6s 活跃采样） | 200ms 采样（3.33%），flat 80% runtime.cgocall，cum 集中在 TLS handshake/证书校验 + ReActLoop.ChatStream——IO/网络等待，无本地热点 |
| heap | inuse 约 5.8MB，top 为 runtime/pprof 采样自身、plugin 自注册工具、protobuf init；本批改动路径（coordinator/TranscriptTailHistory/fitExecutionHistory）未出现在 CPU/heap top，无内存热点 |

结论：pprof 复查未发现死锁、goroutine 泄漏或新增 CPU/内存热点；修复后的
上下文装配开销可忽略，冷恢复与续聊真实链路正常。

## 8. 追加记录（2026-09-08：headless 双进程恢复前缀探针与修复）

评审后按用户要求把“恢复链路梳理 + 前缀一致性验证”落到可运行探针，不再靠
代码推断。探针为真实装配的 headless 双进程对照（mock provider 记录每次真实
请求消息序列）：

- 对照 A3：同一进程跑 12 轮后**不重启**继续提交第 13 轮，记录真实请求；
- 对照 B：同样的 12 轮落盘后 shutdown → 重启 → `ResumeSession` → 提交
  同样的第 13 轮，记录重启后第一条真实请求；
- 断言两条真实请求逐条一致。

探针代码：`headless_restore_prefix_probe_test.go`（仓库根，`package main`，
复用 full-chain harness + 本地 mock provider；全部使用 `t.TempDir()` store）。

运行：

```text
go test . -run TestHeadlessRestorePrefixProbe -count=1 -v -timeout 5m
```

### 现象（修复前，红）

未重启继续的第 13 轮请求 = 9 条消息：round-08..round-11 四轮已定稿 +
新输入；重启后第一条请求 = 11 条：**多出两块 `compact-gap` 合成摘要
（覆盖 round-00..round-07）** + 同样的尾窗 + 新输入。即恢复时由真空区补压
凭空制造了运行期从未出现的头部摘要帧，恢复前后前缀不一致（消息数 9 vs 11）。

### 根因与修复

`coverHistoryGap`（[runtime_context.go](../../seelebridge/runtime_context.go)）
在冷恢复尾窗 Load 时对**从未压缩过的会话**也从头补压并推送 CompactStack
帧，随后装配器把这些帧渲染为“相关记忆/压缩上下文”前缀。运行期应用侧窗口
本身不会生成这些帧，于是恢复请求比“未重启继续”多了两块合成前缀。

修复：真空区补压只在会话已有 `CompactStack` 压缩基线时执行（栈顶之后确有
未覆盖区间才补帧）；无压缩基线的会话恢复只装载尾窗，与运行期上下文组成
一致。

### 结果（修复后，绿）

```text
=== RUN   TestHeadlessRestorePrefixProbe
    对照（不重启）第13轮请求消息数=9
    重启后首请求消息数=9
    恢复前缀一致：重启后首请求与未重启继续运行的第13轮请求逐条相同
--- PASS
```

回归：`go test ./seelebridge ./seelexctx ./sessionstore -count=1` 绿；
该探针保留为 headless 恢复前缀回归用例。

## 9. 请求顺序 ↔ 存储内容匹配（2026-09-08，真实 API 工具会话）

按用户指定方法复查“会话结束后的记录 vs 重启恢复的会话记录”：

- 中间件：新增 `SEELEX_REQUEST_LOG` 门控的请求记录器
  （[request_log.go](../../seelebridge/request_log.go)），包装
  Completer/StreamCompleter，把每次真实 LLM 请求的 pid/seq/role + 内容
  sha256 + 工具调用名按发生顺序写 JSONL（不落正文/参数原文）；headless、
  GUI、TUI 走同一条链路（[gui/headless.go](../../gui/headless.go) 只加回环
  HTTP 控制面，Chat 仍经 application core + seelebridge runtime）。
- 探针：`tmp/headless-smoke/request_store_match_test.go`
  （`REQUEST_STORE_MATCH=1`，真实 API）：进程 1 跑两轮 get_time 工具会话
  （4 次 LLM 请求），杀进程；进程 2 同 store 冷恢复后先不续聊，再续一轮
  工具调用（2 次 LLM 请求）。

结果（PASS，产物 `tmp/headless-smoke/reports/request-log-80688.jsonl`）：

| 检查 | 结果 |
|---|---|
| 会话目录 hash（会话结束 vs 重启恢复后） | **完全一致**：combined=86b4c777819463b87ecec590eab1755bb56de4077da03f16e01bb563de6c9975（9 个文件无增删改） |
| 请求顺序（中间件 seq） | 运行期 4 条单调连续；恢复期 2 条 |
| 存储 provider history（续聊前） | 8 条，roles=[user,assistant,tool,assistant,user,assistant,tool,assistant] |
| 恢复后首个请求（中间件） | 9 条，roles=[…同一 8 条原序, user(当前输入)] |
| 前缀匹配 | 8/8 逐条 hash 一致，尾部仅追加 1 条当前输入——**无重排、无重复、无截断** |

整个 store 层面的差异仅来自进程 2 启动时的 `workspace_index.json` 写入与
`sessions/.lock`（非会话记录），会话目录本身在恢复前后逐字节一致。

## 附录 A：问题一复现用例（临时红测试，已移除工作树）

放置在 `application/core/repro_restore_order_test.go`（package core），
执行 `go test ./application/core -run TestReproColdRestoreTailThenFirstRequestReordersContext -v`，
预期红。控制组（空引擎全量累积）绿。

```go
func TestReproColdRestoreTailThenFirstRequestReordersContext(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	events := appendSettledRoundsLocked(service, 12) // 12 轮 × user+assistant
	service.ViewMu.Unlock()

	// resumeSessionCold 路径：engineHistory = 尾部窗口（≤4 完整协议单元）。
	restored := task_context.TranscriptTailHistory(events, 1_000_000, 4)
	if err := engine.ReplaceHistory("session-1", restored); err != nil {
		t.Fatal(err)
	}
	if _, err := service.components.context.PrepareExecutionContext("task-1", "next"); err != nil {
		t.Fatal(err)
	}
	got := roundLabels(engine.History())
	want := expectedRoundLabels(12)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("after cold restore context order = %v, want original %v", got, want)
	}
}
```

红输出（实际行为）：

```text
after cold restore context order =
[user-08 answer-08 ... user-11 answer-11 user-04 answer-04 ... user-07 answer-07
 user-08 answer-08 ... user-11 answer-11],
want original [user-00 ... answer-11]
```

## 附录 B：问题二复现用例（临时红测试，已移除工作树）

同一临时文件内；执行
`go test ./application/core -run TestReproBudgetOvershootDropsNewestSettledRoundEntirely -v`。

```go
func TestReproBudgetOvershootDropsNewestSettledRoundEntirely(t *testing.T) {
	runtime := runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
	}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	huge := strings.Repeat("A", 600_000) // ≈150k tokens：小于全量预算、大于压缩目标
	for round := 0; round < 3; round++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-0", Role: "user", Content: fmt.Sprintf("request-%d", round),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-0", Role: "assistant", Content: huge,
		})
	}
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-1", "next"); err != nil {
		t.Fatal(err)
	}
	if history := engine.History(); len(history) == 0 {
		t.Fatal("newest settled round silently dropped: engine history is empty after context assembly")
	}
}
```

红输出：

```text
newest settled round silently dropped: engine history is empty after context assembly
```

## 验证命令

```text
go test ./application/core -run "TestRepro(RunningSessionAccumulatesAllRoundsInOrder|ColdRestoreTailThenFirstRequestReordersContext|BudgetOvershootDropsNewestSettledRoundEntirely)$" -count=1 -v
# 控制组 PASS；两个问题用例 FAIL（红后临时文件已从工作树移除）
```
