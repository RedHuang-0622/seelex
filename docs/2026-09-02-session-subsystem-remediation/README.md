# 会话子系统整改 · 打点表

> 来源：2026-09-02 会话子系统审查（会话切换/后台/并行、前端越界、会话过滤生态位）。
> 本文件是一次性工作包的进度台账，不是长期事实来源；完成的判据是代码与测试，
> 每完成一项即把 `[ ]` 改为 `[x]`。
>
> 阶段顺序：A（链路收口）→ D（同步缺口 + 测试凹陷）→ B（下沉越界逻辑）→ C（扩 API）。
>
> **后续对账纠正（见 [target-design.md](target-design.md) M1/M2）**：下文阶段 A 的
> 「订阅传空 sid 跟随视图指针」结论**已被撤销** —— 它把"视图神谕"从数据面搬进了事件
> 面（replay 环因而跨会话，且空 sid 通配让含会话字段的全局载荷畅通）。正确口径是订阅
> 键含 sid、切换即重订阅、会话类 kind 事件 sid 必填。阶段 G 的刀 2 负责收口。

## 阶段 A · 事件分发链路收口（会话过滤只留一处）

设计修正（相对审查原文）：草稿态下 GUI/TUI 无法预先知道会话 ID（ID 由首次提交时
`Engine.StartSession()` 生成），因此不由客户端"切换后重订阅"，而是让**视图归属由
application/core 权威解析**：`SubscribeSession("")` = 跟随当前视图会话，归属判定
只在 `session.Domain` 的视图指针一处。

该修正的前提（草稿无法预知 SID）已随 [target-design.md](target-design.md) 决策 2
（早分配 SID + 建 Unit，不建引擎 bundle）消失，因此"客户端切换后重订阅"重新成为
可行且正确的方案，见刀 2。

- [x] A1 `application/event/hub.go`：订阅者携带过滤谓词，`publish` 在投递端过滤（消除跨会话挤 buffer）；溢出 resync 强制以全局事件（`SessionID=""`）投递，不被过滤吞掉
- [x] A2 `application/event/hub.go`：`SubscribeSession` 改为基于谓词实现（删除中继 goroutine）；新增 `SubscribeFiltered`；新增订阅内 `delivery_seq`（全局 seq 过滤后必然跳号，连续性只能按投递序号判定）
- [x] A3 `application/core/session_scope.go`：`SubscribeSession("")` 合法化 = 跟随当前视图（`Domain.ActiveID()`），显式 ID 仍为严格过滤
- [x] A4 `gui/bridge.go`：删除 `curMu`/`currentSessionID`/`isForeignSessionEvent`/`setCurrentSession` 及 4 处调用；`Start` 改为一次会话级订阅（切换无需重建订阅）
- [x] A5 `gui/frontend/dist/protocol.js`：删除 `isForeignSessionEvent` 与调用；连续性改判 `delivery_seq`；`protocol.test.mjs`/`client-state.test.mjs` 的 S0 靶场改判为"归属由上游保证"
- [x] A6 `tui/tui.go`：`AppController` 增加 `SubscribeSession`，初始化改用跟随视图的会话级订阅（与 GUI 同一模型）
- [x] A7 测试：`application/event/hub_test.go`（投递端过滤/delivery_seq 连续/溢出 resync 全局/谓词跟随视图）、`application/core/subscribe_session_test.go`（草稿物化 + 视图切换）、`gui/bridge_session_test.go`（一次订阅 + 别会话事件不到达渲染层）
- [x] A8 文档：`application/event/README.md`、`gui/README.md`、`gui/frontend/README.md`、`tui/README.md`、`docs/gui/modules/multi-session-pages.md`、`application/core/README-*.md`（索引刷新）

## 阶段 D · 同步缺口与测试凹陷

结论分四类：已修 / 核查后不需要改（附证据）/ 待决（与在途改动语义冲突）/ 仍是缺口。

### 已修

- [x] D1a `session/domain_actor.go`：`SetActive` 等 reply（与 `ActiveID` 对称，建立 happens-before）——**已随阶段 A 完成**：视图指针是投递端归属判定的依据，异步即漏投（新增用例首先抓到了它）。
- [x] D1b `session/domain_actor.go`：`Close` 改 `sync.Once`（原 `select`+`close` 在并发下两个 goroutine 可同时通过检查 → close of closed channel panic）；`Register`/`Remove` 同样改为等回包，消除"刚注册不可见/刚注销仍命中"窗口。
- [x] D2a 解析器字段加锁：`SessionGranularStore`、`EventStore`、`DurableHistory` 三处的 `workspaceResolver` 此前都是裸字段读写（`EventStore` 原有的 `mu` 只锁了写侧、且 `Append` 持 `mu` 时调用 `projectFor`，复用同一把锁会自死锁 → 三处各自使用 `resolverMu`，锁内取回调、锁外调用）。
- [x] D4b **新发现**：`hotAttachSession` 在 `Mu.Unlock()` 之后调用 `SystemPromptForActiveTaskLocked()`（`*Locked` 契约要求持 `Core.Mu`）→ 改为持锁段内取值，解锁后只写引擎（与 `resumeSession` 同形）。
- [x] D5 `plan_tools.go`：`HandlePlanNodeComplete`/`HandlePlanBranchEvent` 在 `Mu.Unlock()` 后读 `Core.Snapshot.Session.ID` → 改为锁内取 `viewSessionID` 快照。节点事件的归属本来就正确（`planEventSession(event)` = 执行会话），只有分支事件写入视图单例的 Plan，因此归属视图是自洽的。
- [x] D6a **新发现**：SQLite 后端并发写立刻 `SQLITE_BUSY`（默认 busy_timeout=0，且 `database/sql` 多连接）→ 本地会话库并行落盘随机失败。修复：sqlite DSN 追加 `busy_timeout` + 收敛为单连接，由 `database/sql` 排队。
- [x] D2b `ResolveProjectForSession` 归属定稿（2026-09-02 决策）：定序为 **resolver（workspace.Repo 绑定，权威）→ record 自带 Binding → 已知项目里扫描数据实际所在 → 未关联（默认项目 `""`）**，彻底不再回退 Router 活跃写作用域。
  - 新增注入面 `SessionGranularStore.SetProjectSource` ← `SessionPort.SetProjectSource` ← `main.go`（`workspace.Repo.List`），因此不需要给远程 `Seele/seelectx/storage` 加项目枚举口。
  - `StorePortAdapter` 的归属来源是绑定表，测试夹具随之按生产形态注入（`session/store_adapter_test.go`）。
  - `TestSessionGranularStoreConcurrentProjectScope` 恢复强断言：绑定会话恒定归属、且任何情况下不得解析到没有该会话数据的项目。
  - 由此"未关联会话"= 解析为 `""` 的会话集合，成为阶段 E 左栏分组与重新绑定的对象。

### 核查后不需要改

- [x] D3 `resumeSession` 切换前 `flushStreamBatcherFor(oldSID)`：**不需要**。流式缓冲器与输出流都挂在**会话单元自身**（`unit.BatcherSink()`/`StreamSink()`，见 `chat.go:491-532`），移动视图指针既不销毁也不搬迁旧单元的 batcher，旧会话由自己的回合收尾/工具边界负责落地；跨 goroutine 追加一次 `FlushPending` 只会与运行中会话自己的写者争用。真正会导致串写的是"视图指针尚未移动就按旧指针判定"，已由 D1a 关闭，并由 `TestViewSwitchDoesNotMutateExecution`（TC-INV-02）与 `TestStressConcurrentSessionsDoNotPollute` 覆盖。
- [x] D4 `UnloadSession` 的"读 running → Remove"TOCTOU：**不加第二把锁**。提交路径 `submitConversation` 与 `UnloadSession` 持同一把 `sessions.TransitionLock()`，整段已对提交原子；再加一层 `Mu.Lock` 覆盖是对同一不变量的重复设防。改为补回归用例 `TestUnloadRejectsRunningSession` 钉住行为。

### 仍是缺口

- [ ] D6b `fork_test.go` 的发散式 `-race`（其余三处已落地：`sessionstore/concurrency_race_test.go`
  的 `TestDurableHistoryConcurrentSessionsStaySeparate`、`TestRouterConcurrentProjectIsolation`、
  `TestProjectRecordConcurrentReadDoesNotTear`；`session_granular` 与 `event_store` 在
  `session_granular_race_test.go`）。
- [x] D7 跨项目 + 后台落盘 + 切换组合：已有 `repro_session_workspace_test.go`、`repro_session_background_test.go`、`repro_session_race_test.go` 覆盖；"切换瞬间 in-flight 事件归属"由阶段 A 的 `application/core/subscribe_session_test.go` 覆盖。

## 阶段 B · 下沉越界逻辑

- [x] B1 运行态：新增 `chat.changed` 事件下发权威 `ChatState`（`Service.publishChatStateFor`，在回合启动/排队入队/回合收尾三类转换点发布），`protocol.js` 删除 `markRunning` 及其两处推断调用。载荷刻意 `revision=0`：转换点往往先发 `snapshot.changed` 触发客户端重拉并抬高 revision floor，带 revision 的 `chat.changed` 会被"已由权威快照表示"规则丢掉。测试：`application/core/chat_state_event_test.go` + `protocol.test.mjs`。
- [x] B2 会话窗口：窗口截断与游标（`total_messages`/`history_offset`/`has_more_history`）收回后端 `view_state`，`protocol.js` 删除 `boundConversation`、`countDurableMessages` 与 message.added 里的计数推算，reducer 只 upsert 消息；权威窗口随下一次 `snapshot.changed` 重拉到达（一次回合内数组最多增长该回合新增消息数）。契约测试改为断言"增量不改游标/不截断"。
- [x] B3 tool/tool_result 配对：核对后**不需要新增 DTO 字段** —— 服务端 `tool_started` 与 `tool_result` 本就携带同一个框架 tool-call id（`tool_hooks.go:34/206`、`chat.go:671/675`），恢复历史也取 `toolCall.ID`（`session_history.go:348`）；越界的只是前端"按 name 回退猜配对"。已从 `components.js` 与 `trajectory.js` 删除该回退与 `pendingTools`，缺 id 的响应独立成行；补同名并发工具乱序完成的配对测试。同步 `docs/gui/modules/trajectory-view.md` 契约。
- [x] B4a Thought 剥离：`tui.go:copyLastResponse` 删除按 `"---"` 倒序猜正文边界的逻辑，直接使用已剥离思考块的 `Message.Content`（思考内容在 `Message.ReasoningContent` 独立字段，application 侧 `chat.StripThoughtBlocks` 是唯一实现）。
- [x] B4b 粘贴折叠：**不下沉**。核对语义后它不是业务不变量：TUI 折叠只作用于输入框显示，提交给后端的仍是完整原文（`handleEnter` 用 `pasteBuffer` 提交），折叠计数器/占位符从不跨进程；GUI 侧是浏览器 textarea，Go 工具函数无法被 JS 复用。为此新建 `application/util` 包只会造出只有单一消费者、且跨语言用不上的包（违反 AGENTS.md 防上帝包/新包判据）。
- [x] B5 `CancelChat`：取消对象的归属判断下沉到 `Service.CancelChat`（`requestID` 降为参考，动作对象恒为视图会话当前运行回合；空闲时拒绝），删除 `bridge.go` 的空 id 重试。行为与改前一致（原先 Bridge 也会重试成空 id），只是三端共用一处实现。测试：`TestCancelChatWithStaleRequestID` + `TestBridgeCancelChatForwardsOnce`。
- [x] B6 会话展示元数据（按定稿方案 ②：项目级 meta blob）：
  - 存储 `sessionstore/session_meta.go` 的 `SessionMetaStore`：一个项目一份 blob，借
    state 通道的伪会话键 `seelex:project:session-meta` 落盘。目录枚举只认有 manifest
    的会话目录，因此它不会变成幽灵会话，`Delete(session)` 也删不到它。不并进
    `SessionRecord` 的理由：record 在回合结束时由存活状态整体重建，夹带的展示字段会被覆盖。
  - 应用面：`session.SessionMetaPort`（可选端口，未实现返回
    `ErrSessionMetaUnsupported`，目录照常枚举）→ `Service.SetSessionMeta` /
    `Service.SessionMeta`（写后唤醒目录刷新）；`model.SessionInfo.Meta` 随快照下发
    （`internal/adapters.SessionPort` 在 `List`/`SessionsOf` 上盖 blob）。
  - 前端：`sidebar.js` 删除 `PIN_STORAGE_KEY`/`read|writePinnedSessions`/`isPinned`/
    `togglePinned`；置顶点击改为 `Bridge.SetSessionMeta(sessionID, pinned, alias,
    sortOrder)` + 刷新快照；分组排序与标题读取 `session.meta`（别名优先于后端标题）。
  - 测试：`sessionstore/session_meta_test.go`（读写/清除/项目隔离/无幽灵会话 +
    `-race` 并发读改写无丢失更新）。
  - 未接：别名**编辑入口**（无 UI 触发点；DTO、Bridge 参数与存储链路已通）。重名会话
    编号仍是渲染期派生：它必须由当次列表算出，存成元数据会在改名/删除后变脏；只有
    跨重启"编到第几号"的尾号存档留在 localStorage。

## 阶段 C · application 查询面补齐（headless 可行）

- [ ] C1 `ListSessions` / `SnapshotOf`（扩展到非活跃驻留）/ `GetSessionTranscript(range)`
  —— **后移到阶段 G 刀 6**。原因：进程内只有一格 `Core.Snapshot.Runtime`，冷拼装只能
  clone 视图的 Runtime，读回来的 model/tokens/replan/子代理树属于别的会话；且
  `SnapshotOf` 返回视图 revision，前端 `revision <= floor` 规则会把该会话后续合法增量
  判为过期丢掉（`gui/frontend/dist/protocol.js:37-39`）。必须先用刀 1'/刀 3 造出每会话
  Runtime 槽、每会话 revision 与快照分型，冷读面才有干净形状。
- [ ] C2 `ArchiveSession`（`SetSessionMeta`/`GetSessionMeta` 已随 B6 落地）—— 依赖刀 6
  的目录按 projectID 索引（归档状态要在目录里过滤，否则仍是全局数组上的标记位）。
- [x] C3 目录刷新完成回执：`Coordinator.RequestCatalogRefresh()` 返回 `<-chan struct{}`（**先登记回执再非阻塞唤醒**，worker 每次唤醒逐批排空：每批跑一轮刷新并在发布后关闭，批次为空才回到等待 —— 因此 wake channel 丢唤醒不会丢请求）；`StopCatalogRefresh` 退出路径释放全部在等回执并置停止标记，之后的请求立即收敛，关闭不会被目录 I/O 挂住。新增 `application/core/session_catalog.go` 的 `Service.WaitCatalogRefresh(ctx) error` 作为公开等待口（ctx 超时返回 `ctx.Err()`，不算失败）。
  - `gui/bridge.go`：`BeginNewSession`/`DeleteSession`/`ForkSessionLatest`/`SetSessionMeta` 成功后调用 `settleCatalog()`（预算 `sessionCatalogSettleTimeout = 2s`），使 renderer 紧接着重拉的 `Snapshot()` 已携带权威目录；超时按最佳努力处理，不向上报错。
  - `gui/frontend/dist/app.js: beginNewSession` **删除**"首轮列表为空就回填上一次 `sessions`/`session_workspaces`/`workspaces` + 250ms 延时重拉"的前端伪造状态。
  - 测试：`application/core/session_catalog_test.go`（新条目在一轮等待后即可见 / 16 个并发等待全部收敛 / Shutdown 后等待立即收敛）、`gui/bridge_test.go: TestBridgeSettlesSessionCatalogBeforeReturning`（四个命令必须等收敛才返回，且只等一次）。
  - 文档：`application/core/session_runtime/README.md`（回执契约）、`docs/arch/session-snapshot-liveness.md` §3（函数名与流程随实现更新）、`gui/README.md`（Bridge 契约）、`gui/frontend/README.md`、`application/core/README-session.md` 与 `session_runtime/README.md` 函数索引刷新。
- [x] C4 慢订阅者策略（订阅内重放 + 投递回执）：
  - `application/event/hub.go`：`Subscription` 新增 `ReplaySince(sinceSeq) ReplayResult` 与
    `DeliveryWatermark()`；新增 `SubscribeWithReplay(filter, buffer, window)`。开启窗口的
    订阅在缓冲写满时**不丢弃载荷、不排空缓冲**（事件先进按 `DeliverySeq` 有序的窗口，
    再尽力写 channel），溢出不再强制整份重拉；`Subscribe`/`SubscribeFiltered` 保持旧语义
    （排空 + 全局 `resync.required`），TUI/headless 行为不变。窗口淘汰掉缺口区间时
    `Covered=false`，调用方必须整份重拉。
  - `application/core/session_scope.go`：`SubscribeSession` 的谓词抽出 `sessionEventFilter`
    复用，新增 `SubscribeSessionWithReplay(sessionID, buffer, window)`（hub 不支持窗口时退化）。
  - `gui/bridge.go`：relay 优先申请带窗口订阅（可选端口 `replayAwareApplication`）；新增
    `AckEvents(seq)`（渲染层应用水位，单调推进，过期回执忽略）与 `ReplayEvents(sinceSeq)`
    （缺口主动补取）；`armResend`/`catchUpRenderer` 在 `eventResendDelay` 内没等到回执就
    从窗口重推未确认事件，封顶 `eventResendMaxTries`，窗口淘汰则改投一条带当前水位的
    `resync.required`（渲染层据此整份重拉并抬水位）。重复投递对渲染层幂等（按
    `delivery_seq` 去重）。
  - 前端：`protocol.js` 缺口分支不再推进水位而是返回 `gap`/`gapSeq`；`client-state.js`
    缺口先 `replay` 后重拉，并把事件应用串行化（链本身吞掉失败，一次抛错不得永久卡住
    后续事件）；`app.js` 新增 150ms 合并的 `reportAppliedEvents` 回执。
  - 测试：`application/event/hub_replay_test.go`（溢出可补取 / 淘汰后不覆盖 / 窗口与投递
    同一归属口径 / 关闭后拒绝补取）、`gui/bridge_events_test.go`（未回执事件被重推且封顶、
    覆盖缺口不得强制 resync、过期回执不改水位、窗口淘汰退化为 resync、未启动时补取显式
    不可用）、`client-state.test.mjs` 3 例、`protocol.test.mjs` 缺口契约改写；
    `gui/bridge_test.go: TestEmbeddedFrontendExists` 的前端契约从"必须内联轮询兜底"改为
    "必须接线 AckEvents/ReplayEvents 且不得引用轮询"。
- [ ] C5 轮询退场 —— 拆分后实情：
  - [x] C5a `active-chat-sync.js` 1s 轮询**已删除**（连同其测试与 `beforeunload` 钩子）：
    它兜的是"Go→WebView 丢了尾部事件"，`delivery_seq` 只能发现**后续还有事件**的缺口，
    尾部丢失永远不跳号。C4 的 `AckEvents` 水位 + 未确认事件重推正面解决了这个场景，
    因此不再需要无条件轮询对账。
  - [x] C5b `refreshWorkTree`/`refreshGitLog`（`app.js:451-525`）：**判定不改**。核查后它
    不是轮询 —— 全函数没有任何定时器，是渲染期惰性加载，触发条件为 rootPath 变化或
    `lastChatRunning && !running`（回合刚结束）。改成推送需要 `worktree.changed`/`git.changed`
    事件面，而 `application/event/hub.go` 现有 19 个 `EventKind` 里两者都不存在：那是"新增
    文件/git 监视"新功能（跨平台监视生命周期），不属于本工作包的整改。留作独立立项。
  - [ ] C5c `nodeDetailPollTimer`（`app.js`，运行中每 2s 拉 `SubagentSessionDetail`）：仍开放。
    实时流 `seelex:subagent_live` 已在，但 `application/contract/dto/subagent_live.go` 的
    `Kind` 只有 `stage|tool`，子代理自己的 assistant 正文没有推送面 —— 要退场必须先在
    `seelebridge/runtime_live.go` 增加正文增量 kind。

## 阶段 E · 未关联会话与左栏重新绑定 —— **已决定不做**（2026-09-02）

决定理由：归属定稿后，重新绑定必然要求把会话五片（record/history/transcript/
tool-results/context + manifest generation）**整体跨项目搬迁** —— 只改绑定不改数据
会让会话立刻打不开。移动用户会话数据的代价与风险不划算，故不做重绑定；
未关联会话继续留在默认项目分组里可读可删。

前提仍然成立且有价值：`ResolveProjectForSession` 返回 `""` 即「未关联」集合，
可作为只读的左栏分组展示（不涉及数据搬迁）。

以下为未采纳方案，保留备查：

- [ ] E1 `sessionstore`：`MoveSession(fromProject, toProject, sessionID)` 原子迁移五片（record/history/transcript/tool-results/context）+ manifest/generation；先写目标并校验，再删源，失败保留源
- [ ] E2 `application/core`：`RebindSessionWorkspace(sessionID, workspaceID)` 用例 —— 持 `TransitionLock`、运行中拒绝、迁移数据、写 `workspace.Repo` 绑定、目录刷新 + 全局事件
- [ ] E3 Snapshot：`SessionInfo.WorkspaceID` 由后端权威下发（取代前端 `sessionWorkspaces` 本地映射），未关联会话以 `workspace_id == ""` 成组
- [ ] E4 `gui`：Bridge 暴露 rebind 动作 + 左栏会话条目「绑定到工作区…」（复用既有目录选择对话框），迁移前确认
- [ ] E5 测试：JSON/SQLite 迁移原子性与可读回、rebind 后端到端、rebind × 视图切换 `-race`、左栏分组渲染契约

> ⚠️ E1/E2 会**移动用户会话数据**（`.seelex` / dist 内副本）。按仓库铁律：执行前先检查运行中的 seelex 进程、备份、用中文说明影响面并获得确认。

## 验证记录

本机 `CGO_ENABLED=1`，`-race` 为真实执行（非跳过）。

阶段 A：

```text
go build ./...                                   # 通过
go test ./... -count=1                            # 通过（含 root 包 repro_*）
go test -race ./session ./application/event ./application/core ./gui ./tui -count=1   # 通过
node --test gui/frontend/dist/*.test.mjs          # 177 pass / 0 fail
```

阶段 D（与 A 的链路改造一并验证）：

```text
go test -race ./sessionstore -count=1             # 通过（新增 2 个发散式并发用例）
go test -race ./session -count=1                  # 通过（actor 并发 + 重复 Close）
go vet ./application/... ./gui/... ./session/... ./sessionstore/... ./tui/...   # 无告警
```

阶段 D6b + B2~B5：

```text
go test -race ./sessionstore -count=1             # 通过（新增 4 个发散式用例）
go build ./...                                    # 通过
go test ./... -count=1                            # exit 0，零 FAIL
go test -race ./application/core ./gui ./tui ./session ./sessionstore -count=1   # 全 ok
node --test gui/frontend/dist/*.test.mjs          # 179 pass / 0 fail
```

新增的 sessionstore 并发用例抓到两条**测试夹具自身**的错误期望（删除效果要看项目索引
而不是 record 存在性；tool-results 跨代累加因此"只该有 2 条"不成立），已按真实存储
语义改写为成对可见性与复制闭包断言；未出现存储缺陷。

阶段 B 收尾（B6 元数据）后复跑：

```text
go build ./...                                    # 通过
go test ./... -count=1                            # exit 0，零 FAIL
go test -race ./application/core ./gui ./session ./sessionstore ./internal/adapters   # 全 ok
node --test gui/frontend/dist/*.test.mjs          # 175 pass / 0 fail（删除 localStorage 置顶 4 例）
```

阶段 D 期间 `gofmt -l` 对 `application/core/session_scope.go` 的报告为**基线既有**
（该文件在本轮改动前已在 `gofmt -l .` 列表中，差异位于 `bindProjectRootIfSafe` 的
注释排版），未顺手改写。

阶段 C3（目录刷新完成回执）：

```text
go build ./...                                    # 通过
go build -tags "gui,desktop,production" ./...     # 通过
go vet ./...                                      # 无告警
go test ./... -count=1 -timeout=180s              # exit 0，零 FAIL
go test -race -run 'TestWaitCatalogRefresh|TestBridgeSettlesSessionCatalog' ./application/core ./gui   # 7 例全 PASS
node --test gui/frontend/dist/*.test.mjs          # 175 pass / 0 fail
```

`gofmt -l .` 仍只报告 12 个**基线既有**文件（CRLF 工作副本），本工作包改动的 5 个 Go
文件不在其中，未顺手改写。

首次全量 `go test ./...` 曾出现 `seelexctx/lifecycle: TestPipelineIntervalFlush`
失败（`interval flush must persist low-traffic chunks, store=0`，用时 0.08s）；单包
连跑 3 次与该用例均绿、全量复跑亦绿。判定为多包并行下的计时抖动，与阶段 C3 无
关（本包未触及 `seelexctx/lifecycle`）。该抖动留在台账备查，若要根治应把断言改
为按预算等待落盘而不是固定 tick。

文档同步：`application/event/README.md`（订阅口径/DeliverySeq/resync 全局）、
`session/README.md`（actor 命令语义与关闭所有权）、`sessionstore/README.md`
（SQLite 单连接 + busy_timeout）、`gui/README.md`、`gui/frontend/README.md`、
`tui/README.md`、`docs/gui/modules/multi-session-pages.md`、
`application/core/README-*.md`（函数索引刷新）。

## 阶段 G · 会话粒度收敛（对账后新增，目标形状见 [target-design.md](target-design.md)）

> 推进波形（刀 1'~7 收成四波、依赖 DAG 与验收锚）见
> [target-design.md](target-design.md) §9；波 3/4 与波 1/2 分会话推进。

### 刀 0：现在就在错写用户数据的三处（彼此独立，各自一个提交）

- [x] G0a 压缩轮次归档按 sid 路由：`application/core/compressed_turn.go:44-60` 忽略
  `ctx`、用 `SessionIDProvider()`（= `app.Snapshot().Session.ID`，`main.go:251`）落
  `SaveCommit` → 后台会话 A 压缩出的轮次原文写进视图会话 B 的 commit 分片。修法：优先
  `sessionIDFromContext(ctx)`（core 在 `chat.go:140` 已注入），provider 仅作兜底。
- [x] G0b effort/plugin 运行守卫：`application/core/service_interaction.go:98-149` 无
  `Chat.Running` 检查，且走 `Engine.SetSystemPrompt`（全局活跃别名）与
  `Engine.ClearHistory`、`promptStack.Reset("")` → 改动正在后台运行的会话的 system 与
  历史。修法：effort 只允许改目标（视图）会话且其 idle，改用
  `Engine.SetSystemPromptFor(sid,…)`；plugin 切换是进程级动作，任一会话 running 即拒绝。
- [x] G0c 退出语义：`gui/shutdown.go:49` 只看视图会话 `Chat.Running` 决定是否等待、
  `:67` 超时只 `CancelChat("")`（`WaitForIdle` 本身是进程 refcount，
  `service_input.go:180-192`）→ 视图空闲而后台在跑时窗口直接关。修法：判定改"任一会话
  非 idle"，超时取消全部 running sid，关闭前逐会话 flush。

### 刀 1'~7：结构收敛

- [ ] G1 每会话 `Runtime` 槽 + `Runtime`/`Revision` 进 `SessionUnit`；
  `view_state/coordinator.go:96-146` 投影全量改 For 端口并带 sid；
  `ApplyRuntimeProjectionLocked(sid, …)` 只写该会话槽、仅 `sid==视图指针` 时镜像；
  seelebridge 会话入口注入 `internal/telemetry.WithSessionID`（`SessionTagHook` 已挂在
  `runtime.go:395-400`，但生产侧无人注入 → INV-T1/T2 目前只在测试成立），
  `internal/adapters/engine_port.go:688-700` 的 `TokenCount()` 改 `TokenCountFor(sid)`；
  `planExecutor` 的 fork 信号量与 `ReplanGuard` 按 sid 建槽（M5）。
  完成后替换 `session_scope.go:191` 的 `cloneRuntimeState(视图 Runtime)`。
  - 进度（波 1）：G1-T（EnginePort 会话入口注入 telemetry 会话 ID +
    `TokenCountFor(sid)`）、G1-A（`SessionUnit` 增每会话 `Runtime` 槽与
    `Revision` 原语；task_context 增 `ActiveSkillIDsFor`/`GoalSkillActiveFor`）、
    G1-B（投影按 sid 收集/写回本会话槽；`SnapshotOf` 读槽并回退旧口径）已
    落地并有回归用例（`TestBackgroundRuntimeProjectionLandsInOwnSlot`、
    `TestS0BackgroundEventsDoNotPolluteActiveSnapshot`）。G1-C 的 ReplanGuard
    按 sid 建槽已完成（`ReplanGuards` 注册表 + `ReplanMetricsFor` + 运行时槽
    replan 统计）；planExecutor 的 binding/policy/fork 按 sid 槽尚未做（与
    G4 的 per-session effort 及 plan 运行上下文绑定相关）。
- [ ] G2 订阅键 `(通道, sid)` + 切换即重订阅；`protocol.js` 补 `session_id` 校验；
  事件通道按 kind 白名单（会话类必填、进程类必空，违例拒绝发布）；Bridge 的 ack 游标与
  replay 环按 sid 分格、resend 定时器合并。
  - 进度（波 1，执行中对账）：kind 白名单分类与 `ValidateSessionRouting` 辅助
    已落地（`application/event`，含单测）；显式会话订阅改为「进程类空 sid 全
    投、会话类必须 sid 精确匹配」，草稿空视图保留过渡口径。Bridge 已改为
    显式当前视图 sid 订阅并在 Resume/Activate/Fork/New 切换成功后重订阅；
    `protocol.js` 增加 `session_id` 校验（带 sid 不匹配即丢弃；缺 sid 的存量
    事件过渡期信任投递端）。验收锚 `TestS0BackgroundEventsDoNotPolluteActiveSnapshot`
    与 `TestS0SwitchResyncsBaseline` 均已转绿。**严格拒绝
    （空 sid 会话类发布直接拒）依赖 G4 早分配 SID**——草稿不再以空 sid 占位
    前不可全量开启（与 §9 依赖说明一致）；per-sid ack/replay 分格由重订阅
    （新订阅窗口/水位）天然重置，resend 定时器保持单例。
- [ ] G3 `SessionSnapshot` / `ProcessSnapshot` 分型（不升 `protocol_version`，走
  `capabilities` 声明），并设计成传输完备制品（为进程隔离保留退路，见决策 5）。
- [ ] G4 Composer（新分片 + `limits.composer_max_chars`）、effort/fullAccess、子代理树、
  approval 归属进 `Unit`；早分配 SID（不建引擎 bundle）；子代理会话按 `Kind=Subagent`
  落盘、不进侧栏、经父树打开，重启标 `stale`；"mainagent 实际接收内容"改为可见可持久
  （撤销 `view_state/coordinator.go:160-162` 的丢弃）。
- [ ] G5 锁拆分（`ViewMu`/`CatalogMu`/`Unit[i].Mu`）+ `TransitionLock` 按会话串行、
  跨会话并行；阻塞式端口调用一律出临界区。
- [ ] G6 驻留 LRU 上限 + 驱逐前置 flush；目录按 projectID 索引（`RequestCatalogRefresh`
  补 projectID，撤销 C3 固化的全局数组形状）；C2 `ArchiveSession`；C1 冷读面。
- [ ] G7 双轨 trace 桥：`seelebridge/events_unified.go:121-176` 的 `UnifiedEvents` 已能
  按 sid/nodeID 合并持久事实与实时遥测，缺 `EventStore` 区间读与"投进
  `application/event`"的一跳；完成后去掉 `nodeDetailPollTimer`（原 #3）。

### 撤销与不做（对账结论）

- [x] 撤销 B6 相关待办「per-session 存储策略」：存储策略只有全局的，`Router` 单份
  `Config`（`sessionstore/sessionstore.go:211-236`）即目标形状（INV-G13）。
- [x] 消息 ID **保持全局分发**（`view_state/coordinator.go:59,169`）：防重复与上下文
  干扰；新增不变量「唯一但不要求连续」（INV-G10），恢复取 max 与分页用计数已兼容，
  只需补一条测试钉住。
- [x] 「core 又成上帝模块」的判定成立，但**先补数据面再搬模块**：刀 1' 之前把会话治理
  迁出 `application/core` 只会把"只有一格 Runtime"的缺陷搬到新地方。

### 遗留待决

- [ ] `session.StorePort` 生产零调用方（`session/store_adapter.go` 只被测试引用）：删除
  还是在刀 1' 接为 `session` 域唯一存储入口，二选一，不留死契约。
- [ ] 待审批计数在 TUI 的呈现口径。

## 阶段 G 刀 0 验证记录（追加）

G0a/G0b/G0c 三处彼此独立，各自聚焦一个行为主题；改动面：
`application/core/compressed_turn.go(+test)`、`service_interaction.go`、
`command.go`、`service_input.go`、`gui/shutdown.go(+test)`，配套
`application/core/close_semantics_test.go`、`service_interaction_guard_test.go`。

```text
go build ./...                                   # 通过
go build -tags "gui,desktop,production" ./...     # 通过
go vet ./application/core ./gui ./seelexctx ./session ./sessionstore   # 无告警
go test ./application/core ./gui ./seelexctx -count=1 -timeout=180s    # 全 ok
go test -race ./application/core ./gui -count=1 -timeout=240s          # 全 ok
go test ./... -count=1 -timeout=240s             # 仅 sessionstore 并发 rename 用例
                                                # TestProjectRecordConcurrentReadDoesNotTear
                                                # 出现一次 Windows Access is denied；
                                                # 该用例单独复跑与全包复跑均绿（未触碰该包），
                                                # 判定为 Windows 临时目录文件锁抖动，与刀 0 无关。
```

README 同步：`gui/README.md`（关闭语义改为进程级空闲判定 + 取消全部
running sid + 取消后等待逐会话 flush）、`application/core/README-service.md` 与
`README-misc.md`（函数索引刷新：`AnyChatRunning`/`CancelAllChats`、
`StoreTurn` ctx 签名、新增守卫测试）。

## 波 2 执行与验证记录（追加，2026-09-03）

波 1 验收锚转绿后继续推进波 2；每次提交保持
`go build ./...`、`go test ./...`、`node --test gui/frontend/dist/*.test.mjs`
全绿。提交：`d5d42c5`（G4 先行第 1 片）、`81d9028`（composer 落盘/恢复）、
`906d3da`（G2 严格白名单）。

已完成：

1. **早分配 SID + 建 Unit（HasSession=false）**（G4 先行第 1 片）：
   - 草稿从新建（含冷启动懒引擎）即持有真实会话 ID 并注册 `SessionUnit`，
     不建引擎 bundle；首次提交经 `EnginePort.ActivateSession` 复用同一 SID
     物化（legacy 单会话引擎仍回退 `StartSession`）；卸载活跃会话后进入新的
     早分配 SID 草稿单元。
   - 订阅键因此恒等于视图 ID：所有作用于当前视图的全局发布改为会话级发布并
     携带 sid（`runtime.changed`/`snapshot.changed`/`interaction.*`/
     `message.added`/目录刷新等）；Bridge relay 检测会话键漂移时自动重订阅。
2. **composer 草稿跨重启可恢复**（G4 先行第 2 片）：`SessionRecord` 增
   `Status`/`Composer` 落盘字段；`SaveComposerDraft` 把未发送正文随草稿
   record 落盘（`Status=draft`），冷启动装配器恢复最近草稿（同 SID + 正文
   回填），物化成功后清空；`Snapshot.Session.composer` 供渲染层恢复，
   GUI 输入防抖保存。限定：当前恢复路径覆盖未绑定工作区的任务会话草稿，
   工作区草稿的 binding 落盘随 G4 完整归属。
3. **G2 严格 kind 白名单开启**：`EventHub.PublishSession` 按
   `ValidateSessionRouting` 拒绝会话类空 sid / 进程类带 sid 的发布并记诊断
   （`event.PublishDiagnostic`，默认 stderr）；订阅测试同步为新契约
   （草稿显式 ID 订阅、进程类全局事件以 resync 验证）。
4. **G1-C plan 额度按 sid 建槽（结构片）**：planExecutor 的
   PlanPolicy/PlanBranchBinding/run ID 收进按 sessionID 索引的槽表，
   提供 For 读写（SetPolicyFor/SetBindingFor/beginRunFor/CurrentRunIDFor）
   与隔离测试；SetBinding/beginRun 保留默认槽 legacy 别名保证单飞执行
   行为不变。运行路径的 For 化读取与 per-session effort 落位耦合，随
   G4 并行执行接线。
5. **G3 快照分型（首片）**：model 层新增 `SessionSnapshot`/`SessionRuntime`
   与 `ProcessSnapshot`/`ProcessRuntime`（进程级目录/能力清单移出会话制品）；
   `SnapshotOf` 统一返回会话粒度制品（活跃/后台同路径），capabilities 声明
   `session_snapshot`，协议版本不升；Bridge/fake 类型同步；契约测试钉住
   JSON 传输形状。桌面 Workbench 仍以联合 Snapshot 下发（进程段消费与前端
   reducer 分型属 G3 后续收口）。
6. **G4 join 证据可见可持久**：撤销 `view_state/coordinator.go` 的
   `SubagentContextMarker` 丢弃点；merge-back 注入引擎历史时同步写会话
   transcript（持久化事实源）与可见对话并发布 snapshot.changed
   （INV-G12：mainagent 实际接收的内容有记录）。测试：可见+transcript
   用例与原 mailbox 用例改写。
7. **G4 effort 归属进 Unit（数据面）**：`SessionUnit` 增 Effort 槽；
   SwitchEffort 写视图会话单元；runtime 投影与预算/任务装配按会话读
   effort（unit 优先、回退进程默认）；测试覆盖 per-session 隔离。

尚未完成（留给下一会话，见 target-design §9 波 2 剩余）：

- G1-C 剩余：planExecutor 的 binding/policy/fork **运行路径** For 化
  （`plan_run` 上下文绑定 per-run 携带）——槽结构已建，执行接线与 G4 的
  per-session effort 及 plan 运行上下文绑定耦合；fork 并发上限仍为
  process 单例（随并行执行落地）。
- G3 收口：桌面 Workbench Snapshot 的进程字段消费/前端 reducer 分型契约
  测试（模型层分型与 SnapshotOf 会话制品已落地，见上第 5 条）。
- G4 其余：effort/fullAccess/approval/子代理树/Composer 完整归属进
  `SessionUnit`（effort 与 join 证据记录已落地）；fullAccess/approval 的
  会话级门控、子代理会话按 `Kind=Subagent` 落盘/不进侧栏/经父树打开/重启
  标 `stale` 尚未做。

波 2 验证命令（本机 CGO_ENABLED=1，-race 为真实执行）：

```text
go build ./...                                   # 通过
go test ./... -count=1 -timeout=300s             # 通过（全仓）
node --test gui/frontend/dist/*.test.mjs          # 176 pass / 0 fail
```
