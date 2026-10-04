# 新会话也必须绑定会话上下文存储：推帧「未绑定」的根因

- 日期：2026-10-04
- 范围：`application/core/session_history.go`（抽出 `attachSessionContextFor`）、
  `application/core/session_draft.go`（物化时挂接新会话的 store；**不再**解绑）、
  `application/core/workspace_usecase.go`（切项目另起独立会话同样挂接）、
  新增用例 `application/core/session_draft_context_attach_test.go` 与
  `seelebridge/runtime_compaction_unbound_test.go`、
  `scripts/gen_core_readme_index.py`（两条 orphan 文件归卷）及其生成的分卷 README 索引。
  历史留痕（CHANGELOG、docs/devlog、docs/2026-*、docs/research）不改。
- 前置：同一现场的上一条改动
  （`docs/devlog/2026-10-04-compaction-summary-switch-default-open-and-fallback-attribution.md`）
  处理的是"帧正文把那句话读成了配置事故"；本文处理的是**那句话本身**——为什么会出现
  `推帧失败：compaction index: 会话上下文存储未绑定（压缩栈不可用）`。

## 1. 根因：三条"让某个会话成为当前会话"的路径，两条没挂接

三条事实叠起来就是一个必然的缺口：

1. **唯一的挂接点只有 resume**：`SessionPort.AttachSessionContext`
   （[session_workspace_ports.go](../../internal/adapters/session_workspace_ports.go)）
   此前只被 `resumeSession`（[session_history.go](../../application/core/session_history.go)）调用；
2. **建会话不挂接**：`seelebridge.Runtime.newMainSession` → `bundleFor(sessionID)`
   （[runtime.go](../../seelebridge/runtime.go)、[runtime_bundle.go](../../seelebridge/runtime_bundle.go)）
   会把 `activeSessionID` 指向该会话，但**不**建也不挂 store；
3. **读的永远是当前活跃 bundle**：`sessionContextStore()`
   （[runtime_context.go](../../seelebridge/runtime_context.go)）读 `activeBundle()`。

于是"让会话成为当前会话"却没挂接的那条路径，会话的**整段活跃期**都处在未绑定态。
当时有三条这样的路径，两条漏了：

| 路径 | 位置 | 原行为 |
|---|---|---|
| 冷恢复 / 热挂载 | `resumeSession` | 挂接 ✓ |
| **新建会话（含冷启动草稿物化）** | `materializeDraftSession` | `DetachSessionContext()`，此后无人挂接 ✗ |
| **切项目另起的独立会话** | `workspace_usecase.go` 的 `startFreshSession` 分支 | 只 `ActivateSession` ✗ |

## 2. 未绑定的代价

- `PushCompactionFrame` / `ReadbackCompactionSummary`
  （[runtime_compaction_index.go](../../seelebridge/runtime_compaction_index.go)）
  **硬失败**：装配层压缩推不了帧、拿不到可回读的 `segment_id`，帧正文只能写一句
  "没有模型生成的读后感"；这正是现场帧 `readback.note` 里那句话的来源。
- `prefixStacks` / `tailStacks` / `stackBlocks`（plan/task/skill/compact 栈块）与
  `relatedMemoryBlocks` 一律返回空——同一个会话的 prompt 里没有栈块可看。
- **同一状态两处语义不一致**（真正的坏味道）：控制器路径
  `runtimeCompactStacks` 在 store 为空时**退回内存栈**（[runtime.go](../../seelebridge/runtime.go)），
  装配层那条路径却硬失败。所以缺口不会安静地少写一帧，而是以一条错误显形——也因此
  我们是从帧里的报错反查到了接线。

## 3. 现场证据

两个会话键（帧归档在各自会话的 `big_tool_result/*.result.json` 里）：

- `seelex-1791042860343116700-1`（`project-03b5b8d29af3f44b/session-172d6cbbf3f38806`）
- `draft_1790523014165652000_1`（`project-97206db0a1944353/session-48c05322bb9e6f1a`）

两者都是**新建会话的早分配 SID**（`newDraftSessionIDLocked` 的前缀：今天是 `seelex`、
2026-10-01 前是 `draft_`，见 [session_draft.go](../../application/core/session_draft.go)），
都不是 resume 出来的会话。旁证：前者目录里**没有** `metadata/compact.json`（从未成功推过
帧），后者**有**（曾绑定并推成功过）——与"只有未绑定期才失败"一致。

## 4. 修法：一条共用挂接 + 一处不该有的解绑

1. `Service.attachSessionContextFor(workspaceID, sessionID)` 落在 `session_history.go`：
   resume、草稿物化、切项目另起会话**共用同一条挂接**，口径与错误文案一致。
2. `materializeDraftSession`：`DetachSessionContext()` → `attachSessionContextFor(workspace, newID)`。
   早分配 SID 就是本会话的最终键；全新键上 `Load` 只会落到空记录（各通道 not-found 按
   S19 口径静默初始化，见 [session_context.go](../../sessionstore/session_context.go) 的
   `loadBlob` 与 [stack_journal_json.go](../../sessionstore/stack_journal_json.go) 的
   `readStackHead`），既不继承上一个会话的四栈，也不多写任何东西——写只发生在 `Persist`。
3. `workspace_usecase.go` 的 `startFreshSession` 分支：建完引擎后接同一条挂接。
4. `BeginNewSession` 的 `DetachSessionContext()` 加上**"只在被离开的会话空闲时"**这条判据
   （复用紧邻上方 `clearEngineHistoryFor` 的同一条 `!currentRunning`）：`DetachSessionContext()`
   解绑的是**当前活跃 bundle**，而跑着的会话的回合会继续装配 provider 上下文——那一步遇到
   装配层压缩就会发现自己"未绑定"，症状与新建会话那条一模一样。既有的"空闲会话离开即解绑"
   口径（`TestBeginNewSessionDetachesSessionContext`）**原样保留**：那条 pin 说的"防止四栈串到
   下一个会话"对空闲会话仍然成立，改的只是"跑着的会话的状态一律不动"。

## 5. 先红后绿

| 用例 | 改前（红） | 改后 |
|---|---|---|
| `TestMaterializeDraftSessionBindsContextStoreForNewSession`（`application/core`） | `attach 调用 = []`（物化后没有任何挂接） | 末次 attach = `:<早分配 SID>`；且全程 `detached = 0` |
| `TestWorkspaceSwitchFreshSessionBindsContextStore`（`application/core`） | `attach 调用 = [project-a:seelex-…-1]`——新会话没有 | 末次 attach = 切项目后那条会话 |
| `TestBeginNewSessionKeepsRunningSessionContextStore`（`application/core`） | 被离开的会话仍在跑时照样 `detach`（`detached=1`） | 跑着的会话不解绑（`detached=0`）；空闲会话仍解绑（既有 pin `TestBeginNewSessionDetachesSessionContext` 保持绿） |
| `TestUnboundSessionContextStoreFailsCompactionPush`（`seelebridge`） | 钉住"未绑定"的确切后果：读数与推帧都报 `会话上下文存储未绑定`（这句是现场帧引用的对外文案） | 同左（它是机制证据，不是修复靶子） |

复跑红灯（想自己验一遍：把 `attachSessionContextFor` 的调用点去掉、或把 `!currentRunning`
判据去掉即可）：

```text
go test ./application/core/ -run TestMaterializeDraftSessionBindsContextStoreForNewSession -count=1
go test ./application/core/ -run TestWorkspaceSwitchFreshSessionBindsContextStore -count=1
go test ./application/core/ -run TestBeginNewSessionKeepsRunningSessionContextStore -count=1
```

## 6. 回归证据

```text
gofmt -l application/core/session_history.go application/core/session_draft.go application/core/workspace_usecase.go
        → 干净
git diff --check                                 → exit 0
go build ./...                                   → exit 0（在并发写者改坏 seelebridge/teamwork 之前）
go test ./application/core/... -count=1          → 全绿（含三条新用例与既有 pin
                                                    TestBeginNewSessionDetachesSessionContext）
go test ./seelebridge/ -run TestUnboundSessionContextStoreFailsCompactionPush -count=1 → ok
go test ./... -count=1 -timeout=900s             → 除 seelebridge/teamwork [build failed]
                                                    外全绿：application/core、seelebridge、
                                                    sessionstore、workspace、internal、gui、tui、
                                                    mcpstack 均 ok
```

两次全量里各有一条**与本次改动无关**的中断，如实记录：

1. `seelebridge/teamwork [build failed]`：`coordinator.go:391: c.unsettledItems undefined`。
   该目录的 `coordinator.go` / `items.go` / `items_chain_gate_test.go` 在本轮进行中被**另一个
   写者**改动（文件 mtime 13:40:52–13:41:14，晚于本轮最后一次源码改动 13:37:12），是一次在飞编辑
   的中间态；工作区里那三个文件的增删**不是本轮改动**，我没有去碰它。
2. `seelebridge.TestForkSubagentsBestEffortKeepsSiblingAlive` 在一次全量里失败
   （`upstream LLM stream failed: context deadline exceeded`）——它真的打上游模型，负载下会超时；
   单独复跑与独占全量复跑都通过。同一轮里 `sessionstore.TestCommitNotBlockedByFullHistoryRead`
   也在"两份套件并发跑"时抖过一次，单独复跑通过。

## 7. 未做 / 边界（如实）

1. **fork 不修**：`ForkSession` 在写子会话快照后走 `resumeSession(childID)`，挂接自愈；
   本轮只堵"没有 resume 兜底"的两条路径（新建会话、切项目另起会话）与被离开会话在跑的
   那条解绑。
2. **`BeginNewSession` 仍在快照层清上一个会话的会话事实**（`Task`/`ReadFiles`/
   `Runtime.Plan`/`Interaction`）：那是 2026-10-01 修过的另一条口径（跨会话污染），
   与 store 无关，本轮没动。
3. **触发场景无法只在盘面上判定**：现场那一帧究竟来自"新建会话从未绑定"，还是来自
   "新建会话时另一个会话正在跑、被连带解绑"，证据只能证到"两个会话键都是新建会话的
   SID"。两条触发都已在代码上堵住，没有为了对齐叙事去改证据。
4. **空闲会话离开时仍解绑 store**：这条口径不动（既有 pin），代价是"空闲会话被解绑 →
   之后作为当前会话之前必须经过 resume/切项目那一跳重新挂接"——生产上切回来就是
   `ResumeSession`，所以不构成缺口；本轮没有去改"离开即解绑"的既有语义。
4. **索引补齐是积压漂移**：`scripts/gen_core_readme_index.py` 的根包分卷此前因两条未归卷
   文件（`teamwork_board_session_switch_test.go`、`teamwork_completion_trigger_test.go`）
   在自检处退出，导致根包分卷 README 一直没刷新（新文件进不了索引）。本轮把这两条归到
   `service` 卷（该卷已持有 `teamwork_service.go`/`teamwork_board_projection_test.go`），
   生成器因此首次跑通。归卷选择是我的判断——不同意就换卷名重跑生成器，生成物是机械的。
   随之刷新的 6 个分卷 README 属积压补齐（`README-chat/context/goal/service/session/work-table`）。
5. **`gofmt` 既有漂移**（不是本轮引入，内容与 HEAD 一致）：`application/core/session_lifecycle.go`、
   `application/core/teamwork_board_projection_test.go`，以及此前记录过的 `seelebridge/teamwork` 若干文件。
6. **提交**：按仓库规范不自动提交；改动留在工作区等用户点头。
