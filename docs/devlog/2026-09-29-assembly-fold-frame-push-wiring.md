# 装配层折叠推帧接线（P1-A）：能力面齐全但没有调用方

- 日期：2026-09-29
- 起因：`docs/2026-09-29-context-compaction-fold-review.md`（同一批审查报告）的 P1-A
- 结论：**接线已落地**；`context_compaction_summary` 三个键随之由"打开也不生效"变为生效（见 §4）
- 未完成：真实 API 复跑（审查报告 P1-C；需要凭据，见 §6）

---

## 1. 现场：三层都在，唯独没有调用方

`190049e`（"装配层折叠推帧进会话压缩栈，检索改按 EventSeq 反查区间"）的提交标题
声称接通了这一条，实现也确实留下了三层：

| 层 | 位置 |
|---|---|
| 能力面 | `application/core/context_runtime/ports.go` 的 `CompactionIndexPort` |
| 落点 | `seelebridge/runtime_compaction_index.go` 的 `PushCompactionFrame` |
| 适配器 | `internal/adapters/compaction_index_port.go` |

但 `git grep PushCompactionFrame HEAD` 只有这三处命中：**没有调用方**。
`context_runtime.Deps` 里没有注入口，协调器内也没有类型断言探测。

后果不是"少一个功能"，而是三处下游同时失效——压缩栈
（`SessionContextRecord.CompactStack`）此前只有两个生产写入方（回合内控制器
`seelexctx/controller.go`、真空区覆盖 `seelexctx/gap.go`），而**最常发生**的那条折叠
（装配层：软线 / `/compact` / `compact_context`）从不推栈：

1. 记忆块初筛读栈（`seelebridge/runtime_context.go` 的 `relatedMemoryBlocks`）：栈空 → 不注入；
2. `search_history` 帧索引读栈：栈空 → 退化为尾部扫描；
3. 真空区覆盖（`coverHistoryGap`）：栈空 → **主动跳过**补压。

于是"只会发生装配层折叠的会话"这三条路径全部不工作，而这恰好是最普通的会话形态。

## 2. 根因：这是"可选窄能力"，而可选能力没有注入口就等于没有

能力面本身的设计是对的（不进 `contract.RuntimePort`：加进大接口会迫使每个
fake/harness 实现它，而断言失败的正确行为本来就是"不索引"）。缺的是**探测**那一步：
`application/core/service_assembler.go` 现在在装配期做一次类型断言——

```go
var compactionIndex context_runtime.CompactionIndexPort
if provider, ok := assembler.deps.Runtime.(context_runtime.CompactionIndexPort); ok {
	compactionIndex = provider
}
```

探测失败 = `Deps.CompactionIndex == nil` = 不索引，折叠与请求照常。

## 3. 改动

### 3.1 调用点（`context_runtime/compaction_index.go`）

在协调器的折叠落点上调一次 `pushCompactionFrame`，位置在**渲染帧正文之前**——
正文要嵌入回执里的 `segment_id` 与摘要来源，否则模型根本拿不到
`read_compressed_turn` 的入参（同一份事实两处表达就会漂移，所以正文一向只嵌回执）。

素材全部取折叠那一刻手里已有的事实，**不重算**：

- **溢出区** `transcript[retainedFrom:compressedTo]`。`retainedFrom` 之前的区间已被更早
  的帧覆盖（帧链自足），重复喂进去只会让检索命中两段同内容；自主压缩时
  `compressedTo = len(transcript)`，即"尚未被任何帧覆盖的全部"。
- **重放素材** `existing`——上一次真实请求的历史字节。它与产出该请求是同一条装配
  路径；从事件流重拼会丢掉字节一致性，前缀重放也就白付费（`seelexctx/replay.go`
  的字节级一致性契约）。
- **区间** `TranscriptPrefixRange` 的记录值。`EventSeq` 是装配层手里的权威事实，
  单元下标由接收侧按 Seq 反查（两者不是减法关系：`CompleteEventUnits` 会跳过孤儿
  tool 与未知角色）。

### 3.2 事实面

- 门禁新增 `index` 关：权威顺序成七关
  `judge → assemble → replace → index → frame → store → record`；前端
  `compaction-format.js` 的 `compactionGateLabels` 同序（两份字面量由
  `TestFrontendGateLabelsMatchBackendOrder` 钉住，少一格/错序都是画给用户看的假事实）。
- 帧正文元数据带 `segment_id` / `summary_source`，`readback` 段据此给出
  `read_compressed_turn` 的入参；读后感**原样嵌入**栈帧 `Summary`，不另写一份措辞。

### 3.3 降级记账：两种"没有 segment_id"必须分开

降级方向天然安全（索引面未装配或推帧失败都不影响折叠与本次请求），但"没尝试"与
"试了失败"是两种事实：

| 情形 | 门禁 Detail |
|---|---|
| 索引面未装配 | `index=unavailable` |
| 索引面在，但这次没有可推区间（溢出为空） | `index=skipped reason=no_overflow` |
| 试了且失败 | `index=error err=<真实原因>` |
| 推成 | `segment=compact-idx-1 source=replay` |

帧正文的 `readback.Note` 同理如实写出来由，不留空段、也不拿"没报错"冒充"推上了"。

## 4. 顺带解掉的事：`context_compaction_summary` 不再"打开也不生效"

审查报告 P1-B 记的是：`limits.context_compaction_summary.{enabled,input_tokens,chapter2_tokens}`
三个键当前完全不生效，因为唯一注入 `Summarizer` 的构造 `MainCompactionDAG` 只被"那个
没有调用方的入口"使用。推帧接线之后，`MainCompactionDAG` 有了真实调用方
（`seelebridge/runtime_compaction_index.go`），这条链变成：

    装配层折叠 → pushCompactionFrame → MainCompactionDAG（注入 PrefixReplaySummarizer）
      → 栈帧 Summary（summary_source=replay|local）→ 帧正文原样嵌入

开关语义因此与现状一致：**关就是关**（`summary_source=local`，一次模型调用都不发），
**开就生效**（每次折叠一次无人值守的付费调用）——与 `async_exec` 同一套纪律。

## 5. 测试（离线，白盒）

`application/core/context_compact_index_test.go` 三条，共同的第一判别力是
"**请求真的发出去了**"——此前这个缺口没有任何测试能抓到（`git grep
PushCompactionFrame` 在测试里也是零命中）：

1. `TestFoldPushesCompactionFrameIntoIndex`：请求恰好一次、归属到正在折叠的会话、
   区间与压缩记录逐一相等（重算就会漂移）、回执落到帧正文与门禁上；
2. `TestFoldWithoutIndexFaceReportsDegradedGate`：索引面未装配时折叠照常，门禁
   `index=unavailable`，帧正文说明原因且**不得**说成"推帧失败"；
3. `TestFoldPushFailureIsReportedNotFatal`：推帧报错时折叠与请求照常，门禁
   `index=error`、帧正文带真实原因。

`context_compact_progress_test.go` 的逐关用例随七关改判（含前端文案表键序）。

## 6. 门禁与遗留

本次实跑（本地，不消耗 API 配额）：

```powershell
go build ./...                                        # 干净
go vet ./application/core/... ./seelebridge/... ./seelexctx/... ./internal/adapters/...
go test ./application/core/... ./seelexctx/... ./seelebridge/... ./internal/adapters/... -count=1
gofmt -l application seelebridge seelexctx internal gui   # 只报既有的 history_safety.go
```

**遗留（未完成，如实记账）**：

- 真实 API 复跑（审查报告 P1-C）：`smoke_live.log` 可查的最后一次运行是 2026-09-23
  **FAIL**（"真实回合未回到 idle"）。推帧接线改变了装配层折叠的副作用面（多一次
  DAG 执行 + 原文归档 + `PushCompact`），因此这次改动**必须**跟着一次
  `-tags compactlive` 三件套复跑才算闭环；本机无凭据，未跑。
- `context_compaction_summary.enabled=true` 的臂仍无 live 用例，且 DAG 单测里也没有
  构造 `Enabled:true` 的断言（fake Summarizer 已有）。
