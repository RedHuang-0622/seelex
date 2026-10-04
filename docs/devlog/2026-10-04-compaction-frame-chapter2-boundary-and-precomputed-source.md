# 压缩帧 Chapter 2 边界错位：读数闸回执交的是整份摘要、预读来源又被盖成 replay

- 日期：2026-10-04
- 范围：`seelexctx`（新增 `Chapter2Body`）、`seelebridge/runtime_compaction_index.go`、
  `application/core/context_runtime/{ports,compaction_index,coordinator}.go`、`internal/adapters`、
  新增两条复现用例与一条归一化件用例。
- 现场（用户原话）：**「压缩成功了，帧却是一具空骨架」**——帧里有 `frame_id`、
  `summary_source` 还写着 `replay`，正文的八个小节却全是 `(none)`，而且 `Chapter 1`
  的标题在同一份摘要里出现了两次。
- 结论：这不是"压缩失败被折"，是**一条字段边界错位** + **一次来源谎报**：
  装配层读数闸回执里的 `Summary` 是**整份两章节摘要**，而它的下游语义是"这次的
  Chapter 2 正文"；整份摘要被当正文包进新帧后，真正的正文再读回来时会被裁成空。
  预读若落在本地确定性压缩上，被当正文塞进来的就正是那具全 `(none)` 骨架，而
  来源被无条件写成 `replay`、失败原因被 `clearDegrade()` 抹掉——于是"压缩成功、
  帧是空骨架、模型为什么没被叫到又查不到"三件事同时成立。

---

## 1. 根因链（逐跳带位置）

| # | 位置 | 事实 |
|---|---|---|
| 1 | `seelebridge/runtime_compaction_index.go` `ReadbackCompactionSummary` | 回执 `Summary: frame.Summary` —— **整份两章节摘要**（Chapter 1 锚点 + Chapter 2 正文）。这里的 `frame` 是 DAG 产物，`Summary` 由 `RenderFrameSummary` 拼好 |
| 2 | `application/core/context_runtime/coordinator.go`（读数闸命中分支） | `precomputedSummary = receipt.Summary` —— 整份摘要被存成"这次的读后感" |
| 3 | `compaction_index.go` `pushCompactionFrame` → `CompactionIndexRequest.PrecomputedSummary` → `internal/adapters` 原样转发 → `seelebridge.CompactionFrameRequest.PrecomputedSummary` | 整份摘要一路当正文传下去，**没有任何一处对齐边界** |
| 4 | `seelexctx/dag.go` `chapter2Node` | `state.chapter2 = normalizeReplayChapter2(precomputed)`；而 `normalizeReplayChapter2` 只剥**领先的** `## 压缩内容 (Compacted Context)`。整份摘要以 `## 上一压缩栈摘要 (Previous Compact Stack)` 开头，**剥不掉** |
| 5 | `seelexctx/dag.go` `mergeNode` | `Summary: RenderFrameSummary(state.chapter1, state.chapter2)` → Chapter 1 锚点与两个标题在帧里**各出现两次** |
| 6 | `seelexctx/frame.go` `frameChapter`（旧实现） | Chapter 2 标题之后**立刻**是另一个 `"## "` 标题 → `strings.Index(body, "\n## ")` 命中下标 0 → 正文被裁成空串 → `FrameChapter2` 退化为"整段摘要"。记忆块、`OneLineSummary`、`CarryPreviousChapter2` 这些读者拿到的都不是正文 |
| 7 | `seelexctx/dag.go` 同一支 | `state.summarySource = CompactSummarySourceReplay` **无条件**写死 + `state.clearDegrade()`。预读回执其实报 `local`（模型调用失败、两次尝试全挂）时，这份事实在帧里被抹成"这次有模型读后感"，报错原文一个字不剩 |

第 7 条就是"帧是空骨架"的直接来源：预读那一次落在本地确定性压缩上时，它的
Chapter 2 就是 `Chapter2Skeleton()`（八小节全 `(none)`），而帧把它当"模型读后感"
收下并盖上 `replay` 的章。

## 2. 复现（先红）

新增 `seelebridge/runtime_compaction_chapter_boundary_repro_test.go`，两条都走生产入口
（`Runtime.ReadbackCompactionSummary` + `Runtime.PushCompactionFrame`，真 Runtime +
绑定该会话的 `SessionContextStore` + 确定性 completer，无网络）：

| 用例 | 改前红灯（实测原文） |
|---|---|
| `TestReproCompactionReadbackSummaryIsChapter2NotWholeFrame` | `读数回执不得把整份两章节摘要当 Chapter 2 交出来：` + 整份摘要（含 `## 上一压缩栈摘要 (Previous Compact Stack)` / `（首帧：无上一压缩栈）` / `## 压缩内容 (Compacted Context)`） |
| `TestReproPrecomputedLocalSummaryMustNotBeRelabelledReplay` | 改前 `receipt.SummarySource = "replay"`（预读回执报的是 `local`）、`SummaryNote` 里没有那条 `connect: connection refused`、栈帧也没有 `compact-local:precomputed-local` 证据 |

第一条的第一个断言（回执不得带 Chapter 1 标题）与第二个断言（帧里每个章节标题恰好
一次）在改前都红；改后两条全绿。中间夹过一次**判别力自查**：`Chapter 2 正文里不得再
有章节标题` 原先写成 `strings.Contains(chapter2, "## ")`，而小节骨架的 `### ` 的第 2–4
个字符正是 `## ` ——那是断言写错、不是实现没修；改成逐行 `strings.HasPrefix(line, "## ")`
后按预期转绿（`### ` 不以 `## ` 开头）。

## 3. 修法

1. **一个归一化件**：`seelexctx.Chapter2Body(text)` —— 取**最后一个** Chapter 2 标题之后
   到下一个 `## ` 标题之前的正文；整段文本里没有 Chapter 2 标题时原样返回（旧记录无章节
   标记的兼容语义不变）。取"最后一个"让已经嵌套过的旧帧**自愈**（剥到最内层那段真正文），
   而不是把嵌套原样带走。`FrameChapter2` 与 `normalizeReplayChapter2` 现在都走它——两个
   实现必然漂移，而这里漂移的后果是"读回来被裁成空"这种只有现场才看得见的事。
2. **预读正文先归一化，且不许冒充**（`dag.go` `chapter2Node`）：`Chapter2Body(precomputed)`
   非空才用；归一化后为空（例如传进来的整份摘要，它自己的 Chapter 2 就是那具骨架）→
   **落回常规路径**，由本地压缩 + 真实降级原因给出可读的交代。
3. **来源与原因一起带下去**：`CompactionInput` / `CompactionFrameRequest` /
   `CompactionIndexRequest` 增 `PrecomputedSummarySource` / `PrecomputedSummaryNote`；
   `source` 为空时按 `replay` 处理（旧调用方语义不变），`note` 非空时落在
   `compact-local:precomputed-local` 这条证据上（新码，与 `replay-failed` 分开记：那一次
   重放的成败已由读数闸定性，这条要交代的是"预读结果被如实沿用、没有被盖成 replay"）。
4. **出口一侧对齐**：`seelebridge.ReadbackCompactionSummary` 的 `Summary` 改交
   `seelexctx.FrameChapter2(frame)`（Chapter 2 正文）；整份摘要仍留在**推帧**回执里
   （前端/门禁读的是它）。字段在两个消费者处语义不同这件事，两边注释都写明了。

## 4. 回归证据

```text
go build ./...                         → exit 0
gofmt -l <改动文件>                     → 干净
go vet ./seelexctx/... ./seelebridge/... ./application/core/... ./internal/adapters/...  → 干净
go test ./seelebridge/ -run 'TestReproCompactionReadback|TestReproPrecomputedLocal|TestCompactionSummarySwitch' -count=1 -v
                                       → 5/5 PASS（2 条新复现 + 开关两臂 + 出厂档）
go test ./seelexctx/... ./seelebridge/ ./application/core/... ./internal/adapters/... ./sessionstore/... -count=1
                                       → 全绿（含 seelexctx 2.6s / seelebridge 37.9s / application/core 38.4s / sessionstore 60.9s）
```

一条既有用例因签名变化同步更新：`application/core/context_runtime/compaction_push_lock_test.go`
的 `startPush` 多加两个空参（来源/原因），语义不变。

## 5. 边界与未做（如实）

1. **运行中的 GUI 是旧构建**：`embed.FS` 在编译期打包，这一修要重编二进制才在现场生效。
2. **真 API 未跑**：`-tags compactlive` 那几条自 2026-09-23 起无复跑记录，本轮同样不烧钱。
3. **"这一帧没落盘"另案**：现场那个会话目录里没有 `compact.jsonl` / `metadata/compaction.json`
   （帧只活在内存 + 注入上下文里）。那是**帧通道落盘**这一条链的独立问题，与本文的
   Chapter 2 边界不是同一处，未在本文处理。
4. **帧 id 的会话前缀与承载对话的会话不一致**（`compact-…-2-…` vs `session_id …-1`）也未在
   本文处理，同属会话记账那一线。
5. **提交**：按仓库规范**不自动提交**，改动留在工作区等用户点头。
