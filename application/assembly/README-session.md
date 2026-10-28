# assembly/session（根包分卷）

## 生态位

会话草稿/恢复/存储用例与集成测试；运行中切到未驻留会话走异步冷加载（restoring 空壳 + 后台装载 + epoch 判定发布基线）

覆盖：`conversation_session*.go`、`edit_fork*.go`、`session*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 会话切换与审批观察（A5.2 / A5.3）

hot attach 的锁外准备、`ViewMu` 内快照事务及锁外发布由
`application/session.AttachApplication` 唯一拥有；assembly 通过
`application/session/history` 的窄端口适配既有 coordinator/residency/workspace/task
机制。审批的会话归属、视图单槽镜像和 open/close 事件由
`application/session.InteractionObserverApplication` 唯一拥有。

## 查询端口（2026-10-25，AP-004/AP-005）

`Session query application` 提供会话目录、项目根、展示元数据、目录刷新等待、
`SnapshotOf`、transcript 区间和全量输入索引的只读对象；
`Conversation history query application` 提供历史分页窗口对象。二者只持窄读依赖，
不持完整 `Service`。历史冷读在共享 `ViewMu` 之外执行，锁内只安装可见窗口；
公开 `Service` 方法只做一行转发。回归见
`session_conversation_history_query_test.go` 与根包
`architecture_budget_test.go`。

AP-108B1 起，`SessionRuntimeOf` 与 Goal 治理历史克隆由
[`application/session/query`](../session/query/README.md) 唯一拥有；assembly 的
`SnapshotOf` 只调用该纯查询。
AP-108B2a 起，Snapshot 冷读/驻留/内容 LRU 路径选择也由该包唯一拥有，assembly 的
`readSnapshotOf` 只提供状态采样与两个 builder。
AP-108B2b 起，驻留 Snapshot 的字段组装、Task/Plan 克隆和 WorkTable enrichment
也由该包完成。

## Session unload 应用编排（AP-010 首个切片）

`Session lifecycle application` 的 `sessionLifecycle` 集中过渡锁、持久化、
内存释放、视图续接和发布顺序。允许/拒绝 unload 的判据仍只在
`middle/switch.Unload`；`Service.UnloadSession` 保留一行转发。

## Session delete 应用编排（AP-012 首个切片）

`Session delete application` 集中删除 record、后台命令终止、完整内存释放、
workspace 解绑、fresh draft 续接和快照发布。记录删除失败直接返回；记录已删除后
的内存释放失败只记日志，保持既有“不制造不可重试删除失败”的语义。

## 显式草稿提交激活（AP-024 首个切片）

`Session draft submission application` 集中显式提交命中未物化草稿时的激活顺序：
廉价目标预判 → 获取视图过渡锁 → 锁内草稿归属复判 → 拒绝过期视图 →
裁剪首输入并物化。草稿槽、引擎激活和 Session 记录所有权不变；
`Service.materializeDraftForSubmit` 只保留一行组件转发。

## 草稿物化编排（AP-025 首个切片）

`Session draft materialization application` 集中首请求草稿物化：读取草稿槽与当前
workspace、绑定项目根与写作用域、打开目标 Session 引擎、生成首请求标题、
提交 Session/Plan/workspace 镜像、发布 runtime/snapshot、刷新目录、落盘工具
记录并登记驻留。错误文案、锁外 I/O 和 ID 复用语义保持不变。

## 新建会话进入草稿（AP-026 首个切片）

`Session draft entry application` 集中 `BeginNewSession`：关闭/排空预检、空闲旧会话
保存、历史与 context 释放、Skill 清理、草稿槽复用/创建、持久档位读取、草稿壳
切换，以及 runtime/snapshot、task 和子代理树的锁外刷新。运行中的旧会话不被
保存、清历史或解绑 context；`Service.BeginNewSession` 只保留一行转发。

## Resume 顶层编排（AP-027 首个切片）

`Session resume application` 集中 `resumeSession` 的应用分支：空 ID 校验、按会话获取
过渡锁、hot/restoring/cold 判定、同目标装载在途时的视图归位、同步/后台冷加载
选择，以及失败恢复调用。resume 文档注释保留在旧入口；冷加载主体、wire 装配、
Engine 恢复与 epoch 发布仍由既有实现拥有。

## Edit/Fork 幂等操作注册表（AP-028 首个切片）

`Edit/Fork operation registry application` 独立拥有编辑/分叉的 operation map、TTL、
完成等待 channel 和失败清理。相同 `operation_id` 与相同请求复用原 operation；
复用给不同 parent/request 时明确拒绝。该状态已从 `serviceState` 移除，并由
组件自己的 mutex 保护，不再占用 `Core.ViewMu`。

## Edit/Fork 父会话准入（AP-029 首个切片）

`Edit/Fork validator application` 集中编辑前检查：父会话必须是当前会话、不得运行中、
不得有排队输入；非零 expected revision 必须匹配、必须存在保留窗口，目标消息
不得落在压缩前缀中。判据顺序和错误文案与迁移前一致。

## Edit/Fork 目标事件解析（AP-030 首个切片）

`Edit/Fork target resolver application` 负责加载父事件、计算最大 EventSeq 作为当前
revision、定位目标用户输入、拒绝 assistant/internal/wire-only 目标，并复用
`store.EventParagraphs` 要求目标位于完整段落起点、fork cut 取前一段落末尾。

## Edit/Fork child 原子编排（AP-031 首个切片）

`Edit/Fork process application` 固定一次编辑分叉的顺序：解析目标、校验父会话、在目标
会话过渡锁内 fork、锁外恢复 child、提交编辑文本，任一切换/提交失败都走既有
child 删除 + 父会话恢复回滚。fork/resume/submit/rollback 本体仍由既有 Session
能力拥有。

## Edit/Fork 回滚错误合成（AP-032 首个切片）

`Edit/Fork rollback application` 集中 child 删除与父会话恢复，并在任一侧失败时把诊断
正文追加到原始编辑/提交错误；原 cause 始终保留为 `%w` wrapped error。

## Edit/Fork 可见消息编辑判据（AP-033 首个切片）

`Edit/Fork annotation application` 复制可见消息并为用户输入回填权威 EventSeq，再按
running、缺序号、无保留窗口、压缩前缀四类判据写 `Editable` 与
`EditDisabledReason`。assistant 和非可见用户行保持不可编辑。

## Fork lineage 只读解析（AP-034 首个切片）

`Session fork lineage application` 从会话 context state 中读取 `forked_from`，完成
`store.SessionContextRecord` 与 `model.SessionForkRef` 解码。未装配 fork 端口、
读取失败、空 payload、损坏 record/ref 或 parent ID 为空时都返回 nil，不制造
不可读状态。

## Restoring 空壳投影（AP-035 首个切片）

`Session restoring shell application` 统一冷加载空壳投影：把目标会话置为 restoring、
切换到 Active、清空上一会话的 Task/Plan/Todo/SubAgent/WorkTable/Teamwork/
Interaction，镜像目标 ChatState、同步可见视图并推进 revision。
`beginAsyncRestore` 与重复 Resume 的视图归位共用这一份字段清单。

## 冷加载失败恢复（AP-036 首个切片）

`Session restore recovery application` 在同一 `Core.ViewMu` 临界区内清除 restoring 并
判定目标是否仍为 active、epoch 是否仍最新。最新且仍 active 时优先 hot attach
前一会话，失败或无前一会话则重置 fresh draft，最后发布 Bridge 视图通知。

## 冷恢复 draft reset（AP-037 首个切片）

`Session restore draft reset application` 统一无可用回退时的草稿兜底：视图锁内复用或创建
草稿槽、切换 fresh draft shell 并 bump revision，锁外发布 runtime/snapshot。

## 冷加载数据读取（AP-038 首个切片）

`Session cold load reader application` 集中冷加载持久化读取阶段：先恢复 pending tail，
再并行读取 SessionRecord 与 transcript；无 record 时回退 legacy history；保持
record/transcript 错误优先级，record 存在时忽略旧 history/transcript 错误。

## 冷加载 history 装配（AP-039 首个切片）

`Session cold history application` 把读取结果装配为 Engine 恢复历史：record transcript
为空/落后时从 Conversation 重建并标记 renumber；优先 wire history；缺失最新
用户输入时回退 record resume history；最终空结果回退
`session_runtime.RecordResumeHistory`。

## 冷加载 Engine 恢复与队列回填（AP-040 首个切片）

`Session cold restore application` 按固定顺序登记 session workspace、恢复/替换 Engine
history、恢复 durable queue 并回填未确认输入，最后写入目标会话 system prompt。
每个失败出口保留原错误文案，后续步骤不继续执行。

## 冷加载 workspace 绑定（AP-041 首个切片）

`Session cold workspace application` 在冷加载锁外处理 workspace：有元数据时在安全条件
下重绑全局根并始终写 per-session binding；元数据缺失但 workspace ID 仍存在时
保持绑定不动；只有 workspace ID 为空才解绑全局根、session binding 和 runtime
per-session root。

## 冷加载激活准备（AP-042 首个切片）

`Session cold activation application` 在 `Core.ViewMu` 内执行装载发布判定：同步清除
restoring；按 epoch/active 决定是否切换 Snapshot Session 与 Active；从 record
或 legacy history 选标题；清 SessionUnit cancel；应用持久权限档位；写入 legacy
与 record 标题。Task/View 安装仍在主流程。

## 冷加载 Task 恢复（AP-043 首个切片）

`Session cold task application` 计算 transcript seq 与 projection 上限，优先恢复
record 旧址 compaction，空时读取新 compaction 通道；仅在 transcript 未重建时
推导 retainedFrom；最后按目标 Session 恢复 Plan/Task/checkpoint/tool result。
无 record 时走原有 reset。

## 冷加载 View 构造与安装（AP-044 首个切片）

`Session cold view application` 构造冷加载可见窗口：有 record 时安装恢复标记、record
tail 和 ReadFiles，且只在目标可见会话为空时覆盖；active 装载镜像 ForkedFrom/
Task。无 record 时 append legacy history 并修正 total/offset/has-more。

## 冷加载解锁后发布（AP-045 首个切片）

`Session cold publish application` 在锁外写入必要的全局 prompt、挂接 context 四栈；
active 装载再恢复 task registry、子代理树与 anchors，刷新并发布 runtime；
所有装载完成后刷新 catalog、resident LRU 与 content LRU。context 失败在运行面
切换与发布前返回。

## 冷加载锁内收口（AP-046 首个切片）

`Session cold commit application` 在 `Core.ViewMu` 内追加 pending-tail notice、镜像
ChatState；active 装载再镜像 Plan、清除 Interaction、应用 workspace，并 bump
revision；system prompt 在同锁内取快照文本后交给锁外发布。非 active 装载不改
共享 Plan/workspace/revision。

## 冷加载紧凑读面（2026-10-24）

冷加载把事实源看成“compact 指针 + 指针后的后缀”，不再把 provider history
当作 v8 record 的重复来源：

1. `recoverPendingMessageTailAtLoad` 只检查 `head.Bytes` 之后的末分片后缀与
   head 未索引的分片，不重读已发布前缀。
2. `LoadSessionResumeRecord` 从最新 compact 帧的 `MessageToSeq+1` 派生可见
   后缀，并带回完整总数；没有 record 的旧格式会话才调用
   `LoadHistoryTailWindow`。
3. `AssembleWireHistoryWorkspace` 仍由 compact 摘要 + 后缀 + 最近 K 次尝试
   生成同一份 provider 上下文。回归用例比较冷重启前后的
   `PrefixDigest`（SHA-256），要求逐字一致。

性能夹具：`store/session_cold_load_bench_test.go` 提供同现场
`parallel_reads_legacy` / `parallel_reads` A/B；
`session_recovery_bench_test.go` 对照应用层 hot/cold 恢复路径。
冻结指针之前仍保留完整 record 回退，不改变旧会话可见性。

## Draft 双指针周期快照（2026-10-24）

长任务只维护两个内存序号：

1. `left`：已经写入 message 草稿尾的最大 seq；
2. `right`：当前 transcript 的最新 seq。

每 5 秒尝试追加 `(left,right]`，成功后推进 `left`。短于一个周期的回合不做
draft 文件 I/O；流式 chunk、工具步骤等高频动作只更新内存 transcript。回合
收尾先停止快照循环，再由 `PersistCurrentSession` 按既有提交协议一次性发布。
如果中途 checkpoint 已发布部分行，下一次草稿追加由存储层跳过已发布 seq；
不会重复占号。快照失败保持左指针不变并在下个周期重试；能力未装配或回合取消
时停止循环。回归见 `session_draft_snapshot_test.go`。

## 历史分页契约（2026-09-11 / 2026-09-25 / 2026-09-29）

`LoadMoreHistory` / `LoadLatestHistory` 是 GUI 顶部 sentinel 与历史栏的应用
边界，语义收口如下：

1. **一页 = 一整窗**（`limits.history_window`，入参只当上限建议）：半页会让
   「窗口」与「页」两个尺寸混在一起，用户翻一次只多出半屏又丢掉半屏。
2. **分页态写进会话可见投影**（事实源）后再镜像 Snapshot：`Snapshot` 是活跃
   会话的只读镜像，只写镜像会在下一次镜像（新消息/工具事件/切换）被整体抹掉，
   offset 退回尾部——表现为「点了加载更早，内容回卷，再点还是同一页」。
3. 窗口 = `[HistoryOffset, HistoryOffset + 窗口条数)` 的连续区间：窗口整体后退
   一页，可见列表不会无限加长（WebView 渲染内存有硬上限）。
4. 回看期间（窗口未贴尾）新消息不回卷窗口、也不进可见列表；`LoadLatestHistory`
   重新读尾部窗口贴尾，并带回回看期间的新消息——但只带得回**已发布**的那部分，
   见第 6~9 条。**用户行例外（2026-09-28）**：回看期间到达的 `user` 行开启新一轮
   对话，追加前先把窗口原地重置为「以这条新消息为尾」（`endBrowsingForNewTurn`）
   ——用户发言意味着「从现在起看最新」，否则这一轮自己的输入落在窗口之外，前端
   reducer 在 `historyWindowed` 时也不落 `message.added`，表现为「输入被吞、整个
   回合界面一动不动」。
5. 无 record 的旧格式会话冷加载同样写入 `TotalMessages/HistoryOffset`（历史
   总数来自 provider 历史），否则 `HasMoreHistory` 恒为 false，早期历史读不到。
6. **冷读面的右界是发布点**：回合内的可见行先进内存窗口，`PersistCurrentSession`
   要到 runChat 收尾才落盘，而存储侧读者闸门是 message 通道的 `head.LastSeq`
   （`decodePublishedRows`）。所以「内存已可见总数 > 磁盘已发布数」在运行中的
   会话里是常态，差额就是本轮尚未落盘的行。
7. `installVisibleHistory`（三条加载方向入口的唯一安装点）据此守三条：已可见
   总数以**会话可见投影**为准（冷加载基线 + 追加路径共同维护），只增不减，窗口
   不得越过总数；磁盘的未过滤总数只回答「这一页读到发布点了吗」，不参与总数——
   它多算的正是内部标记行（「窗口永远差几格够不到尾」就是它造成的）；窗口位置由
   **行身份**（接缝）推出——前置页接在窗口首行之前、尾页把窗口里发布点之后的热尾
   接回页尾；接缝对不上（窗口首行不在磁盘上）时不假装连续，宁可窗口原地不动。
8. **热读短路**：内存窗口已经贴着有效尾时 `LoadLatestHistory` 直接返回——它
   已经在最新处，再冷读只会换上更旧的已发布一页。这条短路就是「加载着加载着
   只剩冷加载内容、尾部没了，一出工具结果又像是恢复了正常」的修复点。
9. 尾部窗口读先探发布点、再向左**扩读**补齐一窗可见行（`loadConversationTailPage`）：
    尾段整段是内部标记行时（回合收尾刚落 checkpoint）不再拿到空页；「加载更早」
    同理按可见下标起读、几何扩读直到接缝（窗口首行）出现。尚未落盘又已滑出窗口
    的行要等这次提交后才能回读（有界滑动窗口的固有语义）。
10. 复现与回归：`session_history_pagination_test.go`（分页态随会话走）+
    `session_history_hot_tail_test.go`（在飞尾部不被冷读抹掉、总数不倒退、
    offset 与内容对齐）+ `session_history_browsing_submit_repro_test.go`
    （回看期间提交的用户行必须回到窗口，「输入被吞」的复现）+
    `session_history_coldload_latest_repro_test.go`（冷加载后「回到最新」必须
    贴尾，尾窗整段被内部行占掉时同样必须贴尾）。

## 提交归属：草稿与运行中会话（2026-09-20）

渲染层把每一条普通输入都显式钉到「当前视图会话 ID」（`SubmitToSession`），包括
草稿的早分配 SID。本卷因此必须自己守住两条边界：

1. **显式提交目标是未物化草稿时，先物化再判定是否加载**（`materializeDraftForSubmit`
   → `materializeDraftSession`）。草稿没有可冷回读的历史：走 `ActivateSession` 会把
   那条 `Status=draft` 的 record 当冷会话装载，新会话以「已恢复会话: seelex-…」开头、
   草稿槽位不消费、composer 不清理（重启后已发送的正文会回到输入框）。
2. **归属不符要显式失败**：草稿槽是进程单例，物化会把共享视图镜像切到该 SID，因此
   只有视图仍停在这份草稿上时才允许物化，否则返回 `ErrDraftNotInView`——渲染层拿着
   过期快照提交时，明确失败比把输入投给别的会话安全。多页签各自持有 composer 后这条
   判据要随草稿槽一起改成分布式（见 `docs/2026-09-02-session-subsystem-remediation/target-design.md`）。
3. **任何生命周期命令都不得经进程级活跃别名读写引擎**（`Engine.History()` /
   `ClearHistory()` / `SetSystemPrompt()`），一律用 `engineHistoryFor` /
   `clearEngineHistoryFor` / `SetSystemPromptFor` 按会话路由。别名指向哪个会话不可
   预期，可能是另一个正在运行的会话——它的 framework `Session` 锁被 `ChatStream` 从
   进函数持到出函数，任何别名调用都排在整轮之后。
4. **别名阻塞的放大效应来自过渡锁**：生产宿主 `PerSessionExecution() == false`，
   `transitionForSession` 一律回退视图 key，所以「一次点击排在运行中会话之后」会连带
   冻住所有会话的 resume/unload 与 ambient 提交。审查生命周期方法时先问：锁内有没有
   可能等另一会话引擎的调用？
5. 复现与回归：原 `session_running_idle_submit_test.go` 已删除，用例散在 `session_draft_*` 与 `service_test.go`（草稿显式提交物化、运行中会话
   不挡新建会话）+ `session_submit_restoring_test.go`（restoring 期延后提交）。

## 会话目录里的「活着但还没落盘」（2026-07 用户口径）

目录的事实源仍是存储层枚举（`SessionsOf` = 会话头），而会话只在**回合收尾**落盘
（`runChat` 的 `PersistCurrentSession`）。中间那段时间以前是整段空窗：视图会话靠
前端补一行"当前会话"勉强可见，后台会话（定时任务新建的）则完全看不到——用户口径
是"会话条目消失，直到完成才重新出现""看不到定时任务的会话条目"。

`Snapshot()` 因此在目录镜像上补两格**视图级事实**，不动存储、不造磁盘条目：

1. `projectDraftWorkspaceLocked`：草稿的工作区绑定补进 `session_workspaces`。
   草稿绑定本来就是视图级事实（`workspace.bindInfo` 的草稿分支刻意不写
   `Store.BindSession`，要等物化才落存储），缺这一格左侧栏只能把刚发起的
   「工作区会话」归进「未关联会话」。反向判据不变：没有工作区的草稿仍然是空的。
2. `withLiveSessionRowsLocked`：内存里 running / queued / awaiting_approval 但目录
   还没有的会话补一行（标题取 `CatalogSessionTitle` 的原始值，绝不借当前视图会话
   的名字）。idle 不补——空闲会话要么已经落盘（目录本来就有），要么是不该进会话树
   的单元；已落盘的行一律以目录为准，不覆盖它的时间/token/元数据。

`StartScheduledSession` 另有一格：会话创建后立刻重采工作区投影并发布一次
`snapshot.changed`——定时触发发生在用户视野之外，不主动发布就只能等这一轮跑完
（旧的 `RequestCatalogRefresh` 对未落盘会话是空转）。

回归：`session_directory_visibility_test.go`（工作区草稿分组 / 任务草稿仍不绑定 /
首轮运行中可见 / 定时会话落盘前可见）。

## 会话上下文存储的挂接（2026-10-04）

`Runtime` 的 `SessionContextStore` 是会话级四栈与**装配层推帧**的共同依赖，且读的是
**当前活跃 bundle**（`seelebridge.Runtime.sessionContextStore()`）——因此"让某个会话成为
当前会话"的每条路径都必须把**该会话自己的** store 挂上去
（`Service.attachSessionContextFor`，落在 `session_history.go`）：

1. 冷恢复 / 热挂载（`resumeSession`）；
2. 新建会话（`materializeDraftSession`，含冷启动草稿物化）——早分配 SID 就是最终键，
   全新键上 `Load` 只落到空记录（各通道 not-found 按 S19 口径静默初始化）；
3. 切项目另起的独立会话（`middle/switch/workspace` 的 `bindInfo` 换项目分支）。

漏一处，那个会话的**整段活跃期**都处于"未绑定"：`PushCompactionFrame` /
`ReadbackCompactionSummary` 直接报 `会话上下文存储未绑定（压缩栈不可用）`
（帧正文因此写"没有模型生成的读后感"），栈块（plan/task/skill/compact）与记忆块一律为空。
对照：控制器路径 `runtimeCompactStacks` 在 store 为空时**退回内存栈**，装配层这条路径
**硬失败**——同一状态两处语义不一致，这正是缺口以报错显形的地方。

另一半是"离开会话"：`BeginNewSession` 只在**被离开的会话空闲时**解绑
（`!currentRunning`）。`DetachSessionContext()` 解绑的是当前活跃 bundle，而跑着的会话
仍在装配 provider 上下文——解绑会让它的推帧退化成同一个错误；空闲会话的"离开即解绑"
（防四栈串台）口径不变。

复现与回归：`session_draft_context_attach_test.go`（三处挂接 + 运行中会话不解绑）、
`backend/engine/assemble/runtime_compaction_unbound_test.go`（未绑定的确切报错文案）。

## 用户消息编辑分叉（2026-10-25）

`EditUserMessageAndFork` 是 GUI 编辑用户消息的唯一写入口，语义固定为：

1. 目标 `TargetEventSeq` 必须是保留窗口内、属于完整段落起点且不是内部注入的真实
   user 事件；
2. fork cut 取目标段落**上一段**的 `EventTo`，因此原用户消息及其后续回复不进入 child；
3. child 创建后自动切换，并把修改文本交给既有 `submitConversationFor` 作为首条新输入；
4. `ExpectedRevision` 是打开编辑器时的会话事件最大 Seq，`ForkRequest.ExpectedEventSeq`
   在真正读盘构建 child 时再次校验，避免父会话在编辑期间推进；
5. `OperationID` 在应用层做短期幂等：双击/重试复用同一个成功 child；失败不缓存，
   允许修正后重试；不同 operation 即便同一 fork 点也创建不同 child；
6. child 首轮启动失败时删除本次刚创建的 child 并恢复 parent 视图。

快照中的 `Message.EventSeq`、`Editable`、`EditDisabledReason` 与
`Snapshot.ConversationRevision` 都由应用层生成：保留窗口起点从
`TaskExecutionState.ContextRetainedFrom` 下标映射到首个保留事件的 Seq，前端不得按
已加载消息数或本地滚窗判断。子会话 `SessionState.ForkedFrom` 含来源
`SourceEventSeq/SourceMessageID`，冷恢复从会话 context 的 `forked_from` 元数据回填。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### session_cold_read.go

- `func (service *Service) newSessionForkLineage() *applicationsession.ForkLineageApplication` — newSessionForkLineage binds the stateless lineage decoder to the narrow
- `func (service *Service) snapshotOfCold(sessionID string) (model.SessionSnapshot, error)` — snapshotOfCold 组装未驻留会话的只读会话快照（record 事实源）。目录已归档
- `func (service *Service) readSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error)` — readSessionTranscript 是查询组件的宿主读面：只走事件库，不进入共享写锁。

### session_draft.go

- `func (service *Service) newDraftSessionIDLocked() string` — newDraftSessionIDLocked 生成早分配的会话 ID（调用方持有 Core.ViewMu）。
- `func (service *Service) enterDraftShellLocked(options draftShellOptions) *session.SessionUnit` — enterDraftShellLocked 把视图单例切到草稿壳（调用方持有 Core.ViewMu），返回草稿的会话单元。
- `func (service *Service) isUnmaterializedDraftTarget(sessionID string) bool` — materializeDraftSession 为首条请求创建引擎会话与项目绑定：复用早分配
- `func (service *Service) draftTargetLocked() switching.DraftTarget` — draftTargetLocked 把草稿归属判据的两个来源（进程单例草稿槽 + 共享视图镜像）装成

### session_draft_snapshot.go

- `func (service *Service) startDraftSnapshotLoop(ctx context.Context, sessionID, requestID string) func()` — startDraftSnapshotLoop 为一次运行中的回合维护 draft 双指针：
- `func (service *Service) draftSnapshotBaseSeq(sessionID, requestID string) (uint64, bool)` — draftSnapshotBaseSeq 找到本回合首条 transcript 事件，返回其前一个 seq。
- `func (service *Service) persistDraftSnapshotSuffix(sessionID string, left uint64) (uint64, bool, error)` — persistDraftSnapshotSuffix 把当前 transcript 中 seq > left 的稳定后缀追加
- `func (service *Service) transcriptSnapshot(sessionID string) []model.TranscriptEvent`

### session_lifecycle.go

- `func (service *Service) hotAttachSession(sessionID string) error` — hotAttachSession 热加载：目标会话已驻留（引擎实例在内存，含运行中），
- `func (service *Service) switchViewToFreshDraftLocked()` — switchViewToFreshDraftLocked 把视图切到一个**就地新建**的早分配草稿单元（调用方

### session_pending_tail.go

- `func (service *Service) recoverPendingMessageTailAtLoad(sessionID string) string` — recoverPendingMessageTailAtLoad 在会话冷加载路径上探测/恢复草稿尾，返回要补进可见会话的
- `func (service *Service) pendingTailDeps() switching.PendingTailDeps` — pendingTailDeps 装配草稿尾决策的宿主面（每次调用构造一次值语义的 Deps；无状态）。
- `func (service *Service) pendingTailSessionRunning(sessionID string) bool` — pendingTailSessionRunning 报告目标会话是否正在跑（在飞写者）。
- `func (service *Service) appendPendingTailNoticeLocked(sessionID, notice string)` — appendPendingTailNoticeLocked 把草稿尾恢复的结论作为一条 system 行补进目标会话的可见投影

### session_permission_tier.go

- `func (service *Service) tierDeps() applicationpolicy.TierDeps` — tierDeps 装配档位用例的宿主面（每次调用构造一次值语义的 Deps；无状态、无锁）。
- `func (service *Service) tierGate() applicationpolicy.TierGate` — tierGate 返回"把档位写进执行门"的闭包；宿主没装配运行面时整步 no-op（与迁移前同口径）。
- `func (service *Service) settingPort() session.SessionSettingPort` — settingPort 返回会话端口的可选"会话级用户设置"扩展（未装配 = 档位退回内存态）。
- `func (units serviceTierUnits) UnitPermissionTier(sessionID string) (string, bool)`
- `func (units serviceTierUnits) SetUnitPermissionTier(sessionID, tier string)`
- `func (view serviceTierView) ViewSessionID() string`
- `func (view serviceTierView) SnapshotSessionID() string`
- `func (view serviceTierView) WriteTier(sessionID, effective string)`
- `func (service *Service) persistPermissionTier(sessionID, tierValue string) error` — persistPermissionTier 把档位写进会话级设置（"" = 清除该会话的选择）。
- `func (service *Service) readStoredPermissionTier(sessionID string) string` — readStoredPermissionTier 读取该会话持久化的权限档位（"" = 从未选择 / 未装配 / 读失败）。
- `func (service *Service) applyStoredPermissionTier(sessionID, storedTier string)` — applyStoredPermissionTier 把持久化档位落到该会话的内存槽 + 执行门 + 审批自动放行，
- `func (service *Service) restorePermissionTierFor(sessionID string)` — restorePermissionTierFor 读回并落地该会话的档位（非锁内衔接场合的便捷入口）。
- `func (service *Service) syncViewPermissionTierLocked(sessionID string)` — syncViewPermissionTierLocked 把 sessionID 的**生效档位**重算进进程快照，保证

### session_scope.go

- `func (service *Service) transitionView() sync.Locker` — transitionView 返回视图命令共用的单例过渡锁。
- `func (service *Service) perSessionExecution() bool` — perSessionExecution 报告宿主是否具备逐会话执行能力。
- `func (service *Service) promptRoutesBySession() bool`
- `func (service *Service) transitionForSession(sessionID string) sync.Locker` — transitionForSession 按宿主能力选择会话 key 或视图 key。
- `func (service *Service) newGeneratedSessionID(prefix string) string` — newGeneratedSessionID 生成显式会话 ID。
- `func (service *Service) setWorkspaceWriteScope(workspaceID string)` — setWorkspaceWriteScope 设置 legacy Router 写作用域。
- `func (service *Service) bindGlobalProjectRoot(rootPath string) error` — bindGlobalProjectRoot 设置进程级项目根。
- `func (service *Service) unbindGlobalProjectRoot()` — unbindGlobalProjectRoot 清空进程级项目根。
- `func (service *Service) bindSessionProjectRoot(sessionID string)` — bindSessionProjectRoot 把指定会话自己的项目根绑到工具面。
- `func (service *Service) writeSessionSystemPrompt(sessionID string)` — writeSessionSystemPrompt 把当前 prompt 栈显式写入目标会话。
- `func (service *Service) openSessionEngine(sessionID string, workspace *model.WorkspaceInfo) (string, error)` — openSessionEngine 给一个**新会话**装上它自己那一格：建引擎 bundle、挂接
- `func (service *Service) transitionForKey(key string) sync.Locker` — transitionForKey 返回指定 key 的会话过渡锁（G5 per-session keyed）：会
- `func (service *Service) sessionUnitLocked(sessionID string) *session.SessionUnit` — sessionUnitLocked 返回指定会话的会话单元（聊天运行态已收进 SessionUnit，
- `func (service *Service) currentViewSessionID() string` — currentViewSessionID 返回当前视图会话 ID（读锁内快照；供解锁后发布
- `func (service *Service) effortForSession(sessionID string) string` — effortForSession 返回指定会话生效的 effort 级别（G4：Unit 内选择优先；
- `func (service *Service) syncSubagentPolicyFor(sessionID string)` — syncSubagentPolicyFor 按会话 effort 向引擎写入该会话的子代理策略槽：
- `func (service *Service) syncSessionReasoningEffortFor(sessionID string)` — syncSessionReasoningEffortFor 是**回合起点**按会话下发 wire 思考强度的唯一入口
- `func (service *Service) syncSessionReasoningEffort(level string) int` — syncSessionReasoningEffort 把档位对应的 wire 思考强度下发给**跟随会话**的账号
- `func (service *Service) permissionTierForSession(sessionID string) string` — permissionTierForSession 返回指定会话生效的权限档位（G4：Unit 内选择优先；
- `func (service *Service) fullAccessForSession(sessionID string) bool` — fullAccessForSession 是档位的**兼容派生读面**：full 档 ⇔ 旧的全权开启。
- `func (service *Service) syncFullAccessFor(sessionID string)` — syncFullAccessFor 按会话档位同步执行门（G4：chat 起点调用，保证每个
- `func (service *Service) anyChatRunningLocked() bool` — anyChatRunningLocked 报告是否存在任意会话的运行中聊天。M1 单飞执行
- `func (service *Service) mirrorActiveChatLocked()` — mirrorActiveChatLocked 把当前活跃会话的聊天运行态写入会话 view（阶段 1：
- `func (service *Service) activeQueuedChatRequestsLocked() []chatRequest` — activeQueuedChatRequestsLocked 返回当前会话域的排队输入（还原为执行内核
- `func (service *Service) publishSessionEvent(kind protocol_event.EventKind, revision uint64, requestID, sessionID string, payload any) protocol_event.Event` — publishSessionEvent 发布事件；装配的 EventHub 支持会话路由时携带
- `func (service *Service) publishViewSessionChanged()` — publishViewSessionChanged 通告权威视图会话已被应用内部切换（进程级事件、
- `func (service *Service) publishChatStateFor(sessionID string)` — publishChatStateFor 下发指定会话的权威聊天运行态（chat.changed）。运行/排队
- `func (service *Service) publishSessionStateChanged(sessionID string)` — publishSessionStateChanged 把"某个会话的运行态变了"通知给所有显示面。
- `func (service *Service) bindProjectRootIfSafe(_ string, rootPath string) bool` — bindProjectRootIfSafe 在安全条件下重绑全局项目根（P3/G5 收口）：
- `func (service *Service) rebindViewWorkspaceWhenIdle()` — rebindViewWorkspaceWhenIdle 在进程变为完全空闲后，把全局项目根/Router 写
- `func (service *Service) awaitRestore(ctx context.Context, sessionID string) error` — awaitRestore 等目标会话的后台冷加载结束（restoring 清除）后返回。多会话
- `func (service *Service) deferSubmitUntilRestored(ctx context.Context, sessionID, text string)` — deferSubmitUntilRestored 把一次对话提交挂到后台冷加载完成点：restoring 期间
- `func (service *Service) sessionLoaded(sessionID string) bool` — sessionLoaded 报告目标会话引擎是否已实例化（后台提交前置检查）。
- `func (service *Service) readSnapshotOf(sessionID string) (model.SessionSnapshot, error)` — ActivateSession 切换当前展示/执行会话。M1 没有每会话驻留快照，切换即
- `func (service *Service) sessionResidentLocked(unit *session.SessionUnit, sessionID string) bool` — sessionResidentLocked 判定目标会话引擎是否驻留（SnapshotOf 热/冷分界；
- `func (service *Service) snapshotOfResident(sessionID string) (model.SessionSnapshot, error)` — snapshotOfResident 组装驻留会话（引擎 bundle 在内存）的会话快照：走单元
