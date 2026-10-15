# 上下文压缩链路 / 折叠链路 + 开关齐全性 + 真实 API 测试覆盖 · 审查报告

- 日期：2026-09-29
- 审查对象：`HEAD = 90ffb57`（`git status` 干净，本报告只读审查，未改任何文件）
- 范围：① 压缩链路（回合内框架控制器）② 折叠链路（回合开始前装配层）③ 两条链路相关的全部配置开关是否接线齐全 ④ 这些开关/链路是否经过真实 provider API 测试
- 已实跑的本地门禁（不消耗 API 配额）：
  - `go test ./seelexctx/... -count=1` → 9 个包全 `ok`
  - `go test ./application/core/... -count=1` → 17 个包全 `ok`

---

## 0. 结论摘要

| # | 结论 | 级别 | 证据强度 |
|---|---|---|---|
| A | **装配层折叠产出的帧从未进入会话压缩栈**：能力面（`CompactionIndexPort`）、落点（`RT.PushCompactionFrame`）、适配器（`internal/adapters`）三层都在，但**全仓没有任何调用方**。`190049e` 提交标题声称「装配层折叠推帧进会话压缩栈…本文件接通的就是这一条」，实际缺调用点。 | P1 | Confirmed（`git grep` 全文零调用方） |
| B | **`limits.context_compaction_summary.{enabled,input_tokens,chapter2_tokens}` 三个键当前完全不生效**：唯一注入 `Summarizer` 的构造 `MainCompactionDAG` 只被 A 里那个没有调用方的入口使用。`CHANGELOG` 已如实标注 "Not yet live"，但 `config/seelex.yaml` 的注释只描述「打开的代价与收益」，没写「当前打开也不生效」。 | P1 | Confirmed（提交自述 + 调用链唯一性） |
| C | **真实 API 测试证据断档**：全仓只有 2 个 `-tags compactlive` 冒烟覆盖压缩/折叠（软线装配折叠 + 手动入口；保护区下限）。仓内可查的**最后一次真实运行是 FAIL**（`smoke_live.log`，2026-09-23）；文档记录的 PASS 是 2026-09-19，2026-09-24 声称门禁全绿。此后 09-26 阈值上调、09-28 帧正文 v2 + 跨回合记录 + 厚摘要开关、09-29 折叠推帧/检索改口径，**均无真实 API 复跑记录**。 | P1 | Confirmed（日志/文档/提交时间线） |
| D | 无「自动压缩总开关」。全仓只有两个 `yaml:"enabled"`（`async_exec`、`context_compaction_summary`），压缩只能靠把比例调到 100% 近似关闭（且 `>100` 会在 `LoadLimits` 报错）。 | P2 | Confirmed |
| E | `context_soft_percent < context_hard_percent` 是配置注释里写明的约束，但**代码不校验**（`LoadLimits` 只校验 `[0,100]`）。配反了会让自主折叠每轮抢跑（注释自己写明了这个后果）。 | P2 | Confirmed |
| F | 文档漂移：`seelexctx/limits.go:376`（"读取 seele.yaml"）、`SessionStorageLimits` 注释、`seelexctx/window.go:31`、`context_control` 包注释都写 `seele.yaml`，而 `main.go` 实际把 `config/seelex.yaml` 传给 `LoadWindowConfig`/`LoadLimits`。 | P3 | Confirmed |
| G | 配置注释「同一份值被两个触发层消费」对 `context_single_item_percent` **不成立**：只有装配层 `task_context` 消费它（`seelexctx` 只把它当结构字段，无消费点）。 | P3 | Confirmed |
| H | 未验证风险：框架控制器 `policy()` 没有 `outputReserve >= window` 防护（`Budget()` 可为负 → 软阈值 ≤ 0 → 每次 `after_tool` 都折叠），是否有上游账号校验拦住未确认（`Seele` 配置包在本仓之外）。 | Hypothesis | 需一次复现或读 Seele 配置校验 |

---

## 1. 两条链路是什么（含调用点）

### 1.1 压缩链路（回合内框架控制器）

| 环节 | 位置 |
|---|---|
| 触发事件 | Seele `ReActLoop` 每次迭代发 `before_model` / `after_assistant` / `after_tool`；宿主只处理后两者 | `seelexctx/controller.go:252`（`Handle`，只有两个 case） |
| 软阈值判据 | `CountHistory(ev.History)+CountText(ev.Query) >= policy().SoftThreshold()` | `controller.go:273-276` |
| 超大工具结果 | `oversizedTool` → `hardThresholdPath`（先归档 `result_ref` 再压） | `controller.go:256, 388-395` |
| 窗口外压缩 | `compressWindowOutsideWith`（只压窗口外轮次） | `controller.go:401-408` |
| 帧生成 | 优先走压缩 DAG（`ControllerOptions.Compaction`），失败回退本地折叠 | `controller.go:478`；`seelexctx/dag.go:157` |
| 产物落栈 | `Stacks.PushCompact(frame)` | `controller.go:453` |
| 开关注入 | `ControllerOptions{Policy, Window, Budget, Stacks, MaxToolResultChars, FrameCarryTokens, Compaction}` | `seelebridge/runtime_context.go:207-257`（主会话）、`nodeController`（节点，栈为内存态） |

### 1.2 折叠链路（回合开始前装配层）

| 环节 | 位置 |
|---|---|
| 入口 | `PrepareExecutionContextFor`，调用点 3 处（正常回合 / ReAct 预算终局 / provider 恢复重试） | `coordinator.go:389, 434` |
| 判据（三条，命中任一即折叠） | `fold := rawTokens >= budget.SoftThreshold \|\| hardCompact \|\| options.forceCompact` | `coordinator.go:528`（`hardCompact` = `windowConfig.MustCompact(allContextTokens)`，`coordinator.go:522`） |
| 纪元节流 + 首压留痕 | `newCheckpoint := fold && (forceCompact \|\| hardCompact \|\| state.CompactedEpoch != state.ProgressEpoch \|\| len(state.ContextCompactions) == 0)` | `coordinator.go:536-541` |
| 保留窗口决策（含下限） | `retainWindowDecision(windowConfig, allContextTokens, budget, limits.Get().ContextRetainFloorPercent)` | `coordinator.go:578`；实现在 `context_runtime/layout.go` |
| 装配（有界窗口） | `fitExecutionHistory(..., target, compacting, 0)`；装不下按 `limits.context_max_units` 逐级收缩 | `coordinator.go:615, 779-812, 804` |
| 达峰抢跑（自主压缩） | `estimated > budget.HardThreshold` → `compressExecutionHistory`（稳定 system 前缀 + 有界 checkpoint 摘要 + plan + 当前输入） | `coordinator.go:626, 850-875` |
| 在飞 tool_call 尾部拼接 | `replacement := c.withInFlightTail(existing, assembled)` | `coordinator.go:653`；`fold_history.go:57-99` |
| 写回引擎历史 + 归一化 | `replaceFoldHistory` → `history.PrepareProviderHistoryFor` | `coordinator.go:654, 657` |
| 累积起点前移 | `state.ContextRetainedFrom = compressedTo` | `coordinator.go:670` |
| 记录（快照面） | `model.ContextCompaction{Version,Reason,Origin,EventFrom/To,MessageFrom/To}` → `RecordContextCompactionLocked` | `coordinator.go:734` |
| 帧正文（内容存储面） | `compactionFrameBody(...)`（v2：JSON 元数据 + Markdown 读后感）→ `StoreToolResultForLocked(sessionID, compactionFrameTool, frame)`，只把 `FrameRef` 放进快照 | `coordinator.go:707, 729-730`；`compaction_frame.go` |
| 回读 | 单元测试证明按 `FrameRef` 可读回正文 | `context_compact_test.go:242-247` |

### 1.3 与本审查相关的另外两条路径

- **真空区覆盖（gap）**：`coverHistoryGap` —— **压缩栈为空时直接 `return nil`**（"会话从未在运行期产生过 CompactStack 帧"就不做补压），见 `seelebridge/runtime_context.go:44-67`（判断在 `:57`）。
- **wire 装配预算压缩**（存储侧第三条压缩：`wire = compact 摘要 + 最新帧之后事件 + 最近 K 次尝试`，超软预算即 `need_compact`）：`sessionstore/wire_assembler.go` + `storage_settings.applyWireBudget`，生产读路径在 `application/core/session_history.go:315`。开关默认值即生效，不依赖显式配置。

---

## 2. 开关清单与接线核对

图例：✅ 有实际消费点且被测试；⚠️ 消费点存在但缺测试；❌ 消费点缺失/不生效。

| 配置键 | 结构字段 | 注入点 | 生效判据 | 状态 |
|---|---|---|---|---|
| `window.rounds` | `WindowConfig.Rounds` | `main.go` → `core.ApplyWindowConfig` + `RuntimeConfig.WindowConfig` → `NewDefaultWindowPolicy`（`seelebridge/runtime.go:312`） | `WindowRounds` 显式覆盖优先 | ✅ 单测 `window_policy_test.go` |
| `window.ratio` | `WindowConfig.Ratio` | 同上 | `min(token1, ticket2)` 的 token2 + 轮数推导 | ✅ |
| `window.retain_tokens` | `WindowConfig.RetainTokens` | 同上 | 保留前缀上限 token1 | ✅ `window_retain_test.go` |
| `window.force_compact_tokens` | `WindowConfig.ForceCompactTokens` | 同上 | `MustCompact(all_context)` → 绕过纪元节流强制折叠（`coordinator.go:522,528,536`） | ⚠️ 有单测，无真 API 用例 |
| `window.min_rounds` / `max_rounds` | 同名字段 | 同上 | 轮数 clamp；`windowTailBudget` 冷读分片宽度 | ✅ |
| `limits.context_safety_reserve_divisor` | `ContextSafetyReserveDivisor` | `NewContextWindowPolicy(window, output, limits)` + `task_context.newContextBudget` | 预算 = 窗口 − 输出 − 窗口/除数（两层同源） | ✅ `controller_limits_test.go` |
| `limits.context_soft_percent` | `ContextSoftPercent` | 同上 | 软线：两层各自的 `SoftThreshold` | ✅ 单测 + `compactlive` 冒烟（见 §3） |
| `limits.context_hard_percent` | `ContextHardPercent` | 同上 | 硬线：装配层抢跑 / 控制器窗口上限 | ⚠️ 仅单测 |
| `limits.context_target_percent` | `ContextTargetPercent` | 同上 | 折叠后落点 = 保留区硬上限 | ⚠️ 仅单测 |
| `limits.context_single_item_percent` | `ContextSingleItemPercent` | 仅 `task_context/token_counter.go:196` | `SingleItemInputLimit` 外置单条超大输入 | ⚠️ 仅单测；**回合内控制器不消费**（与配置注释不符，见 G） |
| `limits.context_retain_floor_percent` | `ContextRetainFloorPercent` | `coordinator.go:578`、`application/core/window.go`（读尾预算 + 启动校验） | `retained = clamp(min(t1,t2), floor, t1)`；`floor > retain_tokens` 启动期报错 | ✅ 单测 + `compactlive` 冒烟（§3） |
| `limits.context_frame_carry_tokens` | `ContextFrameCarryTokens` | `seelexController`/`nodeController`（`runtime_context.go:127,218`）、`coverHistoryGap`（`:72`）、`MainCompactionDAG`（`:321`） | 帧 Chapter 2 并入上限，超出退化锚点 | ⚠️ 仅单测 `frame_carry_test.go` |
| `limits.context_max_units` | `ContextMaxUnits` | `coordinator.go:804` | 装配逐级收缩扫描上限 | ⚠️ 仅间接单测 |
| `limits.max_tool_result_chars` | `MaxToolResultChars` | processor + 控制器 + 应用归档（三层同源） | 超限即外置 `result_ref` | ✅ `controller_limits_test.go` + 离线全链 `prefix_invariant_fullchain_test.go` |
| `limits.context_compaction_summary.enabled` | `CompactionSummaryLimits.Enabled` | 仅 `runtime_context.go:278,307`（`MainCompactionDAG`） | 打开则折叠注入 `PrefixReplaySummarizer`（前缀重放厚摘要） | ❌ **不生效**（唯一调用方缺失，见 A/B） |
| `limits.context_compaction_summary.input_tokens` | `InputTokens` | `replayInputTokens`（`runtime_context.go:260`） | 重放分片片预算；0 → 账号窗口 × 3/4 | ❌ 同上（且分片链只有 fake 单测） |
| `limits.context_compaction_summary.chapter2_tokens` | `Chapter2Tokens` | `MainCompactionDAG`（`:320`） | 厚摘要输出预算；0 → 2048 | ❌ 同上 |
| `limits.session_storage.wire_*` | `WireBudgetTokens/WireSoftRatio/WireTargetRatio` | `storage_settings.applyWireBudget` | wire 装配软阈值 | ✅ 单测 `wire_soft_budget_test.go`、`retention_search_blob_test.go` |
| （缺）自动压缩总开关 | — | — | — | ❌ 不存在（见 D） |

---

## 3. 真实 API 测试覆盖矩阵

真实 API 用例全部 opt-in 且靠 `SEELEX_SMOKE_ACCOUNTS` 环境变量跳过；`config/accounts.yaml` 被 `.gitignore` 忽略（仓内只有 `accounts.example.yaml`）。相关 build tag：`compactlive`（2 个文件）、`manualsmoke`（含长上下文/前缀不变量）、`manualsmoke2`。

| 能力 | 真实 API 证据 | 判定 |
|---|---|---|
| ① 软阈值自动折叠（装配层）+ `/compact` | `compact_live_smoke_test.go`（`-tags compactlive`，`TestCompactLiveSmoke`/`...Preflight`） | 有，但**最后一次可查运行 FAIL**（`smoke_live.log` 2026-09-23「真实回合未回到 idle」）；文档 PASS 记录 2026-09-19（`docs/devlog/2026-09-19-worktable-dispatched-composer-input-compact-smoke.md:99,124`，238.28s） |
| ⑤ 保护区下限 `context_retain_floor_percent` | `prefix_chain_live_test.go`（`TestPrefixChainRetainFloorLiveSmoke`，同时改 `window.ratio=0.05`） | 有：文档记 `baseline retained=1361/floor=0 → floored retained=16476/floor=16476`（`docs/arch/context-prefix-chain.md:472,506`）；**但晚于它的改动未复跑** |
| ② `force_compact_tokens` 硬压缩 | 无 | 只有 `window_retain_test.go` / `context_window_rule_test.go` 单测 |
| ③ `target` 落点 | 无专测（随①②间接） | 仅单测 |
| ④ 单条超大输入外置（`context_single_item_percent` / `max_tool_result_chars`） | 无真 API 专测 | 离线全链 `prefix_invariant_fullchain_test.go`（mock provider + httptest） |
| ⑥ `context_frame_carry_tokens` | 无 | 单测 `frame_carry_test.go` |
| ⑦ 折叠处 LLM 摘要开关（开/关两分支） | 无 | **单测也没有**：全仓无测试构造 `Enabled=true`；`dag_test.go`/`replay_chunk_test.go` 只用 fake Summarizer |
| ⑧ 装配层折叠 | 同 ① | 有（同 ① 的证据链） |
| ⑨ 回合内控制器折叠 | 无（`compactlive` 走的是应用链路与显式入口，未断言控制器独立触发） | 单测 `controller_test.go` / `controller_limits_test.go`（用 fake 事件） |
| ⑩ 真空区覆盖 gap | 无 | 单测 `gap_test.go` |
| ⑪ 冷恢复/重启后折叠产物回读 | 部分：`context_long_live_test.go`（`manualsmoke`）只断言长上下文 + resume 后仍答出开头身份，**不断言折叠产物** | ⚠️ |
| ⑫ 手动 `compact_context` 工具 | 覆盖入口注册（`main.go:420`）与命令同落点；真 API 侧只被 ① 里的 `/compact` 覆盖 | ⚠️ |
| ⑬ `search_history` / `read_compressed_turn` 回读 | 无真 API；单测覆盖（`seelexctx/search/event_seq_range_test.go` 等） | ⚠️ |
| ⑭ 子代理（node）折叠 | 无 | 节点栈为内存态（`nodeController`），无 live 覆盖 |
| ⑮ wire 装配预算压缩 | 无（离线单测） | ⚠️ |

**判定**：真正被真实 API 验证过的只有「装配层软线折叠 + 手动 /compact」与「保护区下限」两件事；`context_compaction_summary`、`force_compact_tokens`、`frame_carry`、`single_item`、`max_units`、回合内控制器触发、gap、检索回读**都没有真实 API 证据**（其中厚摘要开关连单测都没有，且当前不生效）。

---

## 4. 问题清单（含证据与建议）

### P1-A 装配层折叠不推帧 → 三处下游能力对这类会话仍然失效
- **证据（无调用方）**：`git grep PushCompactionFrame HEAD` 只返回 3 处：接口声明 `application/core/context_runtime/ports.go:138`、适配器实现 `internal/adapters/compaction_index_port.go:20`、落点实现 `seelebridge/runtime_compaction_index.go:58`。`CompactionIndexPort` 同样只在这三处出现；`context_runtime.Deps`（`ports.go:142-159`）**没有**注入口，协调器内也无类型断言探测。
- **提交自述与实现不符**：`190049e` 提交信息写「本文件接通的就是这一条」，但其改动清单为「能力面 / 落点 / 区间口径 / 帧正文 v2」四层，**没有调用点层**；该提交对 `coordinator.go` 的 diff 只做了帧正文 v2 的参数改造（`-95 行` 是 v1 渲染器搬走）。
- **后果链（有代码依据）**：装配层折叠只写 `ContextCompactions` 记录 + 内容存储 `FrameRef`（`coordinator.go:729-734`），不写 `CompactStack`；而
  1. 记忆块初筛读栈（`seelebridge/runtime_context.go:136+ relatedMemoryBlocks`，栈空 → 不注入）；
  2. `search_history` 帧索引读栈（栈空退化为尾部扫描）；
  3. 真空区覆盖在栈空时**主动跳过**（`runtime_context.go:57`）。
  即：只会发生装配层折叠的会话（最常见的那条），这三条路径仍然不工作——与提交自己描述的现场一致。
- **测试**：全仓无任何测试引用 `PushCompactionFrame`（`git grep` 零命中），因此这个缺口不会被现有 CI 抓到。
- **建议**：在 `coordinator.go` 的折叠落点（`coordinator.go:707` 帧正文生成附近）补一次可选能力探测调用；`Deps` 增 `CompactionIndex`（或用类型断言探测 `c.Deps.Runtime`），并把「推帧成功/降级原因」写进门禁 Detail；同时补一条离线用例：折叠后 `store.Snapshot().CompactStack` 长度 +1 且 `SegmentID` 与记录互链。

### P1-B `context_compaction_summary` 三个键当前不生效
- **证据**：`e2e197d` 提交信息自述「application/core/context_runtime 的折叠入口尚未接这个 DAG（其 Deps 没有注入口），因此当前树里 **MainCompactionDAG 没有调用方**——打开开关还不会改变行为」；`CHANGELOG.md` 同批条目同样标注 "**Not yet live:** nothing calls `MainCompactionDAG`"。代码侧：`MainCompactionDAG` 仅被 `runtime_compaction_index.go:70` 使用，而该入口无调用方（见 A）。
- **不一致点**：`config/seelex.yaml` 该块注释只讲「打开的代价/收益」，未写「当前打开也不生效」；`seelexctx/limits.go:140-175` 的字段注释同理。以「关就是关、开就生效」为默认预期的使用者会被误导。注意 `CHANGELOG` 的诚实标注与配置注释的沉默形成落差。
- **建议**：要么接线（同 A），要么在 `config/seelex.yaml` 该块首行加「当前未接线，打开不生效（截至 2026-09-29）」并把 `LoadLimits` 在 `enabled: true` 时打一条启动告警（可选），让它像 `async_exec` 那样「开关语义与现状一致」。

### P1-C 真实 API 证据断档（含一次可查的 FAIL）
- **证据**：`smoke_live.log`（2026-09-23 20:21-20:25）`--- FAIL: TestCompactLiveSmoke (240.25s)`，失败点是「第 1 轮：真实回合未回到 idle（context deadline exceeded）」，当时会话停在 `tool_result: Path...` 的工具行。
- 文档记录：`docs/devlog/2026-09-19-...-compact-smoke.md:99,124` PASS 238.28s；`docs/arch/context-prefix-chain.md:472` 给出 `compactlive` 门禁命令并声称 2026-09-24 三门口禁全绿。
- 之后的相关改动都没有复跑记录：`7507138`（阈值上调 75/90 → 95/98/80）、`e2e197d`（厚摘要开关）、`366fd1b`（InLoop 删除、折叠走回合闸门）、`190049e`（推帧 + 帧正文 v2 + 检索改按 EventSeq 反查）。
- **建议**：把 `compactlive` 三件套纳入发布前门禁（至少 release 前跑一次并把日志留档），并在 `CHANGELOG` 相应条目补「真 API 复跑状态」。

### P2-D 无自动压缩总开关
- **证据**：`git grep 'yaml:"enabled"'` 全仓只有 `seelexctx/limits.go:154`（`async_exec`）与 `:164`（`context_compaction_summary`）。压缩只能靠 `context_soft_percent=100` + `context_hard_percent=100` + `force_compact_tokens=0` 近似关闭，而这条路语义并不等价「不压缩」：装配层仍会在 `estimated > budget.Budget` 时以 `ErrProviderContextBudgetExceeded` 拒绝发送（`coordinator.go:651`）。
- **建议**：明确产品口径——「宁可拒绝也不折叠」要么成为显式开关（`context_compaction.enabled: false` → 超预算即拒绝，且日志说明），要么在配置注释里写明「如何近似关闭 + 代价」。

### P2-E `soft < hard` 无校验
- **证据**：`seelexctx/limits.go` 的 `LoadLimits` 只对五个比例做 `[0,100]` 越界报错（键名表 `:422-426`，报错点 `:429`）；无 soft/hard 相对关系校验。`config/seelex.yaml` 却把它写成约束：「约束：context_soft_percent 必须 < context_hard_percent」。
- **建议**：在 `LoadLimits`（或 `main.initRuntime` 与 `ValidateRetainWindow` 同处）加一条 soft ≥ hard 的显式报错，理由与 `ValidateRetainWindow` 相同——静默接受非法组合会让「每轮重压」以性能症状出现。

### P3-F / P3-G 文档与注释漂移
- F：`seelexctx/limits.go:376`、`SessionStorageLimits` 注释、`seelexctx/window.go:27`、`application/core/context_control/window_policy.go` 包注释都称配置段在 `seele.yaml`；实际 `main.go` 传给 `LoadWindowConfig`/`LoadLimits` 的是 `config/seelex.yaml`（`firstExisting("config/seelex.yaml", "seelex.yaml")`）。`config/README.md` 也把 `limits` 画在 `seele.yaml` 一侧。**处置**：改注释/图，别改代码。
- G：`config/seelex.yaml` 压缩预算块注释称「同一份值被两个触发层消费」；对 `context_soft/hard/target` 成立，对 `context_single_item_percent` **不成立**（只有 `application/core/task_context/token_counter.go:196` 消费）。**处置**：注释按条目分别说明消费者。

### H（Hypothesis，未验证）控制器侧 `policy()` 缺下界防护
- 现象：`seelexctx/controller.go` 的 `policy()` 只兜 `Window<=0` 与 `OutputReserve<=0`；若 `OutputReserve >= Window`，`Budget()` 为负 → `SoftThreshold()` ≤ 0 → `tokens >= 软阈值` 恒真 → 每次 `after_tool`/`after_assistant` 都走折叠。
- 与任务层的不对称：装配层 `task_context.ContextBudgetFor` 有显式防护（`outputReserve+window/8 >= window` → 回退默认预算）。
- 待验证：是否存在上游账号校验（`compact_live_smoke_test.go` 注释提到「校验规则要求 max_tokens < window − window/8」，该规则不在本仓，应在 Seele 配置包内）。**验证方法**：构造 `ContextWindowPolicy{Window:1000, OutputReserve:1000}` 调 `SoftThreshold()` 看是否为 0，并追 Seele 账号校验是否可达。

---

## 5. 建议的复跑与补测清单

```powershell
# 本地门禁（已跑，全绿）
go test ./seelexctx/... -count=1
go test ./application/core/... -count=1

# 真 API 冒烟（需要凭据；当前树最后一次可查运行是 FAIL，务必复跑）
$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
go test -tags compactlive . -run 'TestCompactLivePreflight|TestCompactLiveSmoke|TestPrefixChainRetainFloorLiveSmoke' -count=1 -v -timeout=15m

# 长上下文 + resume（manualsmoke）
go test -tags manualsmoke . -run 'TestManualSmokeRealAccountLongContextRehydration' -count=1 -v -timeout=15m
```

补测优先级：
1. 装配层折叠推帧的离线用例（P1-A）——这是当前唯一能防「声称接通其实没接通」的钉子；
2. `context_compaction_summary.enabled=true` 的 DAG 单测（fake Summarizer 已有，只差一条 `Enabled:true` 的构造断言）+ 接线后的一条 live 用例（开关两臂）；
3. `force_compact_tokens` / `frame_carry` / `context_max_units` 各一条带牙用例（当前只有纯函数单测）。

---

## 6. 证据索引

- 提交：`190049e`（装配层推帧，无调用点）、`e2e197d`（厚摘要开关，自述未接线）、`366fd1b`（InLoop 删除）、`7507138`（阈值上调）、`35c1331`（CHANGELOG 如实标注 Not yet live）
- 代码：`application/core/context_runtime/coordinator.go:389,434,522,528,536,578,615,626,651,653,654,657,670,707,729,734,779,804,850`；`fold_history.go:57-99`；`ports.go:125-159,138`；`seelebridge/runtime_context.go:44-67,57,136,207,260,278,304,307,320`；`seelebridge/runtime_compaction_index.go:58,70`；`internal/adapters/compaction_index_port.go:20`；`seelexctx/controller.go:252,273,388,401,453,478`；`seelexctx/limits.go:376,422-429`；`seelexctx/window.go:27,184-192`；`application/core/task_context/token_counter.go:150-200`；`application/core/window.go:41,59`；`main.go:420,526-551`
- 测试/日志：`compact_live_smoke_test.go`、`prefix_chain_live_test.go`、`context_long_live_test.go`、`real_api_prefix_live_test.go`、`smoke_live.log`、`docs/devlog/2026-09-19-worktable-dispatched-composer-input-compact-smoke.md:99,124`、`docs/arch/context-prefix-chain.md:472,506`、`docs/devlog/2026-09-28-compaction-record-across-rounds-and-browsing-submit.md`
- 本次实跑：`go test ./seelexctx/... -count=1`（9 包 ok）、`go test ./application/core/... -count=1`（17 包 ok）
