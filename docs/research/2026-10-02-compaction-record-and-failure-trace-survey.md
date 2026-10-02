# 上下文压缩 / 压缩失败的「记录与留痕」现状调研

- 日期：2026-10-02
- 性质：**只读调研**（不改代码、不提交）。除一处临时探针（跑完即删、工作区已复原）外，全部证据为代码/测试/文档读取 + 定向用例实跑。
- 口径起点：用户提问「关于上下文压缩和压缩失败的**日志记录**（旧名字是叫做**折叠**）的情况调研清楚」。
- 结论分级：**Confirmed** = 有代码/实跑证据；**Hypothesis** = 有代码依据但未实跑；**文档口径** = 仅文档陈述，未经代码核对。

---

## 0. 一句话结论

| # | 事实 | 级别 |
|---|---|---|
| 1 | 压缩只有**一条**写记录落点：装配层 `prepareExecutionContextFor` → `RecordContextCompactionLocked`（`/compact`、`compact_context`、自动阈值三条入口共用它）。 | Confirmed |
| 2 | 「压缩失败」**没有**独立的持久记录：失败/跳过只活在**瞬态门禁进度**（成功 2.5s、失败 6.0s 后自动撤条）、**显式入口的回执文本**、以及（真折出帧时的）**帧正文自答段**里。 | Confirmed |
| 3 | 全仓**没有**把压缩事实写进任何应用日志或事件日志：`framework-events.json`（执行事实事件库）只装执行事实，压缩进度按 `revision=0` 发布（不进快照）。 | Confirmed |
| 4 | ⚠️ **压缩记录在当前生产存储布局（JSON v8/S20）里写不进也读不回**：`record.Execution.Task.ContextCompactions` 所在的 record 通道已退役。而冷恢复仍从它读 → **进程重启后压缩记录（右栏「上下文压缩」+ 保留窗口起点）会空**。仓内的重启回归用例用测试替身，故 CI 恒绿。 | Confirmed（真布局实测，见 §4.3） |
| 5 | 命名不是「改名」，是**分工定名**：「压缩（compaction）= 流程/记录面」，「折叠（fold/folding）= 流程里的那一步」。用户可见文案一律写「压缩」+「已折叠」并存。 | Confirmed |

---

## 1. 术语现状：谁叫「压缩」、谁还叫「折叠」

- **产品/记录面 = 压缩**：`ContextCompaction`（`application/model/state.go:47`）、`context_compactions` 快照字段（`state.go:43`）、`/compact`、`compact_context`、`CompactFrame`、`read_compressed_turn`、英文口径 `folding is one step of context compaction`（`CHANGELOG.md:207`，文档口径）。
- **动作面 = 折叠**：`fold_history.go`（`replaceFoldHistory`/`setFoldSystemPrompt`）、`ineffectiveFold`、`hasFoldableSessionContext`、`commitFold`、`compactionFoldedRange`、`localFoldReason`、`## 折叠材料 (Folded Material)`、`CompactFoldOverflow`/`CompactFoldGap`、证据 ref 前缀 `fold-local:`（`seelexctx/replay.go:253`）。
- **用户可见文案混用（现状如此，不是漏改）**：
  - 后端：「压缩当前上下文（折叠为有界 checkpoint，原始轮次仍可回读）」（`application/core/command.go:71`）、「已压缩上下文：v3（…）」（`context_compact.go:117`）。
  - 前端：「上下文压缩」（面板标题，`context-summary.js:30`）、「以上 消息 …..… 已被折叠」（分界虚线段，`compaction-format.js:159-162`）、「已折叠并写入压缩记录」/「已折叠，本轮纪元未到期不写记录」/「没有模型读后感：不折叠，上下文原样继续」（`compaction-format.js:244-251`）。
- 历史脉络（文档口径，日期→出处）：
  - 2026-08-30 起「压缩」已是产品词（`docs/devlog/2026-08-30-context-prefix-chain.md`）。
  - 2026-09-14 起「折叠」用于**动作**（`docs/devlog/2026-09-14-proactive-context-compaction.md:23`）。
  - 2026-09-29 审查报告把两条链路分名：压缩链路 = 回合内控制器、折叠链路 = 装配层（`docs/2026-09-29-context-compaction-fold-review.md:29,41`）。
  - 2026-09-30 **定名并合并为一条链路**：回合内不再折对话，只剩装配层唯一一条阈值线（`docs/devlog/2026-09-30-loop-no-fold-single-threshold.md:11`；代码侧 `seelexctx/controller.go` 的 `Handle` 只做超大工具结果兜底归档）。
  - 全库无「折叠→压缩改名」的决策记录（子代理检索结论，与代码分布一致）。

---

## 2. 记录（record）本体：字段与写点

### 2.1 字段（`application/model/state.go:47-88`）

`ContextCompaction{ Version, Reason, Origin, MessagesBefore, EstimatedTokens, CompactedAt, MessageFrom/To, EventFrom/To, SegmentID, FrameRef, FrameBytes, FrameTokens }`

- 头注释写死：**不含 prompt 正文、checkpoint 正文、工具参数/结果、对话内容**；帧正文只在内容存储里，记录只带 `FrameRef`。
- `Origin ∈ {auto, explicit, explicit_after_turn}`（`state.go:92-104`；空串 = 旧记录按 auto 读）。
- 区间字段是**记录值**，不事后推算；`MessagesBefore` 只是装配前引擎历史条数（冷加载合法为 0），**禁止**拿它当「压了多少条」（`model.CompactionRangeLabel` 的 doc 明写）。

### 2.2 写点（唯一一条链）

```
context_runtime/coordinator.go  prepareExecutionContextFor
  A 段（锁内）: state.ContextVersion 推进 → 构造 record{Version,Reason,Origin,MessagesBefore,
                EstimatedTokens,CompactedAt,MessageFrom/To,EventFrom/To}   (coordinator.go:875-903)
  B 段（锁外）: pushCompactionFrame(...) → 会话压缩栈帧落盘；compactionFrameBody(...) 渲染帧正文
  C 段（锁内）: frame → StoreToolResultForLocked(sessionID, "context_compaction_frame", frame)
                → record.FrameRef/Bytes/Tokens 回填                            (coordinator.go:991-1000)
                → recorded = RecordContextCompactionLocked(requestID, record) (coordinator.go:1000)
                → 成功则 view.BumpLocked() 并发 snapshot.changed               (coordinator.go:1001-1015)
```

### 2.3 写不写记录的门槛（`application/core/task_context/task_context_state.go:1015-1046`）

1. `state == nil` 或 `state.RequestID != requestID` → **false**（回合已换人）；
2. `state.Status != Running` 且**非**显式来源 → **false**（自动路径回合收尾后不补记）；
3. 其余 → append 到 `TaskExecutionState.ContextCompactions` 并镜像进 `Snapshot.Task`。

上游还有一层「要不要构造记录」（`coordinator.go:870-903`，`commitFold = newCheckpoint || autonomous`），以及**空区间不折**（空会话只登记：`coordinator.go:331-346`、`context_compact.go:260-268`）——即「不把『没做事』记成『做了事』」。

---

## 3. 「没有落记录」的四种终局，以及它们各自留在哪

同一轮折叠走到 `fold == true` 但没落记录时，**只把事实写进瞬态进度**（`coordinator.go:887-914`）：

| 终局（CompactOutcome / Detail） | 触发 | 是否折了上下文 | 留痕位置 |
|---|---|---|---|
| `compacted` | 折+落记录 | 是 | 压缩记录 + 帧 + 帧正文 |
| `folded_without_record` + `skipped=epoch_throttled compacted_epoch=.. progress_epoch=.. context_version=..` | 本 progress 纪元已压过 | 是 | **仅瞬态进度** |
| `folded_without_record` + `skipped=ineffective_fold landing=.. soft=.. overhead=.. retained=.. all=..` | 折叠换不来余量（幂等/有效性校验） | 是 | **仅瞬态进度** |
| `skipped_no_summary` + `skipped=no_summary reason=no_model_summarizer` 或 `reason=no_model_readback source="…" note="…"` | 没有模型读后感（结构判断 / **运行时实测读数**） | **否**（不折、不推栈顶、不推进版本） | **仅瞬态进度** + 显式入口回执文本 |
| `below_threshold` | 判据未命中 | 否 | 显式入口回执文本 |

「读过再折」的读数闸（`coordinator.go:683-712`）是 2026-10-02 落地的：把「这次到底有没有模型读后感」从事后（推帧阶段）提前到折叠**之前**实测；读不到就 `noSummary=true` 并写 `readback=no_model_summary source=%q note=%q`（**真实报错**）。

---

## 4. 持久化面（关键）

### 4.1 代码「意图」中的持久化链

```
TaskExecutionState.ContextCompactions
  → Snapshot.Task.ContextCompactions                （可见面镜像）
  → record.Execution.Task = *copy                   （archive.go:244-250，按会话取）
  → LoadSessionRecordWorkspace → LoadRecordRaw      （冷恢复读回）
  → RestoredTaskState.ContextCompactions            （session_history.go:412-440）
```

配套：跨回合内存继承无条件成立（`task_execution.go:100-127`，2026-09-28 `d09c190` 修）；fork 继承 + FrameRef 可达性（`session_runtime/fork.go:263-295, 457-470`）；保留窗口起点由区间事实推（`RetainedFromForCompactions`）。

### 4.2 ⚠️ 但这条链在当前布局下**不落盘**

- `sessionstore/session_granular.go:196-215`（`SaveRecordRaw`，v8 分支）：**只把 `status`/`title` 写穿**到目录面，payload 本体丢弃。
- `sessionstore/json_layout.go:96-101`（`writeCommitLayout`）：`D9/S20：state.json 通道停写`。
- `sessionstore/json_layout.go:251-282`（`derivedRecordPayload`）：派生 record 只有 `version/id/status/updated_at/conversation` —— **没有 `execution`**。
- `sessionstore/session_granular.go:266-288`（`LoadRecordRaw`，v8 分支）：交回上面那份派生记录。
- 权威口径（仓内自陈，非本文推断）：
  - `application/core/session_permission_tier.go:5-14`：「这是**实测结论**，不是设计偏好：现行存储布局（v8/S20）下 record 通道已退役……**往 SessionRecord 加字段既写不进去也读不回来**。」
  - `sessionstore/README.md:205`：「`LoadRecordRaw` 交回按 message head/行派生的最小 record，新增字段写不进也读不回。」
- 而冷恢复**只**从 `record.Execution.Task` 取压缩记录（`application/core/session_history.go:419-440`、`:466-472`；`task_context/task_context_state.go:900-935`）。

### 4.3 真布局实测（临时探针，跑完已删）

在 `sessionstore` 包内用真 `Router`（JSON v8）+ `SessionGranularStore` 写一份带 `execution.task.context_compactions` 的 record，再读回：

```
写进去: {"version":3,"id":"s-survey","status":"idle","execution":{"task":{"context_compactions":[{"version":1,"reason":"context_budget","event_from":1,"event_to":9}]}}}
读回来: {"version":3,"id":"s-survey","status":"idle","updated_at":"2026-10-02T14:50:21.7001089Z","conversation":{"updated_at":"…"}}
→ 结论：record 通道丢掉了 execution.task.context_compactions
```

**预测后果（Hypothesis，需一次真机「开应用→压缩→重启→看右栏」复核）**：

1. 重启后右栏「上下文压缩」为空（可见面唯一数据源是 `snapshot.task.context_compactions`）；
2. `ContextRetainedFrom` 归零 → 下一次装配把**已被折出的前缀**重新计入 → 长会话稳定越线（2026-09-28 修过的「一发消息一条压缩记录」症状可能从这条路上复现，但这次是**跨进程**而不是跨回合）；
3. 压缩栈帧与帧正文**仍然活着**：它们走独立通道（`compact.jsonl` + 内容存储），有跨重启用例（`seelebridge/cold_restore_compaction_context_probe_test.go:233`「R3 帧链必须跨重启存活」）。也就是说，重启后**「帧在、记录不在」**：模型侧回读入口（`read_compressed_turn`）不受影响，受影响的只有前端记录列表/分界/保留窗口起点。

**为什么 CI 没拦住**：`application/core/context_compactions_restore_repro_test.go`（2026-10-01 冷恢复回归）用的是测试替身 `archiveSessions`，文件头自述「与生产 sessionstore 的 record 通道**同形**」——在 v8 下这句话对**形状**成立、对**内容留存**不成立；替身自己把 record 存进内存/自己的 `record` 通道，所以用例恒绿。

> 另有一处**同族**的文档漂移：`application/core/README-context.md:102-105` 的「已知边界①」仍写「压缩记录在下一条消息的回合收尾持久化后可能从 `record.Execution.Task.ContextCompactions` 消失（回合状态按 `IsContinuableStatus` 重建，不继承记录）」——这条已在 2026-09-28 修掉（`continuationTaskExecutionState` 无条件继承会话事实），该段落是 2026-09-24 的旧口径。

---

## 5. 「压缩失败」的留痕现状（逐面）

| 面 | 内容 | 生命周期 | 证据 |
|---|---|---|---|
| 瞬态门禁进度 | 7 关门禁（judge→assemble→replace→index→frame→store→record）+ 终局 `Outcome` + `Detail`（含 `skipped=…` 与真实报错） | **不进快照**（`PublishSession(..., 0, ...)`）；前端成功终局保留 **2500ms**、失败终局 **6000ms** 后撤条 | `context_runtime/compaction_progress.go:17-56, 248-300`；`gui/frontend/dist/app.js:1165-1179`；`protocol.js:162-168` |
| 显式入口回执 | `/compact` 命令 notice、`compact_context` 工具 JSON（`Note` 逐字解释为什么没折/没记录） | `/compact` 走 `AddNotice` → 可见会话里的 system 行（`service_snapshot.go:310`；**是否随之落盘未验证**，见 §8）；工具结果走工具结果事件 | `context_compact.go:230-285`；`command.go:71-91` |
| 帧内自答 | 帧正文 JSON 元数据的 `summary_source`（`replay`/`local`）+ `summary_note`（「开关关闭 / QuickChat 装配失败 / 摘要器构造失败 / 重放失败的**真实报错**」），Markdown 侧的「## 折叠材料 (Folded Material)」段 | **持久**（帧正文进内容存储，按 `frame_ref` 回读） | `context_runtime/compaction_frame.go:105-215, 221-260`；`seelebridge/runtime_context.go:236-260`；`seelexctx/replay.go:248-262`（证据 ref `fold-local:<code>`） |
| 推帧失败 | 帧正文 `readback.note` 写明「本次没有压缩栈帧（推帧失败：<原因>）」；门禁 `index` 关 Detail 记回执 | 持久（在那一帧里）/瞬态（门禁） | `compaction_frame.go:151-172`；`runtime_compaction_index.go:130-145` |
| 应用日志 / 事件日志 | **没有**。全仓 `log.SetOutput` 只出现在一个测试里；`seelebridge/runtime_context.go:245` 注释直说「windowsgui 构建下 `log.Printf` 也无处可看」；`framework-events.json` 是执行事实事件库（`sessionstore/README.md:100-125`、`docs/devlog/2026-09-29-event-log-append-only.md`），压缩不进 | — | Confirmed |

**一句话**：压缩失败**不是「没有留痕」，而是「留痕不落盘」**——真折出帧时靠帧正文持久自答；**没折成**（读数闸拦下）时，只有 6 秒的进度条和（显式入口）一条回执。

---

## 6. 可见面（前端 / TUI）

- 右栏「上下文压缩」= 压缩栈表格（`gui/frontend/dist/context-summary.js:24-72`）：列 `栈 | 折叠 | 帧`；栈顶（`message_to` 序号最大者）标「栈顶」，更早的 `is-stale`；有 `frame_ref` 才能展开/弹框读帧正文；没帧的显式写「无帧正文」。
- 对话区：会话**单例**分界线「以上 … 已被折叠」（`compaction-format.js:186`，`compactionFrontier`）。
- 轨迹：底部「压缩」轨刻度 + 详情按 `frame_ref` 分页回读（`trajectory.js:485+`）。
- 数据来源只有一个：`snapshot.task.context_compactions`（`app.js:216/680/807/1204/1410`）。
- **TUI 没有压缩面**：`git grep 压缩 -- tui` 零命中，TUI 也不读 `Snapshot.Task`（Confirmed）。

---

## 7. 本会话实跑的证据（定向用例）

```
go test ./application/core/ -run "TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance|
  TestReproContextFactsSurviveCompletedTurnBoundary|
  TestFoldWithFailedModelReadbackLeavesContextUntouched|
  TestFoldWithModelReadbackStillFolds|
  TestFoldWithoutModelSummaryLeavesContextAndStackUntouched" -count=1 -v
→ 5/5 PASS，ok github.com/RedHuang-0622/seelex/application/core 0.653s
```

即：**跨回合不丢**（内存态）、**失败读数不折上下文**（不折/不推栈/不落记录）、**有读后感照折** 三条当前行为都成立；**跨进程不丢**这一条没有被真实布局覆盖（§4.3）。

---

## 8. 未验证 / 待确认清单

1. **真机重启复核**：开 GUI → `/compact` → 退出 → 重开 → 看右栏「上下文压缩」是否为空、`/compact` 是否立刻又折一次。这是 §4.3 预测的判定性实验。
2. `/compact` 的 notice（system 行）是否随会话落盘：`AddNotice` 只写 SessionView（`view_state/coordinator.go:665`），而落盘的 `record.Conversation` 由 transcript 重建（`archive.go:232`）——**推断不落盘**，未实测。
3. `read_compressed_turn` 在「记录丢、栈在」状态下的实际体验（重启后模型是否仍能按 `segment_id` 回读）——栈通道有跨重启用例，但**端到端**未跑。真 API 冒烟（`-tags compactlive`）自 2026-09-23 起无复跑记录（`docs/2026-09-29-context-compaction-fold-review.md` P1-C）。
4. 子代理侧（node）折叠：节点栈为内存态，无 live 覆盖（同前引 P1-C 表）。

---

## 9. 顺手记下的文档/命名漂移（不改代码即可处置）

| 位置 | 现状 | 事实 |
|---|---|---|
| `application/core/README-context.md:102-105` | 「压缩记录可能在下一回合持久化后消失」 | 2026-09-28 已修（无条件继承）；且真正的问题换成了 §4.2 的通道退役 |
| `application/core/context_runtime/README.md:16,75`、`seelexctx/README.md:165`、`seelexctx/controller.go:4,46,83` | 仍把「软阈值触发、回合内控制器折叠」写成现行机制 | 2026-09-30 起：回合内不折、全局只剩硬线一条（`context_soft_percent` 保留键兼容）；判据 Detail 里的 `soft=`/`ineffective_fold … soft=…` 也随之名不副实 |
| `context_runtime/coordinator.go:235` 注释 | `SoftThreshold = 判据量的软阈值（limits.context_soft_percent，默认 95%）` | 同一批改动后 `SoftThreshold` 是按**硬线**比例产出的（`task_context/token_counter.go`），注释未跟 |

---

## 10. 证据索引

- **记录结构与写点**：`application/model/state.go:38-108`；`context_runtime/coordinator.go:214-230, 600-720, 800-916, 991-1016`；`task_context/task_context_state.go:1015-1046, 900-935`；`application/core/context_compact.go:17-120, 230-285`
- **进度/门禁（瞬态）**：`context_runtime/compaction_progress.go`（全文）；`application/event/hub.go:147`（发布口径）；`gui/frontend/dist/{protocol.js:162-168, app.js:1150-1200}`
- **持久化**：`application/core/session_runtime/archive.go:211-253`；`internal/adapters/session_workspace_ports.go:384-410, 989-1016`；`sessionstore/{session_granular.go:196-215, 266-288, json_layout.go:63-101, 251-282, sessionstore.go:794-810, session_context.go:754-830}`；`sessionstore/README.md:100-205`；`application/core/session_permission_tier.go:5-14`；`application/core/session_history.go:412-472`
- **失败留痕**：`context_runtime/compaction_frame.go`（全文）；`seelebridge/{runtime_compaction_index.go:60-200, runtime_context.go:236-310}`；`seelexctx/replay.go:248-262`
- **用例/实跑**：`application/core/context_compact_local_fallback_repro_test.go`、`context_compactions_restore_repro_test.go`、`context_compact_no_summary_test.go`、`context_compact_across_rounds_repro_test.go`、`seelebridge/cold_restore_compaction_context_probe_test.go`；本会话实跑见 §7
- **历史**：`docs/devlog/2026-09-28-compaction-record-across-rounds-and-browsing-submit.md`、`2026-09-30-loop-no-fold-single-threshold.md`、`2026-09-30-restart-restore-context-lost-fix.md`、`2026-10-01-compaction-session-scope-and-fold-fallback.md`、`2026-10-02-compaction-readback-gate.md`、`docs/2026-09-29-context-compaction-fold-review.md`

### 附：§4.3 的探针原文（还原即可复跑）

```go
// sessionstore/zz_survey_probe_test.go（临时件，跑完删除）
package sessionstore

import (
	"strings"
	"testing"
)

func TestSurveyZZRecordChannelKeepsExecution(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionGranularStore(router)
	const projectID, sessionID = "p-survey", "s-survey"
	router.SetWorkspace(projectID)

	saved := []byte(`{"version":3,"id":"s-survey","status":"idle",` +
		`"execution":{"task":{"context_compactions":[{"version":1,"reason":"context_budget","event_from":1,"event_to":9}]}}}`)
	if err := store.SaveRecordRaw(projectID, sessionID, saved); err != nil {
		t.Fatalf("SaveRecordRaw: %v", err)
	}
	got, err := store.LoadRecordRaw(projectID, sessionID)
	if err != nil {
		t.Fatalf("LoadRecordRaw: %v", err)
	}
	t.Logf("写进去: %s", saved)
	t.Logf("读回来: %s", got)
	if strings.Contains(string(got), "context_compactions") {
		t.Logf("结论：record 通道仍承载 execution.task.context_compactions")
		return
	}
	t.Logf("结论：record 通道丢掉了 execution.task.context_compactions（v8/S20 停写停读）")
}
```
