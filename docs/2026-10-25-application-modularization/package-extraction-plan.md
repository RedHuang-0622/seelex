# Application 包提取计划

日期：2026-10-26

状态：**执行中（P1～P4、P6～P7 已落地；P5 查询与 P8 主链待迁移）**。
本文件修正“只在 `application/core` 内增加私有组件”的迁移方式，
明确哪些结构拆分可接受、哪些只是过渡层，以及组件迁出 core 的顺序和硬门禁。

## 1. 当前判断

前期把大量散落编排收拢为窄组件，已经形成两类正反馈：

1. **真正的所有权收拢**
   - `lifecycleApplication`
   - `sessionReleaseApplication`
   - `contextCompactionApplication`
   - `faultGuardApplication`
   - `roleAccountApplication` / `rolePermissionApplication` /
     `roleInheritanceApplication`
   - `referenceApplication`
   - `scheduledRunApplication` / `scheduledSessionApplication`

   这些组件拥有明确流程、排他顺序或短生命周期状态，后续可以整块迁出 core。

2. **查询/适配边界**
   - `sessionQuery`
   - `conversationHistoryQuery`
   - `projectBoardQuery`
   - `historySearchApplication`
   - `completionApplication`
   - `perfStatsApplication`
   - `sessionMetaApplication`
   - `Workspace` 的 Environment/Git 查询

   这些组件本身较薄，不适合继续拆成更多更薄的 wrapper；它们的价值是作为下一阶段
   包提取的边界。

因此：

- 结构拆分导致的短期生产行数增加可以接受；
- **继续新增无状态、1～2 方法的 core 内薄组件不可接受**；
- 从下一批开始，每个结构批次必须同时确定目标领域包和迁出条件；
- 包提取批次必须以 `application/core` 生产代码净下降为硬门禁。

## 2. 当前债务基线

最新实测（AP-095 后）：

| 指标 | 当前 |
|---|---:|
| `application/core` 生产文件 | 125 |
| `application/core` 生产代码 | 12,463 行 |
| `Service` 公开方法 | 83 |
| `application/core` 直接内部依赖 | 34 |

组件化提升了结构可读性；包提取已开始让 core 规模净下降。下一步必须继续压缩
门面兼容方法并把领域实现移出 core。

## 3. 目标包结构

不建立按技术词汇命名的 `helpers/`、`models/`、`managers/`。按领域和使用方端口拆：

```text
application/
  api/                 # 入站窄接口和稳定回执形状
  session/             # 元数据、SSH、release、query、descriptor
  conversation/        # queue、history window、turn process、edit/fork
  policy/              # role account / permission / inheritance
  admin/               # account admin、runtime knobs、scheduler change faces
  workspace/           # project environments、git、project-root queries
  execution/           # WorkItem / teammate / team view queries
  lifecycle/           # drain/cancel/shutdown/fault guard
  reference/           # read_tool_result / read_plan application adapter
  input/               # completion and input-facing policies
  observability/       # PerfStats and runtime projections
  compat/              # temporary old Service adapters; deleted at M8
```

约束：

- 包不得持有完整 `*Service`；
- 包不得导入 `application/core`；
- 包通过消费方定义的 deps/port 接收底层能力；
- 具体 Engine/Runtime/Store 实现在 `wiring` 装配；
- `compat` 只放一行转发，不承载状态机。

## 4. 提取顺序

### 阶段 P1：`application/session`

先迁出低耦合、已组件化的能力：

1. `sessionMetaApplication`
2. `sessionSSHApplication`
3. `sessionReleaseApplication`
4. `sessionQuery` 的只读目录/元数据/项目根部分

退出条件：

- core 中删除对应私有实现文件或缩减为装配 adapter；
- `Service` 对应入口保形；
- core 生产行数净下降；
- facade closure allowlist 仅新增 `application/session` 一条；
- 查询不进共享写锁；
- session release 行为 parity。

### 阶段 P2：`application/policy`

迁出：

- role account；
- role permission vector；
- role inheritance / SSH path scopes。

Role Permissions 的 14 个入口已经从状态机收拢为组件，本阶段应删除 core 中的完整实现，
而不是再造代码 executor。

### 阶段 P3：`application/admin`

迁出：

- account add/details/update/delete/balance/select/window；
- runtime refresh；
- scheduled task CRUD/run/session。

### 阶段 P4：`application/workspace`

迁出：

- project environment table；
- git repository discovery/query routing；
- project root query；
- workspace batch registration projection。

### 阶段 P5：查询包分离

- `application/session/query`
- `application/history`
- `application/execution`
- `application/observability`

查询包不得依赖命令写入实现。

### 阶段 P6：`application/lifecycle`

迁出：

- graceful drain；
- idle/running/cancel；
- shutdown；
- fault guard。

### 阶段 P7：`application/reference` / `application/input`

迁出：

- read_tool_result / read_plan（AP-085 已完成）；
- suggestions；
- history search adapter。

首批完成证据：

- `application/reference` 只持有窄 `Deps`，不导入 core；
- core 只保留三个一行兼容转发和宿主回调装配；
- core 生产代码净减 53 行；
- `application/reference_package_test.go` 钉住 core 不再定义引用用例实现。

### 阶段 P8：Conversation / Session 主链

最后处理：

- ChatTurnProcess；
- hot attach / cold resume；
- session switch；
- WorkTable publisher（AP-115C1 已完成：`application/execution/table`）；
- WorkTable TraceBlock 输入投影（AP-115C2a 已完成：`application/execution/table`）；
- async/teamwork WorkTable consumer（AP-115C2b1 已完成：`application/execution/table`）；
- subagent/task WorkTable consumer（AP-115C2b2 已完成：`application/execution/table`）；
- WorkTable 行/批次输入投影（AP-115C2c 已完成：`application/execution/table`）；
- tool hooks（AP-115A 已完成：`application/execution/toolhooks`）；
- tool record（AP-115B 已完成：`application/execution/toolrecord`）；
- command registration（AP-115D1/D2 已完成：`application/input/command`）。

这些主链只有在低风险组件和查询已迁出后才允许移动。

## 5. 每批硬门禁

### 5.1 结构门禁

从 P1 起，每个包提取批次必须满足：

1. `application/core` 生产行数 `after < before`；
2. 新包不得导入 `application/core`；
3. 新包不得持有完整 Service；
4. 新包直接依赖必须能被现有 allowlist 解释；
5. 旧 Service 方法只允许一行 compat 转发；
6. 删除清单必须给出 `core 文件/函数 → 新包位置`；
7. 至少一个结构测试钉住 core 不再定义该实现；
8. 至少一个行为 parity 测试通过新包构造路径运行。

### 5.2 指标门禁

每批报告：

- core 生产文件前后；
- core 生产行数前后；
- `Service` 方法数前后；
- core 直接内部依赖前后；
- 新包生产行数；
- 门面依赖闭包新增条目及理由；
- 是否删除旧测试/迁移旧测试。

### 5.3 依赖闭包

`application/<domain>` 是一方包，允许登记进 `facadeClosureAllowList`，但必须：

- 每批只新增目标领域的一条或少量包；
- 新包自身依赖闭包清晰；
- 同步更新 allowlist max 和理由；
- 不允许为了迁移顺手放宽 backend/middle 边界。

## 6. 第一批 P1 实施设计

目标包：`application/session`

拟导出：

```go
type MetaDeps struct { ... }
type MetaApplication struct { ... }

type SSHDeps struct { ... }
type SSHApplication struct { ... }

type ReleaseDeps struct { ... }
type ReleaseApplication struct { ... }
```

迁移策略：

1. 先把私有组件类型和 deps 改名导出；
2. 将组件与直接依赖搬入 `application/session`；
3. core 只保留构造 adapter 和兼容 Service 入口；
4. 删除 core 中不再需要的 provider 方法；
5. 更新 `service_assembler` 注入；
6. 更新 facade closure allowlist；
7. 跑全量及 race。

首个 P1 子批建议只迁 `Session Meta`，用它验证：

- 新包构造；
- core compat 转发；
- facade closure 登记；
- core 行数净下降；
- 测试迁移。

验证成功后，同一模式迁移 Session SSH 与 Session Release。

### 6.1 P1-P3 并行施工契约（2026-10-26 冻结）

本阶段开始把 core 私有组件物理迁入领域包。为避免三个并行代理再次把逻辑复制回
`application/core`，先冻结以下共同契约：

1. 新包只导出“组件 + 窄 deps + 能力接口”，不导出完整 `Service`，不导入
   `application/core`。
2. deps 使用函数取端口，保留现有“调用时解析”语义；不得缓存可变端口。
3. 新包只搬应用判据和编排顺序；存储、Runtime、权限、工作区事实仍由原唯一实现拥有。
4. `application/core` 只保留装配 adapter 和旧 `Service` 一行转发。
5. 每批先在新包补 characterization/parity 测试，再删 core 中的原实现。
6. 每批必须让 core 生产行数净下降；若因兼容 adapter 暂时增加，下一子批必须偿还。
7. 根门面、`service_components.go`、`service_assembler.go` 与 facade closure
   由主线串行集成；并行代理不得自行扩大允许清单。
8. 不改 `backend/**`、`middle/**`、`contract/**` 和 JSON/wire；若契约不足即停止并报告。

#### P1 `application/session`

首个已落切片是 `session.MetaApplication`：

```go
type MetaDeps struct {
    Meta     func() switching.MetaDeps
    ReadMeta func(sessionID string) (model.SessionMeta, error)
}

type MetaApplication struct{ /* narrow deps only */ }
```

并行子批只迁 SSH：

```go
type SessionSSHPort interface {
    SessionSSHState(sessionID string) (model.SessionSSHInfo, error)
    SetSessionSSHState(sessionID string, state model.SessionSSHInfo) error
}

type SSHDeps struct {
    Sessions  func() SessionSSHPort
    Workspace func() contract.WorkspacePort
}

type SSHApplication struct{ /* narrow deps only */ }
```

导出方法固定为 `State`、`SetAllowed`、`SetEnv`。项目环境表子集校验、固定环境
不可变和路径缺省语义随组件迁入；`Service` 三个旧入口只保留一行转发。

#### P2 `application/policy`

首个子批迁角色账号、权限向量和继承/SSH 路径。新包导出以下消费方端口：

```go
type RoleDefaultPort interface {
    RoleDefault(sessionID, role string) ([]string, bool, error)
    SetRoleDefault(sessionID, role string, vector []string) error
    RoleAccount(sessionID, role string) (string, bool, error)
    SetRoleAccount(sessionID, role, account string) error
    RoleInheritance(sessionID, role string) (bool, bool, error)
    SetRoleInheritance(sessionID, role string, inherit bool) error
    RoleSSHUserSpaces(sessionID, role string) ([]string, error)
    RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)
    SetRoleSSHUserSpaces(sessionID, role string, paths []string) error
    SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error
}
```

`RoleApplication` 另接收 `PermissionRegistryPort`、`PermissionLayerPort`、
`contract.AccountRoleSelectionPort`。五层优先级仍只由 `contract/dto` 的
`ResolvePermissionVector` / `ResolveRoleSettings` 决定，不在新包重写词表。

#### P3 `application/admin`

首个子批只迁账号管理、账号选择和账号窗口，不迁调度器：

```go
type AccountApplication struct{ /* narrow deps only */ }
```

`AccountDeps` 直接使用 `contract.AccountAdminPort`、`contract.AccountBalancePort`，
Runtime/projection 的锁内应用顺序用窄回调注入。账号档校验、热注册、余额探测、
窗口落盘仍由宿主端口和 `backend/limit/accountwindow` 唯一拥有。

以上接口是代理施工契约，不是新的入站 API；GUI/TUI 仍只消费原 `Service` 和冻结的
能力型 Application 接口。

## 7. 停止条件

出现以下任一情况时停止该包提取并回到设计：

- 新包必须导入 `application/core`；
- deps 需要完整 Service 或大量双向回调；
- 需要修改 backend/middle/contract 才能继续；
- 锁或事件顺序无法通过 parity；
- core 行数没有下降；
- facade closure 需要无解释地扩张。

## 8. 最终目标提醒

解除 core 内部编排只是一个中间成果。最终验收仍是：

- 剩余 83 个 Service 方法退场；
- 34 个直接内部依赖继续下降；
- `application/core` 最终删除；
- GUI/TUI 只消费窄 API；
- 全量、race、E2E、前端门禁通过。

## 9. 2026-10-26 拆包契约冻结

### 9.1 为什么 core 仍在变大

上一阶段把散落在 `Service` 各文件里的分支、端口断言、锁外采集和
锁内提交收拢为组件。这属于**结构拆分成本**，不是最终形态：

- 正反馈：一个能力只有一个入口和一个状态机；
- 负反馈：组件暂居 `application/core`，生产行数和装配图继续增长；
- 判定：短期增长可以容忍，但从此**每个新批次必须物理迁包并让 core 净减少**。

新增一行只收拢逻辑、不迁包的生产代码，不再算作本轮合格交付。

### 9.2 并行车道

本轮冻结三个互不共享根门面的车道：

| 车道 | 目标包 | 首批能力 | 明确不迁 |
|---|---|---|---|
| ADM | `application/admin` | 账号 CRUD、余额、账号选择、窗口挡位 | Scheduler、Runtime、Session 状态机 |
| WS | `application/workspace` | 项目环境表、多仓库 Git 查询与 containment | workspace binding、Session 切换 |
| REF/LIFE | `application/reference`、`application/lifecycle` | 工具结果/Plan 只读适配；fault guard | Chat、Engine 执行、Session 释放 |

并行子代理只能新增目标包、包测试和 README。以下共享文件由主代理在子代理
交付后单车道集成：

- `application/application.go`
- `application/faces.go`
- `application/core/service.go`
- `application/core/service_components.go`
- `application/core/service_assembler.go`
- `application/core/service*.go`

### 9.3 包间契约

#### `application/admin`

```go
type AccountDeps struct {
    Admin     func() contract.AccountAdminPort
    Balance   func() contract.AccountBalancePort
    Select    func(name string) bool
    Window    func(name string, window int) (int, error)
    Projection ProjectionPort
}

type ProjectionPort interface {
    Collect(context.Context) state.RuntimeStateProjection
    LockView()
    SetRuntimeAccount(name string)
    ApplyRuntimeProjectionLocked(state.RuntimeStateProjection)
    BumpLocked() uint64
    UnlockView()
    CurrentViewSessionID() string
    RuntimeSnapshot() model.RuntimeState
    PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)
    AddNotice(message string)
}

type AccountApplication struct { ... }
```

前五项能力来自 `contract`；`ProjectionPort` 由 Application 消费方定义，宿主
适配留在 core。账号校验、写入和热注册仍由 `AccountAdminPort` 实现拥有。

#### `application/workspace`

```go
type EnvironmentDeps struct {
    Workspace func() contract.WorkspacePort
}

type GitDeps struct {
    WorkspaceRoot func() (contract.WorkspacePort, string, error)
}

type EnvironmentApplication struct { ... }
type GitApplication struct { ... }
```

只迁环境表和 Git 查询。`resolveWorkspaceRepo` 的 containment 判据迁移后必须
仍是唯一实现；workspace binding 与视图投影继续留在 core，直到 Session 车道
接管。

#### `application/reference`

```go
type Deps struct {
    PageSize       func() int
    MaxPageSize    func() int
    NodeResult     func(nodeID, ref string) (string, bool)
    ResolveAlias   func(ref string) string
    ResolveResult  func(ref string) (model.StoredToolResult, bool, error)
    ActivePlanID   func() string
    PlanFrame      func(planRef string) (string, model.RuntimeState, bool)
}

type Application struct { ... }
```

工具归档、Plan 栈和 Engine 读取继续由回调背后的唯一实现拥有。包只拥有参数
校验、分页、节点筛选和 JSON 回执。

#### `application/lifecycle`

```go
type FaultGuardDeps struct {
    BeginDrain      func() bool
    CancelAll       func()
    StopCatalog     func()
    CloseSessions   func()
    StopConsumers   func()
    CloseWorktable  func()
    CloseApproval   func()
    RequestExit     func()
}

type FaultGuard struct { ... }
```

`FaultGuard` 只拥有一次性降级和固定顺序；panic 重抛仍由 core 的
`handleLifecycleFault` 保持 fail-fast。关闭动作的实现不得复制进新包。

### 9.4 新包硬门禁

每批必须同时满足：

1. 新包生产代码不导入 `application/core`；
2. 新包不持有 `*core.Service`；
3. 至少一个红测先证明 core 仍定义实现，迁移后改为包定义；
4. 旧 Service 方法只保留一行转发；
5. core 删除对应实现后，生产行数必须净下降；
6. 新包 README 写清生态位、端口、依赖方向、并发语义和验证命令；
7. 不改 `backend/**`、`middle/**`、`contract/**`；
8. 共享装配文件由主代理串行修改；
9. 不修改 GUI/TUI wire 或行为。

### 9.5 批次顺序

1. 三个子代理分别建立 ADM、WS、REF/LIFE 目标包及包内红转绿测试；
2. 主代理先集成 REF/LIFE，再集成 ADM，最后集成 WS，避免共享装配交错；
3. 每集成一批即更新预算、README 和 implementation，并运行局部测试；
4. 三批完成后运行 Application 全量、race、E2E、build、vet、README 和 diff 门禁；
5. 只有 core 文件数与生产行数实际下降，才登记批次完成。
