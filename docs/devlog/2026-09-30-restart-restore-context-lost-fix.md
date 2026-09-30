# 重启恢复上下文丢失：折叠索引带上被折内容 + 控制器链路降级自答改正

- 日期：2026-09-30
- 上游：`docs/devlog/2026-09-30-restart-restore-context-lost-probe.md`（复现与定位；本文件收口其 §4 的三条判据）
- 范围：`seelexctx`（本地确定性折叠的索引 + 降级自答默认措辞）、`seelebridge`（控制器/节点链路的折叠 DAG 注入自答）
- 结论：探针由红转绿（R1/R2/R3）。**不**给控制器链路注入模型摘要器——那条路的前提经实测不成立（见 §3）。

---

## 0. 一句话

折叠发生后模型看不到被折内容，是因为本地确定性折叠的索引**只留用户问了什么、助手回答了什么一个字都不留**；而帧的栈顶 Chapter 2 是模型唯一能看到的被折内容。本次把助手答复的首段一并写进索引，并写明"这是索引、正文经回读"，同时把控制器链路"为什么没有模型摘要"的自答从**假冒的配置事故**改成**结构性原因**。

---

## 1. 缺陷的两半（对应探针 R1/R2）

| 判据 | 缺陷 | 修法 |
|---|---|---|
| R1 内容可达 | `renderUnitLine` 的 assistant 分支只写工具调用名，被折轮次的助手正文完全不进帧；索引也不写明"正文不在本帧" | 助手答复取首段预览（rune 上限 80）写进索引；索引尾补一句"每轮只留首段预览，完整原文经 read_compressed_turn / search_history 回读" |
| R2 自答与事实一致 | 控制器链路恒不注入摘要器（结构性），帧却写"摘要器未装配（开关关闭或 QuickChat 装配失败）"——而进程里开关是开的 | 给控制器/节点链路的折叠 DAG 注入 `SummarizerNote`（结构性说明）；`chapter2Node` 的**默认**措辞改成不冒充配置/装配事故 |
| R3 帧链跨重启 | 已成立（回归钉，未动） | — |

两处都落在"模型可见面"这一侧：R1 让内容真的进去，R2 让读帧的人不再被误导去查一个并不存在的配置事故。

---

## 2. 改了什么

| 文件 | 改动 |
|---|---|
| `seelexctx/controller.go` | `renderUnitLine`：用户/助手各取首段预览（新常量 `maxUnitPreviewRunes = 80`），**按 rune 截断**（原来按 byte，中文会在多字节字符中间断开）；助手带工具调用时同时留正文预览与调用名 |
| `seelexctx/frame.go` | `localCurrentWork`：索引尾补"以上为折叠索引……经 read_compressed_turn / search_history 回读"（只在真有轮次时写，空骨架仍 8 个 `(none)` 不变） |
| `seelexctx/dag.go` | `SummarizerNote` 文档补"结构性不注入"这一路；`chapter2Node` 的默认措辞改为"这条折叠链路没有注入摘要器，调用方未说明原因" |
| `seelebridge/runtime_context.go` | 新增 `controllerFoldSummarizerNote`，注入 `seelexController()` 与 `nodeController()` 的折叠 DAG（`SummarizerNote`） |

---

## 3. 为什么**不**给控制器链路注入摘要器（实测，不是推测）

上游探针 §5 把修法选择押在一个未决前提上：「控制器链路的 `ev.History` 是否与 wire 同源」。本次把它量了：

- `seelectx.ContextEvent.History` 的产地是 `vendor/.../Seele/session/loop.go:258,300` 的 `rl.History()`——**引擎工作历史**的拷贝；
- 真实请求的字节由 `callLLM`（`loop.go:793`）从同一份工作历史 + `promptBlocks` 经 `seelexAssembler.Assemble` 装配而成：`system → project → 记忆 → 稳定前缀栈(skill/compact) → 调用方静态块 → WorkingHistory → 尾部栈`（`seelexctx/assembler.go`）。

即：`ev.History` 是**装配前**的工作历史，system 块、项目/记忆/前缀栈块、工具面都还没进去。前缀重放要求 `SystemPrompt/History/Tools` 与真实请求**同字节**（详设 §4.4 的字节级一致性约束、§9 风险 1），拿工作历史当重放素材会**付一次全价调用却换不来前缀缓存**。装配层链路之所以能注入，是因为它在折叠那一刻手里正好握着上一次真实请求的三样原件（`coordinator` 的 `systemPrompt/existing/tools`）。

结论：**不注入**。控制器链路保持本地确定性折叠，改为"索引带上内容 + 如实自答"。要给这条链路付费调用，前置是先把字节级装配出口快照固化下来（`seelexAssembler` 出口处留一份 wire 级快照）——那是另一条改动，本次不做。

---

## 4. 验证证据

```powershell
# 探针（双门禁：build tag restoreprobe + SEELEX_RESTORE_PROBE）
$env:SEELEX_RESTORE_PROBE='1'; go test -tags restoreprobe ./seelebridge/ -run TestRestoreProbe -v -count=1
# → PASS；模型可见面自检: 被折轮次助手正文=true 装配层厚摘要正文=true 80 字用户行=true

# 新补的常驻钉子（不依赖 build tag）
go test ./seelexctx/ -run 'LocalChapter2|RenderUnitLine|CompactionDAG' -count=1
go test ./seelebridge/ -run TestControllerFoldDegradeNoteIsStructural -count=1

# 本地门禁
go test ./seelexctx/... ./application/core/... -count=1
go test ./seelebridge/... -count=1
```

新增三条常驻用例（都带牙——删掉对应改动即红）：

| 用例 | 钉住 |
|---|---|
| `seelexctx.TestLocalChapter2CarriesAssistantAnswerPreview` | 助手答复首段进帧 + 索引写明回读入口 |
| `seelexctx.TestLocalChapter2AssistantPreviewBoundedByRune` | 预览按 rune 截断（多字节安全）；无正文的助手轮次不留空预览行 |
| `seelexctx.TestCompactionDAGNoSummarizerNoteStaysHonest` | 未说明原因时自答不冒充配置/装配事故；调用方 note 原样进帧 |
| `seelebridge.TestControllerFoldDegradeNoteIsStructural` | 生产入口（控制器 `Handle`）折出的帧：自答是结构性、与开关无关；助手正文进 Chapter 2（小窗口账号让软阈值用小历史即可越过） |

### 4.1 判别力（变异验证，跑完已还原）

| 变异 | 期望红 | 实测 |
|---|---|---|
| `renderUnitLine` 去掉 assistant 的正文预览分支 | 两条 `seelexctx` 用例 | ✅ 红（"本地折叠必须留下助手答复行"/"轮次行缺少 `- 助手: `"） |
| `seelexController` 不注入 `SummarizerNote` | `TestControllerFoldDegradeNoteIsStructural` | ✅ 红（自答变成"…调用方未说明原因"，不含"结构性"） |

---

## 5. 边界（如实记账）

- **索引不是摘要，仍是"有损但可回读"**：每轮只留首段预览（80 rune）。被折轮次的**全文**依赖 `read_compressed_turn / search_history` 回读——本次把这条写进了帧正文，但没有改"帧不携带全文"这个取舍。
- **索引会随折掉单元数增长**：`当前工作` 的预览行数是线性的（原就如此，本次每轮多一行）。栈顶帧的**帧间传递**受 `limits.context_frame_carry_tokens` 约束（超限退化为锚点），但**单帧索引本身**没有 token 上限——这是既有边界，与 R1 同向，值得随后一起设计。
- **开关 `limits.context_compaction_summary.enabled` 对控制器链路仍然无效**（这条链路本就不消费它）。现在帧会如实这么说；配置注释是否需要同步注明"只作用于装配层链路"，属文档项，未动。
- **真实 API 未复跑**：本批全离线（确定性 completer），证明的是链路形状与内容面；真摘要质量、前缀命中率仍需 `-tags compactlive` 覆盖（审查报告 P1-C 仍未闭环）。
