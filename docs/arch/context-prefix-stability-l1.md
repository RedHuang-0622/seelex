# 上下文前缀稳定性：L1 / draft / actor 三层 + memory 纪元表

> 状态：**设计 + 核对结论，未改任何代码**。三条决策已拍（§9），实现按 §8 的 M0–M5 推进。
> 关联文档：[`context-prefix-chain.md`](context-prefix-chain.md)（现行装配顺序与压缩四区模型，本文替换其"每轮重投影"部分）、
> [`../research/2026-09-12-cache-hit-vs-codex-root-cause.md`](../research/2026-09-12-cache-hit-vs-codex-root-cause.md)（断点归因基线）、
> [`../research/2026-10-29-agent-prefix-cache-mechanisms.md`](../research/2026-10-29-agent-prefix-cache-mechanisms.md)（同行实现对照与取舍）。

---

## 0. 一句话

> **凡是已经发到 wire 上的字节，此后只允许被"指针移动"或"新帧替换"，永远不允许被重渲染。**

这条就是全部设计的判据。L1 缓存只是把"重渲染"从**每轮**降到**每次冷加载 / 每次压缩**；它本身不产生命中率，它只是让正确的前缀被复用。反过来说：如果 draft 提交到 truth 时"按逻辑内容重新拼一遍"，L1 再快也没有意义——因为那正是今天的缺陷（wire 上是 `环境块 + 输入`，记录里是 `原始输入`）。

因此本设计的核心不是"加三层缓存"，而是**把重渲染从热路径上彻底赶走**。

---

## 1. 五条不变量（缺一条整套就失效）

| # | 不变量 | 违反它时的现象 | 当前代码谁会违反 |
|---|---|---|---|
| **I1** | **字节冻结**：进入 truth 的正文只来自"已发出字节"（wire material），禁止按逻辑内容重渲染 | 下一轮重投影与上次请求分叉，从该消息起整段失效 | `composeRequestTail`（``kernel/context/coordinator.go:602-607``）；`ProviderContent` 只覆盖工具结果 |
| **I2** | **只追加**：动态状态变化 → 追加新 item；禁止回头改写既有 item | 每轮在注入点断裂，上一轮整段重新计费 | plan 上下文消息（`coordinator.go:1331`）、tailStacks（``backend/engine/context/seelectx/assembler.go:120``） |
| **I3** | **指针而非平移**：窗口移动 = 移动 `retained_from` + 换 compact 帧；禁止从全量历史重新切窗口 | 头被切掉 ⇒ 请求不再是上一条的扩展 ⇒ 近乎全 miss | `TranscriptTailWindowBy` 每轮重算（`coordinator.go:1328`） |
| **I4** | **前缀有键**：进入 prefix 的每一段必须有版本键；**键里不得含本轮 query**；无键的动态信息一律走 draft | 键随 query 变 ⇒ 前缀整段作废 | memory 按 `LastUserQuery` 选（`assembler.go:88`）；工具定义顺序（`vendor/.../Seele/tools/tools.go:171,286`） |
| **I5** | **L1 是派生数据**：可从 `truth + compact + memory 纪元表` 重建，不持久化、不是事实源 | 一旦当事实源，崩溃即丢事实；或出现两份互相打架的真相 | 无（新增约束） |

I5 在仓库里有先例可循：项目看板是 `.seelex/board/` 的**派生投影**、不是事实源（见 [`seelex-project-board.md`](seelex-project-board.md)）。L1 按同一纪律处理。

---

## 2. 三层职责

| 层 | 内容 | 事实源 | 读/写 | 生命周期 |
|---|---|---|---|---|
| **L1**（快路径） | 已装配前缀：`system + project + memory@epoch + compact@seq + truth[retained_from:]` | 否（派生） | 无锁读 | 进程内；崩溃即丢，冷加载重建 |
| **draft**（慢路径） | 本轮未定稿尾巴：current input、env/worktable、plan/task 帧、工具轮增量 | 否 | actor 单写 | **轮尾提交，ack 确认后清空工作区**（提交内容逐字节提升进 truth，§3.5） |
| **truth**（事实源） | append-only transcript（`Seq`/`MessageID` 已定）+ compact 栈 + `retained_from` + memory 纪元表 | 是 | actor 单写 | 落盘，跨重启 |

**"draft 每轮清空"的唯一正确读法**：清空的是**工作区**（下一个轮次从空开始），不是把已发出的字节从历史上抹掉。draft 一旦随请求发出，它的字节就是既成事实，必须原样进 truth；否则第二天又回到今天这个 bug。而且**清空动作以 ack 为前提**：没收到提交回执就清空，等于赌落盘成功（§3.5）。

---

## 3. 四种时序（字符画）

### 3.1 请求打入

```
req ──▶ seq := alloc(session)                      ← 在 actor 外分配，会话内单调
         │                                          （入队序 == seq 序，actor 只做 FIFO）
         ├──▶ L1.messages(prefix_key)                ← 快路径：只读、零锁
         │
         ├──▶ tail := draft(open)                    ← 慢路径：本轮新字节
         │
         └──▶ signal(seq) ──▶ ┌──── actor（每会话单写者）────┐
                               │ ① append  draft(seq, bytes)    │
                               │ ② promote draft(seq) ─▶ truth  │
                               │     （逐字节；禁重渲染）        │
                               │ ③ bump version / 失效派生项     │
                               └── FIFO · 有界 · (session,seq) 幂等 ┘
```

三条硬约束：

1. **seq / messageID 必须在 actor 外分配**，且与 transcript seq 同源。若让 actor 自己排序，削峰会退化成排序，队列一长就变回锁。
2. **幂等键是 `(session, seq)`**：重复投递合并，不产生第二条。崩溃重放靠这条。
3. **队列满 = 背压（让调用方等或显式拒绝），绝不丢弃**。丢一条 draft 就等于把前缀永久劈开，比慢一点糟得多。

### 3.2 冷加载恢复

```
cold_load:
  system_prompt
  ‖ active compact seq（stack top）
  ‖ memory@epoch（= compact_seq；由 memory_update 表 fold 出来，不重新选择）
  ‖ truth_messages[retained_from : ]
  ‖ draft[open]                      ← 未定稿尾部（中断恢复时按 seq 重放）
  ▼
  L1 ← 上面这一段（派生，不落盘）
```

冷加载是**唯一**允许"从头拼一遍"的路径，因为它本来就低频，且拼出来的字节由 truth 决定、不由本轮 query 决定。

### 3.3 压缩（唯一允许改写前缀的事件）

```
压缩前:  [system][mem@e1][compact@c1][ …… retained_from ──▶ right ……▶ tail ]
                                        ▲left                    ▲right

压缩后:  ① 新 compact seq = c2
         ② left ← 新位置（只移指针，不平移内容）
         ③ epoch ← c2（memory 重新快照一次，走纪元表写 add/retire 行）
         ④ [compact@c1 … left) 并入 c2；e1 的 memory 一并落在该区间内

         [system][mem@e2][compact@c2][ …… retained_from' ──▶ right ……▶ tail ]
                               └ 已在 wire 出现过的字节一个都没改 ┘
```

压缩是**全链唯一**允许"整条前缀失效一次"的地方（这也是 DSH/Codex 的共同取舍），此后重新吃长命中。

### 3.4 memory 纪元滚动

```
e1:  [mem@e1][ctx 累加 ……………………………………………………………][draft]
     └ 整个 e1 内 memory 不移位、不重选（命中稳定）

     │ 压缩（见 3.3）
     ▼
e2:  [mem@e2][ctx 从 retained_from' 重新累加 …………………][draft]
     └ e1 的 memory 已被 c2 的摘要覆盖 → 在纪元表里 retire
```

回答"既有 memory 去哪了"：它落在 `[compact@c1, retained_from)` 这段被 fold 的区间里，随区间一起进 c2；纪元表只记一条 `retire`，**不删行**。

### 3.5 draft 提交与 ack（已裁决：轮尾提交，ack 之后才清空）

```
轮内:   req1 ──▶ req2 ──▶ req3        每次只往后追加 draft，draft 不重渲染
                              │
轮尾:                         ▼
        draft ──commit(seq_range)──▶ actor ──▶ truth（append + 落盘）
          │                                        │
          │◀──────────── ack{seq_through, digest} ─┘
          ▼
        清空 draft 工作区 ＋ 推进"上次提交水位" = seq_through
```

- **ack 之前 draft 不清空。** 没收到 ack 就清空等于赌落盘成功：崩一次就丢一轮字节，且下一轮前缀永久分叉。
- **重试幂等**：重试携带同一 `(session, seq_range)`，actor 侧合并，不产生重复条目。
- **轮间屏障**：下一轮开轮前 await 上次 ack（有界超时）。否则可能在"半提交"状态上读 L1。
- **提交失败不丢**：重试 N 次；仍失败则把 draft 标 `uncommitted` 且**保留**，下一轮开轮时先合并提交。静默放弃是不可接受的——用户已经看到输出了、历史里却没有，那就是 wire ≠ truth 的另一种形态。

---

## 4. 前缀键 `prefix_key`

L1 的复用判据必须是**显式键**，不允许隐式"看起来一样就复用"——否则失效无法归因。

```
prefix_key = {
  system_digest    // plugin / effort / instructions / 插件级 skill 目录
  project_version  // storeproject.Version（内容 hash）
  memory_epoch     // = compact_seq；绝不含 query
  compact_seq      // 压缩栈顶序号
  tools_epoch      // 工具集纪元（§5.6）；只在 catalog 变化时 bump
  tools_digest     // 该纪元 catalog 的规范化序列化 hash（工具名排序后）
  retained_from    // left pointer
}
```

规则：

- 键不变 → 直接复用 L1；键变 → **显式失效并打点**（记哪一格变了）。
- **键里不允许出现"本轮"任何东西**（query、时间、cwd、打开的文件）。这类信息一律走 draft 作为尾部追加。
- **`tools_digest` 的语义是"catalog 的 hash"，不是"本轮暴露了哪些工具"**。按后者算，goal 门开合、权限档变化、MCP 懒加载都会让它变，等于把工具集变成逐轮变量。按前者算，它在会话内恒定（§5.6）。
- 这条规则有外部佐证：Aider 的 repo-map 缓存曾把"本轮提到的文件名/标识符"放进 cache key，导致每次请求都全量重扫，后来专门修掉（`Aider-AI/aider` PR #5556，把 mentions 从 key 里移除）。我们现在的 memory 块就是同一个错误的另一种形态。

---

## 5. memory 纪元表（`memory_update`）

### 5.1 为什么需要一张表，而不是直接存一块文本

memory 的内容来自"按相关性从 compact 帧里选"，**选择本身不确定**（词法/模型相关）。如果冷恢复时重新选一次，得到的字节几乎不可能与上次发出的一致——这就是 I1 被违反。表的作用是：**选择只发生一次，之后只重放结果**。

### 5.2 行结构（append-only，与 transcript 同一分配器的 seq）

```
MemoryUpdate {
  Seq       uint64   // 会话内单调；与 transcript seq 同源分配（全序）
  Epoch     uint64   // 写入时的 compact_seq
  Kind      add | supersede | retire
  SegmentID string   // 来源压缩帧 segment_id（空 = 会话级 pin）
  Digest    string   // sha256(Bytes)；冷恢复时校验
  Bytes     string   // 渲染后的**已发出正文**，逐字节；禁止 fold 时重渲染
  From, To  int      // 来源区间，供 read_compressed_turn 回读
  Reason    relevance | manual | epoch-roll
  CreatedAt time.Time
}
```

### 5.3 派生规则

```
memory_prefix(E) = render( fold(rows where Epoch <= E, order by Seq) )
                  其中 add 追加、supersede 覆盖同 SegmentID 的前项、retire 标记失效
                  render 只做 Bytes 的顺序拼接 —— 不重新选择、不重新渲染
```

### 5.4 写入时机（只有三个）

| 时机 | 写什么 |
|---|---|
| 压缩完成、epoch 从 c1 滚到 c2 | 对 `[c1, left)` 内的 SegmentID 写 `retire`（Reason=`epoch-roll`）；基于 c2 帧集合**重新选一次**并写 `add`（Epoch=c2） |
| 用户/模型显式 pin 某段记忆 | `add`（Reason=`manual`） |
| 同一 SegmentID 内容变化 | `supersede`（保留旧行，不改） |

**epoch 内绝不写表**——这是"前缀不变"的直接保证。

### 5.5 冷恢复与校验

```
冷恢复：
  rows := load(memory_update where Epoch <= current_compact_seq)
  for row in rows: assert sha256(row.Bytes) == row.Digest    ← 表被手改成不一致 → 拒绝启动/降级并留痕
  memory_block := concat(row.Bytes)
```

这条校验让"表被谁改过"成为可发现的事实，而不是静默漂移。

### 5.6 工具集纪元（`tools_update` + 哨兵块）

与 memory 同一范式：**选择只做一次，之后逐字节重放**。区别是 memory 的内容块在 prefix 里，而工具的表在请求头里——所以必须拆成两半。

**为什么哨兵不能单独解决问题**：`tools` 是请求的**顶层字段**，位于 messages 之前。schema 一变，缓存从第一个字节就断了，追加一条哨兵消息救不回来。所以纪元要拆成两个独立的东西：

| 半边 | 落点 | 变化时的失效范围 |
|---|---|---|
| `tools` 数组（schema / catalog） | 请求头 | **整条前缀失效一次** |
| 哨兵块（可用性声明） | 消息流尾部（append-only） | 只影响自身之后的尾部 |

设计：**catalog 在会话内冻结，默认全量下发；纪元变化只改哨兵块，不改 schema。** 于是 `tools_digest` 会话内恒定，而工具**可用性**的变化（切插件、MCP 加载完成、goal 门开合、权限档变化）**零失效**。只有当 catalog 本身变了（装/卸插件、MCP 增删 server），才 bump `tools_epoch` 并接受一次全额失效——这是罕见且可登记的事件。

**表结构**（与 `memory_update` 同族，append-only，seq 同一分配器）：

```
ToolsUpdate {
  Seq           uint64   // 与 transcript / memory_update 同源
  Epoch         uint64   // tools_epoch
  Kind          add | remove | restate
  ToolNames     []string // 受影响的工具（排序后）
  Digest        string   // sha256(SentinelBytes)
  SentinelBytes string   // 渲染后的哨兵块正文，逐字节
  Reason        plugin-switch | mcp-load | goal-gate | permission-tier | catalog-change
  CreatedAt     time.Time
}
```

**哨兵块规格**（"特殊的字符"那一半）：

```
⟦toolset⟧ epoch=7
可用: bash, read_file, write_file, edit_file, team_dispatch, team_accept
本纪元新增: team_dispatch, team_accept
本纪元移除: (无)
⟦/toolset⟧
```

硬规则：

1. 定界符固定为 `⟦` U+27E6 / `⟧` U+27E7（数学白方括号）。**正文序列化时必须转义这两个字符**，保证解析器与探针能无歧义定位：出现一次 `⟦toolset⟧` 即一条纪元声明。
2. 只在 epoch 变化时追加一条，append-only，落在**消息流尾部**——不插历史中间，否则立刻退回今天的缺陷。
3. system 里放一条**固定**解读规则（常量，永不变）：「出现 `⟦toolset⟧` 块时，以最后一块为准，此前同名块作废」。这与 Claude Code 的 `<system-reminder>` 是同构做法。
4. 模型若调用了被哨兵声明"移除"的工具，由工具门拒绝并回一条结构化错误。**用可见性换前缀稳定**——代价是模型偶尔会试一个已被禁的工具，收益是工具集变化不再逐次打断命中。
5. 渲染同样遵守"只 fold `SentinelBytes`，不重新拼"。冷恢复校验 `Digest`。

**取舍**：**catalog 冻结 + 哨兵限权**把工具集变化从"每次事件一次全额失效"降成"每次 catalog 变更一次"，其余全部零成本；代价是请求里常驻全量工具定义（固定 token 开销）。Claude Code 明确接受了同一代价（"会话中途绝不增删工具"）。

---

## 6. 核对：设计 × 代码现状

### 6.1 总表

| 设计要素 | 代码现状（证据） | 判定 | 动作 |
|---|---|---|---|
| left pointer | `TaskExecutionState.ContextRetainedFrom`（`kernel/task/task_execution.go:71-76`；`task_context_state.go:156` 注释即"transcript 绝对下标"） | **已有** | 复用，语义从"压缩边界"扩成"L1 边界" |
| 每轮清空工作历史 | `turn_completion.go:48` → `internal/host/engine_port.go:946` `ClearHistory()`；生产默认开（`boot/app.go:187`） | **与本设计直接冲突** | 删除。它是旧模型（每轮强制重建）的化石 |
| 每轮重切窗口 | `coordinator.go:1328` `TranscriptTailWindowBy(events, target, maxUnits, …)` 每次装配都跑 | 违反 I3 | 改成 `truth[retained_from:]` 直接切片 |
| user 消息 wire ≠ 记录 | 探针 harness 复刻生产形状：`AppendTranscriptEventLocked(Content: 原始输入)` + `{Content: currentInput}` 进 wire（`context_probe_harness_test.go:119-131`） | **违反 I1（最普遍）；已实测复现，见 §6.3** | `composeRequestTail` 产物落 `ProviderContent` |
| `ProviderContent` 覆盖面 | `contract/protocol/context.go:94-107`：注释明写"当前只有工具结果会分叉" | 覆盖不足 | 扩到**所有**消息（user / plan / task 帧） |
| plan 上下文消息 | 已改：`kernel/task/plan_frame.go:recordPlanFrameLocked` 在计划状态写入那一刻落 internal 事件（`WireMaterial=true`），`coordinator.go:tryFitExecutionHistory` 删除每轮现插（旧形态是"现算现插在 events 之后、当前输入之前"） | **已修**（§6.3.2 实测 `UnexpectedMiss` 1 → 0） | 已落地；用例 `context_prefix_plan_frame_test.go` + `kernel/task/plan_frame_test.go` |
| plan/task 尾帧 | plan 半边已改「落 transcript、按记录位置重放」（`kernel/task/plan_frame.go`）；task 半边仍在 `backend/engine/context/seelectx/assembler.go:120-122` 追加在 WorkingHistory 之后，装配不写回历史 | plan 半边**已修**（§6.3.2）；task 半边经核对**不漂**（§6.3.3：损坏面上界 = 帧自身字节，前缀本体一字不丢） | plan 已落地；task 不改，开回归用例钉住机制（`assembler` 包的 `TestTailFrameStaysAtRequestSuffix`） |
| memory 位置与键 | `assembler.go:88` `Memories(ctx, LastUserQuery(...))`，渲染在 prefix 主体之前；生产已接线（`backend/engine/assemble/runtime_context.go` 的 `Memories`） | **违反 I4** | 改为 epoch 快照 + §5 纪元表 |
| 工具定义顺序 | `vendor/.../Seele/tools/tools.go:171` `providers map[string]ToolProvider`；`:286` 直接 `range` | **违反 I4** | 发布前按 name 排序；`tools_digest` 进键 |
| 工具集可变性 | 切插件 / MCP 懒加载 / goal 门 / 权限档都会改变暴露的工具集合 ⇒ schema 变 | **违反 I4** | catalog 冻结 + `tools_epoch` + 哨兵块（§5.6） |
| 命中可观测性 | `strategy_openai.go:34-38` 只解析 prompt/completion/total；`client.go:251-256` `sseState` 无 usage 字段（流式 usage 整帧丢） | **无法验收** | 解析 `cached_tokens` / `prompt_cache_hit_tokens`；`stream_options.include_usage` |
| seq / messageID | `sessionTaskRuntime.transcriptSeq`（`kernel/task/coordinator.go:63-85`）、`TranscriptEvent.Seq/MessageID/UnitSeq` | 已有 | 复用；actor 外部分配 |
| actor 形状 | `EnginePort.armPendingLocked` / `queueSessionHistory`（`internal/host/engine_port.go:173-180,719-730`）：忙时排队到检查点生效 | **同形先例** | 升级为正式 actor（补 seq 幂等 + 有界背压 + 明确单写者） |
| 压缩复用热前缀 | `MainCompactionDAG` 注入 `PrefixReplaySummarizer`，压缩那一刻握着上次真实请求的 system/history/tools | **已是优势** | 保留（DSH 同一做法） |
| system 稳定性 | `kernel/prompt/coordinator.go` `lastSystemPrompt` 去重缓存；skill 正文已移出 system 改为 append-only internal 事件 | 已达标 | 纳入 `system_digest` |
| 派生数据纪律 | 看板投影 `.seelex/board/` 可再生、非事实源（`seelex-project-board.md`） | 有先例 | L1 按同一纪律 |

### 6.2 四条"设计表述不足、必须补"的地方

核对不是复述设计，下面四条是我认为**设计原文没写清、照字面实现会出问题**的点：

**(a) 轮内多次工具迭代与 draft 的关系（已裁决：轮尾提交 + ack 后清空）。**
一轮里有 N 次工具迭代、N 次请求，全部先落在 draft，轮尾一次性提交（§3.5）。**前提是 draft 自身必须 append-only**：第 k 次请求 = `L1 + draft[:k]`，第 k+1 次 = `L1 + draft[:k+1]`，只往后长。若 draft 在轮内被重渲染（例如每轮重算 plan 帧），轮内就断前缀了——所以"draft 内部也只追加"是硬约束而非建议。代价：L1 与 draft 的分界每轮移动一次（轮尾提交后纳入 L1），这是指针移动，不算改写。

**(a′) ack 屏障把"轮尾提交"变成关键路径上的等待。** 下一轮开轮要 await 上次 ack。若 actor 拥塞，这个等待会直接体现为下一轮的首 token 延迟。所以 §10 里 actor 队列上限与超时不是调优项，而是正确性参数：超时后必须走"先拼 draft 后提交"的降级路径，而不是读到半提交状态。

**(b) 压缩期间的在飞请求需要版本门。**
压缩会把 `retained_from` 前移、把 L1 换成新 epoch。若此时有请求正在读旧 L1，就会读到"半个新前缀"。需要 `(prefix_key, version)` 双读校验：读到半更新则重试一次。这条与现有 `context_compact_gate.go`（同会话压缩门）是同一类问题，可复用其等待语义。

**(c) 崩溃时 draft 的重放边界。**
draft 未提交就被中断（用户按停、进程死），重放时必须区分"已发出但未提交"与"未发出"。判据是 `seq` 是否出现在上次请求的 wire 里——这需要把"上次提交到哪个 seq"落盘（一格水位），否则恢复会重复发送或漏发。

### 6.3 已实测复现 → 已修复（M0 复现，M1 转绿，2026-10-29）

本节两条实测：用户消息字节分叉（M0 复现 → M1 已修复，6.3.1）、plan 帧位置漂移（M0 复现 → M1 已修复，6.3.2）。6.3.3 是 task 尾帧的**核对结论（不漂）**——它不满足致漂条件，改法因此不是"照抄 plan 帧"，而是开一条机制用例把它钉住。

#### 6.3.1 user 消息字节分叉（M0 复现 → M1 修复，已转绿）

用 `application/assembly/integration/context_prefix_worktable_test.go` 驱动生产装配路径，观测器为 `kernel/context/prefixprobe`。

**M0（修复前，缺陷复现）**：

| 观测项 | 实测值 |
|---|---|
| 回合内相邻请求（t1.iter1 → t1.iter2） | 前缀完整，复用 16893/20503 字符（82.4%，尾部新增属正常） |
| **跨轮回合首请求（t1.iter2 → t2.iter1）** | **断在 byte 16766 / char 16688，`msg#0 role=user`** |
| 该请求总量 / 复用 / 新增 | 20724 / 16688 / 4036 字符 |
| 会话累计（3 次请求） | hit 50474/58120 = **86.8%**，`UnexpectedMiss = 1`，`ExpectedMiss = 0` |

**M1（修复后，同一夹具）**：

| 观测项 | 实测值 |
|---|---|
| 跨轮回合首请求（t1.iter2 → t2.iter1） | **前缀 OK，复用 20503/20867 字符（98.3%）** |
| 新增字符 | 364（全部来自真实新内容：上一轮答复 + 本轮输入） |
| 会话累计 | `UnexpectedMiss = 0` |

**修复接缝**：`application/conversation.TurnInputApplication.Prepare` 在 `Normalize` 之后调 `RecordWire` → `tasks.RecordUserWireContentLocked`，把**最终送进引擎的那一份正文**写回该轮 user 事件的 `ProviderContent`；`providerContentForEvent` 与 `store.providerContentOrContent` 两处投影本就读 `ProviderContent`，因此重投影与已发出字节逐字节一致，且冷加载后仍成立（`store/wire_assembler.go` 会恢复该字段）。

两条关键事实（修复前后都成立，是设计依据）：

1. **分叉点落在"承载动态块的那条 user 消息"上，其后的全部历史都被重算。** 本夹具里它是 `msg#0`（因为是首轮输入）；生产里它在多轮历史的中间某处。关键不在于位置，而在于 **history 全部排在 system 之后**——一处污染 ⇒ 从该点起整段重算（本例 16766 B 之后全 miss），而不是只丢一条尾巴。这解释了生产上"命中率莫名掉一大截"而不是"掉一点"。
2. **比率区分不了健康与缺陷：回合内 82.4%（健康，尾部新增）与跨轮 80.5%（缺陷，从头失效）看起来只差 2 个点。** 能区分的是前缀关系（`HasPrefix`）。这就是 §7.5 把判据定成前缀关系而非比率的原因——同一份实测数据即证。

#### 6.3.2 plan 帧：位置漂移（M0 复现 → M1 已修复，2026-10-29）

`context_prefix_plan_frame_test.go` 钉住的不是"plan 正文变了"，而是更糟的一类：**帧位置漂移**。

```
turn 2:  [user(t1)  system(plan)  user(t2)]           ← plan 在第 1 格
turn 3:  [user(t1)  user(t2)  system(plan)  user(t3)] ← plan 滑到第 2 格

LCP 断在 byte 16869 / char 16767，msg#1：
  上一侧 msg#1 = system("<!-- seelex:active-plan:v1 -->{...}")
  这一侧 msg#1 = user("第 2 轮：按计划推进。")
实测：hit=16767/17520 = 95.7%，UnexpectedMiss=1
```

**为什么会滑**：plan 上下文消息每轮由 `planContextMessageLocked` **现算现插**在"保留历史之后、当前输入之前"。历史每轮变长，plan 的插入点就往后挪一格——上一次请求里 plan 占的那一格，这一次被新历史占住。所以失效不是"plan 那一段"，而是**从 plan 那一格起直到请求末尾全部重算**（本例含上一轮用户消息与本轮输入）。

**对 D1 的修正**：原来写"只在 `plan_hash` 变化时追加"仍不够——**只要 plan 帧不进 transcript，它的位置就永远在漂**。D1 的完整形态是：

1. plan 帧首次出现时**作为 internal 事件（wire_material）落进 transcript**，此后按记录位置重放；
2. `plan_hash` 不变 ⇒ 一条都不发（天然 append-only，位置不动）；
3. `plan_hash` 变化 ⇒ 在**尾部**追加一条新帧，旧帧留在历史里不删，靠"后写覆盖前写"的读法约定。

判定"这一格是不是 plan 帧"用固定前缀 `<!-- seelex:active-plan:v1 -->`（`planContextPrefix`），探针据此把它与对话消息分开归因。

**M1 落地（2026-10-29）：帧改在"计划状态写入那一刻"落 transcript，装配不再现插**

修复形态（不是"装配时把帧插到更靠前的位置"，那条路走不通，理由见下）：

| 判据 | 落地位置（文件:符号） |
|---|---|
| ① 帧首现即作为 internal 事件（`Kind=internal`、`WireMaterial=true`）落 transcript | `kernel/task/plan_frame.go`：`recordPlanFrameLocked`（渲染 `PlanContextMessage`、`planFrameHash` 去重、`appendTranscriptEventLocked` 追加） |
| ② `plan_hash` 不变 ⇒ 一条不发 | 同上 `recordPlanFrameLocked` 的 `digest == st.planFrameHash` 早退；冷加载用 `seedPlanFrameHashLocked` 从事件流对齐基线 |
| ③ 变化 ⇒ 尾部追加新帧、旧帧留史 | 同上；计划清除/收尾发墓碑 `PlanContextClearedMessage`（后写覆盖前写） |
| 帧自成协议单元，wire 上仍是 system | `kernel/task/plan_transcript.go`：`transcriptProtocolUnitList` 的 `planFrameUnit` 分支 + `providerRoleForTranscriptEvent` |
| 装配侧不再每轮插入 | `kernel/context/coordinator.go`：`tryFitExecutionHistory` 删掉 plan 消息追加；`planContextMessageLocked` 只保留给自主压缩帧 |
| 记录时刻的挂点 | `SetPlanStateLocked` / `RestoreSessionTaskLockedFor` / `PushLoadedPlanLocked` / `SyncActivePlanFrameLocked(For)` / `ClearActivePlanLocked` / `ResetPlanStateLocked`（kernel/task），`mirrorActivePlanAfterTerminal`（application/assembly/chat.go），另在装配前兜底收敛一次（`RecordPlanFrameForLocked`，幂等） |
| 不冒充用户消息 | `application/conversation/context.IsInternalContent` 增 `IsActivePlanContext`（否则帧会作为一条 user 消息进可见会话/归档） |

**为什么不能"在装配时把帧插进去"**：装配发生在**本回合用户提问事件已经落进 transcript 之后**（`turn_process.go:BeginLocked` 先写 user 事件，再 `PrepareExecutionContextFor`），而请求的形态是 `历史 + 当前输入`——当前输入被 `excludeCurrentInputEvent` 从事件流里剔掉后**追加在末尾**。于是任何在装配时追加的帧都排在提问事件之后：本轮请求里它落在"输入之前"（因为提问被剔掉了），下一轮重放它却落在"提问之后"——两轮对同一格的读法不同，前缀照样断。帧必须记在提问事件**之前**，也就是只能记在计划状态写入的那一刻。

**翻转前后两次运行（同一夹具、同一判据）**

翻转前（缺陷态，`TestContextPrefixPlanFrame_KnownDefect`，HEAD b2cbca2b）：

```
call=t1.iter1 msgs=1 / t2.iter1 msgs=3 / t3.iter1 msgs=4
turn=3 prefix BREAK byte=16869 char=16767 segment=truth msg#1 role=user [UNEXPECTED] hit=16767/17520 (95.7%) new=753
  prev roles=[user system user]
  cur  roles=[user user system user]
累计: requests=2 hit=34186/34939 (97.8%) unexpected=1
```

翻转后（`TestContextPrefixPlanFrame`，含"计划不变的第 4 轮"）：

```
call=t1.iter1 msgs=1 / t2.iter1 msgs=3 / t3.iter1 msgs=5 / t4.iter1 msgs=6
turn=3 prefix OK hit=17419/18091 (96.3%) new=672
turn=4 prefix OK hit=18091/18192 (99.4%) new=101
累计: requests=3 hit=52929/53702 (98.6%) unexpected=0
transcript plan 帧 #0 seq=2 kind=internal wire_material=true chars=441
transcript plan 帧 #1 seq=4 kind=internal wire_material=true chars=441
plan 帧去重: 计划未变化的第 4 轮帧数保持 2
```

`turn=3` 的 msgs 由 4 变 5、`turn=4` 多出的一格，正是"plan 变化才追加"的那一帧落在记录位置上的结果：第 3 轮请求是 `[user(t1) plan(v1) user(t2) plan(v2) user(t3)]`，第 4 轮是它的严格扩展（`hit=18091/18192=99.4%`，新增 101 字符全部来自本轮输入）。

单元级判据：`kernel/task/plan_frame_test.go`（① 首现即 internal+wire_material、provider role=system、不算用户提问；② 同内容幂等；③ 变化追加且 seq 递增；墓碑；冷加载基线对齐；独立协议单元）。

#### 6.3.3 task 尾帧：同类"现算现插"，但**不漂**（M1 收口核对，2026-10-29）

§6.1 把 `assembler.go:120` 的 tailStacks 与 plan 帧并列成同一类疑点。核对结论：**它不是同一类缺陷**——plan 帧致漂的两个条件它一个都不满足，所以改法不是"照抄 plan 帧"，而是**开一条机制用例把它钉住**（免得以后有人把帧搬进历史中间，退化成 plan 帧的旧缺陷）。

**注入位置（文件:行号）**：`backend/engine/context/seelectx/assembler.go:120-122`

```go
for _, block := range a.tailStacks() {            // :120
	history = append(history, block.Messages...)  // :121
}                                                 // :122
```

帧源是 `Runtime.tailStacks()`（`backend/engine/assemble/runtime_context.go:305-310` → `RenderTailBlocks(st.PromptStackSnapshot())`，只渲染 plan/task 栈顶一帧，`assembler.go:223-240`）。追加发生在 `SanitizeProviderToolProtocol`（`assembler.go:114`）之后、委托 `DefaultRequestAssembler` 之前；后者把 `Blocks` 全排在 `WorkingHistory` 之前（Seele `seelectx/assembly.go:87-90`，模块缓存 `…/Seele@v0.3.6`，`go.mod` 无 `replace`）。因此尾部帧的位置**恒等于本请求最末格**，与历史长度无关；框架 `ReActLoop.callLLM` 只把 `rl.history` 的**快照**传给装配器（`session/loop.go:796-806`），装配结果只进 `llm.Complete*`、**不写回历史**。

**为什么不会漂（两条机制，缺一条就会漂成 plan 帧）**：

| # | plan 帧（旧） | task 尾帧（现状） |
|---|---|---|
| ① 位置 | 由装配器现算现插在"保留历史之后、当前输入之前"——**历史中间**，历史变长则位置后移 | 由装配器**追加在 WorkingHistory 之后**——请求最末格，历史变长不影响"它是最后一格" |
| ② 是否进历史 | 每轮重算，下一轮同一格被新历史占住（§6.3.2 实测） | 只来自 `tailStacks()` 的即时投影，**从不写回历史**，下一轮不可能从中间重放 |

由 ①② 推出的判据：相邻两次请求的字节 LCP 必定 ≥「上一条请求减去尾部帧」，断点不可能早于尾部帧起点；**损坏面上界 = 尾部帧自身的字节数**。plan 帧旧缺陷相反——它顶掉的是**已经进过 wire 的历史**，于是从该格起到请求尾整段重算。

**实测（单元用例，生产形态：一轮三次迭代 + 跨轮，含轮内 task 终态变化）**：
`backend/engine/context/seelectx/tail_frame_position_test.go`

```
t1.iter1   msgs=4 prefix=2 tail=2 tailChars=228
t1.iter2   msgs=5 prefix=3 tail=2 tailChars=228
t1.iter3   msgs=6 prefix=4 tail=2 tailChars=231   ← 轮内 task 进终态，帧变、位置不变
t2.iter1   msgs=8 prefix=6 tail=2 tailChars=234   ← 跨轮 + plan 进终态
t1.iter1 -> t1.iter2: LCP=68  prev=295(prefix=67  tail=228) loss=227  unexpected=0
t1.iter2 -> t1.iter3: LCP=100 prev=327(prefix=99  tail=228) loss=227  unexpected=0
t1.iter3 -> t2.iter1: LCP=126 prev=356(prefix=125 tail=231) loss=230  unexpected=0
```

`prefix` = 同 system / 同前缀栈块 / 同历史但**不带尾部帧**的对照装配（字节定义"前缀本体"）。三行 `LCP ≥ prefix` 即"前缀本体一字不丢"，`loss ≤ tailChars` 即"损坏面不越出帧自身"。用例同时钉住：装配不改写调用方 `WorkingHistory`（写回即会退化成 plan 帧形态）、尾部帧恒在最末格、出口协议规整（`SanitizeProviderToolProtocol`）挪不动也删不掉它。

**代价与口径提醒**：现状下尾部帧**每个请求全价重算**（实测 228~234 字符/请求，占 20k 级请求约 1.2%）——因为它在请求最末格，任何按"追加"组织的下一轮请求都无法复用它。这是"注意力焦点贴近当前输入"（`assembler.go:42-45,116-119` 的设计意图）付出的确定性小代价，不是漂移。**口径缺口**：M0 探针的 `serializeRequest(system, history, "", nil)` 只测 system + 引擎历史，不含 project/memory/前缀栈/尾部帧（`context_probe_harness_test.go:capture`），所以这 228~234 字符在探针报表里是不可见的——本节的数字来自装配器级用例。

**发现的相邻残余口（不在 M1 改动范围内）**：节点子代理会话把 `RenderStackBlocks`（= 前缀栈 + **尾部帧**）走 `InheritedBlocks` 并入 `Blocks`（`backend/engine/assemble/runtime.go:605-613`），再经 `ScopeAssembler` 排到 `WorkingHistory` **之前**（`backend/tool/node/coordinator.go:428-447`）。也就是说：主会话的尾部帧在最末格（不漂），节点会话的**同名帧却在历史之前**——若 plan/task 快照在节点运行期间变化，断点会落在该块、其后整段节点历史重算。这是 plan 帧旧缺陷的同形状接缝，建议单开一条工作项（节点侧也应"追加在最末"，或干脆不继承尾部帧、只保留 plan 记录化后的 transcript 重放）。


---

## 7. 探针：字符级前缀命中计量（计数叠加）

> 定位：这是 M0 的**计量底座**。没有它，后面五步都无法验收——因为"前缀稳住了"只能靠字节说话，不能靠"看起来没动"。

### 7.1 为什么按字符

- **token 是 provider 侧的量**，我们拿不到（流式 usage 目前整帧丢，见 §6.1）。字符是**我们完全掌握**的量，且是 wire 字节的直接函数。
- **字符级是可复现的**：给定两条请求的序列化字节，`LCP` 唯一确定，不依赖 tokenizer 版本、不依赖 provider。
- **字符级能定位到段**：字节偏移可映射回"这是 memory 段还是 plan 帧"，token 偏移做不到。

**但必须写清它是代理**：provider 按 token 前缀块匹配（DeepSeek 按 64-token 块），字符比与 token 比不等价——ASCII 约 3~4 字符/token，中文约 0.6~1 字符/token。所以：

| 口径 | 用途 |
|---|---|
| **字符级**（本探针） | **归因与趋势**：哪一段断的、断了多少、哪一轮回归 |
| **token 级**（provider usage） | **账单验收**：真实命中率、成本 |

两个都跑，偏差超过阈值（建议 5%）即说明字符代理在该会话失真，需按段做 token 权重校正。

### 7.2 采样点：draft 每次追加之后、请求发出之前

```
draft.append(seq, bytes)
      │
      ▼
  探针采样 ──▶ 取"即将发出"的那一份序列化字节（不重新序列化）
      │
      ├──▶ LCP(prevWire, curWire)   → hit / miss 字符数
      ├──▶ 段表映射                  → 断点落在哪一段
      ├──▶ 计数器叠加                 → cum / perTurn / perSegment
      └──▶ 与 prefix_key 对撞         → 预期失效 or 意外失效
```

**硬约束：采样的必须是真正要发出去的字节，不是重新序列化一遍的结果。** 否则探针会测到"我以为的请求"，而不是"实际发出的请求"——这正是 I1 要防的那类自欺。生产路径上复用待发送的序列化产物；若装配层没有产出单一序列化结果，先补这一步（M0 的一部分）。

### 7.3 计数器与叠加口径

```go
type ProbeCounters struct {
    Requests        int
    TotalChars      int64              // Σ 本请求总字符数
    HitChars        int64              // Σ LCP 字符数
    MissCharsBySeg  map[string]int64   // 归因：各段吃掉的不命中字符
    ExpectedMiss    int                // prefix_key 变了导致的失效（应有）
    UnexpectedMiss  int                // prefix_key 没变却断了（缺陷）
}

type PrefixProbe struct {
    prevWire  []byte          // 上一条真发出去的请求字节
    prevSegs  []SegmentSpan   // 上一条的分段表（offset/len/digest/name）
    cum       ProbeCounters   // 会话累计（对齐"账单聚合"口径）
    byTurn    map[uint64]ProbeCounters  // 逐轮（对齐"末轮稳态"口径）
    last      ProbeCounters   // 最近一次请求（对齐"最近请求"口径）
}
```

三套口径**必须同时出**，因为市面上 97% 与 99% 的争论有一半是口径差：

- `cum`：`HitChars/TotalChars`，会话加权聚合——体积大、早期请求分母小，天然低于末轮。
- `last`：最近一次请求——最容易"好看"。
- `byTurn`：趋势；**回归检测只在这条线上做**（本轮比上轮掉了 >2% 就报警）。

### 7.4 断点归因：`FirstDivergence` + 段表

把每条请求切成命名段，逐段记 `{Name, Offset, LenChars, Digest}`：

```
[tools][system][project][memory@epoch][compact@seq][truth[…]…][sentinel][plan-frame][env][worktable][draft][input]
 └──────────── 前缀（进 prefix_key）────────────┘ └──────────── 尾部（draft）────────────┘
```

`FirstDivergence` 记四格：`{CharOffset, SegmentName, MessageIndex, Role}`。这样一条 miss 不只是一张红牌，而是"**第 3 条消息的 memory 段整段换了**"。

### 7.5 与 `prefix_key` 对撞（本探针最有价值的一条）

- `prefix_key` **变了** → 失效是预期的，记 `ExpectedMiss`，并记**哪一格变了**。
- `prefix_key` **没变** → 判据是**前缀关系**：`LCPBytes(prevWire, curWire) == len(prevWire)`，即上一条请求的全部字节被本条完整复现。不成立即 `UnexpectedMiss`，**直接指认缺陷**。

⚠️ **判据不是 `HitChars/TotalChars == 1`。** 健康的 append-only 轮次里尾部一定有新增，所以比率必然小于 1；把「比率 < 1」当失效会把每一轮正常请求都误报。比率是**利用率**指标（对齐 provider 的 hit/total 口径），前缀关系才是**正确性**判据。

这条把"键说应该命中"与"字节说没命中"强制对撞：**差异即 bug**。它比任何断言都强，因为它同时覆盖了"键设计漏了一格"和"渲染不稳定"两种失效。

### 7.6 落地方式：扩现有 smoke，不另造一套

仓库已有现成的字节级计量（`application/assembly/integration/context_cache_smoke_test.go`）：

- `serializeRequest(system, history, input, tools)` — 确定性的请求序列化（探针的采样面）。
- `lcpBytes(a, b)` — **字节级**最长公共前缀。
- `blockFloor(shared, block)` — 64 / 1024 块向下取整（模拟 provider 的块匹配）。
- `measure(prev, current, totalTok)` — 但当前把 LCP **换算成 token** 报比率。

改动是**加一列字符口径**，不是重写：

```
sharedBytes := lcpBytes(prev, cur)
sharedBytes  = backoffToRuneBoundary(cur, sharedBytes)   // ① 回退到合法 rune 边界
sharedChars  = utf8.RuneCountInString(cur[:sharedBytes])
totalChars   = utf8.RuneCountInString(cur)
ratioChars   = sharedChars / totalChars
```

① **必须回退到合法 rune 边界**，而且这不是防御性代码：**合法 UTF-8 下字节 LCP 也可能停在 rune 中间**。同一 Unicode 区的码点共享前导字节——`🙂` U+1F642 = `F0 9F 99 82` 与 `🙃` U+1F643 = `F0 9F 99 83` 共享前 3 字节，LCP 落在第 4 字节（rune 内部）；`中` U+4E2D = `E4 B8 AD` 与 `丮` U+4E2E = `E4 B8 AE` 同理。不回退时 `RuneCountInString` 会把残字节各数成一个 `RuneError`，字符数虚高（已由 `kernel/context/prefixprobe` 的 `TestLCPBytes_MidRuneIsReachable` 钉住：该样本不回退得 8，回退后得 5）。

### 7.7 上报与阈值

- 运行期：`ProbeCounters` 随 `TokenAudit` 落盘（会话级一行汇总 + 最近 N 条明细，避免膨胀）。
- 冒烟：`docs/test/` 报告加三列——`charRatio(cum)` / `charRatio(last)` / `unexpectedMiss`。
- 阈值（建议值，实现时按实测调）：同一 `prefix_key` 下 `unexpectedMiss` 必须为 **0**；`byTurn` 相邻跌幅 > **2%** 判回归；字符比与 provider token 比偏差 > **5%** 判代理失真。

### 7.8 局限

- 字符比**不等于**账单比（§7.1）；只能做归因与趋势。
- 探针看到的是**我们发出的字节**，看不到 provider 的缓存单元粒度与逐出。`blockFloor` 只是模拟。
- 多会话交错时的"前缀单元争用"不在单会话探针视野内，需另做 K×L 竞争模型（现有 `TestContextCacheSmoke_ProviderCacheContentionModel` 已是雏形）。

---

## 8. 实施计划（M0–M5，每步带验收判据）

> **进度（2026-10-29 / M1 收口）**：M0 ✅（字符级探针 + worktable / plan 两个变体钉死；env / compact 变体与 `cached_tokens` 待补——后者是 Seele 上游依赖）· **M1 ✅**（用户消息字节冻结实测 **80.5% → 98.3%**；plan 帧落 transcript 实测转绿 `UnexpectedMiss=0`；**task 尾帧核对为「不漂」并用机制用例钉住**——损坏面上界 = 帧自身 228~234 字符，见 §6.3.3）· M2–M5 未开工。
> **进度（2026-10-29 / M2–M5 实现收口）**：M0 ✅ · M1 ✅ · **M2 ✅**
> （六格真键、L1 连续水位增量、truth 直切、删除每轮 release、冷重建一致、
> N=10/50/200 热路径结构量测）· **M3 ✅**（调用方分 Seq、会话单写者、
> 有界背压、`(session,seq)` 幂等、轮尾 durable ack、失败保留 uncommitted、
> SessionRecord 水位恢复）· **M4 ✅**（memory 行持久化、Digest 校验、epoch fold、
> 压缩滚动 retire/add、同 epoch query 不重选）· **M5 ✅**（catalog 冻结/排序、
> tools epoch、可用性零失效、append-only sentinel、截断解析失败、冻结 schema +
> 当前工具门结构化拒绝）。子代理 `plan/task` 尾部帧接缝也已拆为稳定块/尾部块。

> 纪律：**先红后绿**。M0 不改任何行为，只把不变量变成可执行判据；之后每一步都要让 M0 的某条红灯转绿。

### M0 · 观测与红灯（不改行为）

- 解析 provider usage：`prompt_tokens_details.cached_tokens`（OpenAI 兼容）、`prompt_cache_hit_tokens/prompt_cache_miss_tokens`（DeepSeek）、`cache_read_input_tokens/cache_creation_input_tokens`（Anthropic）；补 `sseState` 的 usage 分支与 `stream_options.include_usage`。
  - **上游依赖（已核实，2026-10-29）**：`openaiUsage` 只解析 prompt/completion/total（`vendor/.../agent/core/api/strategy_openai.go:34-38`），且 Seelex 侧的 usage 是从 **telemetry 属性**读的（`telemetry.AttributeGenAIUsageInput/Output`，`internal/host/engine_port.go:1074`），不自己解析 provider JSON。所以 token 级口径要 Seele 侧补两件事：解析 cached 字段 + 新增 telemetry 属性。**不在 vendor 里改**（`go.mod` 无 `replace`，直接改会被 `go mod vendor` 覆盖）。字符级探针不依赖这条，**M0 不因此阻塞**。
- `TokenAudit` 增两个字段：`RequestCacheHitTokens` / `SessionCacheHitRatio`，随会话落盘。
- **字符级探针（§7）**：在 draft 每次追加后采样"即将发出的字节"，算 `LCP` 字符数、`FirstDivergence`、段表归因；计数器三口径（cum / last / byTurn）叠加，与 `prefix_key` 对撞分出 `ExpectedMiss` / `UnexpectedMiss`。落地方式为扩 `context_cache_smoke_test.go`（`measure` 加字符列 + rune 边界回退），不另造计量。
- 扩 `application/assembly/integration/context_prefix_invariant_test.go`：同一 harness 上开**环境表 + 活动 plan + 非空 compact 栈**，断言第 2 轮首个请求逐字节以上一轮末请求为前缀。

**验收**：① 新用例在现网形状下**红**，失败信息能指出首个差异消息（role + 内容摘要）；② 每条红都能被探针归因到**具体段**（memory 段 / plan 帧 / env 段），而不是只说"命中率低"；③ 同一 `prefix_key` 下 `UnexpectedMiss > 0` 且可复现；④ 字符比与 provider token 比偏差 ≤ 5%（否则先校正代理再谈验收）。

### M1 · 字节冻结（I1）✅（2026-10-29 收口）

- `composeRequestTail` 产物写入 transcript 事件的 `ProviderContent`；`providerContentForEvent` 已是唯一投影出口，无需改渲染。
- plan 上下文消息、tailStacks 改为 draft 追加条目（不占历史中间位置），提交时逐字节提升。
- 工具定义发布前按 name 排序；`tools_digest` 计算并进键。

**已落地（用户消息半边）**：`TurnInputApplication.Prepare` 在 `Normalize` 之后调 `RecordWire` → `task_context.RecordUserWireContentLocked`（`kernel/task/task_wire_content.go`），把最终送进引擎的正文写回该轮 user 事件的 `ProviderContent`；作用域限定 `requestID`，幂等，wire==Content 时清空以保持记录紧凑。夹具 `prefixProbeHarness.beginTurn` 同步镜像该步（否则测的是生产里已不存在的分叉）。

**已落地（plan 帧半边，2026-10-29）**：依据 `context_prefix_plan_frame_test.go` 的实测（§6.3.2），帧必须落进 transcript 才不漂；这是 D1 的完整形态，与工具集哨兵共用"后写覆盖前写"读法约定。

**落地（2026-10-29）**：`kernel/task/plan_frame.go` 在计划状态写入的那一刻把帧记为 internal 事件（`wire_material`），装配侧删掉每轮现插（`kernel/context/coordinator.go:tryFitExecutionHistory`）；`context_prefix_plan_frame_test.go` 已由"钉死缺陷"翻转为"断言前缀完整"，实测 turn3 `hit=17419/18091 (96.3%)`、turn4 `hit=18091/18192 (99.4%)`、累计 `UnexpectedMiss=0`（翻转前 turn3 断在 char 16767、`95.7%`、`UnexpectedMiss=1`，详见 §6.3.2）。

**收口（task 尾帧半边，2026-10-29）**：核对的结论是**不改**——`assembler.go:120-122` 的 tailStacks 追加在 `WorkingHistory` 之后（请求最末格）、装配不写回历史，两条机制使它不可能像 plan 帧那样漂（§6.3.3）。按"不漂就钉住"的纪律开了机制用例 `backend/engine/context/seelectx/tail_frame_position_test.go`（`TestTailFrameStaysAtRequestSuffix` 断言尾部帧恒在最末格 / 前缀本体一字不丢 / 损坏面 ≤ 帧自身字节 / 装配不改写 WorkingHistory；`TestTailFrameSurvivesToolProtocolSanitize` 断言出口规整动摇不了帧位置），实测 loss=227~230 字符、`unexpected=0`。**M1 由此收口**：用户消息（6.3.1）+ plan 帧（6.3.2）已修，task 尾帧（6.3.3）核对为不漂并钉住。

**M1 完成判据逐条**：① 用户消息 wire≠记录 → 已修（`RecordUserWireContentLocked`，跨轮 80.5% → 98.3%）；② plan 帧位置漂移 → 已修（落 transcript、同内容一条不发、变化尾部追加，`UnexpectedMiss=0`）；③ task 尾帧同类路径 → 核对不漂 + 机制用例钉住（§6.3.3），并记下节点子代理侧的残余口；④ 全量回归 `go test ./application/assembly/... ./kernel/context/... ./kernel/task/ ./application/conversation/... -count=1` 全绿；⑤ 未改 vendor（仓库无 `vendor/` 目录，依赖走模块缓存，`go.mod` 无 `replace`）。

**验收**：M0 的跨轮用例转绿（env + plan + memory 三种拉开分别各有一条用例）；`cached_tokens` 在真 API 冒烟里出现在第二轮之后。

### M2 · L1（快路径）

- `sessionTaskRuntime` 增 `l1`（不持久化）+ `prefix_key`（§4）。
- 删除 `ReleaseWorkingHistory` 调用链；`PrepareExecutionContextFor` 改为 `truth[retained_from:]` 切片，不再 `TranscriptTailWindowBy`。
- 失效打点：哪一格键变了。

**验收**：同一会话连续两轮，L1 命中（打点 `l1=hit`）；装配耗时不再随历史长度线性增长（对比 M1 基线）；冷加载仍能从 truth 重建出**字节相同**的前缀。

### M3 · draft + actor（慢路径，含 ack）

- seq / messageID 在入队前分配（与 transcript 同源）。
- actor：每会话单写者、FIFO、有界、`(session,seq)` 幂等、满则背压。
- **轮尾提交 + ack 回执**：ack 落地才清空 draft 并推进水位；轮间 await ack（有界超时 + 降级）（§3.5）。
- "上次提交水位"落盘（§6.2c）；失败标 `uncommitted` 且保留。

**验收**：并发提交压测下无丢失、无乱序（`-race` 全绿）；**注入提交失败**后 draft 不丢、下一轮合并提交成功；模拟崩溃后按水位重放，重建字节与崩溃前最后一次 ack 一致。

### M4 · memory 纪元表

- 落 `memory_update` 表（§5），随会话记录持久化。
- memory 渲染改为 fold；压缩时写 `retire` + `add`；epoch 内不写。
- 冷恢复校验 `Digest`。

**验收**：跨一次压缩 + 一次冷加载，memory 块字节与压缩后那一刻**完全一致**；同一 epoch 内换 query 不再改变前缀（用例钉住）；表被手改时能拒绝并留痕。

### M5 · 工具集纪元 + 哨兵块

- catalog 冻结在会话内；`tools_epoch` / `tools_digest` 进 `prefix_key`（§4）。
- 落 `tools_update` 表；epoch 变化只追加 `⟦toolset⟧` 哨兵块（§5.6）。
- system 里加固定解读规则；正文序列化转义 `⟦ ⟧`；工具门对被移除工具返回结构化拒绝。

**验收**：切插件、MCP 懒加载完成、goal 门开合、权限档变化这四类事件发生后，`tools_digest` **不变**、前缀不失效，只有哨兵块追加（用例逐个注入事件后比对前缀）；装/卸插件使 `tools_epoch` bump 时，显式打点一次全额失效；哨兵块被截断或乱序时解析器能报错而不是静默取错。

---

## 9. 已拍决策（2026-10-29）

| # | 决策 | 后果与实现纪律 |
|---|---|---|
| **D1** | **plan/task 帧只在 `plan_hash` 变化时追加**；没变则一条不发 | **（2026-10-29 修正）**"只在变化时追加"不够——帧若不进 transcript，位置每轮都会漂（§6.3.2 实测）。完整形态：① 首现即作为 internal 事件（wire_material）落入 transcript，此后按记录位置重放；② `plan_hash` 不变 ⇒ 一条不发；③ 变化 ⇒ 尾部追加新帧、旧帧留史，靠「后写覆盖前写」读法约定（与工具集哨兵同一约定）。**（2026-10-29 收口回写：实际实现形态）** ① **plan 帧**：`kernel/task/plan_frame.go` 的 `recordPlanFrameLocked`（渲染 + `planFrameHash` 去重 + `appendTranscriptEventLocked`）在计划状态写入那一刻落 `Kind=internal`/`WireMaterial=true` 事件；哈希不变早退、变化追加、清除发墓碑；冷加载 `seedPlanFrameHashLocked` 对齐基线；`kernel/context/coordinator.go:tryFitExecutionHistory` 删除每轮现插；`kernel/task/plan_transcript.go` 让它自成协议单元（wire 上 role=system）。② **task 尾帧**：核对后**不采用**同一改法——它不满足致漂条件（追加在 `WorkingHistory` 之后 = 请求最末格、装配不写回历史，`backend/engine/context/seelectx/assembler.go:120-122`），实测损坏面上界就是帧自身 228~234 字符、前缀本体一字不丢（§6.3.3）；纪律改为**机制用例钉住**（`backend/engine/context/seelectx/tail_frame_position_test.go`），若将来它被搬进历史中间或不进 transcript，用例即红。③ 两者的分界判据：**帧在请求里的位置是否会因历史变长而后移**——会（plan 旧形态）就必须落 transcript，不会（尾部追加）就只需钉住 |
| **D2** | **draft 轮尾提交；收到 ack 确认提交成功之后才清空** | 引入轮间 ack 屏障 + 幂等重试 + `uncommitted` 不丢（§3.5）；轮内 draft 只追加不重渲染（§6.2a′）。actor 队列上限与超时因此是正确性参数 |
| **D3** | **工具集变化走 memory 式纪元 + 哨兵块**（`tools_update` + `⟦toolset⟧`） | catalog 冻结 ⇒ `tools_digest` 会话内恒定；可用性变化零失效；仅 catalog 变更 bump `tools_epoch` 并接受一次全额失效（§5.6） |

仍待定的参数（实现时按打点数据定，不阻塞开工）：L1 内存上限与 LRU 容量；actor 队列上限与 ack 超时；哨兵块在角色会话（teammate）里是共用父 epoch 还是各自独立。

---

## 10. 风险与明确不做

| 项 | 说明 |
|---|---|
| L1 内存占用 | 每会话一份装配前缀。需给上限 + LRU，且**逐出不影响正确性**（L1 派生）。参照现有 `resident_lru` 口径 |
| actor 排队 vs 首 token 延迟 | 背压会推迟首 token。上限与超时须可配，且"等待"要区分于"卡死"（打点） |
| 压缩期间版本门 | 见 §6.2b；不做就是偶发整段 miss，且极难归因 |
| ack 屏障拖慢轮间首 token | 见 §6.2a′。actor 拥塞时等待直接体现为下一轮延迟 ⇒ 队列上限与超时是正确性参数，不是调优项 |
| 哨兵块被误读 | 模型可能忽略"以最后一块为准"的约定 ⇒ system 规则要写成强约束，并在冒烟里放一条"两纪元 + 调用已移除工具"的用例观察模型行为 |
| 工具集冻结的可见性代价 | 模型永远看得见全量 catalog，偶尔会试一个被哨兵禁用的工具 ⇒ 工具门必须回结构化拒绝（含"当前 epoch 不可用"），不能静默失败 |
| **不做**：把 memory 做成"每轮相关 top-K" | 那是当前缺陷的根源。查询相关性改用工具按需检索（`search_history` / `read_compressed_turn`）承载 |
| **不做**：L1 持久化 | 违反 I5。冷加载重建即可，且能被 M2 验收判据钉住 |
| **不做**：为压缩引入"续命 keep-alive 请求" | 那是绕开 TTL 的偏方（Cline 生态有这类工具），与本设计的正确性无关，且烧 token |

---

## 11. 证据索引

| 结论 | 位置 |
|---|---|
| 每轮清空工作历史 | `application/conversation/turn_completion.go:48`；`internal/host/engine_port.go:946`；`boot/app.go:187` |
| 每轮重切窗口 | `kernel/context/coordinator.go:660,1328` |
| left pointer 已存在 | `kernel/task/task_execution.go:71-76`；`kernel/task/task_context_state.go:156` |
| user 消息 wire≠记录 | `application/assembly/integration/context_probe_harness_test.go:119-131` |
| `ProviderContent` 只覆盖工具 | `contract/protocol/context.go:94-107` |
| 工具结果外的投影出口 | `kernel/task/plan_transcript.go:280-298`（`providerContentForEvent`） |
| plan 消息现算现插 | `kernel/context/coordinator.go:1331,1420` |
| tailStacks | `backend/engine/context/seelectx/assembler.go:120-122`（追加在 `WorkingHistory` 之后 = 请求最末格；实现在 `backend/engine/assemble/runtime_context.go:305-310`）；不漂的判据与实测见 §6.3.3，用例 `backend/engine/context/seelectx/tail_frame_position_test.go` |
| 框架装配器把 WorkingHistory 排在块之后 | Seele `seelectx/assembly.go` `DefaultRequestAssembler`（模块缓存 `…/Seele@v0.3.6`）；`session/loop.go` `callLLM` 只传历史快照、装配结果不写回 |
| 节点会话把尾部帧并入 Blocks（残余口） | `backend/engine/assemble/runtime.go:605-613`（`InheritedBlocks` = project + `RenderStackBlocks`）→ `backend/tool/node/coordinator.go:428-447`（排在 WorkingHistory 之前） |
| memory 位置与 query 依赖 | `backend/engine/context/seelectx/assembler.go:88`；`backend/engine/assemble/runtime_context.go`（`Memories: r.relatedMemoryBlocks`） |
| 工具顺序 map range | `vendor/github.com/RedHuang-0622/Seele/tools/tools.go:171,286` |
| usage 解析缺口 | `vendor/.../Seele/agent/core/api/strategy_openai.go:34-38`；`client.go:251-256` |
| actor 同形先例 | `internal/host/engine_port.go:173-180,719-730` |
| 压缩复用热前缀 | `backend/engine/assemble/runtime_context.go`（`MainCompactionDAG` 注释） |
| system 稳定化 | `kernel/prompt/coordinator.go`（`lastSystemPrompt` 缓存 + skill 正文移出 system 的注释） |
| 派生数据先例 | `docs/arch/seelex-project-board.md` |
| 字符级探针底座（已有） | `application/assembly/integration/context_cache_smoke_test.go`：`serializeRequest`（确定性序列化）、`lcpBytes`（字节级 LCP）、`blockFloor`（64/1024 块量化）、`measure`（当前按 token 报比率，§7.6 要加字符口径） |
| 前缀不变量用例（已有） | `application/assembly/integration/context_prefix_invariant_test.go` |
| 探针 harness（复刻生产形状） | `application/assembly/integration/context_probe_harness_test.go:119-131` |
| 会话争用模型（雏形） | 同文件 `TestContextCacheSmoke_ProviderCacheContentionModel`（K×L 逐出推演） |
| 同行机制对照 | `docs/research/2026-10-29-agent-prefix-cache-mechanisms.md` |
