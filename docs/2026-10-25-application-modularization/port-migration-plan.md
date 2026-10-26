# Application 外部端口迁移设计

日期：2026-10-26

状态：History Window、Residency、Conversation Stream、Account Window、Skill Activation、
Session Records、Error Presentation 端口、Context Retain Floor 契约、Prompt Assets 入口收口、
Approval Broker 工厂外置、Input Router 外部端口、Subagent Tool Event DTO 收口与
Team Role Sessions 端口、Teamwork Board Reader 端口、Assemble DTO/Host Session 收口、
Goal Session Coordinator 迁移、Middle Model alias 退场、Middle View 只读模型归位、
Engine Prompt Coordinator、Context Window Policy、Event Protocol、Board Contract、
Subagent View、WorkTable Projection、View State、Switch Workspace、Switch Policy、
Limit Tier、Limit Base、Engine Context、Engine Task Context、Engine Session 入口归位
与 core package-function helper 退场已落地；其余端口待实施。
本文只规定拆除 `application/core` 剩余 2 个
直接内部依赖所需的端口，不把本阶段的 `application/compat` 转发层当成终态。

## 0. 当前事实与总原则

AP-116F2f 后：

- `application/core`：115 个生产文件、11,709 行；
- `Service` 公开方法：23；
- core 直接内部依赖：2。

解除外部包依赖不能只写“把 import 改成 interface”。必须满足：

1. 端口由消费方定义，只有 DTO、标准库和纯值类型；
2. 端口实现放在 `wiring/adapters` 或真正拥有状态的 Backend/Middle 包；
3. `application/core` 不持有具体 Backend/Middle 类型；
4. 同一时刻只有一个端口接口和一个适配实现；
5. 每个端口必须有真实调用方，禁止创建尚未接入的“未来接口”；
6. 每批先删一个直接依赖并编译转红，再补端口和 adapter 转绿；
7. 端口迁移不改变锁顺序、事件 revision/seq、持久化格式或 GUI/TUI wire。

## 1. `middle/switch/history`：历史窗口端口

**状态：EP-001 已实现。** `application/core` 已不再 import
`middle/switch/history`；`application/history.WindowAssembler` 由 Wiring 注入
`middle/switch/history.Assembler`。真实调用面需要六方法（额外包含
`ContainsSameRow` / `HasRowIdentity`），详情见
[`history-window-plan.md`](history-window-plan.md)。

**状态：已完成 AP-116F2a。** `application/history.WindowAssembler` 已定义，
`middle/switch/history.Assembler` 由 wiring 注入，core 已退出该直接依赖。

### 当前问题

`application/core/session_history.go` 直接 import `middle/switch/history`，同时
`application/history.Application` 已承载分页编排，但 core 仍直接调用中间层的
窗口装配函数。当前无法删除该依赖，因为 core 仍在锁外/锁内边界处直接拼接窗口。

### 唯一所有者

- 历史坐标与窗口装配判据：`middle/switch/history`；
- 冷读页、可见投影、快照安装、事件发布：Application/core host；
- 持久化读取：`store` / session repository。

### 拟增端口

放在 `application/history`，使用 `middle/protocol/model` 或后续下沉的
`contract/dto` 消息 DTO：

```go
type WindowAssembler interface {
    ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int)
    Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int)
    DurableRows(messages []model.Message) []model.Message
    IndexOfSameRow(rows []model.Message, want model.Message) int
}
```

`middle/switch/history` 增加唯一 adapter：

```go
type Assembler struct{}
func (Assembler) ReplaceTail(...) (...) { return ReplaceTail(...) }
// 其余方法逐项调用现有唯一函数，不复制判据
```

`application/history.Deps` 改为持有 `WindowAssembler`，core 不再直接 import
`middle/switch/history`。

### 迁移顺序

1. 冻结 `Page`、可见/未过滤坐标与接缝行为；
2. 把 `application/history.Deps` 的四个窗口 helper 改为端口；
3. 在 `wiring/adapters` 注入 `history.Assembler`；
4. 删除 `application/core/session_history.go` 的直接 import；
5. 删除 core 中残留的窗口装配调用，只保留锁内安装和发布。

### 必写测试

- 同一输入下，middle 原函数与 adapter 输出逐字段一致；
- 接缝对不上时返回空、窗口不动；
- 可见总数不倒退；
- 空 assistant / tool call 行不参与内容对位；
- `LoadMoreHistory` 冷读期间 `Snapshot` 可读，不持共享写锁。

### 退出门禁

- `rg "middle/switch/history" application/core` 为零；
- 只有 `middle/switch/history` 保留窗口装配判据；
- core 生产代码净下降；
- race 与历史分页回归全绿。

## 2. `middle/switch/sessionusecase`、`middle/residency`：会话与驻留端口

**状态：ResidencyPort 已实现（EP-002A）；SessionUseCase 半批在设计门禁停止
（EP-002B）。** core 已不 import `middle/residency`。移除 `sessionusecase`
需要把 ChatState、Queue、View、审批、Runtime slot 与 Domain actor 一并端口化，
会镜像完整 session state，等待会话聚合归属裁决。

### 当前问题

core 直接使用 session use case、residency Order、驻留释放和恢复 helper。会话状态
仍由 core 的 `SessionUnit` / `state.Core` 持有，但中间层规则、LRU 顺序和恢复判据
直接穿透到 core。

### 所有权必须拆清

| 状态 | 唯一所有者 |
|---|---|
| 单会话 ChatState、Queue、Approval | `session.Domain` / session unit |
| 当前视图会话指针 | core 的共享 state（迁移前） |
| 驻留集合与 LRU 顺序 | `middle/residency` |
| 可见正文 LRU | `middle/residency` + Application host |
| restore/archive/delete/unload 顺序 | `application/session` |
| 持久化记录 | `store` repository |

### 拟增端口

放在 `application/session`，由 `wiring/adapters` 实现：

```go
type SessionHost interface {
    CurrentViewSessionID() string
    SessionExists(sessionID string) bool
    CreateSession(sessionID string) error
    ActivateSession(sessionID string) error
    RestoreSession(sessionID string) error
    ReleaseSession(sessionID string) error
}

type ResidencyPort interface {
    ResidentOrder() []string
    TouchResident(sessionID string) error
    EvictResidents(limit int, keep func(sessionID string) bool) error
    LoadedContentOrder() []string
    TouchLoadedContent(sessionID string) error
    EvictLoadedContent(limit int, keep func(sessionID string) bool) error
}

type SessionCatalogPort interface {
    List() ([]dto.SessionInfo, error)
    Meta(sessionID string) (dto.SessionMeta, error)
    WaitRefresh(ctx context.Context) error
}
```

`SessionHost` 不是整个 `Service` 的镜像。每个方法必须对应实际状态所有者；不能用
一个万能 Host 把 core 的所有私有字段重新暴露出去。

### 迁移顺序

1. 先迁只读 catalog / residency preview；
2. 再迁 resident LRU 与正文 LRU；
3. 再迁 archive/unload/delete；
4. 最后迁 resume、fork、cold restore 与 draft 生命周期；
5. chat/turn 只消费稳定的 session 状态读面和提交结果。

### 必写测试

- 切换项目不串写 session workspace；
- release/unload/archive/delete 幂等；
- LRU 驱逐不驱逐视图、running、queued、approval、restoring 会话；
- 冷恢复失序不会发布旧意图；
- restore 与 submit 并发时只延后、不丢输入；
- `-race` 下 session map、LRU、revision 无数据竞争；
- 切换失败恢复原视图和原驻留顺序。

### 退出门禁

- core 不再 import `middle/switch/sessionusecase`、`middle/residency`；
- `application/session` 不持有 `state.Core` 或 `*Service`；
- 会话状态机只有 `session.Domain` 一份；
- 查询路径不获取共享写锁。

## 3. `backend/engine/{context,react,session,task}`：Engine/Runtime 窄端口

**状态：Conversation Stream 切片已实现（EP-003）。** `StreamOutputPort` 已覆盖
visible-output 与 batcher；其余 Context、Session Runtime、Task 和 React run/cancel
面仍待迁移。

### 当前问题

core 直接 import：

- `backend/engine/context`：历史、压缩、输入索引、内容分类；
- `backend/engine/react`：ReAct 生命周期/types；
- `backend/engine/session`：会话 bundle、restore、fork、transcript；
- `backend/engine/task`：Task/Plan/Tool observation/context；

这些是执行核心，不能迁进 Application。Application 只应持有完成用例所需的窄端口。

### 拟增端口

#### ContextPort

```go
type ContextPort interface {
    History(sessionID string) []dto.EngineMessage
    ReplaceHistory(sessionID string, history []dto.EngineMessage) error
    AppendHistory(sessionID string, message dto.EngineMessage)
    PrepareProviderHistory(sessionID string) error
    Compact(ctx context.Context, request dto.ContextCompactRequest) (dto.ContextCompactResult, error)
    InputIndex(sessionID string) (dto.SessionInputIndex, error)
}
```

#### ReactPort

```go
type ReactPort interface {
    Run(ctx context.Context, request dto.ReactRequest, hooks dto.ReactHooks) (dto.ReactResult, error)
    Cancel(requestID string) bool
}
```

#### SessionRuntimePort

```go
type SessionRuntimePort interface {
    Open(sessionID string) error
    Activate(sessionID string) error
    Snapshot(sessionID string) (dto.SessionRuntimeSnapshot, bool)
    Resume(sessionID string, history []dto.EngineMessage) error
    Fork(parentID, childID string, history []dto.EngineMessage) error
    Transcript(sessionID string, from, to uint64) ([]dto.TranscriptEvent, error)
    HistoryTail(sessionID string, window int) (dto.HistoryPage, error)
    Release(sessionID string) error
}
```

#### TaskPort

```go
type TaskPort interface {
    ActivePlan(sessionID string) (*dto.ActivePlanProjection, bool)
    ApplyToolObservation(sessionID string, observation dto.ToolObservation) error
    Restore(sessionID string, state dto.RestoredTaskState) error
    TaskContext(ctx context.Context, sessionID string) (dto.TaskContextProjection, bool)
    CloseTask(sessionID string, summary dto.ChatEndSummary) error
}
```

### 依赖落点

- 端口定义：`contract`（只用 DTO）或消费方 Application 包的 `Deps`；
- adapter：`wiring/adapters`；
- 具体实现：Backend Engine；
- `application/core` 只接收端口。

### 迁移顺序

1. context 历史读取与输入索引；
2. task 只读投影与 tool observation；
3. session snapshot/history tail；
4. react run/cancel；
5. compact、restore、fork 等高风险写路径。

### 必写测试

- DTO 与当前 model 的 JSON/wire 逐字一致；
- adapter 一致性用例：同一输入下原类型与端口逐字段一致；
- React cancel 与 stale request 拒绝；
- session restore/fork/release 顺序；
- task observation 幂等；
- context prepare/compact 不在 ViewMu 内执行慢 I/O；
- race、取消、超时和关闭路径。

## 4. `backend/multiagent/*`、`backend/limit/*`、`store`：领域端口

### 4.1 Multiagent

当前 core 直接使用 Goal、Subagent、Team、Teamwork 具体类型。目标端口：

```go
type GoalPort interface {
    Begin(ctx context.Context, sessionID string, request dto.BeginGoalRequest) (*dto.GoalRecord, error)
    Update(ctx context.Context, sessionID string, request dto.UpdateGoalRequest) (*dto.GoalRecord, error)
    Finish(ctx context.Context, sessionID string, request dto.FinishGoalRequest, abort bool) (*dto.GoalRecord, error)
    Status(sessionID string) (dto.GoalStatusView, error)
}

type SubagentPort interface {
    Detail(nodeID string) (*dto.SubagentDetail, error)
    ListRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
    Resume(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)
    Fork(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)
    ClearTree() error
}

type TeamPort interface {
    RoleSessions() []string
    Snapshot(mainSessionID, role string, roleSessionID string) (dto.RoleSnapshot, error)
    AssembleWire(mainSessionID, role string, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)
}

type TeamworkPort interface {
    Board(sessionID string) (dto.TeamworkBoardView, error)
    Plan(sessionID string) (dto.TeamworkPlanView, error)
    State(sessionID string) (dto.TeamworkStateView, error)
}
```

迁移顺序：

1. Goal DTO/adapter；
2. Subagent recovery/detail；
3. Team RoleSessions；
4. Teamwork read projection；
5. 最后迁 team plan/dispatch 写路径。

### 4.2 Limit

**状态：Account Window 切片已实现（EP-004）。** 账号窗口写入已通过
`application/admin.AccountWindowPolicy`；其他 limits/knobs/tier/approval 面仍待迁移。

当前 core 使用 limits、knobs、tier、account window、approval。目标端口：

```go
type LimitsPort interface {
    Current() dto.Limits
    ContextWindow(account string) int
}

type PolicyPort interface {
    SetReasoningEffort(effort string) error
    SetLoopLimit(limit int) error
    SetPermissionTier(tier string) (string, error)
    SetSessionReleaseWindow(window int) (int, error)
    SetAccountWindow(name string, window int) (int, error)
}

type ApprovalPort interface {
    Open(sessionID, requestID string, interaction *dto.Interaction)
    Resolve(id string, decision dto.ApprovalDecision) error
    PendingBySession(sessionID string) []dto.PendingApproval
}
```

所有状态词、档位词和上限值继续只在 `backend/limit` 或 `contract/dto` 保留一份。
Application 不复制合法档位表。迁移顺序：

1. limits 纯读；
2. approval 读面；
3. knobs/effort 写面；
4. tier 写入；
5. account window 持久化。

### 4.3 Store

当前 core 使用 session、board、event、tool result、workspace 等具体存储类型。
目标端口按仓储拆分：

```go
type SessionRepository interface {
    Save(ctx context.Context, sessionID string, data dto.SessionRecord) error
    Load(ctx context.Context, sessionID string) (dto.SessionRecord, error)
    Delete(ctx context.Context, sessionID string) error
    List(ctx context.Context, projectID string) ([]dto.SessionInfo, error)
}

type EventRepository interface {
    Append(ctx context.Context, sessionID string, event dto.TranscriptEvent) error
}

type ToolResultRepository interface {
    Load(ctx context.Context, ref string) (dto.StoredToolResult, error)
}

type BoardRepository interface {
    Save(ctx context.Context, key dto.ProjectKey, board dto.TeamworkBoardView) error
    Load(ctx context.Context, key dto.ProjectKey) (dto.TeamworkBoardView, error)
}
```

约束：

- Application 中不得出现 `store.*` 类型；
- key/scope 必须显式包含 projectID + sessionID；
- adapter 负责现有多工作区 key/transaction 语义；
- 关闭、批量写、LRU 清理和 flush 顺序仍由 store 实现拥有。

### 必写测试

- DTO 深拷贝与 JSON 字段 parity；
- store 项目作用域不会串库；
- 关闭后的写入错误可判定；
- approval pending 与会话归属一致；
- Goal/Subagent/Teamwork 读视图字段与现有 wire 一致；
- 所有 adapter 的乐观/幂等语义与原实现一致；
- race、取消、关闭和恢复。

## 5. 推荐执行顺序

1. Limit 纯读端口；
3. ToolResult/Event/Session repository 读面；
4. Context/Task 只读端口；
5. Multiagent 只读端口；
6. SessionRuntime/React 写路径；
7. residency + sessionusecase 生命周期；
8. 最后删除 chat/session 主循环旁的 host adapter。

## 5.1 已完成切片

- EP-001：History Window；
- EP-002A：Residency；
- EP-003：Conversation Stream；
- EP-004：Account Window；
- EP-005：Skill Activation；
- EP-006：Session Records。
- EP-007：Error Presentation。
- EP-008：Context Retain Floor 契约化。
- EP-009：Prompt Assets 入口收口。
- EP-010：Approval Broker 工厂外置。
- EP-011：Input Router 外部端口。
- EP-012：Subagent Tool Event DTO 收口。
- EP-013：Team Role Sessions 端口。
- EP-014：Teamwork Board Reader 端口。
- EP-015：Assemble DTO / Host Session 收口。
- EP-016：Goal Session Coordinator 迁入 Application。
- EP-017：Middle Model alias 退场。
- EP-018：Middle View 只读模型归位。
- EP-019：Engine Prompt Coordinator 入口归位。
- EP-020：Context Window Policy 入口归位。
- EP-021：Event Protocol 入口归位。
- EP-022：Board Contract 入口归位。
- EP-023：Subagent View 入口归位。
- EP-024：WorkTable Projection 入口归位。
- EP-025：View State 入口归位。
- EP-026：Switch Workspace 入口归位。
- EP-027：Switch Policy 入口归位。
- EP-028：Limit Tier 入口归位。
- EP-029：Limit Base 入口归位。
- EP-030：Engine Context 入口归位。
- EP-031：Engine Task Context 入口归位。
- EP-032：Core assembly helper 转发退场。
- EP-033：Engine Session Runtime 入口归位。

每组都必须单独提交，提交前后报告：

- 删除的直接 import；
- 新增端口和唯一 adapter；
- 首调用方；
- 删掉的重复判据；
- 红测/绿测；
- core 依赖数、文件数、行数。

## 6. 停止条件

出现以下任一情况立即停止该端口批次：

- 端口必须暴露完整 `Service`、完整 Engine 或完整 session state；
- 需要在本包复制 history/residency/permission/state machine；
- DTO 不完整，必须直接暴露 Backend/Middle 具体类型；
- 锁顺序、revision/seq 或事件顺序无法 parity；
- 端口没有真实调用方；
- core 依赖数或生产代码没有净下降。

