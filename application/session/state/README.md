# application/session/state（Go 包名 `sessionstate`）

## 生态位

`application/session/state/` 是 Seelex 的**会话聚合状态**：会话资源（身份、可见投影、
聊天运行态、队列、生命周期状态）的唯一所有者。执行内核（`application/assembly`）经本包
暴露的 `Domain` / `SessionUnit` 读写会话；会话之间零共享，继承只走深拷贝。

Go 包名是 `sessionstate`。原 `middle/switch/sessionusecase` 已物理迁出并删除；
主要调用方是 `application/assembly`、`application/conversation`、`application/session`
的用例组件与 Wiring adapter。

**本包不 import store / Seele / application**：存储形状已下沉 `contract/dto`
（`SessionRecord` / `SessionBinding` / `SessionContextStack` / `SessionInfo`），
存储句柄由 `internal/host.SessionManager` 持有。

## 职责与非职责

- 做：会话单元注册表（`Domain` actor）、`SessionUnit` 状态与队列、运行时投影槽、
  深拷贝。
- 不做：不落盘、不调引擎、不 switch 视图、不决定 LRU（分别属于 store /
  engine / middle/switch 其它文件 / residency）。

## 目录与文件

| 文件 | 职责 |
|---|---|
| `domain.go` / `domain_actor.go` | `Domain`：单 goroutine 独占注册表与 active 指针 |
| `ports.go` | `SessionUnit` / `InputQueue` / 可选会话端口（meta/setting） |
| `runtime_slot.go` | 会话级运行时投影槽 |
| `deepcopy.go` | 会话上下文四栈与记录前缀的深拷贝 |
| `application_runtime.go` | 进程级会话 ID 序号、权限默认值与草稿槽的唯一状态 owner |

## 核心实现

- `Domain` 是 actor：`Register` / `Remove` / `SetActive` 等写命令等回包后才返回；
  读命令直接取一致快照。
- `SessionUnit` 持有 `Chat`（running/queued/requestID）、`approvals`、
  `resident`、`View`、`Queue`、`Context`、`Binding`、`Runtime`、`Revision`。
- `InputQueue` 的序号与重排只在本包内实现；展示下标与操作下标同一套空间。
- `DeepCopyRecord` / `DeepCopyContextStack` 是 fork/继承的唯一拷贝口径。

## 数据流

`application/assembly` 的 `setSessionChatLockedFor` / `sessionUnitLocked` →
`SessionUnit.UpdateChat`（单元状态）→ active 视图由 `kernel/state` 镜像；
后台会话变化由 `application/assembly` 发进程级 `sessions.changed` 通知显示面。

## 依赖方向

`sessionstate → contract/dto + contract/protocol`（+ stdlib）。
禁止 import `backend/*`、`middle/*`、`store`、`Seele`。

## 并发与存储

- 注册表与 active 指针由 actor 串行化；`SessionUnit` 内部用互斥量保护可变状态。
- 本包不持有存储句柄；持久化由装配层注入的端口完成。

## 扩展方式

新增会话级状态：先加到 `SessionUnit` 并给出读写方法，再由
`application/session` / `application/conversation` 的状态转换点更新；
不要在本包外直接改单元字段。

## Review 指南

- 是否有调用方绕过 `Domain` 直接改注册表/active；
- 是否把 store/engine 依赖重新带进本包（编译期检查 `go list -deps`）；
- 单元字段的拷贝是否仍走 deepcopy（fork/继承）。

## 测试与验证

```text
go test ./application/session/state/ -count=1
go test ./application/assembly/ -run Session -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### deepcopy.go

- `func DeepCopyContextStack(src ContextStack) ContextStack` — DeepCopyContextStack 深拷贝上下文四栈（C_i 拷贝面；切片逐层复制）。
- `func DeepCopyRecordPrefix(src SessionRecord) SessionRecord` — DeepCopyRecordPrefix 深拷贝会话记录前缀（record 前缀 = 身份/种类/parent/
- `func ForkDeepCopy(parent *SessionUnit, childID string, opts ...func(*SessionUnit)) (*SessionUnit, error)` — ForkDeepCopy 从父单元创建 fork 子单元：View/Queue/Context 深拷贝

### deepcopy_test.go

- `func TestForkSessionDeepCopy(t *testing.T)` — TestForkSessionDeepCopy（T2.7）：fork 后父子引用不相交，改子不影响父。
- `func TestDeepCopyBoundary(t *testing.T)` — TestDeepCopyBoundary（B4）：fork/冷加载引用不相交；共享面仅只读常量。
- `func TestForkDeepCopyNilParent(t *testing.T)` — TestForkDeepCopyNilParent（B1 边界）：nil 父单元明确错误，不 panic。
- `func TestS0ForkDeepCopyIsolation(t *testing.T)` — TestS0ForkDeepCopyIsolation（G4 验收锚）：fork 子单元不携带父单元的运行期

### domain.go

- `func (view *View) Mutate(mutate func(*View))` — Mutate 在视图私有锁内应用变更（会话写路径；活跃镜像在 Core.ViewMu 下经 Read 克隆）。
- `func (view *View) Read(read func(*View))` — Read 在视图私有锁（读）内读取投影快照字段。
- `func (view *View) Clone() *View` — Clone 返回视图的深拷贝（镜像/快照用；返回指针避免拷贝内部锁）。
- `func (unit *SessionUnit) ChatState() model.ChatState` — ChatState 返回当前聊天可见状态。
- `func (unit *SessionUnit) SetChatState(chat model.ChatState, view *View)` — SetChatState 写入聊天可见状态并同步到单元视图镜像。
- `func (unit *SessionUnit) UpdateChat(mutate func(*model.ChatState), view *View)` — UpdateChat 在单元锁内应用变更并同步视图镜像（执行内核热路径用）。
- `func (unit *SessionUnit) SetCancel(cancel context.CancelFunc)` — SetCancel 设置会话取消函数。
- `func (unit *SessionUnit) CancelFunc() context.CancelFunc` — CancelFunc 返回会话取消函数（可能为 nil）。
- `func (unit *SessionUnit) SetStream(stream VisibleOutputSink)` — SetStream 设置流式可见输出接收器。
- `func (unit *SessionUnit) StreamSink() VisibleOutputSink` — StreamSink 返回流式可见输出接收器（可能为 nil）。
- `func (unit *SessionUnit) SetBatcher(batcher StreamBatcherSink)` — SetBatcher 设置流式批处理接收器。
- `func (unit *SessionUnit) BatcherSink() StreamBatcherSink` — BatcherSink 返回流式批处理接收器（可能为 nil）。
- `func (unit *SessionUnit) Enqueue(request QueuedRequest)` — Enqueue 追加一条排队输入（Q_i：InputQueue 承载，Payload 不透明）。
- `func (unit *SessionUnit) PendingRequests() []QueuedRequest` — PendingRequests 返回排队输入列表（拷贝；无法还原的载荷跳过）。
- `func (unit *SessionUnit) SetRequests(requests []QueuedRequest)` — SetRequests 整体替换排队输入（幂等重建 Q_i）。
- `func (unit *SessionUnit) QueueProjection() ([]string, int)` — QueueProjection 返回排队输入的展示投影（逐项文本 + 计数）——它是
- `func (unit *SessionUnit) RecallRequest(index int) (QueuedRequest, bool)` — RecallRequest 移除并返回指定下标的排队输入（撤回编辑：调用方把
- `func (unit *SessionUnit) ReorderRequests(from, to int) bool` — ReorderRequests 把「投影下标」from 的排队输入移动到 to（调换排队顺序；

### domain_actor.go

- `func NewDomain() *Domain` — NewDomain 启动会话域 actor（单 goroutine 持有注册表与 V）。
- `func (domain *Domain) loop()` — loop 是注册表与 V 的唯一所有者。
- `func (domain *Domain) reply(cmd domainCmd, reply domainReply)`
- `func (domain *Domain) send(cmd domainCmd)`
- `func (domain *Domain) call(cmd domainCmd) domainReply`
- `func (domain *Domain) Register(unit *SessionUnit)` — Register 注册一个会话单元。等 actor 回包：注册返回后即对所有 goroutine
- `func (domain *Domain) Remove(sid string)` — Remove 注销会话单元（unload 后调用）。等回包，理由同 Register。
- `func (domain *Domain) Unit(sid string) *SessionUnit` — Unit 返回指定会话单元；不存在时返回 nil。
- `func (domain *Domain) UnitIDs() []string` — UnitIDs 返回全部驻留会话 ID（排序由调用方负责）。
- `func (domain *Domain) Live() int` — Live 返回当前驻留会话数（测试与监控用）。
- `func (domain *Domain) SetActive(sid string)` — SetActive 移动当前视图指针 V（hot_attach / cold_load 发布后调用）。
- `func (domain *Domain) ActiveID() string` — ActiveID 返回当前视图指针指向的会话 ID。
- `func (domain *Domain) Close()` — Close 停止会话域 actor（幂等；Shutdown/测试 teardown 使用）。关闭是 actor

### domain_test.go

- `func TestThreadIsolation(t *testing.T)` — TestThreadIsolation 并发写两个会话的队列互不串行、互不污染
- `func TestViewIsolation(t *testing.T)` — TestViewIsolation 两个会话的可见投影互不影响。
- `func TestDomainActivePointer(t *testing.T)` — TestDomainActivePointer V 指针只影响视图，不触碰任何会话单元内容。
- `func TestDomainConcurrentCommands(t *testing.T)` — TestDomainConcurrentCommands（-race）：注册/注销/移动视图指针与读取并发进行，
- `func isTestSessionID(id string) bool` — isTestSessionID 报告 id 是否形如本测试注册的 "session-<n>"。
- `func TestForkDeepCopyIsolation(t *testing.T)` — TestForkDeepCopyIsolation（S0）：fork 继承面 = 深拷贝——子会话 View/队列

### input_queue_edit_test.go

- `func TestInputQueueMove_ReordersPositionsKeepsSeq(t *testing.T)` — TestInputQueueMove_ReordersPositionsKeepsSeq 调换排队顺序：位置变化、seq
- `func TestInputQueueMove_OutOfRangeOrNil(t *testing.T)` — TestInputQueueMove_OutOfRangeOrNil 越界与 nil 句柄：返回 false、不 panic、
- `func TestInputQueueRemoveAt_PopsIdentity(t *testing.T)` — TestInputQueueRemoveAt_PopsIdentity 撤回：按位置取出元素并保留 seq/载荷，
- `func TestSessionUnitRecallAndReorder(t *testing.T)` — TestSessionUnitRecallAndReorder 会话单元层的调换/撤回：撤回交还展示原文
- `func TestQueueProjectionDefinesProjectionIndexSpace(t *testing.T)` — TestQueueProjectionDefinesProjectionIndexSpace（节点 156 域层硬化）：投影
- `func assertProjection(t *testing.T, unit *SessionUnit, want []string)`
- `func assertQueue(t *testing.T, queue *InputQueue, wantText []string, wantSeq []uint64)`
- `func assertDisplays(t *testing.T, requests []QueuedRequest, want []string)`

### ports.go

- `func NewInputQueue() *InputQueue` — NewInputQueue 构造空输入队列。
- `func (queue *InputQueue) Enqueue(text string, payload any) QueuedInput` — Enqueue 追加一条输入并分配递增 seq。
- `func (queue *InputQueue) Dequeue() (QueuedInput, bool)` — Dequeue 弹出队首输入；空队列返回 false。
- `func (queue *InputQueue) Clear()` — Clear 清空队列（幂等重建 Q_i 用）。
- `func (queue *InputQueue) Len() int` — Len 返回队列长度。
- `func (queue *InputQueue) Snapshot() []QueuedInput` — Snapshot 返回队列只读拷贝（seq 有序）。
- `func (queue *InputQueue) RemoveAt(index int) (QueuedInput, bool)` — RemoveAt 移除并返回指定下标的排队输入（撤回编辑用；载荷随元素一起返回，
- `func (queue *InputQueue) Move(from, to int) bool` — Move 把 from 下标的输入移动到 to 下标（调换排队顺序；等价于先删除 from
- `func NewSessionUnit(id string, opts ...func(*SessionUnit)) (*SessionUnit, error)` — NewSessionUnit 构造会话单元骨架；空 ID 拒绝（B2 边界）。
- `func NewDraftUnit() *SessionUnit` — NewDraftUnit 构造草稿槽会话单元（ID 为空：未物化的新会话占位；不参与
- `func WithKind(kind dto.SessionRecordKind) func(*SessionUnit)` — WithKind 设置会话种类（main/subagent）。
- `func WithParent(parentID string) func(*SessionUnit)` — WithParent 设置子代理归属主会话（main 为空）。
- `func WithTitle(title string) func(*SessionUnit)` — WithTitle 设置会话标题。
- `func (unit *SessionUnit) Status() SessionStatus` — Status 返回当前可见状态：有待批审批时提升为 awaiting_approval（覆盖
- `func (unit *SessionUnit) AddApproval(requestID string)` — AddApproval 登记一条本会话待批审批（会话级归属记账；幂等去重）。
- `func (unit *SessionUnit) RemoveApproval(requestID string)` — RemoveApproval 移除一条待批审批（结案/超时/取消；幂等）。
- `func (unit *SessionUnit) ApprovalIDs() []string` — ApprovalIDs 返回本会话待批审批 ID 拷贝（快照/目录行数据源）。
- `func (unit *SessionUnit) PendingApprovalCount() int` — PendingApprovalCount 返回本会话待批审批数（awaiting_approval 判定源）。
- `func (unit *SessionUnit) Resident() bool` — Resident 返回引擎 bundle 是否驻留（G6 驱逐/诊断/目录 Resident 列）。
- `func (unit *SessionUnit) SetResident(on bool)` — SetResident 更新驻留标记（冷加载/激活置 true；驱逐/卸载置 false）。
- `func (unit *SessionUnit) HasSession() bool` — HasSession 返回引擎热/冷判定（loaded 由 ColdLoad/Unload 驱动，与
- `func (unit *SessionUnit) ColdLoad(hasSession bool) error` — ColdLoad 冷加载：cold → idle（要求引擎已创建，hasSession=true）。
- `func (unit *SessionUnit) Submit(hasSession bool) error` — Submit 提交输入：idle → running；running/queued 保持（入队由 Queue 承担）。
- `func (unit *SessionUnit) Finish() error` — Finish 完成/取消：running/queued → idle。
- `func (unit *SessionUnit) Unload() error` — Unload 卸载：释放引擎（loaded=false，回到 draft）。运行中卸载拒绝。

### ports_test.go

- `func TestSessionUnitComponents(t *testing.T)` — TestSessionUnitComponents（T2.1）：元组契约 S_i=(id,K,parent,E,V,Q,C,B,status)。
- `func TestLifecycleThinStateMachine(t *testing.T)` — TestLifecycleThinStateMachine（T2.3）：status 由 HasSession 驱动，
- `func TestEmptyAndNilHandles(t *testing.T)` — TestEmptyAndNilHandles（B1）：空 id / nil 队列 / 空会话不 panic、明确错误。
- `func TestIllegalLifecycleTransitions(t *testing.T)` — TestIllegalLifecycleTransitions（B2）：空 id / 运行中 unload /
- `func TestLockOrdering(t *testing.T)` — TestLockOrdering（R2）：Transition→Domain→Unit→View 锁序下并发迁移 +

### runtime_slot.go

- `func (unit *SessionUnit) ComposerText() string` — ComposerText 返回本会话未发送输入草稿正文。
- `func (unit *SessionUnit) SetComposerText(text string, updatedAt time.Time)` — SetComposerText 写入本会话未发送输入草稿（调用方负责持久化；空正文
- `func (unit *SessionUnit) EffortLevel() string` — EffortLevel 返回本会话选择的 effort 级别（空 = 未选择，回退进程默认）。
- `func (unit *SessionUnit) SetEffortLevel(level string)` — SetEffortLevel 写入本会话的 effort 选择（运行守卫由调用方持有：目标会话
- `func (unit *SessionUnit) PermissionTier() (tier string, ok bool)` — PermissionTier 返回本会话的权限档位选择（ok=false = 未选择，回退进程
- `func (unit *SessionUnit) SetPermissionTier(tier string)` — SetPermissionTier 写入本会话的权限档位选择（chat 起点按生效档位同步引擎门；
- `func (unit *SessionUnit) FullAccessMode() (on bool, ok bool)` — FullAccessMode 是档位的**兼容派生读面**（ok=false = 未选择，回退进程默认）：
- `func (unit *SessionUnit) SetFullAccessMode(on bool)` — SetFullAccessMode 是**兼容写面**：true → full 档、false → manual 档。
- `func (unit *SessionUnit) SetRuntimeState(state model.RuntimeState)` — SetRuntimeState 把一次运行时投影写入本会话槽（深拷贝：投影的 slice
- `func (unit *SessionUnit) RuntimeState() model.RuntimeState` — RuntimeState 返回本会话运行时投影槽的深拷贝（无槽内容时返回零值）。
- `func (unit *SessionUnit) RuntimeStateLoaded() bool` — RuntimeStateLoaded 报告本会话运行时投影槽是否已写入过（区分「槽为空」
- `func (unit *SessionUnit) SnapshotRevision() uint64` — SnapshotRevision 返回本会话快照修订号（未发生任何会话事件时为 0）。
- `func (unit *SessionUnit) BumpSnapshotRevision() uint64` — BumpSnapshotRevision 递增并返回本会话快照修订号（INV-G5：会话级修订推进，与

### runtime_slot_test.go

- `func TestSessionUnitRuntimeSlotIsolation(t *testing.T)` — TestSessionUnitRuntimeSlotIsolation（G1-A）：每会话运行时槽写入/读出是
- `func TestSessionUnitRevisionBumpsPerSession(t *testing.T)` — TestSessionUnitRevisionBumpsPerSession（G1-A/INV-G5）：Revision 按单元

### shared_face_test.go

- `func TestGlobalSharedFaceBounded(t *testing.T)` — TestGlobalSharedFaceBounded（T5.5 静态断言）：G=(V,registry,bus,services)
