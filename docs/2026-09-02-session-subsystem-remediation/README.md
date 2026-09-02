# 会话子系统整改 · 打点表

> 来源：2026-09-02 会话子系统审查（会话切换/后台/并行、前端越界、会话过滤生态位）。
> 本文件是一次性工作包的进度台账，不是长期事实来源；完成的判据是代码与测试，
> 每完成一项即把 `[ ]` 改为 `[x]`。
>
> 阶段顺序：A（链路收口）→ D（同步缺口 + 测试凹陷）→ B（下沉越界逻辑）→ C（扩 API）。

## 阶段 A · 事件分发链路收口（会话过滤只留一处）

设计修正（相对审查原文）：草稿态下 GUI/TUI 无法预先知道会话 ID（ID 由首次提交时
`Engine.StartSession()` 生成），因此不由客户端"切换后重订阅"，而是让**视图归属由
application/core 权威解析**：`SubscribeSession("")` = 跟随当前视图会话，归属判定
只在 `session.Domain` 的视图指针一处。

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

- [ ] D6b `durable_history_test.go` / `router_storage_test.go` / `project_record_test.go` / `fork_test.go` 的发散式 `-race`（本轮补了 `session_granular` 与 `event_store`）。
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
- [ ] C2 `ArchiveSession`（`SetSessionMeta`/`GetSessionMeta` 已随 B6 落地）
- [ ] C3 `RequestCatalogRefresh()` 返回完成回执，删除 `app.js: beginNewSession` 补数据代码
- [ ] C4 事件面：慢订阅者策略（`SubscribeSince(seq)` 或增量读回）
- [ ] C5 轮询退场：`active-chat-sync.js` 1s 轮询、`refreshWorkTree`/`refreshGitLog`、`nodeDetailPollTimer` 改事件驱动

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

文档同步：`application/event/README.md`（订阅口径/DeliverySeq/resync 全局）、
`session/README.md`（actor 命令语义与关闭所有权）、`sessionstore/README.md`
（SQLite 单连接 + busy_timeout）、`gui/README.md`、`gui/frontend/README.md`、
`tui/README.md`、`docs/gui/modules/multi-session-pages.md`、
`application/core/README-*.md`（函数索引刷新）。
