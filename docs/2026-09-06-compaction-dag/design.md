# 上下文压缩 DAG 详设：前缀重放 + 章节级 fork/merge + 超上下文检索

> 日期：2026-09-06
> 状态：设计定稿待评审；本文件描述目标实现，代码与测试以实施后为准。
> 依赖事实：Seele `workplan`（github.com/RedHuang-0622/Seele v0.1.2）是仓库
> 已采用的 DAG 执行内核；本设计**复用** workplan/codec + core/plan +
> runtime/runner + sugar/fork，不自造编排轮子。

---

## 0. 目标与背景

1. 把 Seelex 的上下文压缩从“本地确定性折叠”升级为 **DSH 式前缀重放压缩**：
   压缩生成厚摘要的辅助请求复用上一次真实请求的字节前缀（system + 历史原样
   重放，仅尾接固定压缩指令），使压缩本身几乎全命中缓存，只有指令与摘要输出
   未命中。
2. **保留 Seelex 压缩策略**：软阈值 75% / 硬阈值 90% / 压缩目标 60%、
   只压窗口外完整协议单元、CompactFrame 栈顶自足、去重（lastCompactedTo）、
   Evidence + 原文可读回等语义不变；只替换“摘要文本怎么生成”与“帧怎么入栈”。
3. 帧携带 **requestID 首尾**作为“超上下文索引”（元数据，不进模型可见正文）。
4. 压缩过程本身用 **DAG** 表达，fork 粒度是**章节**（不是历史分片），支持
   fork 出去 → merge 回来；兜底是**串行化**执行同图节点。
5. 超上下文检索（search_history）拆成两个原子工具后，用**简单 DAG** 编排：
   `search_compact_stack` →（可选 `read_compressed_turn`）/ 独立
   `search_current_history`。
6. 压缩的整体逻辑优化发生在**超上下文确认内容之前**：先产出“厚摘要 + 锚链”，
   检索只是事后确认与取回。

---

## 1. 术语

| 术语 | 含义 |
|---|---|
| 章节级 fork | 按摘要章节拆并行任务：Chapter 1（上一帧锚点）与 Chapter 2（当前厚摘要），只拆两支，不做历史分片 |
| 前缀重放 | 生成 Chapter 2 的模型请求 = 当前会话真实请求的前缀字节 + 固定压缩指令 |
| 锚点（链元数据） | `prev_segment_id` + `prev_request_from/to` + 一句话摘要 |
| 超上下文索引 | CompactFrame 的 requestID 首尾（`request_from/to`），高于轮次/事件/消息坐标 |
| 保留后缀窗口 | 压缩后仍以原文保留的最新轮次/单元（计划放开） |

---

## 2. 总体数据流

```text
真实对话（append-only 已定稿轮次）
   │
   ▼
┌─ 压缩 DAG（先做；产出帧后超上下文才确认内容）───────────────┐
│ 选段+request 定界 ─ fork{ Chapter1 锚点, Chapter2 厚摘要 }  │
│                 ─ merge ─ 建帧/入栈 ─ 渲染（栈顶厚内容）      │
└──────────────────────────────────────────────────────────┘
   │ CompactStack 帧链（厚内容每帧一份；prev 锚点成链）
   ▼
┌─ 超上下文检索 DAG（后做；确认/取回）─────────────────────────┐
│ search_compact_stack → read_compressed_turn                │
│ search_current_history（独立，可并行）                       │
└──────────────────────────────────────────────────────────┘
   │
   ▼
provider 请求 = 稳定前缀 + 栈顶厚内容 + 保留窗口 + plan/输入
```

---

## 3. 数据契约

### 3.1 CompactFrame 扩展（sessionstore）

在 [`sessionstore/session_context.go`](../../sessionstore/session_context.go) 的
`CompactFrame` 上新增元数据字段（全部 `omitempty`，旧记录向后兼容）：

```go
type CompactFrame struct {
    // ……现有字段：SegmentID / From / To / Round* / Event* / Message* /
    // EventRevision / ConversationRevision / Evidence / CompressedAt

    // 超上下文索引：本帧覆盖的首尾 requestID（仅元数据，不进模型正文）。
    RequestFrom string `json:"request_from,omitempty"`
    RequestTo   string `json:"request_to,omitempty"`

    // 链锚点（二阶导）：只指向前驱，不复制前驱全文。
    PrevSegmentID      string `json:"prev_segment_id,omitempty"`
    PrevRequestFrom    string `json:"prev_request_from,omitempty"`
    PrevRequestTo      string `json:"prev_request_to,omitempty"`
    PrevSummaryOneLine string `json:"prev_summary_one_line,omitempty"`
}
```

`Summary` 字段仍为单字符串，但内部固定两章节（见 3.2）。

### 3.2 帧摘要内容格式（两章节）

```markdown
## 上一压缩栈摘要 (Previous Compact Stack)
<!-- 链锚点；仅当模型需要链上下文时渲染，正文默认不带 requestID 索引 -->
segment_id: compact-sess-118
request: chat-1 .. chat-3
一句话: 完成了模块 X 的迁移与验收

## 压缩内容 (Compacted Context)
<!-- 厚摘要：模型主要消费内容 -->
### 目标 (Goal)
### 关键概念 (Key Concepts)
### 文件与代码 (Files and Code)
### 错误与修复 (Errors and Fixes)
### 待办 (Pending)
### 当前工作 (Current Work)
### 下一步 (Next Step)
### 关键约束 (Constraints)
```

规则：
- Chapter 1 只放**锚点**（`segment_id` + request 首尾 + 一句话），不复制前驱
  摘要全文；栈序正确性靠 `prev_segment_id` 存在性 + request 首尾接续校验。
- Chapter 2 是 DSH 风格结构化厚内容；空章节写 `(none)`，不丢章节骨架。
- **requestID 属于帧元数据**：不写进 Chapter 2 正文，也不作为普通内容渲染；
  需要定位时由 `search_compact_stack` / `read_compressed_turn` 消费。

### 3.3 渲染契约（模型可见范围）

模型请求只渲染**栈顶帧的 Chapter 2 厚内容**（延续
[`seelexctx/assembler.go`](../../seelexctx/assembler.go) 的
`RenderStablePrefixBlocks` 栈顶语义；字段从 `summary` 全文改为
`summary_chapter2` 视图或渲染时截取第二章节）。锚点与超上下文索引在
CompactStack 记录中维护，默认不进请求正文。

### 3.4 不变量（PushCompact 校验扩展）

`sessionstore.SessionContextStore.PushCompact` 追加校验：

- `RequestFrom/RequestTo` 同空或同非空；非空时按字符串序非倒置（或由调用方
  保证首尾覆盖语义）。
- `PrevSegmentID == ""` 当且仅当本帧为首帧；非首帧必须给出
  `PrevSegmentID` 且（若启用事件坐标）与栈内上一帧 `SegmentID` 一致。
- `PrevRequestFrom/To` 与栈内上一帧的 `RequestFrom/To` 一致。

---

## 4. 压缩 DAG（复用 Seele workplan）

### 4.1 节点与边（codec.Document 形态）

```json
{
  "version": 1,
  "entry": "select_range",
  "nodes": [
    {"id": "select_range", "kind": "function", "input": {"purpose": "select_overflow"}},
    {"id": "chapter1_anchor", "kind": "function", "input": {"purpose": "build_prev_anchor"}},
    {"id": "chapter2_thick", "kind": "function", "input": {"purpose": "prefix_replay_summary"}},
    {"id": "merge_frame", "kind": "function", "input": {"purpose": "merge_chapters"}},
    {"id": "publish_stack", "kind": "function", "input": {"purpose": "push_compact_frame"}}
  ],
  "edges": [
    {"from": "select_range", "to": "chapter1_anchor"},
    {"from": "select_range", "to": "chapter2_thick"},
    {"from": "chapter1_anchor", "to": "merge_frame"},
    {"from": "chapter2_thick", "to": "merge_frame"},
    {"from": "merge_frame", "to": "publish_stack"}
  ]
}
```

- `select_range`：窗口外溢出单元切分 + `request_from/to` 定界（纯计算）。
- `chapter1_anchor` 与 `chapter2_thick`：**两个并列分支（章节级 fork）**，
  通过 `workplan/sugar/fork`（`ForkNode`）或 `core/node` 的并行分支执行，
  最大并发 2，互不依赖、失败互不牵连。
- `merge_frame`：合并两章节输出为 `CompactFrame.Summary` + 锚字段。
- `publish_stack`：`PushCompact` + 渲染/事件发布（`ContextCompaction`）。

执行入口：`codec.Import`/`codec.ImportEdgeList` 构造 Plan →
`runner.New(plan).Run(ctx)`。节点语义由 Seelex 的 `codec.NodeFactory[T]`
把产品 kind 装配成下述节点实现，Seele 不解释产品语义。

### 4.2 节点实现（core/node.Node 契约）

各节点实现 Seele 最小执行契约：

```go
type Node interface {
    ID() string
    Run(context.Context, *workplan/core/types.WorkflowContext) (string, error)
}
```

节点持有的依赖全部构造注入（沿用 seelexctx 控制器依赖注入风格）：

- `select_range`：`WindowPolicy` / `TokenCounter` / 溢出单元源（现有
  `chatUnits` 复用）→ 输出 `{overflow, requestFrom, requestTo}`。
- `chapter1_anchor`：`CompactStackStore.Snapshot()` 取栈顶帧 → 输出锚点
  JSON（含一句话凝练；一句话可由本地凝练或小模型生成，见 4.4 兜底）。
- `chapter2_thick`：`PrefixReplaySummarizer`（4.4）→ 输出厚摘要文本。
- `merge_frame`：纯函数拼装 `CompactFrame`（复用现有
  `buildCompactFrame` 的 SegmentID/From/To/Evidence/归档逻辑）。
- `publish_stack`：`PushCompact` + `lastCompactedTo` 推进 + 渲染发布。

### 4.3 章节级 fork（不按历史分片）

只拆两支的原因（用户已确认）：

- 分片 fork 会让每一片都完整重放前缀，任何一片失败都要整批重来，重来即
  重新消耗 token，前缀重放的价值归零；
- 章节级 fork 只有两支且相互独立：Chapter 1 失败只重做小锚点任务，
  Chapter 2 失败只重做一次厚摘要重放，互不牵连。

并发执行用 Seele 现有 fork 机制：

- `workplan/sugar/fork`：`Add`/`NewNode` 注册两支 `ForkBranch`，执行时由
  `forkexec.ForkCoordinator` 创建隔离上下文并合并结果（并发布节/汇合事件，
  可挂到 seelex 已有 workplan 事件面）。
- 若部署不使用 fork sugar，退化为两分支顺序执行（同一 DAG、串行调度）。

### 4.4 前缀重放摘要协议（Chapter 2）

`PrefixReplaySummarizer` 接口（放 `seelexctx`，由 seelebridge 注入实现）：

```go
type PrefixReplaySummarizer interface {
    Summarize(ctx context.Context, req ReplayRequest) (ReplayResult, error)
}

type ReplayRequest struct {
    // 必须与真实请求同源、同序、同序列化的字节素材：
    SystemPrompt string          // 与引擎当前 system 同字节
    History      []types.Message // 与最近真实请求同序（含溢出区）
    Tools        []types.Tool    // 与最近真实请求同 schema/顺序
    Instruction  string          // 固定压缩指令（唯一新增尾巴）
    MaxTokens    int
}
```

协议约束：

- **字节级一致性**：History/System/Tools 必须来自真实请求同一条装配路径
  （即 application/context_runtime 或 seelebridge Assembler 产生的同一份
  消息数组），不能从事件流重拼后当“重放”用；
- 摘要模型与 provider 与主会话一致（换 provider/model 即放弃前缀复用）；
- 输出进入 `CompactFrame.Summary` 的 Chapter 2；
- 失败语义：返回结构化错误，由调用方走 4.5 兜底，绝不让请求发送中断。

### 4.5 兜底策略（串行化）

```text
DAG 正常：select_range → fork{chapter1, chapter2} → merge → publish
  │
  ├─ runner/fork 不可用或装配失败 → 同图节点按依赖序串行 Run（串行化兜底）
  │
  ├─ chapter2 失败（一次重试后） → 本地确定性折叠（现有 summarizeOverflow）
  │      产出薄摘要并标记 summary_source=local（不消耗模型 token）
  │
  └─ chapter1 失败 → 锚点降级：prev_segment_id + request 首尾照记，
          一句话摘要留空并标记 anchor_source=degraded
```

任何一层兜底都不改变帧的结构与渲染契约，只改变摘要来源与质量标记。

### 4.6 与现有压缩路径的集成

改动点（保留策略，只替换摘要生成/入栈封装）：

- [`seelexctx/controller.go`](../../seelexctx/controller.go)
  `compressWindowOutsideWith`：原来直接 `buildCompactFrame`，改为调用
  “压缩 DAG 执行器”（注入 `CompactionDAG`），成功取帧；失败逐级兜底到
  现有本地折叠。阈值/窗口/去重/归档/history_safety 全部不动。
- [`seelexctx/gap.go`](../../seelexctx/gap.go) `buildGapFrame`：真空区补压缩
  走同一 DAG（Chapter 1 为空/取栈顶锚；Chapter 2 由本地或重放生成）。
- [`seelebridge/runtime_context.go`](../../seelebridge/runtime_context.go)
  `seelexController()`/`nodeController()`：注入 `PrefixReplaySummarizer`
  （复用 `r.completer` 与 `seelectx.NewQuickChat` 或等价的带 tools 通道）
  与 `CompactionDAG` 执行器。

---

## 5. 超上下文检索 DAG

### 5.1 原子能力

| 工具/动作 | 语义 | 状态 |
|---|---|---|
| `search_compact_stack(query)` | 在 CompactStack 帧（摘要/证据/request 索引）上检索，返回段命中 | 新拆（现 seelexctx/search 索引路径） |
| `search_current_history(query)` | 在未压缩区（栈顶 To 之后事件）检索真实记录 | 新拆（现 seelexctx/search 尾部扫描语义，限定到未压缩区） |
| `read_compressed_turn(segment_id)` | 读回命中段归档原文 | 已有 |

两个检索动作先各自原子化并测试通过，再进 DAG 编排。

### 5.2 DAG 约束

```text
search_history 编排入口
  ├─ search_compact_stack(query)          （可独立执行）
  │     └─ 命中 → read_compressed_turn(segment_id)  前置依赖：先有帧命中
  ├─ search_current_history(query)        （与栈检索互不阻塞，可并行）
  └─ merge：按相关性共享 token 预算合并，截断标注
```

帧的 requestID 索引在这里被消费：命中段以 `request_from/to` 暴露覆盖范围，
作为“超上下文”定位信息；模型正文不携带它。

---

## 6. 缓存/渲染语义

- 压缩后请求 = 稳定前缀（system/tools）→ 栈顶 Chapter 2 厚内容 → 保留窗口
  → plan/输入。厚摘要每帧只存一次，不再递归复制全文，前缀更稳定。
- 前缀重放请求本身与最近真实请求共享字节前缀 → 辅助请求几乎全命中。
- 保留后缀窗口“打开更多”：
  - `seelexctx/limits.go` 的 `ContextMaxUnits` 默认 4 → 建议默认 8；
  - `window.go` 的 `DefaultWindowConfig`：`min_rounds 4 → 8`、`max_rounds 40`
    不变（或按实测调）；
  - 所有值保持可配置（yaml `limits.*` / `window.*`），文档同步。

> 具体默认值属调参决策，实施时按冒烟数据再定；本设计只把“可开大”落成
> 配置面。

---

## 7. 实施顺序（原子先行，DAG 最后）

1. **契约与纯函数**：CompactFrame 字段 + PushCompact 校验 + 两章节拼装/
   渲染截取函数 + 单元/request 定界函数（controller/gap 共用）→ 单测。
2. **Chapter 2 生成器**：`PrefixReplaySummarizer` 接口 + QuickChat 实现 +
   本地折叠回退 → 单测（fake completer）。
3. **Chapter 1 锚点生成器**：栈顶 → 锚点/一句话 + 降级 → 单测。
4. **压缩 DAG 执行器**：见 4.1 codec 文档装配 + runner.Run；先串行跑通，
   再接 fork 并发；controller/gap 接入 → controller/gap/assembler 测试更新。
5. **原子检索**：`search_compact_stack`/`search_current_history` 拆出并
   各自测试；search_history 工具与 GUI 数据面接线。
6. **检索 DAG 编排**：简单 codec 图组合（5.2）→ 集成测试。
7. **参数与冒烟**：后缀窗口默认值调整 + `context_cache_smoke_test.go`
   对照（前缀稳定性/命中率/兜底路径）+ 真实 API 冒烟读 usage 归因。

---

## 8. 测试策略

- `sessionstore`：帧字段持久化/兼容（旧 blob 无新字段可读）、PushCompact
  不变量、首尾 request 范围校验。
- `seelexctx`：两章节拼装/渲染只含 Chapter 2；Chapter 2 失败回退本地折叠；
  Chapter 1 降级；DAG 串行与并发结果一致；controller/gap 帧内容回归。
- `search`：两原子工具各自命中/预算/空查询/无栈语义；DAG 编排合并去重。
- 缓存语义冒烟：复用 context_cache_smoke_test 的 LCP/块对齐模型，
  断言压缩后下一轮不再“整段悬崖”于重放路径（usage 归因需真实 API）。

---

## 9. 风险与未决

- 字节级一致性的获得点：主会话 system 由 application 侧注入、历史在引擎
  侧装配，Chapter 2 重放需拿到与真实请求同字节的
  `SystemPrompt/History/Tools`——实施第一步要确认装配出口并固化快照。
- fork 并发与主会话请求的并发：压缩发生在 ReAct 迭代间隙，须避免与正在
  进行的请求争用 completer/账号限额（用现有 heartbeat/限流/串行兜底）。
- 默认参数（后缀窗口、一句话锚点长度、Chapter 2 token 预算）以冒烟为准。

---

## 10. 参考

- Seele workplan：`workplan/README.md`、`workplan/codec`、
  `workplan/runtime/runner`、`workplan/sugar/fork`、`workplan/runtime/forkexec`
  （github.com/RedHuang-0622/Seele v0.1.2）。
- 本地既有实现：[seelexctx/controller.go](../../seelexctx/controller.go)、
  [seelexctx/gap.go](../../seelexctx/gap.go)、
  [seelexctx/assembler.go](../../seelexctx/assembler.go)、
  [sessionstore/session_context.go](../../sessionstore/session_context.go)、
  [sessionstore/paragraphs.go](../../sessionstore/paragraphs.go)、
  [seelebridge/runtime_context.go](../../seelebridge/runtime_context.go)、
  [seelexctx/search/search.go](../../seelexctx/search/search.go)。
- 现有 workplan 用法：seelebridge fork/plan_run 系列（
  [seelebridge/fork/fork.go](../../seelebridge/fork/fork.go) 等）与
  [sessionstore/checkpoint_store.go](../../sessionstore/checkpoint_store.go)。
