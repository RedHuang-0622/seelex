# Seelex 上下文管理实现审查与理论依据调研

> 审查日期：2026-08-20
> 审查性质：只读技术审查 + 论文/博客理论依据调研，未修改任何代码
> 审查范围：上下文继承（父→子）、上下文合并（子→父）、上下文压缩（窗口外压缩/真空区补压/跨会话快照压缩）、压缩格式契约
> 事实来源：代码与测试是“已实现”的最终事实来源；本文所有实现描述均以 2026-08-20 工作树为准，文档仅用于佐证设计意图

---

## 0. 结论速览

1. **设计初衷是质量优先，不是缓存**。各工作包的问题陈述全部是长任务收敛、死锁、内存、上下文超限等质量/稳定性问题；缓存命中率是 2026-08-09 R3 才纳入的次级目标，其解法是 system prompt 稳定化 + 请求尾部打点表，而不是改继承/合并。
2. **继承和 merge 的布局对前缀缓存是提高或中性的**：稳定前缀（system/project/stack）前置、动态块（memory）尾部、合并文本尾部追加。唯一会打断前缀缓存的是 ReplaceHistory 压缩，这是“有界上下文”的固有代价，压缩完成后稳定前缀恢复。
3. **质量判断是分层的**：同一会话内质量偏好（窗口保留最新完整轮次、旧轮折叠为栈顶自足摘要、原文可归档读回、记忆按查询 top-K 选取）；跨会话与摘要层有明确质量损失（投影整体替换、Decisions/Findings 折叠为计数、词法记忆属工程降级）。
4. **需求/事故驱动为主，对标为辅**。早期 arch 方案参考了 Claude Code / LangChain / Continue.dev，但 LoopX 与 Seele gap 分析中有明确“不借鉴”记录；不是“为了对标而做”。
5. **理论依据充分**：窗口+摘要位置、开头 token 稳定、压缩提质量、递归摘要、可逆分页、记忆检索/分层、多代理合并与失败模式、结构化通信、前缀缓存、官方 context engineering 实践均能支撑对应设计；摘要层计数化与词法记忆是工程降级，无直接理论支撑。

---

## 1. 审查范围与证据基线

涉及模块（均为当前实现）：

- `seelexctx/snapshot/`：`ContextSnapshot` DTO、`Format()`、`Validate()`、Builder；
- `seelexctx/provider/`：`EngineProvider`（会话历史导出）与 `TraceProvider`（telemetry 生命周期事件导出 Findings/Decisions/TokenEstimate）；
- `seelexctx/compactor/`：基于 token 预算的三级快照压缩；
- `seelexctx/merger/`：`MergeBack` 双向合并；
- `seelexctx/` 根包：`controller.go`（软/硬阈值、窗口外压缩、CompactFrame 栈）、`gap.go`（真空区补压）、`history_safety.go`（marker 清理与空正文配对修复）、`assembler.go`（请求投影顺序）、`processor.go`（超大工具结果归档）、`compressor.go`（压缩器适配）、`memory/memory.go`（词法 top-K 记忆选取）、`bridge.go`（兼容 API）；
- `seelebridge/session/subagent_context.go`：父证据 actor、mailbox；
- `seelebridge/runtime_session.go`、`seelebridge/node/agent_node.go`：merge-back 调用链；
- `application/core/runtime_projection.go`、`service_input.go`、`chat.go`：父证据投影发布与 `[子代理产出]` 注入；
- `sessionstore/session_context.go`：`CompactFrame` / `SessionContextRecord` 持久化契约。

验证基线（审查时执行，全部通过）：

```text
go test ./seelexctx/... -count=1
go test ./sessionstore -count=1
```

---

## 2. 技术实现梳理

### 2.1 上下文继承（父 → 子）

数据流：

```text
Application snapshot
  → publishRuntimeProjections（锁外发布，避免回调死锁）
  → Runtime.SetParentEvidenceProjection
  → SubagentContextActor.handleSetProjection
  → ExportSnapshotFromData（无锁数据面：sessionID/goal/messageCount + telemetry）
  → atomic.Pointer 无锁读面 state
```

子代理请求投影顺序（`seelexctx/assembler.go` 与节点组装器）：

```text
system（effort/skill 动态生成，永不持久化）
  → project 块（项目模块语义，会话前预读）
  → stack 块（plan/task/skill/compact，now using = 栈顶）
  → 会话其余块
  → 节点块（charter/skills/父证据）
  → memory 块（按当前查询从历史压缩段选取）
  → 调用方静态块（plan authority / task checkpoint / evidence）
  → WorkingHistory（窗口轮次）
```

设计要点：**稳定块前置、动态块尾部**。system/project/stack 与主会话同源且在会话内不变，构成可复用的缓存前缀；动态 memory 块单独放最后，避免打断前缀命中。

### 2.2 上下文合并（子 → 父）

数据流：

```text
子代理 Session 结束（含超时/失败，2026-08-10 C 修复）
  → seelexctx.ExportSnapshot（EngineProvider + TraceProvider）
  → merger.MergeBack（copy-on-write）
  → SubagentContextActor.handleMerge（单 goroutine 串行“读-合并-写回”，防并行覆盖）
  → merged.Format() 文本 → actor mailbox Enqueue（soft cap 只计数不丢弃）
  → Application 在 ChatStream 边界外 Drain + AppendHistory
  → 注入为以 [子代理产出] 前缀标记的 user 消息
```

`merger.MergeBack` 合并策略：

| 字段 | 策略 |
|---|---|
| Findings / Decisions | append（copy-on-write，不去重、无上限） |
| Progress / PendingWork | 子代理非空则替换 |
| Constraints | append + 去重 |
| TokenEstimate / MessageCount | 累加 |
| Escape | 子代理非空则替换 |
| Goal | 父主导；父为空时继承子目标 |

可见会话通过 marker 剔除（`subagentContextMarker`），模型上下文保留、用户界面无噪音。

### 2.3 上下文压缩

阈值与窗口（`controller.go`）：

- 预算：`Window - OutputReserve - SafetyReserve`（safety = window/8）；软阈值 75%、硬阈值 90%、压缩目标 60%；
- 窗口轮次由 `WindowPolicy` 推导（配置 + provider 预算），保守回退 `MinRounds=4`；
- 软阈值在片段闭合（after_assistant / after_tool）时触发，只压缩**窗口外**轮次，窗口内原样保留；
- 硬阈值（超大工具结果）先归档为 result_ref，仍超限才收窄窗口（不低于 MinRounds）。

协议单元切分（`chatUnits`）：user 轮、assistant 文本轮、assistant 工具链轮（调用 ID 全配对）为完整单元；孤儿 tool、未闭合工具链、上下文控制块不构成单元。窗口投影从最后一个溢出单元结束处保留，窗口内完整单元与未闭合尾部都保留。

CompactFrame 栈（`sessionstore.CompactFrame`）：

- 新溢出帧**合并上一栈顶帧**生成，栈顶自足（Summary 为该时刻窗口外全部轮次的综合摘要）；
- From/To 是 ChatQueue 单元**累计索引**（首帧 To = len(overflow)-1；合并帧 To = prevTop.To + len(overflow)），单调递增；
- 去重基准是 `lastCompactedTo`：新帧没有覆盖到上次压缩点之后的新单元则跳过（2026-08-06 评审 R2）；
- 原文归档（`TurnArchiver`）后帧 Evidence 携带读回引用，模型可经 `read_compressed_turn(segment_id)` 读回原文。

真空区补压（`gap.go`，2026-08-06）：会话冷启动/恢复后 Load 时，把 [栈顶 To+1 .. 尾窗起点-1] 的未压缩轮次压成合并帧（From/To 连续、摘要含真空区轮次、原文可选归档），保证“压缩栈覆盖 + 窗口”连续无断档。

ReplaceHistory 安全规则（`history_safety.go`）：压缩替换前清理 checkpoint / 旧压缩帧 marker，并按配对规则修复空正文（assistant+tool_calls 保留协议文本、assistant 推理正文保留协议文本、其余空正文补恢复注记；恢复注记不得作为助手回复渲染）。

跨会话快照压缩（`compactor.Compactor`，三级策略）：

| 预算 | 策略 |
|---|---|
| ≥500 或已满足预算 | 全量快照 |
| 200~499 | 摘要模式：Decisions/Findings/Constraints/Pending 折叠为计数 |
| <200 | 极简模式：仅最小安全快照 + Goal/Progress/Escape 按剩余预算截断 |

预算不足返回 `ErrBudgetTooSmall`，不会产出不可用快照。

---

## 3. 压缩格式要求（未压缩部分 / 已压缩部分）

### 3.1 已压缩部分（CompactFrame 与 working history 中的压缩帧）

持久化帧字段（`sessionstore/session_context.go`）：

```text
SegmentID   string    // 段标识：compact-{sessionID}-{unixms} / compact-gap-{sessionID}-{unixms}
From / To   int       // ChatQueue 单元累计索引（协议坐标，单调递增）
RoundFrom/RoundTo     // 会话轮次范围（RoundNo 体系启用前为 0）
EventFrom/EventTo     // Transcript EventSeq 范围（含端点）
MessageFrom/MessageTo // UI 消息定位键范围
Summary               // 栈顶自足摘要文本
Evidence              // EvidenceRef 列表（result:callID / 归档读回 ref）
CompressedAt
```

进入 working history 的压缩帧块格式：

```text
<!-- seelex:compact-context:v1 --> segment=<SegmentID> from=<From> to=<To>
```

摘要正文不重复注入（避免双重投喂）：正文由 Assembler 从 CompactStack 栈顶渲染为 JSON 结构块（`RenderStackBlocks`，`## 压缩上下文 (now using compact context)` + `{segment_id, from, to, summary, evidence}`）。

其他 marker 约定：

```text
<!-- seelex:compact-context:v1 -->   // 压缩帧块
<!-- seelex:context-checkpoint:v1 --> // 应用侧任务检查点（旧应用侧）
<seelex-tool-result-omitted>          // 超大工具结果省略块开始
</seelex-tool-result-omitted>         // 省略块结束
[子代理产出]                           // merge-back 注入的 user 消息前缀
```

规则要点：

1. 控制块（checkpoint / 压缩帧 marker）只作为上下文控制消息，**不得作为对话内容渲染或持久化**；ReplaceHistory 前统一清理；
2. 帧坐标单调：新帧 To 必须大于栈顶 To，否则去重跳过（跨 Load 重复覆盖同一区间时幂等）；
3. 摘要必须“栈顶自足”：新帧 Summary 内嵌上一栈顶 Summary + 本轮溢出轮次代表性内容，消费方只读栈顶即可；
4. 原文可逆：配置 `TurnArchiver` 后压缩轮次原文持久化，帧 Evidence 携带 `read_compressed_turn(segment_id=...)` 读回引用——压缩丢失可逆，减少幻觉；
5. 超大工具结果：进入历史前归档为 result_ref，省略块显式声明“不得从省略内容推断事实”，指引 `read_tool_result` 分页/过滤读取。

### 3.2 未压缩部分（窗口内）

无特殊文本格式要求，原样保留，但必须满足：

1. **完整协议单元**：窗口保留的是完整 chatUnit（含未闭合尾部），参与切分时孤立 tool 与未闭合工具链不构成单元；
2. **空正文配对修复**：provider 拒绝空 content 字段，所有消息按 history_safety 规则补配对文本；恢复注记不得渲染为助手回复；
3. **预算边界**：窗口大小由 WindowPolicy 推导，硬阈值路径可用更小窗口但不得低于 MinRounds；窗口内永不主动压缩；
4. **注入边界**：merge-back 文本以 `[子代理产出]` 前缀注入，在 ChatStream 边界外进行，不参与窗口内轮次切分。

### 3.3 跨会话继承块格式（ContextSnapshot.Format）

父证据注入子代理 system prompt 的结构化文本块（`snapshot.Format()`）：

```text
## 继承上下文 (Inherited Context)
> 来源会话: <sessionID> | 导出时间: <time> | 消息数: <count>

### 目标 (Goal)
...
### 关键决策 (Decisions)
...
### 重要发现 (Findings)
...
### 已完成进度 (Progress)
...
### 约束条件 (Constraints)
...
### 待完成工作 (Pending)
...
### 逃逸信息 (Escape)
...
---
以上为继承的上下文。请基于这些信息继续工作，在决策时引用上述目标和约束。
```

必填校验（`Validate()`）：`SourceSessionID`、`ExportedAt` 非空；`Goal` 非空（有父目标 Escape 时除外）。

---

## 4. 技术实现优势

1. **结构化优于全文搬运**：Goal/Decisions/Findings/Constraints 分字段传递，避免把父会话全文塞给子代理；对多代理与长任务，结构化块比完整历史更抗“lost-in-the-middle”退化。
2. **稳定前缀友好**：system/project/stack 前置且会话内不变，动态块尾部，天然契合前缀 KV 缓存；子代理与主会话同源组装，块序固定。
3. **栈顶自足 + 递归摘要**：窗口外全部轮次折叠进一帧，消费方无需展开历史；摘要递归内嵌，覆盖与有界同时成立。
4. **可逆压缩**：原文归档 + `read_compressed_turn` 读回，压缩不再是单向破坏；`result_ref` 同理，工具长输出按需分页读取。
5. **真空区补压**：以 append-only 事件流为真相源，冷启动/恢复不丢段，压缩栈与窗口连续无断档。
6. **并发安全**：父证据读-合并-写回收敛进单 goroutine（actor/CSP），读取面 atomic 无锁；mailbox 有界但溢出只计数不丢内容；避免并行子代理合并互相覆盖。
7. **确定性记忆选取**：词法 bigram 打分的 top-K 选取零外部依赖、可复现，接口上可被 embedding 检索替换。
8. **copy-on-write**：压缩与合并不修改调用方快照，多消费者可安全共享。

---

## 5. 文档中的原因与站得住脚程度

### 5.1 文档给出的设计原因

`docs/arch/context-improvement-plan.md`（v1.0，2025-12-21）列出的 9 个已知问题全部是**质量/能力问题**：

| # | 问题 | 严重程度 |
|---:|---|---|
| 1 | Export 太 naive（仅取首条用户消息作 Goal） | 中 |
| 2 | Import 覆盖已有 prompt | 高 |
| 3 | TokenEstimate 从未填充 | 低 |
| 4 | 缺少上下文压缩，历史过长无法处理 | 高 |
| 5 | 缺少双向合并，子代理结果无法回写父代理 | 中 |
| 6 | 紧耦合 engine.Engine，不便测试 | 中 |
| 7 | 缺少 ContextProvider 模式 | 中 |
| 8 | Import 无返回值、缺错误反馈 | 低 |
| 9 | 缺少快照验证机制 | 低 |

工作包记录（`docs/2026-08-04-context-memory-lifecycle/`、`docs/2026-07-29-planact-context-control/`、`docs/2026-07-30-context-summary/` 等）的问题陈述同样集中在长任务收敛、死锁、内存、上下文超限。

### 5.2 站得住脚程度

- “长任务必须控制上下文窗口、必须做摘要/压缩/合并”这一原因**站得住脚**，且有论文与官方实践支撑（见 §10）；
- 早期对标（Claude Code Provider/Budget、LangChain Memory、Continue.dev Context Provider）为接口与策略提供了参照，但**不构成设计初衷**；
- 缓存命中率不在设计文档的原因列表里，是事后（R3）才出现的优化目标，且解法与继承/合并正交。

### 5.3 文档-实现不一致（已发现问题）

1. arch 文档 P4 约束写“压缩不可逆（不保存被压缩部分）”，当前实现可逆（TurnArchiver + read_compressed_turn）；
2. arch 文档描述 TraceProvider 用 `engine.ExportTrace() *tracer.Tree`，当前实现改用 telemetry 生命周期事件（llm/tool intent-effect）；
3. arch 文档路径是 `context/`，当前实现已迁移到 `seelexctx/`；
4. 文档状态仍为“方案评审中”，未同步标注已实现。

---

## 6. 设计初衷：缓存命中 vs 结果产出

**结论：设计初衷是结果产出（质量），缓存是次级目标。**

证据链：

- 2026-08-04 之前的所有上下文工作包问题陈述均为质量/稳定性（收敛、死锁、内存、上下文超限），无一以缓存为主题；
- 缓存首次进入决策是 2026-08-09 复盘 R3：Seelex 命中率约 66%（命中 142,976 / 未命中 72,176），codex-cli 约 99.8%；根因是 system prompt 每次 turn 重建且嵌入随节点变化的 `current_node`；
- R3 的解法是：system prompt 只放 plan 级稳定信息（plan_ref），`current_node` 移出；动态任务状态改为请求尾部的“工作打点表”（`<!-- seelex:worktable:v1 -->`）承担，尾部更新缓存友好；system prompt 幂等（内容不变不重设）；
- 该解法作用于 prompt 组装层，**不是修改继承/merge 本身**；
- `docs/devlog/2026-08-11-subagent-interview-answers.md` 明确边界：“不能为了命中缓存而把动态上下文强塞进前缀或牺牲正确性”。

---

## 7. 缓存命中影响分析

| 机制 | 对前缀缓存的影响 | 说明 |
|---|---|---|
| 继承块布局（system/project/stack 前置、memory 尾部） | 提高或中性 | 稳定前缀尽量长且不变；子代理与主会话同源组装，可复用公共前缀 |
| 合并文本注入（`[子代理产出]` 尾部追加） | 中性（一次尾部失效） | 追加在历史尾部，不影响既有前缀；新内容本身必然产生一次未命中 |
| 压缩（ReplaceHistory） | 降低一次 | 窗口替换为 marker + 新窗口，前缀整体失效；这是“有界上下文”的固有代价，压缩后稳定前缀恢复 |
| 真空区补压 | 中性 | 仅影响压缩栈与持久化，不改变请求前缀 |
| 超大工具结果归档 | 提高 | 长输出不入历史，历史体积受控，前缀更稳定 |
| 记忆 top-K 选取 | 尾部动态 | memory 块单独放尾部，避免打断前缀 |

现有数据点：

- 66% → codex-cli ~99.8%（R3 前后对照，2026-08-09）；
- 97.6%：perf 报告显示 3,769 万 token 中 97.6% 是“同一段大上下文被反复携带”的缓存命中输入（130+ 次工具调用，每次发送 system + 完整历史 + 请求参数）——这说明前缀稳定时命中率可以很高，也说明“稳定前缀”策略有效。

缺失的测量：目前没有“压缩/合并动作前后”的前缀命中率对照实验；66%/99.8% 与 97.6% 是孤立快照，不能直接归因到继承/merge 布局。

---

## 8. 质量判断

### 8.1 同一会话内：质量偏好

- 窗口保留最新完整轮次，压缩只作用于窗口外；
- 旧轮折叠为栈顶自足摘要，且原文可归档读回（`read_compressed_turn`），压缩不丢事实；
- 真空区补压保证冷启动/恢复后无断档；
- 超长会话按当前查询注入相关记忆块，比全部历史拼接收敛性更好。

### 8.2 跨会话与摘要层：明确质量损失

- **投影整体替换**：`publishRuntimeProjections` 每次发布用当前会话快照重建父证据，`handleSetProjection` 整体替换 state；合并累积只存在于同一次投影周期内（同一次 plan_run），跨 plan_run 的结构化 Findings/Decisions 不延续（人可见内容已通过注入块留在会话历史中）。该语义需要明确或修复。
- **摘要层计数化**：`compactor` 摘要模式把 Decisions/Findings/Constraints 折叠为“N decisions / N findings / details compacted”，明确丢失条目级信息；只有最小安全快照保证身份与坐标。
- **词法记忆**：CJK bigram / ASCII 词匹配是零依赖的工程降级，无检索质量理论支撑；作为确定性兜底可用，但不能替代语义检索（代码已预留替换接口）。
- **token 估算（已替换，2026-08-20）**：旧 `len/3` 字节估算（约 3 字节 ≈ 1 token；它是字节→token 的保守换算系数，不是压缩比例，也不是压缩触发阈值）已淘汰，统一替换为 `seelexctx/tokens` 的脚本感知估算（CJK 1 字符≈1 token、ASCII 4 字符≈1 token、其余符号 2 字符≈1 token，偏保守）：
  - 接入点：`seelexctx/controller.go` 默认 `ConservativeTokenCounter`、`gap.go:71` 兜底、`seele.go` 兼容变量 `EstimateTokens`、`compactor/compactor.go`、`memory/block.go`、`search/search.go`、`mcpstack/interceptor.go`、`application/core/token_counter.go`；
  - application 侧生产默认改为 `calibratedTokenCounter`：每次 LLM 调用返回真实 usage 后，按 EMA 修正因子校准（clamp 0.5~2.5），使事前估算向 provider 真实计数收敛（`task_context_state.go` 的 `recordLLMComplete` 触发 `Observe`）。
  真实计数仍记入 `TokenAudit.ActualPromptTokens` 与 telemetry `gen_ai.usage.*`。设计文档（`docs/2026-07-29-planact-context-control/plan.md:44`）曾保留 len/3 作为稳定估算、不把 provider 统计当唯一依据；本次替换后该约束已过期。此前调研（`docs/research/agent-harness-research-report.md` P2-6/P2-10、`docs/research/coding-agent-harness-comparison.md` §2.2）建议接入模型感知 tokenizer（tiktoken）或 provider 计数接口并对窗口/压缩预算使用同源计数——本替换完成“同源计数”与“usage 反馈”，剩余差距：模型感知 tokenizer（tiktoken，需引入依赖与离线 BPE 数据决策）、压缩帧“压缩前后估算与实际计数”落盘二次校验（P2-6）。

### 8.3 总判断

继承/合并/压缩的整体设计以“在有限窗口内保住最多可用的结构化信息”为目标，质量主线成立；跨会话与摘要层的损失是明确取舍，需要产品决策确认，而不是隐藏缺陷。

---

## 9. 需求驱动 vs 对标

**结论：需求/事故驱动为主，“别人有我也有的能力用了行业参照”，不是“为了对标而做”。**

需求/事故驱动证据：

- R1：成功子代理“跑完即清走”，无证据 → 父证据注入与合并回流；
- R3：缓存命中率 66% vs 99.8% → system prompt 稳定化 + 打点表；
- R4：四个子代理全部卡 queued 的环路死锁 → CSP/actor 重构；
- R5：WebView 200+MB → 70MB 内存优化；
- 2026-08-06：冷启动丢段 → 真空区补压；
- 超大工具结果撑爆上下文 → result_ref 归档；
- 超长会话摘要不可选择 → memory top-K 选取。

对标与“不借鉴”记录：

- 早期参考：Claude Code Context Provider / Budget、LangChain Memory 体系、Continue.dev Context Provider（`docs/arch/context-improvement-plan.md`）；
- LoopX：借鉴“类型化的 gate”，明确不借鉴完整事件溯源 JSONL、quota 分钟槽账本、heartbeat 19 步（`docs/product/pmstory.md` 决策 35、`docs/2026-08-07-loopx-context-research/`）；
- Seele gap 分析（`docs/2026-08-14-probe-context-plugin/seele-gap-analysis.md`）：明确“框架无必须补的硬缺口”“血缘线可视化不排期”，全部 seelex 侧落地。

---

## 10. 论文与博客理论依据映射

以下文献均经检索核实，支撑“设计有据、主线指向质量”的判断：

| 设计点 | 文献 | 关键结论 |
|---|---|---|
| 窗口 + 摘要位置（开头/结尾保留关键内容） | Lost in the Middle, Liu et al., TACL 2024（arXiv:2307.03172） | 上下文 U 型曲线，中间位置退化；GPT-3.5-Turbo 中间位置比闭卷低 56.1% |
| 开头 token 稳定、attention sink | StreamingLLM, Xiao et al., ICLR 2024（arXiv:2309.17453） | 保留开头 token 可维持稳定注意力，实测 22.2× 加速 |
| 压缩提升质量并降本 | LongLLMLingua, Jiang et al., ACL 2024（arXiv:2310.06839） | NQ 任务 +21.4%，token 约 4× 减少，LooGLE 成本降 94% |
| 递归摘要覆盖超长文本 | OpenAI 2021 论文（arXiv:2109.10862） | 递归摘要深度 3 可覆盖数十万字 |
| 可逆分页 / 外部上下文 | MemGPT, Packer et al.（arXiv:2310.08560） | 主上下文/外部上下文虚拟管理，分页取回 |
| 记忆检索（recency+importance+relevance） | Generative Agents, Park et al., UIST 2023（arXiv:2304.03442） | 记忆检索按近因+重要性+相关性排序 |
| 记忆分层 | CoALA（arXiv:2309.02427） | 工作记忆 vs 情景/语义/程序记忆 |
| 多代理逐级合并 | Chain-of-Agents, NeurIPS 2024（arXiv:2406.02818） | 层级 worker + 合并最优，9 数据集最高 +10% |
| 多代理失败模式 | MAST, NeurIPS 2025（arXiv:2503.13657） | 1600+ 轨迹、14 类失败模式、3 簇，κ=0.88 |
| 结构化通信/分块避免中间退化 | MetaGPT（arXiv:2308.00352）；Tree of Agents, EMNLP 2025 | SOP/分块沟通降低 lost-in-the-middle 影响 |
| 前缀 KV 缓存 | Prompt Cache, MLSys 2024（arXiv:2311.04934）；SGLang（arXiv:2312.07104） | 前缀 KV 复用；SGLang 最高 6.4× 加速 |
| 官方实践：compaction 保留决策/未解决 bug、丢弃冗余工具输出 | Anthropic《Effective context engineering》(2025)；Claude Code prompt caching / agent-loop 文档 | 前缀匹配、中途改动越少命中率越高；压缩聚焦决策与未完成项 |
| 并发模型：消息传程序列化 | Hoare CSP (CACM 1978)；Actor model (IJCAI 1973) | 消息传程序列化消除共享锁与死锁 |

对应关系（实现 → 理论）：

- 窗口内原样保留 + 栈顶摘要前置 → Lost in the Middle 与 StreamingLLM；
- 压缩目标是质量而非省 token → LongLLMLingua / OpenAI 递归摘要；
- 原文归档 + read_compressed_turn → MemGPT 分页；
- 父证据结构化继承 → CoALA 记忆分层；
- merger 逐级合并 → Chain-of-Agents 层级合并；
- 子代理独立 Session + mailbox → CSP/Actor；
- 稳定前缀布局 → Prompt Cache / SGLang / Anthropic 前缀实践；
- 工作打点表尾部注入 → Anthropic“中途改动越少命中率越高”。

无直接理论支撑的工程降级（建议如实标注）：

- Decisions/Findings 折叠为计数（摘要层信息损失）；
- 词法 bigram 记忆选取（确定性兜底，非语义检索）；
- len/3 token 估算（2026-08-20 已替换为脚本感知估算 + usage 校准，见 §8.2；模型感知 tokenizer/tiktoken 仍未接入，属可选升级）。

---

## 11. 已发现问题（记录，未修改）

1. **arch 文档与实现矛盾**：`context-improvement-plan.md` 的“压缩不可逆”/tracer.Tree/`context/` 路径与当前实现不一致，且状态仍为“方案评审中”（见 §5.3）。
2. **merger 非幂等**：Findings/Decisions append 无去重、无上限，多子代理快照可能无界膨胀。
3. **跨会话合并累积被投影覆盖**：`SetParentEvidenceProjection` 整体替换 state，结构化累积生命周期与投影发布时机不一致，语义需要明确或修复（见 §8.2）。
4. **token 估算升级（部分完成，2026-08-20）**：len/3 已替换为 `seelexctx/tokens` 脚本感知估算，application 侧增加 usage 反馈校准（Observe/EMA）；仍缺：模型感知 tokenizer（tiktoken）接入，以及压缩帧落盘“压缩前后估算与实际计数”的二次校验（调研 P2-6 建议）。
5. **缓存归因证据不足**：缺少“压缩/合并动作前后”前缀命中率对照；66%/99.8% 与 97.6% 是孤立快照。

---

## 12. 后续建议

1. 修正或归档 `docs/arch/context-improvement-plan.md`，使文档状态与实现一致；
2. merger 增加 Findings/Decisions 去重与上限（例如按内容 hash 幂等 + 上限截断）；
3. 明确“父证据合并累积”的跨会话语义：同一次 plan_run 累积 vs 跨 plan_run 重导出，并补文档与测试；
4. 以本文 §3 为底稿，整理一份 `seelexctx` 压缩格式契约文档（marker、协议单元、最小安全快照、坐标语义）；
5. 补“压缩/合并动作前后前缀命中率”对照测量，用真实 usage 的 `prompt_cache_hit_tokens/miss_tokens` 归因。
6. （已完成 2026-08-20）len/3 → `seelexctx/tokens` 脚本感知估算 + application usage 校准（Observe/EMA）；可选下一步：接入模型感知 tokenizer（tiktoken，需先决策依赖引入与离线 BPE 数据）。

---

## 13. 参考来源

### 论文

- Liu et al., Lost in the Middle: How Language Models Use Long Contexts, TACL 2024 — https://arxiv.org/abs/2307.03172
- Xiao et al., Efficient Streaming Language Models with Attention Sinks, ICLR 2024 — https://arxiv.org/abs/2309.17453
- Jiang et al., LongLLMLingua: Accelerating and Enhancing LLMs in Long Context Scenarios via Prompt Compression, ACL 2024 — https://arxiv.org/abs/2310.06839
- OpenAI, Summarizing Books with Human Feedback — https://arxiv.org/abs/2109.10862
- Packer et al., MemGPT: Towards LLMs as Operating Systems — https://arxiv.org/abs/2310.08560
- Park et al., Generative Agents: Interactive Simulacra of Human Behavior, UIST 2023 — https://arxiv.org/abs/2304.03442
- Sumers et al., Cognitive Architectures for Language Agents (CoALA) — https://arxiv.org/abs/2309.02427
- Zhang et al., Chain-of-Agents: Large Language Models Collaborating on Long-Context Tasks, NeurIPS 2024 — https://arxiv.org/abs/2406.02818
- MAST: Multi-Agent System Failure Taxonomy, NeurIPS 2025 — https://arxiv.org/abs/2503.13657
- Hong et al., MetaGPT: Meta Programming for Multi-Agent Collaborative Framework — https://arxiv.org/abs/2308.00352
- Tree of Agents, EMNLP 2025 — 见 arXiv 检索
- Gim et al., Prompt Cache: Modular Attention Reuse for Low-Latency Inference, MLSys 2024 — https://arxiv.org/abs/2311.04934
- Zheng et al., SGLang: Efficient Execution of Structured Language Model Programs — https://arxiv.org/abs/2312.07104
- Hoare, Communicating Sequential Processes, CACM 1978
- Hewitt et al., A Universal Modular Actor Formalism for Artificial Intelligence, IJCAI 1973

### 官方实践

- Anthropic, Effective context engineering for AI agents (2025)
- Claude Code prompt caching / agent-loop 文档（前缀匹配、compaction 语义）

### 仓库内部文档

- `docs/arch/context-improvement-plan.md`（v1.0 方案，含 9 个已知问题与对标分析）
- `docs/2026-08-09-worktable/retrospective.md`（R1~R5，含缓存命中率问题）
- `docs/devlog/2026-08-11-subagent-interview-answers.md`（子代理/缓存/不牺牲正确性边界）
- `docs/2026-08-07-loopx-context-research/research-loopx-context-management.md`（借鉴与不借鉴点）
- `docs/2026-08-14-probe-context-plugin/seele-gap-analysis.md`（Seele 缺口重评，无硬缺口）
- `docs/product/pmstory.md`（决策 35 等）
- `docs/test/REPORT-perf-latest.md`（97.6% 缓存命中数据）
