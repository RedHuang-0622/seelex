# 重启恢复出来的上下文，模型拿不到之前的对话：链路不对称的复现

- 日期：2026-09-30
- 触发：用户报告 —— 「重启恢复出来的上下文需要让 LLM 有之前上下文的内容，现在不是；
  可能跟压缩的**时机和链路**有关；压缩有时候不调用 LLM 做内容摘要，有时候压完了只是
  折叠了上下文」
- 范围：压缩的三条链（装配层 / 回合内控制器 / 真空区）→ 会话压缩栈 → 重启后装配进请求的
  「压缩上下文 (now using compact context)」块
- 产出：**确定性复现探针**（无网络、无凭据）+ 本次现场。**本文件只做复现与定位，不改产品代码。**
- 后续：本文件提出的 §4 判据已修复并转绿（R1/R2/R3）——修法与实测前提见
  `docs/devlog/2026-09-30-restart-restore-context-lost-fix.md`；探针文件与本文档的现场保持原样，作为回归钉。

---

## 0. 一句话

会话压缩有**三条**写帧的链路，其中只有**装配层**那条注入了模型摘要器（`Summarizer`）；
回合内控制器（`after_assistant`/`after_tool` 软线）与节点控制器链路**恒不注入**，因此恒落到
本地确定性折叠 —— 帧里只有「溢出轮次: N 个完整协议单元」+ 每轮 80 字截断的用户行，助手侧
正文一个字都没有。折叠后这条本地帧就是**栈顶**，而模型请求只渲染栈顶帧的 Chapter 2
（`seelexctx/assembler.go` 的 `RenderStablePrefixBlocks` → `FrameChapter2`）。于是重启（冷加载）
之后，被折掉的那些轮次在模型眼里只剩这份"折叠记录"——**能力上不是丢了数据（原文可按区间回读），
是模型看不到内容**。

这正好解释用户口径里的三条：①重启后没有之前的内容；②时有时无（哪条链先折，取决于那一轮是
在回合内越线还是在下一次装配时越线）；③"有时候不调用 LLM 做摘要"。

## 1. 复现（确定性；RED 现场保留在本文档）

```powershell
$env:SEELEX_RESTORE_PROBE='1'; go test -tags restoreprobe ./seelebridge/ -run TestRestoreProbe -v -count=1
# 现场同时落盘：seelebridge/_tmp/restore-probe-report.md
```

探针（`seelebridge/cold_restore_compaction_context_probe_test.go`）在同一条会话上：

1. **链①** 走生产入口 `Runtime.PushCompactionFrame`（= 装配层折叠那条链，注入摘要器）；
2. **链②** 走 `Runtime.seelexController().Handle(after_assistant)`（= 回合内控制器那条链，同一份
   内容、同一个进程、同一个开着的开关）；
3. 重启：`Shutdown()` + 关 Router，在同一份 root 上**重新装配** Runtime 并 `store.Load()`
   （顺序与生产入口 `internal/adapters.SessionPort.AttachSessionContext` 一致：先 Load 再挂接）；
4. 打印**模型真能看到**的那一个块（`seelexctx.RenderStablePrefixBlocks` 里 `name=compact`）。

实测（`restore-probe-report.md` 原文）：

```text
配置: summary.enabled=true window=200000 budget=166808 soft=158467 hard=163471 carry_tokens=1024

链① 装配层折叠: source="replay" note="" 模型调用累计=1
链② 回合内控制器折叠: replace=true 本次模型调用=0（累计 1）
栈: 2 帧
  frame[0] id=compact-session-restore-probe-…432 source="replay" evidence=[]
  frame[1] id=compact-session-restore-probe-…520 source="local"
    evidence=[{frame-carry:kept:95/1024 previous frame chapter-2 carried into this frame (limits.context_frame_carry_tokens)}
              {fold-local:no-summarizer 摘要器未装配（开关关闭或 QuickChat 装配失败）}]

重启后: 压缩栈 2 帧
重启后模型可见的压缩上下文块（栈顶帧 Chapter 2，节选）:
  ### 目标 (Goal)                      (none)
  ### 关键概念 (Key Concepts)          (none)
  ### 文件与代码 (Files and Code)      (none)
  ### 错误与修复 (Errors and Fixes)    (none)
  ### 待办 (Pending)                   (none)
  ### 当前工作 (Current Work)
  溢出轮次: 163 个完整协议单元
  - 用户: 第 0 轮问题：这里的根因是什么？
  …（163 行，全部是 80 字截断的用户行）…
  - 用户: 第 162 轮问题：这里的根因是什么？
  先前压缩摘要: ### 目标 (Goal) 摘要正文：二十七号那批 flaky 用例的…
  ### 下一步 (Next Step)               (none)
  ### 关键约束 (Constraints)           (none)
```

> 探针 runtime 不加载出厂配置（`newTestRuntime` 用零值 limits → `carry_tokens` 落到兜底
> `DefaultFrameCarryTokens` = 1024；出厂 `config/seelex.yaml` 是 4096）。开臂是探针显式设置的
> `limits.ContextCompactionSummary.Enabled = true`——**只动这一个字段**，其余保持零值语义，
> 所以"开关是开的"在链①/链② 两条链上是同一个进程事实。

两条判据（红）：

```text
--- FAIL: TestRestoreProbeModelSeesNoPriorContent
R2: 帧把「没有模型摘要」自答成「开关关闭或 QuickChat 装配失败」，但本进程的开关是开的
    （enabled=true）。回合内控制器链路本次模型调用=0 次
R1: 被折的 163 个轮次的正文（助手侧含判据串 "根因：A3 写者在提交临界区里重入了读路径…"）
    一条都没进模型可见面——模型拿到的只是「溢出轮次: N 个完整协议单元」+ 每轮 80 字用户行
```

> 探针的保真度自检（R3，绿）：帧链跨重启存活（重启前后都是 2 帧）。所以红的是内容与自答，
> 不是"读不到帧"。

## 2. 链路：谁注入了摘要器

| 链路 | 触发时机 | 代码位置 | `Summarizer` | 复盘 |
|---|---|---|---|---|
| 装配层（回合开始前） | 软线 / `/compact` / 硬线 / 自主压缩 | `context_runtime/coordinator.go` → `PushCompactionFrame` → `seelebridge/runtime_compaction_index.go` → `Runtime.MainCompactionDAG()` | **注入**（`compactionSummarizerWithNote`，开关开 = QuickChat 前缀重放） | 0 次或 1 次调用，`source=replay`（探针链①） |
| 回合内控制器 | `after_assistant` / `after_tool` 越软线、超大工具结果 | `seelebridge/runtime_context.go` 的 `seelexController()` → `seelexctx.NewCompactionDAG(...)` | **恒 nil**（该构造点不设 `Summarizer`；注释写明"字节级装配出口未固化，暂不注入"） | 恒 `source=local`，0 次调用（探针链②） |
| 真空区覆盖（gap） | 冷恢复 / 滑动窗口装配 | `seelebridge/runtime_context.go` 的 `coverHistoryGap` → `seelexctx.CoverHistoryGap` → `LocalChapter2` | 无 DAG，直接本地 | 恒 `source=local` |
| 节点子代理 | 子代理回合 | `seelebridge/runtime_context.go` 的 `nodeController()` | 恒 nil | 恒 `source=local` |

降级出口在 `seelexctx/dag.go` 的 `chapter2Node`：`Summarizer == nil` → `degrade("no-summarizer", note)`；
`note` 取 `SummarizerNote`，为空时用**默认措辞**「摘要器未装配（开关关闭或 QuickChat 装配失败）」。
控制器那条链既没注入摘要器、也没注入 note，于是帧只会告诉读帧的人"开关可能关着、或装配失败"——
而事实是**这条链从来不注入摘要器**（探针链② 与同一进程里链① 的 `replay` 同时成立即为证）。
这正是 2026-09-29 那轮"降级必须自答"遗漏的一半：那轮修的是**装配层**链路的三种 nil 出口，控制器
链路的 nil 是**结构性**的，不在这三种出口里。

## 3. 本地折叠的 Chapter 2 里到底有什么

`seelexctx/frame.go` 的 `LocalChapter2WithCarry`：

| 小节 | 来源 | 本探针取值 |
|---|---|---|
| 目标 / 关键概念 / 关键约束 | 会话记录的 Task/Plan 栈 | `(none)`（冷加载会话栈空） |
| 文件与代码 | 被折单元里的**工具名** | `(none)`（无工具轮） |
| 错误与修复 / 待办 / 下一步 | 恒空 | `(none)` |
| 当前工作 | `溢出轮次: N` + 每单元 `renderUnitLine`（用户行前 80 字；assistant 只出**工具调用名**，tool 只出**工具名**）+ 上一帧正文并入 | 163 行用户问句预览 |

即：**它不是摘要，是索引**——用户问了什么（截断）、调用了哪个工具，仅此而已；助手回答了
什么（本探针里是 163 条各 12 KB 的答复）完全不进帧。帧正文是模型唯一能看到的被折内容
（`assembler.go` 只渲染栈顶帧 Chapter 2），所以"压缩完了只是折叠了上下文"是**当前代码的
确定行为**，不是偶发。

## 4. 判据（建议写进设计/测试）

- **R1 内容可达**：折叠后，被折轮次的**内容**（或由模型产出的摘要）必须出现在模型可见的
  压缩上下文块里；只有轮次计数 + 用户行预览不算通过。若选择"只给索引、内容靠
  `read_compressed_turn` 回读"，那必须让模型**知道**该回读（帧内的回读引导必须与"索引不含
  内容"这个事实一起写出），不能既不给内容也不给入口。
- **R2 自答必须与事实一致**：`summary_source=local` 的每一帧都要能自答"模型为什么没被叫到"，
  且答案要区分**配置**（开关关）、**故障**（调用失败，带报错）与**结构性**（这条链从来不注入
  摘要器）。当前控制器链路把第三种答成了第一种，读帧的人会去查一个并不存在的配置事故。
- **R3 帧链跨重启**：帧的持久化与冷加载回读必须成立（本探针绿；2026-09-29 的推帧接线修的就是
  这条，此处是回归钉）。

## 5. 边界（如实记账）

- **帧摘要传递上限（`limits.context_frame_carry_tokens`，出厂 4096）**：本地折叠会把上一栈顶帧
  的 Chapter 2 正文并入新帧；超限时正文退化为**锚点**（`segment_id` + request 首尾 + 一句话），
  并在正文里自答"正文退化为锚点，细节经 search_history / read_compressed_turn 回读"
  （`seelexctx/frame.go` 的 `CarryPreviousChapter2` / `renderCarryAnchor`）。**已实测**：把上限压到
  32，重启后模型可见块里确实只剩锚点 + 一句话，正文不见，但降级有自答——这是**有意的取舍**
  （不设上限会让栈顶帧随帧数线性膨胀），本轮不把它算作缺陷；但它是"多帧之后内容越来越少"
  的第二条来路，与 R1 同向，值得和 R1 一起设计。
- **帧区间到事件 seq 的映射**：compact 通道的帧区间由 `sessionstore/json_layout.go` 的
  `resolveCompactRange` 映射（`EventFrom/EventTo` 已给定时直接用，否则按 ChatQueue 单元下标反查
  可见行）。本探针里 `ev.History` 与存储里的 message 行一一对齐，映射成功。**冷加载后**
  `ev.History` 是尾窗（有界）而内容区号是**累计**口径（`compactedUnitBase` / `dagOrdinalBase`），
  两者是否始终对齐**未验证**——若不对齐，`commitCompactFrameWorkspace` 会返回 `ok=false` 而
  `SessionContextStore.bridgeCompactFrame` **静默忽略**（注释口径：compact.jsonl 可重建、下次提交
  重试）。这条属于 Hypothesis：需要一次"冷加载 + 立即折叠"的现场或另一条探针来证实/排除，
  本轮不猜。
- **真实 API 复跑**：本探针全离线（`injectScriptedCompleters` 的确定性 completer），证明的是
  **链路形状**；帧里的真摘要文本质量、前缀缓存命中率仍需 `-tags compactlive` 类的真实调用
  覆盖（审查报告 `docs/2026-09-29-context-compaction-fold-review.md` 的 P1-C 仍未闭环）。
- **本轮不改代码**：修法至少有两条（给控制器链路注入与装配层同源的摘要器；或让控制器链路
  的帧明确降级为"索引 + 回读引导"并如实自答第二种原因）。选哪条取决于"控制器链路的
  `ev.History` 是否与 wire 同源"这个既有未决前提（`seelebridge/runtime_context.go` 的注释），
  需要先量它，再决定是否愿意为这条链路付一次付费调用。
  **（后续 2026-09-30：已量——`ev.History` 是装配前的工作历史，与 wire 不同源，付全价也换不来
  前缀缓存，故不注入；改为"索引带上被折内容 + 如实自答结构性原因"。见
  `docs/devlog/2026-09-30-restart-restore-context-lost-fix.md`。）**
