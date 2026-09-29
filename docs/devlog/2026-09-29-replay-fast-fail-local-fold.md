# 重放调用"很快失败"被静默吞掉：local 折叠必须自答"模型为什么没被叫到"

- 日期：2026-09-29
- 症状：`limits.context_compaction_summary.enabled` 已随 commits 打开、**运行中的进程确实读到了
  它**，折叠产出**依然是**本地确定性摘要（`summary_source=local`），帧的「错误与修复 /
  待办 / 下一步」三节恒 `(none)`
- 范围：`seelexctx/dag.go`（chapter2 降级出口留痕）、`seelebridge/runtime_context.go`
  （三个 nil 出口自报原因）、`seelebridge/runtime_compaction_index.go`（回执带原因）、
  `application/core/context_runtime`（帧正文写原因）
- 结论：**这次不是配置漂移**（上一份 devlog 的根因已修好并验证）。现场能证明的是
  「模型调用**没有成功发生**」，而**它为什么没成功，帧里一个字都没有**——
  静默降级把唯一的口供吞了。这就是本轮修的东西。

---

## 1. 现场（两条硬证据）

现场帧：`session-7834e40e49e594bc` 的 `compact.jsonl`，`compressed_at`
`2026-09-29T18:06:19.5027129+08:00`，`frame_id`
`compact-draft_1790672815210067200_1-1790676379502`。

| 事实 | 取值 | 从哪读的 |
|---|---|---|
| 走的是哪条路 | `reason=context_budget`、`origin=explicit_after_turn`，回执含「会话没有在飞回合…会话级显式压缩」 | 帧正文 JSON + 回执 |
| 摘要来源 | `summary_source=local` | 帧正文 JSON（`tr-563e5c0dc11bfd79118125bf`） |
| 判据量 vs 装配量 | `compared_tokens=103211` > `all_context_tokens=91298` | 帧正文的 `layout` |
| 逐关门禁 | `reached=7/7 judge20ms assemble74ms replace<1ms **index458ms** frame<1ms store<1ms record<1ms` | 该轮压缩回执（会话消息里的「门禁 …」一行） |
| 整轮墙钟 | 帧 `at=18:06:19.0688` → 记录 `compressed_at=18:06:19.5027` ≈ **0.43s** | 帧正文 + 记录 |

两条硬证据各自排掉一半可能性：

1. **`compared_tokens = 103211` 说明重放素材非空**。`prepareExecutionContextFor` 里
   `rawTokens` 取的是「system + 全量累积 context + 当前输入 + tools」与
   「system + **引擎历史** + 当前输入 + tools」的**较大者**；后者赢出 1.2 万 token，
   即 `ReplayHistory`（`existing = c.foldHistory(sessionID)`）约 10 万 token →
   `len(state.input.History) > 0` 成立，`no-replay-material` 这条出口可以排除。
2. **`index 458ms` 说明模型调用没有成功发生**。`index` 门禁包住 `pushCompactionFrame`
   → `seelebridge.PushCompactionFrame` → `MainCompactionDAG` 整条 DAG（含 chapter2 的重放
   调用）。实测（下面第 2 节）一次**成功**的前缀重放调用要 **3.5~14 秒**；458ms 里
   塞不下哪怕一次成功调用。于是剩下的可能只有：调用**很快失败**（两次尝试合计 ~0.43s），
   或者摘要器压根是 nil（开关在进程里为假、或 QuickChat 装配失败）。

## 2. 排除法（每一条都带证据）

| 假设 | 判定 | 证据 |
|---|---|---|
| 配置漂移（进程读的是包内旧档） | **排除** | 包内 `dist/seelex-gui-dev/config/seelex.yaml` 与仓库 `config/seelex.yaml` **逐字节相同**（SHA256 `2479C12D…`，17232 字节），mtime `18:03:53`；GUI 进程 PID 8464 `18:05:50` 启动、CWD=包目录（`main.go:525` 按 CWD 相对路径读配置）→ 读到的就是 `enabled: true` |
| 开关没生效（`LoadLimits` 后 `Enabled=false`） | **排除** | `TestShippedCompactionSummarySwitchShipsOpen` 读同一份出厂文件断言 `Enabled`；同一份 limits 在探针里读出 `enabled=true`（`seelexctx.LoadLimits` 同一入口） |
| QuickChat 装配失败 | **排除（同一配置下）** | 探针用仓库配置 + 真账号装配出 `*seelexctx.quickChatPrefixReplaySummarizer`（provider=openai，3 个账号） |
| 重放**请求形态**被 provider 拒（system 重复/历史原样/超大/缺工具） | **排除** | 探针 P1/P2/P3 全部成功：小历史单 system 14.2s；小历史 **system 重复（生产形态）** 3.6s；**10 万 token 历史 + 1 万 token system + 重复 system** 9.9s |
| 节点 ctx 被超时掐死（`workplan` 给每个节点套 deadline） | **排除** | `workplan/runtime/runner` 全文无 `Timeout/Deadline`（唯一一处在测试里）；且 ctx 若死，`chapter2Node` 会 `return ctx.Err()` → 推帧失败 → 帧里根本不会有 `segment_id`，而现场帧有 |
| 帧里的降级原因 | **无迹可查** ← 本轮的缺陷 | `readingNotes` 只写「折叠摘要开关未开启，或前缀重放失败已回退本地折叠」——一个 or 句把两种截然不同的处置（配置 vs 故障）糊在一起 |

**探针**（本次新增，`seelebridge/replay_live_shape_probe_test.go`，`-tags replayprobe` +
`SEELEX_REPLAY_PROBE=1` 双门禁，默认跳过）：

```powershell
$env:SEELEX_REPLAY_PROBE='1'; go test -tags replayprobe ./seelebridge/ -run TestReplayProbeShape -v -count=1 -timeout 12m
#   limits: enabled=true …
#   摘要器就位：*seelexctx.quickChatPrefixReplaySummarizer   provider="openai" accounts=3
#   P1-小历史-单system:              成功（14.216s）
#   P2-小历史-system重复(生产形态):   成功（3.571s）
#   P3-十万token-重复system-无工具:   成功（9.872s）
```

**剩下的唯一解释**：chapter2 的两次重放尝试**很快失败**（或摘要器在该进程里为 nil），
失败原因被 `chapter2Node` 丢掉 → 静默落本地折叠。**为什么快**：能在一两百毫秒内返回的
失败只有"本地就拒"这一类（账号租约/限额拒绝、请求构造失败）或 provider 网关的快速 4xx；
一次成功的调用要 3.5 秒起。

## 3. 修复：降级必须自答

四条出口各自留痕，风格沿用既有的 `replay-chunked:<n>` 帧证据：

| code | 出处 | note 里写了什么 |
|---|---|---|
| `fold-local:no-summarizer` | `chapter2Node`（`Summarizer == nil`） | Runtime 给的三种 nil 出口自述：开关关闭 / QuickChat 装配失败（带 error） / 摘要器构造失败（带 error） |
| `fold-local:no-replay-material` | `chapter2Node`（`len(History) == 0`） | 无重放素材（引擎历史为空） |
| `fold-local:chunk-replay-failed` | 分片链失败后落回单次重放 | 片数 + 真实报错（若单次重放随后成功，会被 `clearDegrade` 清掉） |
| `fold-local:replay-failed` | 两次尝试均失败（含"返回空摘要"） | **真实报错** |

流转：`CompactionDAG` 写进栈帧 `Evidence` → `seelebridge.PushCompactionFrame` 读出成
回执的 `SummaryNote` → `context_runtime` 写进帧正文（元数据块 `summary_note` +
「折叠材料」那一段的原因句）。渲染侧不新立字段：`summary_source` 说"是本地折叠"，
`summary_note` 说"为什么是本地折叠"。

口径三条：

- **只记降级**：重放成功会清掉记录——note 有值就是"这次真的降级了"；
- **不编原因**：没有 note（更早版本写的帧、推帧失败）保留原来的兜底措辞，
  不把"没有解释"说成"没有降级"；
- **一个实现**：`compactionSummarizerWithNote` 是三个 nil 出口的唯一实现，
  `compactionSummarizer` 只是丢掉 note 的那一层（避免两处各写一份出口判断）。

## 4. 验证

```powershell
go build ./...                                   # 通过
go test ./application/core/context_runtime/ -run CompactionFrame -count=1   # 通过（含新用例）
go test ./seelebridge/ -run "CompactionSummarySwitch|ShippedCompaction" -count=1  # 三条臂通过
node --test gui/frontend/dist/*.test.mjs         # 519 通过 0 失败
```

- `TestCompactionSummarySwitchOpenReplayFailureWritesReason`（新）：开臂 + 稳定失败的
  completer → `summary_source=local`、回执 note 含「前缀重放两次调用均失败」**与真实报错**、
  栈帧留 `fold-local:replay-failed` 且证据正文带该报错。
- `TestCompactionFrameBodyWritesWhyNoModelSummary`（新）：帧正文元数据块 `summary_note`
  与「折叠材料」一段都写出原因；无原因时保留兜底措辞且不丢「不是对被折原文的总结」的口径。
- 关臂（既有用例）加断言：note 说明"开关关闭" + 留 `fold-local:no-summarizer`。

## 5. 遗留（如实记账）

- **本机 GUI 仍需重启才能看到新写入的 note**：配置与代码都在启动时进内存。
  重启后在 状态 → 上下文压缩 里展开那一帧（或点弹框），看 `summary_note`——
  它会直接写出是"开关关闭""无重放素材"还是"重放失败 + 具体报错"，届时按报错类型
  再修（本轮的证据只能证明"没有成功调用"，不能替下一次折叠说出报错原文）。
- **重放失败没有退避/重试**：现有口径是"连续两次尝试"，对"账号限额/租约拒绝"这类
  瞬时拒绝等于白试。是否给一次短退避、或改成"降级并如实记账，由用户决定要不要重压"，
  等拿到真实报错再定——不先猜一个机制。
- **探针只覆盖无工具面的形态**：裸 `Runtime` 的 `VisibleTools` 为空（P4 自动跳过），
  带完整工具面（约 40+ 工具）的生产形态尚未在探针里跑过；如果下次报错指向"请求过大"
  一类，先把工具面补进探针。
