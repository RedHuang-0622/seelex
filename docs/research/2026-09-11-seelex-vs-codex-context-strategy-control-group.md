# Seelex vs Codex：上下文策略「对照组」实验

> 调研日期：2026-09-11 | 方法：同脚本、同串行化器、同估算器的三臂对照实验（本地可复现，不发网络请求）
> 探针：`application/core/context_strategy_ab_probe_test.go`（新增，未改任何生产代码）
> 上游：`docs/research/2026-09-12-cache-hit-vs-codex-root-cause.md`（根因）、`application/core/context_cache_divergence_probe_test.go`（首版探针）
> 复现：`go test ./application/core -run ContextStrategyAB -v -count=1`

---

## 0. 对照组设计（唯一变量 = 上下文管理策略）

四件事固定，只让"下一轮请求的消息列表怎么来"变化：

| 固定的 | 怎么固定 |
|---|---|
| 会话脚本 | 4 个用户轮，每轮 2 次工具调用 + 工具结果 + assistant 叙述/结论（`abDemoScript`） |
| 串行化 | `abSerialize`：常量头（工具 schema 签名 + instructions）+ item 流；字段序固定 |
| token 估算 | 生产同款 `seelexctx/tokens` |
| 工具目录 | 按 `name` 排序后序列化（消除"工具序"这个额外变量） |

| 臂 | 策略 |
|---|---|
| **S-prod** | 生产实测：wire 丢工具轮正文（`../Seele/session/loop.go` callLLM `Content: nil`）→ 收尾把**整个会话视图**的正文合并回 transcript（`application/core/chat.go:792-812`）→ 下一轮从 transcript **全量重投影** |
| **S-fix** | 只做一个改动：wire 与 durable 逐字节一致（工具轮正文在 wire 上保留），关掉事后合并 |
| **Codex** | 按 Codex 公开源码语义实现的**策略模型**：常量 `instructions` + append-only item 流（已发布 item 永不改写、不编造占位）+ 动态事实只在变化时写**尾部**片段 |

> 口径声明：S-* 是沿生产装配路径得到的**真实字节**；Codex 臂是按公开语义（`openai/codex@main`：`core/src/client.rs` 常量 instructions、`core/src/context_manager/updates.rs:32-60`、`core/tests/suite/prompt_caching.rs` 前缀断言）实现的**策略模型**，度量该策略在同一脚本上的可达上界，**不是**对真实 Codex 进程的抓包。工具目录在该测试服务里只有 1 个 schema，常量头偏小；真实会话约 20 个工具，结论方向不变。

---

## 1. 主结果

| 臂 | 全部相邻请求命中 | **跨轮首请求命中** | 跨轮按 miss 计费 | 跨轮"上一请求是下一请求前缀"违反 | 回合内违反 |
|---|---|---|---|---|---|
| S-prod | 83.0% | **65.9%** | **8,837 tok** | **3/3** | 0/8 |
| S-fix | 91.4% | **98.6%** | 371 tok | 0/3 | 0/8 |
| Codex | 91.4% | **98.3%** | 435 tok | 0/3 | 0/8 |

- 三个臂的**回合内**都是纯追加（0 违反）——差距全部来自**回合边界**。
- S-prod 的跨轮失效是 S-fix / Codex 的 **20.3×**（8837 vs 435 tok）。
- 把"wire 丢正文 + 事后合并"这一件事修掉，跨轮悬崖就消失（65.9% → 98.6%）：**这正是上一份报告根因 1 的单因素验证**。
- Codex 臂比 S-fix 略低（98.3% vs 98.6%）只因脚本里第 3 轮加了一次"世界状态变化"（尾部片段 188 tok）——动态事实按尾部追加的代价被如实计入了。

> ⚠️ 本表是**修复前**的实测（2026-09-11）。2026-09-12 已按 S-fix 方向落地修复（红灯用例 → 修 `RepairEmptyHistoryContent` → 转绿），修复后 S-prod 的跨轮首请求命中 **98.5%**、失效 **371 tok**、违反 **0/3**，与 Codex 臂（98.3% / 435 tok）持平；详见 §7。

### 逐边界（横轴 = 相邻请求对，跨轮用粗体）

| 边界 | S-prod | Codex | 说明 |
|---|---|---|---|
| t1.i1→i2 | 72.8% | 72.4% | 回合内追加 |
| t1.i2→i3 | 85.6% | 85.6% | 回合内追加 |
| **t1.i3→t2.i1** | **60.5%** | **98.0%** | 跨轮边界 |
| t2.i1→i2 | 85.4% | 85.0% | 回合内追加 |
| t2.i2→i3 | 90.6% | 90.5% | 回合内追加 |
| **t2.i3→t3.i1** | **63.1%** | **97.8%** | 跨轮边界 |
| t3.i1→i2 | 90.1% | 89.8% | 回合内追加 |
| t3.i2→i3 | 90.9% | 90.9% | 回合内追加 |
| **t3.i3→t4.i1** | **71.5%** | **98.9%** | 跨轮边界 |
| t4.i1→i2 | 92.2% | 92.0% | 回合内追加 |
| t4.i2→i3 | 94.3% | 94.2% | 回合内追加 |

---

## 2. 举例：同一条"第 2 轮输入"，两侧发出去的字节

脚本第 2 轮输入：`第 2 轮：继续，说明上一轮工具结果在新轮首个请求里的失效影响。`

### arm S-prod：第 1 轮最后一请求(6286 tok) → 第 2 轮首请求(6505 tok)

```
msg#0  user       92B   第 1 轮：核对 seelex 上下文装配路径…       ← 未变
msg#1  assistant  168B  我先读取装配入口与回合收尾代码…第 1 轮结论…  ← !! 被改写（wire 时 content=0B/set=false）
msg#2  tool      4560B  coordinator.go: PrepareExecutionContext…  ← 未变
msg#3  assistant  118B  [Seelex recovery note: the assistant issued…] ← !! 被改写（空正文被填成恢复注记）
msg#4  tool      2640B  chat.go: mergeStreamedToolNarration…      ← 未变
msg#5  assistant   87B  第 1 轮结论：装配顺序与文档一致…            ← 尾部新增
msg#6  user        90B  第 2 轮：继续，说明上一轮工具结果…           ← 尾部新增
```

- 首字节一致到 **12,800 / 20,628 字节**处即分叉，首个差异 **msg#1**。
- **命中 3,934 / 6,505 tok = 60.5%**（@1024 块对齐 47.2%）；**按 miss 计费 2,571 tok**。
- 代价来源不是"新增了内容"，而是**上一轮已经发过的字节被改了**：msg#1 从空正文变成"叙述+结论"，msg#3 从空正文变成恢复注记。

### arm Codex：第 1 轮最后一请求(6312 tok) → 第 2 轮首请求(6443 tok)

```
msg#0  user       92B   第 1 轮：核对 seelex 上下文装配路径…       ← 未变
msg#1  assistant  81B   我先读取装配入口与回合收尾代码…            ← 未变（发布时就带着叙述）
msg#2  tool      4560B  coordinator.go: PrepareExecutionContext…  ← 未变
msg#3  assistant   0B   （空，不编造占位）                          ← 未变
msg#4  tool      2640B  chat.go: mergeStreamedToolNarration…      ← 未变
msg#5  assistant   87B  第 1 轮结论：装配顺序与文档一致…            ← 尾部新增
msg#6  user        90B  第 2 轮：继续，说明上一轮工具结果…           ← 尾部新增
```

- 首字节一致到 **20,709 / 20,709 字节**（上一请求**整段**是本次的前缀），首个差异 = 尾部追加。
- **命中 6,312 / 6,443 tok = 98.0%**（@1024 95.4%）；**按 miss 计费 131 tok**（只有新输入 90B + 常数头尾部对齐）。
- 同样的内容、同样的 token，只是"已发字节不许改" → 差 **19.6×**。

---

## 3. 新发现：事后合并的**跨轮漂移**（本轮首次确证）

`mergeStreamedToolNarration` 每轮收尾都把**整个会话视图**里所有非空 assistant 正文作为候选交给 `MergeToolNarration`，而后者把候选**按顺序填进"最早仍然为空正文"的工具轮事件**（`task_context/task_context_state.go:449-478`）。

在长会话里"每轮遗留一个空正文工具轮"⇒ 候选数与空位数持续错位，结果是**叙述被安到不属于它的轮次上**，且每轮都在改写更早的旧事件：

```
call-1-1 content="T1 叙述：…T1 结论：…"  → ok
call-1-2 content="T1 叙述：…T1 结论：…"  → ok（同一轮第二个工具轮被写成同一条，重复）
call-2-1 content="T2 叙述：…"            → ok
call-2-2 content="T1 叙述：…T1 结论：…"  → DRIFT（正文来自第 1 轮）
call-3-1 content="T2 叙述：…"            → DRIFT（正文来自第 2 轮）
call-3-2 content="T3 叙述：…"            → ok
```

- 复现：`go test ./application/core -run ContextStrategyAB_MergeNarrationDrift -v`（**直接调用生产函数**，不经测试台装配）。
- 两个后果：① **语义错位**——模型在新轮看到的"自己上一轮说过的话"与它当时的轮次不符（不只是钱的问题）；② **前缀失效随轮数加深**——每轮都在更早的字节上改写（S-prod 的跨轮首个差异从 msg#1 → msg#3 → msg#9 递进，raw 命中 60.5% → 63.1% → 71.5%，但每轮失效的绝对 token 量在增长：2571 → 3194 → 3072）。
- 这是上一份报告根因 1 的**加深项**，不是替代项：即使把 wire/durable 正文对齐（S-fix），只要"逐步合并视图正文"这个机制还在，漂移仍会发生。修法应是在 wire 上就带上正文，并让合并只作用于**本轮**事件。
- 未验证边界：本结论由生产函数 + 生产调用序在本地复原得到，未跑真实进程端到端；真实会话中若某个回合没有"叙述"文本，漂移的形状会不同（建议用真实会话 trace 复核一次）。

---

## 4. 动态事实的安置：头部块 vs 尾部片段

同一份内容（`memory.Select` 选出的记忆块）、同一次"重新选择"事件，只改投影位置：

| 臂 | 安置 | 失效量 |
|---|---|---|
| S | 生产装配器：`0:system → 1:memory-block → 2:skill/compact-block → 3..43:累积 context → 44:当前输入` | **79,651 tok（95.3% of prompt）** |
| C | 常量 instructions + 已发布 item 不变；变化内容作为**尾部片段** | **96 tok（0.1%）** |

比值 **×830**。装配投影序由生产装配器实际输出（不是推断）：记忆块紧跟 system，位于累积 context **之前**；因此一次选择变化 = 其后全部重新计费。这正是上一份报告根因 3 的对照证实。

---

## 5. 结论

1. **跨轮悬崖 = "改已发字节"**，与"新增多少内容"无关。单因素验证：只把 wire/durable 正文对齐，65.9% → 98.6%。
2. **Codex 的优势不是玄学**：常量头 + append-only item + 动态事实只追加尾部 ⇒ "上一请求是下一请求的字节前缀"这一不变量在 3/3 个跨轮边界成立（S-prod 0/3）。
3. **动态事实的**安置位置**和它的内容一样重要**：同一份 108 tok 的记忆块，放头部失效 79,651 tok，放尾部失效 96 tok。
4. **新缺陷（漂移）**：多轮收尾的正文合并会把叙述写到别的轮次上，同时持续改写旧字节——需要独立修复，不会被"字节对齐"顺带治好。
5. 建议的修复顺序仍是：先让**命中可观测**（当前 `TokenAudit.ActualPromptTokens` 恒 0），再按 S-fix → 漂移 → 记忆块位置 → 工具序确定化 推进；本实验的 S-fix 臂即验收基线。

---

## 6. 未覆盖 / 不确定性

- Codex 臂是策略模型（见 §0 口径声明），不是真实 Codex 抓包；provider 侧严格字节前缀匹配、TTL、逐出策略仍是外部语义，本地不可验证。
- 未覆盖子代理/节点会话装配、GUI 多会话热切换的真实交错时序、`seelexctx/controller.go` 压缩路径的前缀代价。
- 漂移结论经生产函数直接复现，但未跑真实进程端到端（见 §3 尾部）。
- 工具目录在测试服务里只有 1 个 schema，常数头偏小（不影响"改动 vs 追加"的结论，但会小幅抬高两侧的绝对命中率）。

---

## 7. 修复记录（2026-09-12）：红灯 → 修复 → 转绿

### 7.1 红灯用例

`application/core/context_prefix_invariant_test.go::TestContextPrefixInvariant_CrossTurn`：沿生产装配路径（`PrepareExecutionContextFor` → `replaceEngineHistory` → `PrepareProviderHistoryFor`）驱动「第 1 轮 2 次工具调用 + 终答 → 第 2 轮继续」，断言**每一条请求的字节都以更早发出的某条请求为前缀**（相邻请求即上一条）。修复前实测（逐字摘录）：

```
prefix OK   t1.iter1  → t1.iter2  (12680 B ⊑ 22177 B)
prefix OK   t1.iter2  → t1.iter3  (22177 B ⊑ 27811 B)
prefix BREAK t1.iter3  → t2.iter1 : shared=12732/27811 B,
  first_diff=msg#1(role=assistant, tool_calls=call-read-1),
  reason=已发出字节为空、重投影时被补写正文（事后改写）
  sent    = ""
  rebuilt = "我先读取装配入口与回合收尾代码，核对工具轮的前缀语义。…"
```

### 7.2 修复（一处：`application/core/context_runtime/history.go`）

`RepairEmptyHistoryContent` 的规则改为：**携带工具调用的 assistant 消息，其 provider 投影正文恒为空**。

- 依据（wire 事实）：provider 从未收到工具轮正文——框架构造该消息时直接置 nil（`../Seele/session/loop.go:564`）。
- 旧行为有两处补写都会让下一轮重投影 ≠ 已发出：① 空正文被注入 `ToolCallHistoryContent` 占位；② durable 转写里由 `mergeStreamedToolNarration` 补写的流式叙述进入投影。
- 归零只作用于 provider 投影：**durable 转写照旧保留叙述**，视图/轨迹/重启恢复不变（红灯用例日志中 `transcript assistant(tool) #0 content="我先读取…"` 仍在）。

### 7.3 转绿（同一用例 + 两个探针，均为本次实测）

| 度量 | 修复前 | 修复后 |
|---|---|---|
| 前缀不变量（跨轮边界） | BREAK @msg#1 | **OK ×3/3** |
| S-prod 全部相邻请求命中 | 83.0% | 91.5% |
| S-prod **跨轮首请求命中** | **65.9%** | **98.5%** |
| S-prod **跨轮失效计费** | **8,837 tok** | **371 tok** |
| S-prod 跨轮违反 | 3/3 | **0/3** |
| Codex 臂（对照） | 98.3% / 435 tok | 98.3% / 435 tok（不变） |

`TestContextCacheDivergenceProbe_TwoTurnToolCliff` 的 A-production 臂同步转绿：`prefix_intact=true`、跨轮 `raw=100.5% / @1024=98.1%`、`invalidated=-40 tok`（失效后缀为 0）。

### 7.4 修复引入的耦合（报警器，不是退化）

S-fix / C 两臂（模型假设"wire 保留工具轮正文"）在修复后由 98.6% / 98.3% 掉到 **74.4% / 46.8%**：它们与本次修复对 wire 行为的假设相反，因此**变成耦合报警器**——若上游框架改为在 wire 上保留工具轮正文，必须同步取消归零；届时这两臂会重新变好而 S-prod 变差。

### 7.5 仍未修（同类"事后改写"的剩余来源）

1. **合并漂移**（§4）：叙述仍会被写进别的轮次的工具事件。修复后它只影响视图/轨迹准确性，不再影响 provider 字节与计费，仍需单独修。
2. **空工具结果的占位**：`RepairEmptyHistoryContent` 对 `tool` 角色空正文仍注入 `MissingHistoryContent`；若某工具返回空结果，下一轮重投影会从 "" 变成占位（同类分叉）。**未复现，列为待验证项**。
3. **命中观测**仍缺（`TokenAudit.ActualPromptTokens` 恒 0）：前缀不变量现在**测试内可测且被守卫**，但线上仍看不到真实命中率。
