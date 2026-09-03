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
  - 进度（波 3，2026-09-03）：`Core.Mu` 收口为 `ViewMu`（只护 Snapshot 与
    可见投影）；CatalogMu 独立（目录 worker/缓存/标题表）；会话可见投影写
    一律经 View.mu（访问器化）；TransitionLock 拆 per-session keyed
    （`SessionTransitionManager`，视图命令保留 view key）；hotAttachSession
    的持锁 workspace 端口查询移出临界区。**剩余**：task/prompt/context/
    plan 投影等协调器自有状态仍在 ViewMu 下（per-coordinator 锁 + 进程级
    引擎副作用清除后放开视图过渡 key，随波 4 G6 落地，见"波 3 尚未完成"）。
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

- [x] `session.StorePort` 生产零调用方（`session/store_adapter.go` 只被测试引用）：删除
  还是在刀 1' 接为 `session` 域唯一存储入口，二选一，不留死契约。
  —— **波 3 决策：删除**（提交 `195e875`）。适配职责已由
  `internal/adapters.SessionPort` 承担，消费端口定义在
  `application/core/session_runtime/ports.go`；session 包不留死契约。
- [x] 待审批计数在 TUI 的呈现口径。
  —— **波 3 决策：先定口径、落地随波 4**。TUI 现为 `Snapshot.Interaction`
  单格模态（`tui/dialog.go`），无待审批计数面；波 4 approval 会话级归属
  （Unit.Approvals + `awaiting_approval` 状态）落地后，口径 = 侧栏会话
  条目标记 `awaiting_approval` + 状态行待审批计数（跨会话总数），单格
  Interaction 只表达当前视图会话的审批。

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

### G4 子代理持久化形状对账（2026-09-03）

target-design §2.5 的「子代理按 `Kind=Subagent` 落五分片 + `SessionsOf`
按 kind 过滤」与现状实现（2026-08-24 用户约定：`NodeSessionRecord` 存于
主会话目录 `subagents/<main>-<sub>.json`，见 `sessionstore/node_session_store.go`
与 `seelebridge/runtime_subagent_recovery.go`）是**等价实现**，不需要迁移：

- 不进侧栏：node 记录不在主目录枚举路径内，天然不进会话目录/侧栏；
- 经父树打开：恢复锚点挂回父会话的 SubagentTree（节点带 subSessionID），
  GUI 经 Plan 树节点/`SubagentSessionDetail` 打开；
- 落盘：运行期 History/ContextSnapshot/Stages/Result/Worktree 持续落盘，
  终态结论（`seelex.subagent.result`）随父会话事件库持久化；
- 重启标 stale：崩溃遗留的 running/queued 记录经恢复路径标记为
  `interrupted`（提交 `77121f1`），父树合成根显示 interrupted，工作表条目
  映射 `TaskStatus=interrupted`，供用户在父会话指挥 mainagent 重跑。

若未来需要子代理出现在会话目录（归档/冷打开/独立管理），再做
`Kind=Subagent` 五分片迁移，不改变现网行为。

尚未完成（剩余项，见 target-design §9 波 2/波 3 剩余）：

- G4 其余：approval 的会话级归属与 awaiting_approval 状态——波 3 审计决策
  **延后到波 4**（证据见文末"波 3 执行与验证记录"）；Composer 工作区草稿的
  binding 落盘（草稿在 `BindWorkspace` 后 record 仍落默认项目，跨重启不可
  枚举——随 G4 完整归属收口；波 3 拆锁未显著降低其修复成本，保持延后）。

波 2 验证命令（本机 CGO_ENABLED=1，-race 为真实执行）：

```text
go build ./...                                   # 通过
go test ./... -count=1 -timeout=300s             # 通过（全仓）
node --test gui/frontend/dist/*.test.mjs          # 176 pass / 0 fail
```

### 波 2 收尾追加（2026-09-03，G1-C 收口 + G3 收口）

提交：`40ebd46`（G1-C 运行路径 For 化收口）、`0e53a03` +
`75d3fc0`（G3 前端分型契约与 reducer 进程段保留）。

1. **G1-C 收口（运行路径）**：plan_load/plan_validate/plan_run/resume 按
   执行 ctx 会话读取自己的策略、绑定与 run ID 槽（`runSessionID(ctx)` =
   telemetry 路由键）；runner 生命周期/节点事件一律使用本次 plan_run 的
   执行绑定（sink 写入 + `newPlanRunner` per-run locators），绝不回读全局
   默认槽；fork 的 ctx 由 Background 派生时重新注入会话 ID（fork plan_run
   与主会话同槽登记）；`SetBinding` 的默认槽 legacy 别名保留给无 sid 端口。
   application 在 `runChat` 起点按会话 effort 同步 plan 策略槽
   （`syncPlanPolicyFor`），后台会话不继承进程默认额度。测试：
   `seelebridge/plan_session_run_test.go`（策略隔离 / per-run runID 与事件
   归属 / 无槽会话不消费默认绑定 / kernel 节点投影携带执行会话）；
   `application/core/session_effort_test.go: TestPlanPolicySlotSyncPerSession`。
2. **G3 收口（前端分型）**：新增 `gui/frontend/dist/snapshot-shape.js`
   （SessionRuntime/ProcessRuntime/顶层键所有权表 + splitRuntime/
   classifySnapshot/assertTypedShape/processContextOf，与 Go 侧
   `application/model` DTO 键一一对应）；`client-state.js` 在快照边界保留
   桌面进程段，会话粒度基线（`capabilities.session_snapshot`）到达时与进程
   段合并渲染，session-only 的 `runtime.changed` 增量不抖动账户/插件/技能/
   模型面板；联合 Workbench 快照原样通过（对象引用不变）。契约测试钉住
   三形状分类、泄漏拒绝（INV-G1 前端镜像）、进程上下文跨载荷保留与增量
   穿透。桌面进程字段消费路径与归属表记录在 `gui/frontend/README.md`。

验证（本机 CGO_ENABLED=1）：

```text
go build ./...                                   # 通过
go vet ./seelebridge/... ./application/... ./internal/adapters ./gui   # 无告警
go test ./seelebridge ./application/core ./gui ./internal/adapters -count=1   # 全 ok
go test ./gui -count=1                           # 通过（TestBridgeRelaySubscribesToViewOnce
                                                 # 在全包并行下偶发一次，单独/复跑均绿）
node --test gui/frontend/dist/*.test.mjs          # 184 pass / 0 fail
```

### 波 2 G4 fullAccess 数据面（追加，2026-09-03）

提交 `9d42a98`：`SessionUnit` 增 FullAccess 槽（`FullAccessMode`/
`SetFullAccessMode`，与 Effort 槽同模式）；视图 `SetFullAccess` 记录本会话
选择并即时同步引擎门，未选择会话回退装配期捕获的进程默认（不继承其它会话
遗留的引擎门值）；view 协调器经 `CurrentFullAccess` 按会话投影（镜像
`currentEffort` 注入模式）；`runChat` 起点 `syncFullAccessFor` 保证每个
会话按自己的模式运行。fork 子单元不携带父运行期选择且互不 alias
（`TestS0ForkDeepCopyIsolation` 验收锚落地）。

```text
go build ./...                                   # 通过
go vet ./session ./application/core ./application/core/view_state          # 无告警
go test ./session ./application/core ./gui ./internal/adapters ./seelebridge -count=1   # 全 ok
```

### 波 2 子代理 interrupted 标记（追加，2026-09-03）

提交 `77121f1`：`dto.SubAgentNodeStatus`/`TaskStatus` 新增 `interrupted`；
恢复路径把崩溃遗留的 running/queued 节点记录标记为 interrupted（不再伪装
运行/排队），done/failed 原样保留，未知状态保守标 interrupted；父树合成根
在只剩中断/完成子节点时显示 interrupted；工作表映射显式分支
（`taskStatusForSubagent`）；plan-dsl/work-table 标签与 failed 色调 CSS
覆盖 interrupted。

## 波 3 执行与验证记录（追加，2026-09-03）

波 3 只做 G5 + 第 3 节顺带审计；提交（每个提交点全绿）：
`9b9ac2c`（ViewMu 收口）、`1110cc4`（CatalogMu 独立）、`546a991`
（View/Unit 访问器化 + 并行靶场）、`b69c33e`（TransitionLock per-session
keyed）、`0de02c9`（hotAttach 出临界区）、`195e875`（删除 StorePort 死契约）。

### G5 落地内容

1. **ViewMu**：`internal/state.Core.Mu` 更名 `ViewMu`（职责面 = Core.Snapshot
   与可见投影），目录/标题/会话单元不再借用这把锁。
2. **CatalogMu**：`session_runtime` 的目录三态（最近一轮枚举缓存、会话标题
   表、刷新回执队列）统一由 `catalogMu` 保护；worker 锁外做 SessionPort/
   WorkspacePort I/O，锁内只换内存态，发布 Snapshot 镜像时另取 ViewMu 短
   临界区；新增 `CatalogCache()` 观察口。archive 的 record 标题改经
   `catalogTitleOf` 原始读（不回退活跃会话名，避免后台落盘借用视图名）。
3. **Unit[i].Mu / 访问器化**：会话可见投影字段写一律经 View.mu
   （`SessionViewMutateLocked`/`SessionViewReadLocked`；流式增量、推理回挂、
   ReadFiles、工具状态写回、冷加载装载）；单元 View 指针注册后不再整体
   替换。`-race` 全量 application/core 期间发现并修复
   `TestStressConcurrentSessionsDoNotPollute` 的真实竞态：多个 runChat 起点
   并发写进程级引擎 fullAccess 门（测试桩加锁，镜像生产 PermissionGate
   语义）。
   验收锚 `-race ./gui` 期间定位波 2 台账记载的
   `TestBridgeRelaySubscribesToViewOnce` 偶发根因：Bridge 中继 goroutine
   持续 `Snapshot()`，与测试主 goroutine 的会话切换方法并发写
   `sessionAwareFakeApplication.snapshot`（测试桩无锁）——fake 增
   `snapshotMu` 修复，非生产路径缺陷。
4. **TransitionLock per-session keyed**：`SessionTransitionManager`（每 key
   一把显式 actor；同 key 串行、跨 key 并行；空 key 归一视图保留 key）。
   fork 落盘段按父会话 key；视图命令（BeginNew/Resume/Unload/Bind/
   SaveComposerDraft/Submit）保持视图 key；遗留单会话引擎全部归一到视图 key。
5. **出临界区化**：hotAttachSession 持 ViewMu 的 `Workspace.SessionWorkspace`
   查询（可能磁盘索引读）改为锁外一次并保存拷贝；审计其余锁定段（persist/
   目录/上下文装配/工具打点）的端口 I/O 均在锁外或纯内存态操作。
6. **测试**：`TestConcurrentStreamingViewSwitchNoPollution`（流式输出 × 多
   会话热切换 × Snapshot 读 × 目录刷新并发）、`TestCatalogCacheMirrorsWorker
   Round`、`TestCatalogCacheObservesProjectDiscoveredBindings`、
   `TestCatalogRefreshConcurrentWithTitleWritesAndSnapshotReads`、
   `TestSessionTransitionManager*`（同 key 互斥/跨 key 并行/关闭释放等待者）。

### 波 3 尚未完成（不在本波收口，台账保持开放）

- G5 剩余：task_context/prompt/context/plan 投影等协调器自有状态仍在 ViewMu
  下（后台会话的 task/plan 状态与视图写仍共享一把视图锁）；对应 per-
  coordinator 自有锁 + 把 task 镜像进 Snapshot 的耦合收口，随波 4 G6
  （驻留 LRU/驱逐）一并落地。波 4 需要先清除视图过渡里剩余的进程级引擎
  副作用（fork 的 StartSession 活跃别名、全局项目根绑定、legacy Router 写
  作用域），之后才可把视图命令从 view key 放开为 per-session key。
- approval 会话级归属与 awaiting_approval 状态（见下）。

### 顺带审计决策（波 3，证据见上）

- **approval 会话级归属：仍延后到波 4**。证据：`ApprovalRequest` 无
  SessionID 字段；broker 是进程级单例（`pending` 全进程一张表、observer
  单回调）；`Service.observeInteraction` 把打开的审批写进唯一的
  `Snapshot.Interaction` 并按**当前视图** sid 发布；`main.go
  newPermissionBridge` 用 `context.Background()`（无会话路由），
  `PlanApprovalGate.Ask`/`ask_approve` 的 ctx 虽已携带会话 ID（G1-T），但
  broker/observer/UI 三侧都没有按 sid 分格的承载。当前单格语义 = 任一时刻
  只有一个审批可见（后开覆盖先开），进程单飞期间成立；波 3 落地 per-session
  过渡/锁拆分后，跨会话并行面扩大，必须先做 ApprovalBroker 会话感知 +
  Unit.Approvals + awaiting_approval + UI 每会话待批列表才能声明归属正确，
  且波 3 验收锚的"审批并发"用例在无归属承载前只能断言"视图审批可解析、
  后台会话执行不受阻"（不虚报归属）。Composer 工作区草稿 binding 与 TUI
  计数口径同理延后（TUI 现无计数面，口径已定：侧栏 awaiting_approval +
  状态行计数，落波 4）。

### 波 3 收尾全量验证（追加，2026-09-03，提交 `5d3a741`）

```text
go build ./...                                   # 通过
go build -tags "gui,desktop,production" ./...     # 通过
go vet ./...                                      # 无告警
go test ./... -count=1 -timeout=300s             # 通过（全仓，零 FAIL）
go test -race ./session ./sessionstore ./application/... ./gui ./seelebridge -count=1
                                                 # 全 ok（真实 -race，CGO_ENABLED=1）
node --test gui/frontend/dist/*.test.mjs          # 184 pass / 0 fail
```

波 3 提交链：`9b9ac2c` → `1110cc4` → `546a991` → `b69c33e` →
`0de02c9` → `195e875` → `902ab98` → `5d3a741`。每个提交点
`go build ./...` 与受影响包测试全绿；收尾处执行上表全量门禁。

## 波 4 承接项决策（追加，2026-09-03，波 4 开工）

按本波执行提示第 3 节逐项先出结论与证据，再决定是否/如何实施：

- **approval 会话级归属 + awaiting_approval：本波实施**（波 3 记账给波 4 的
  显式承接）。证据链（波 3 审计 + 本次代码核对）：`ApprovalRequest` 无
  SessionID；框架 `toolspermission.ApprovalRequest` 已带 SessionID 但
  seelebridge 权限中间件未填（`seelebridge/tools/registry_state.go:161`）、
  `main.go newPermissionBridge` 丢弃并换 `context.Background()`；
  `ask_approve`/`PlanApprovalGate.Ask` 的 ctx 已携带会话路由键；broker
  进程级单表、observer 单回调；`Snapshot.Interaction` 单格按当前视图发布。
  实施分片：① 数据面（broker 会话化 + sessionstore/model 状态枚举扩展 +
  Unit Approvals/状态访问器 + 测试）→ ② core 门控与事件面（observe 按
  sid 路由、awaiting_approval 状态机、待批查询、审批并发用例升级为真归属
  断言）→ ③ 前端呈现（GUI 待批计数/侧栏状态 + TUI 状态行口径落地）。
- **Composer 工作区草稿 binding 落盘：保持延后**。证据：成本集中在
  「draft 在 BindWorkspace 后 record 仍落默认项目 + 跨重启恢复枚举路径」
  的存储归属收敛，与 approval/G6/G7 不共享改动面；bind 是视图命令，与
  per-session 过渡 key 放开无耦合，波 4 拆锁不显著降低其修复成本。
- **G5 剩余锁面（task/prompt/context/plan 协调器自有状态仍在 ViewMu 下；
  视图过渡 per-session key 放开）：仍延后**。证据：per-coordinator 拆锁
  需要 Snapshot.Task 镜像耦合收口 + 全部 task 读路径改造；视图过渡放开
  需要先清 fork 的 StartSession 活跃别名、全局项目根绑定与 legacy Router
  写作用域——各自是独立大改动面，不适合与 G6 驱逐/冷读在同一波并线；
  本波继续按既有锁序（ViewMu → catalogMu → Unit/View.mu）推进，不扩大
  锁面。
- 其余遗留待决若被触碰（TUI/headless 呈现面、`StorePort` 已删不留死契约），
  随实现显式记录。

实施顺序（每提交点全绿，`SKIP_BUILD=1` 提交）：approval 数据面 → core
门控/事件 → 前端呈现 → G6 驻留/驱逐 → G6 目录 projectID/C2 → C1 冷读 →
G7（EventStore 区间读/双轨桥/去轮询）。若本波无法在会话内全部完成，未完成
项与下一步留在「波 4 尚未完成」段，不把部分完成当完成。

## 波 4 执行与验证记录（追加，2026-09-03；本会话部分完成）

提交链（每个提交点全绿）：`b36d0ca`（承接项决策）→ `fe8dc53`
（approval 数据面 + core 门控）→ `4b68855`（approval 源头接线 + GUI 呈现）
→ `ca5c82a`（G6 驻留 LRU）→ `12901ee`（G7 EventStore 区间读）→
`5eadfb5`（-race 修复：投影收集改经 session actor 读视图指针）→
`3ddb55b`（core README 函数索引刷新）。

### 本会话已完成

1. **approval 会话级归属 + awaiting_approval（承接项，数据面 → core →
   前端）**：
   - broker 会话化：`ApprovalRequest.SessionID`、observer 三参
     `(sessionID, requestID, interaction)`、`Pending()/PendingBySession()`
     待批查询（`application/approval/broker.go`）；`contract.ApprovalBroker`
     同步扩展。
   - 状态枚举扩展：sessionstore.Status 与 model.SessionStatus 增加
     `awaiting_approval`/`archived`；`SessionUnit.ApprovalIDs` 槽 +
     `Status()` 提升 awaiting_approval（Unload 拒绝）；`SessionUnit.resident`
     标记（G6 共用）。
   - core 路由：`observeInteraction` 按所属 sid 记账（Unit.Add/Remove
     Approval）、单格 `Snapshot.Interaction` 只镜像当前视图会话/空归属审批；
     `ResolveInteraction` 增加跨会话按 id 结案回退；hotAttach 激活后镜像
     目标会话首笔待批；`sessionStatusLocked` awaiting 优先；`SessionInfo`
     下发 `ApprovalCount`/`Resident`；`SnapshotOf` 填充 `Approvals`。
   - 源头接线：seelebridge 权限中间件把调度 ctx 会话 ID 写入
     `toolspermission.ApprovalRequest.SessionID`（根包注入
     SessionFromContext）；main.go 权限桥/ask_approve/PlanApprovalGate
     随请求携带会话归属。
   - 前端：侧栏 awaiting_approval/archived 状态徽标 + 状态行「待审批 N 项」
     （app.js/styles.css）；TUI 无会话列表/状态行面，口径已文档化（见
     「波 4 尚未完成」#5）。
   - 测试：`approval_session_ownership_test.go`（状态/单格/快照/跨会话按
     id 结案/两会话并发归属）、`broker_test.go` 会话归属与待批查询、
     `seelebridge/tools/permission_session_test.go`。
2. **G6 驻留 LRU（INV-G8 主骨架）**：
   - `seelexctx.Limits.ResidentSessionLimit`（`resident_limit`，默认 6，
     WithDefaults/负数校验同步）。
   - core 会话治理侧 LRU（`application/core/resident_lru.go`）：
     `residentOrder`（ViewMu 护）；冷加载/热切换/物化 touch；
     超限按最旧优先驱逐：非当前视图、非 running/queued/awaiting_approval、
     驱逐前非活跃会话 `PersistCurrentSession` flush → 引擎
     `UnloadSession` → Unit/View/Runtime 槽保留（Resident=false）→ 重开
     走 cold_load（驱逐后再进 = 冷）。
   - 测试：`resident_lru_test.go`（LRU 顺序/驱逐、busy 会话守卫、默认值 6）。
3. **G7 第一片：sessionstore `EventStore.LoadRange`**——按会话/Seq 区间
   （含端点）读回统一事件库，倒置区间显式报错，(0,0) 保持 Load 全量语义
   （`event_store_test.go`）。
4. **-race 修复（本波触出）**：`view_state` 投影收集改经注入的
   `CurrentSessionID`（session.Domain.ActiveID，线程安全）判定视图/后台
   分区，不再无锁读 `Snapshot.Session.ID`（与 INV-G2「视图指针唯一持有者
   在 Domain」一致）。

### 波 4 尚未完成（后续会话，逐项下一步）

- **G6 目录按 projectID 索引**：`RequestCatalogRefresh` 补 projectID 维度，
  目录缓存/镜像（catalogSessions/catalogWorkspaces/标题表）按项目分格；
  同步桌面 joint 快照进程段与 `docs/gui/modules/multi-session-pages.md`/
  `snapshot-shape.js` 契约。现状：枚举源头已按 projectID（SessionsOf），
  缓存仍是合并后的单一列表。
- **C2 `ArchiveSession`**：命令/门控 + 目录归档过滤（archived 状态已进
  枚举与前端徽标，存档写入与过滤未接）。
- **C1 冷读面**：`ListSessions`/`SnapshotOf(非驻留)`/
  `GetSessionTranscript(range)`；替换 `session_scope.go` 的
  `cloneRuntimeState` 回退为从 record/Transcript/DurableHistory 冷拼装。
- **G7 剩余**：UnifiedEvents「投进 application/event」的一跳（EventStore
  区间读已具备）；`runtime_live` 子代理 assistant 正文增量 kind（需先定
  LLM 输出内容源：OnLLMComplete/telemetry effect 或节点会话历史增量）；
  删除 `app.js` 的 `nodeDetailPollTimer` 并把「去轮询后详情仍新鲜」契约
  测试落地（现有 TestEmbeddedFrontendExists 的禁轮询断言届时启用）。
- **TUI 待批计数面**：TUI 现无会话侧栏/状态行；口径已定（侧栏
  awaiting_approval + 状态行跨会话计数），呈现需在 TUI 增加会话列表或
  状态行后落地，本会话未做（避免在无计数面处空造 UI）。
- **Composer 工作区草稿 binding 与 G5 剩余锁面**：按「波 4 承接项决策」
  保持延后，本波未触碰。

验证（本机 CGO_ENABLED=1，-race 为真实执行）：

```text
go build ./...                                   # 通过
go vet ./...                                      # 无告警
go test ./... -count=1 -timeout=300s             # 通过（首轮全量出现一次
                                                 # Windows 临时文件锁类 FAIL，
                                                 # 立即复跑零 FAIL；判定与
                                                 # 历史台账同类的环境抖动）
node --test gui/frontend/dist/*.test.mjs          # 184 pass / 0 fail
go test -race ./session ./sessionstore ./application/... ./gui ./seelebridge -count=1
                                                 # 全 ok（含 TestStress*、
                                                 # TestApproval*、
                                                 # TestResidentLimit*）
```
