# Backend / Middle 领域化与契约化修正方案

日期：2026-10-25

状态：**方案，尚未开始实施**。本文与
[`2026-10-25-application-modularization/plan.md`](../2026-10-25-application-modularization/plan.md)
构成同一轮 Application / Backend / Middle 模块化计划；当前代码仍是事实来源。

## 1. 背景与基线

`backend` 和 `middle` 当前已经完成一次较大的物理归位，但还存在三类结构问题：

1. 部分 `middle` 读模型直接依赖 backend 具体实现；
2. 部分 backend 为了共享 DTO 反向依赖 `middle/protocol/model`，甚至有 backend
   依赖 `application/prompt`；
3. `backend/engine/assemble` 是组合根，却留在 backend 领域层，并直接认识几乎所有
   backend 子域。

2026-10-25 实测基线：

- `backend`：343 个生产 Go 文件，约 77,017 行生产代码。
- `middle`：48 个生产 Go 文件，约 9,193 行生产代码。
- backend 内跨域边：106 条。
- backend → middle：16 条。
- middle → backend：6 条。
- middle 内跨域边：18 条。
- `backend/engine/assemble` 直接认识 46 个 Seelex 内部包，横跨 engine、tool、
  multiagent、workunit、middle、store 等几乎所有域。
- `middle/view/state`、`middle/view/subagent` 各自有 7 个跨域依赖。

主要大型生产文件：

| 文件 | 行数 |
|---|---:|
| `backend/engine/task/task_context_state.go` | 1,888 |
| `backend/engine/context/coordinator.go` | 1,745 |
| `backend/multiagent/teamwork/items.go` | 1,295 |
| `middle/protocol/model/state.go` | 1,233 |
| `backend/tool/worktree/worktree_manager.go` | 1,153 |
| `backend/tool/registry/async_exec.go` | 1,069 |
| `backend/engine/assemble/runtime.go` | 1,024 |
| `backend/engine/schedule/schedule.go` | 949 |
| `backend/engine/assemble/runtime_teamwork.go` | 899 |
| `backend/tool/computer/mcp/main.go` | 894 |

当前最明确的 6 条 middle → backend 反向边：

```text
middle/view/board    -> backend/limit
middle/view/state    -> backend/engine/react
middle/view/state    -> backend/limit
middle/view/subagent -> backend/engine/context/seelectx/snapshot
middle/view/subagent -> backend/limit
middle/view/subagent -> backend/multiagent/subagent
```

当前 backend → middle 主要来自共享协议模型：

- `middle/protocol/model`；
- `middle/protocol/event`；
- 少量 `middle/switch`、`middle/view/board`、`middle/view/sessionaccess` 依赖。

此外还存在跨层反向边：

```text
backend/engine/prompt -> application/prompt
backend/engine/task   -> application/prompt
```

这说明共享类型和提示词策略还没有完成契约化。

## 2. 目标与非目标

### 2.1 目标

1. backend 的跨包依赖必须对应真实业务关系，否则改用消费方端口。
2. middle 不依赖 backend 具体实现，只依赖契约、协议 DTO 和只读查询端口。
3. backend 不依赖 middle 展示模型，也不依赖 application。
4. 组合根从领域层移出，`backend/engine/assemble` 最终变成 `wiring/runtime`。
5. middle 只保留协议与展示投影；会话切换等应用用例归 Application。
6. 用 DDD 聚合约束状态一致性，用消费方端口约束跨域协作。
7. 建立跨域边白名单和兼容层退场机制，避免迁移后形成新的全局 `contract` 上帝包。

### 2.2 非目标

- 不为“看起来更 DDD”而全仓重命名。
- 不把所有端口都塞进顶层 `contract`。
- 不把工具适配器伪装成领域层。
- 不在一轮内同时重写 backend、middle 和 Application。
- 不改变现有前端 wire 契约和事件序号语义。

## 3. 分层与依赖规则

### 3.1 目标分层

```text
UI
  ↓
Application inbound API / use cases
  ↓
Contract / protocol DTO / domain ports
  ↑
Backend domain
  ↑
Backend tool / store / infrastructure adapters
  ↑
Wiring / runtime composition
```

`middle` 分两类：

- `middle/protocol`：共享协议形状。
- `middle/view`：只读投影与展示适配。

当前 `middle/switch` 中的会话切换、工作区和历史编排更接近 Application 用例，
后续迁入 Application 对应模块，或在迁移完成前收敛为纯策略包。

### 3.2 依赖矩阵

| 来源 | 允许依赖 | 禁止依赖 |
|---|---|---|
| `contract` / `contract/dto` | stdlib、明确允许的纯协议包 | backend、middle、application、store |
| `kernel/state` | contract、纯数据结构 | application、UI、组合根 |
| backend 领域包 | contract/dto、自身子包、白名单业务域端口 | middle/view、application、具体 UI |
| backend tool 适配器 | domain ports、contract、标准库 | 其它 tool 适配器内部实现 |
| `middle/protocol` | contract/dto | backend、application、store |
| `middle/view` | contract/dto、protocol/model、query ports | backend 具体实现、application/core |
| `middle/switch` | 迁移期 contract/dto + 纯策略端口 | backend 具体实现；最终迁入 Application |
| `application/*` | 入站契约、domain/query ports、DTO | backend engine 具体实现、middle/view 具体实现 |
| `wiring/*` | 所有实现与端口 | 不得反向被领域包 import |

### 3.3 backend 跨域允许清单

不是所有 backend → backend 边都要删除。以下关系可能是业务必需：

| 边 | 业务理由 | 目标形态 |
|---|---|---|
| `tool/worktree -> workunit` | worktree 收尾分类复用 workunit 统一词表 | 保留，或改为 workunit 提供的窄分类接口 |
| `multiagent/teamwork -> workunit` | WorkItem 生命周期必须与收尾分类一致 | 保留 |
| `tool/node -> workunit/worktree` | node 执行与现场生命周期相关 | 收窄为 Run/Scene 端口 |
| `tool/registry -> limit/policy/gate` | 工具路由必须服从权限与安全门 | 保留 |
| `multiagent/subagent -> agentsession` | 子代理独立持久会话构造 | 保留 |
| `multiagent/subagent -> resume` | 中断恢复模板 | 保留 |

需要重点治理的非业务或过宽边：

- `tool/fork -> tool/node/task`：fork 工具不应直接认识 node/task 的全部内部面。
- `engine/context -> engine/session/task`：应明确是流程编排还是领域依赖。
- `engine/task -> engine/session`：Task 不应为了读会话实现而跨域。
- `multiagent/subagent -> tool/fork`：避免 multiagent 与 tool 执行面双向渗透。
- `tool/registry -> tool/io/fs`、`tool/ssh`：优先注入 capability port。
- `engine/prompt -> application/prompt`：彻底删除反向依赖。
- `backend/* -> middle/protocol/model`：迁移所需纯 DTO，之后删除。

## 4. 领域与聚合

| 领域 | 聚合 / 核心对象 | 状态与不变式 |
|---|---|---|
| Session | `SessionDescriptor` / `SessionLifecycle` | CWD、workspace binding、draft、归档、fork lineage |
| Conversation | `Conversation` | 单活动回合、队列、revision、restore、edit/fork CAS |
| Context | `ContextPolicy` / `CompactionState` | 窗口预算、压缩栈、前缀一致性、历史读回 |
| Task | `TaskRun` / `TaskLedger` | 状态机、checkpoint、token 审计、批次作用域 |
| WorkUnit | `ExecutionUnit` | running/settled/reclaimed、恢复与分类 |
| Workspace | `WorktreeScene` / `ProjectBinding` | worktree 生命周期、脏状态、merge、批准后的 main mode |
| Multiagent | `SubagentRun` / `TeamPlan` / `WorkItem` | fork 树、角色在编、排队、验收、收口 |
| Plugin / Skill | `CapabilityAssembly` | 插件可见性、能力收窄、装配失败回滚 |
| Permission | `PermissionVector` / `RouteGrant` | 主体、路由组、位、黄牌和 deny |
| Goal | `GoalRecord` | 目标状态机、治理会话、终态 gate |

聚合之间不得通过共享可变对象协作。跨聚合协作使用：

- 消费方窄端口；
- 命令/查询 DTO；
- 应用流程管理器；
- 明确的事务补偿。

## 5. 契约策略

契约驱动不等于把所有接口放进顶层 `contract`。需要分三类：

### 5.1 跨层契约

UI、Application、Backend、Wiring 之间共享的 port/DTO：

- 放 `contract` 或 `contract/dto`；
- 零实现依赖；
- 字段是 wire 契约；
- 变更必须同步前端 reducer、事件和测试。

### 5.2 消费方本地端口

仅一个领域消费的端口：

- 定义在消费方包内；
- 由装配层注入；
- 只有跨层或广泛共享时才上提到 `contract`；
- 避免 `contract` 变成没有语义的接口仓库。

### 5.3 协议模型

- `middle/protocol/model` 只服务快照、事件和前端投影；
- backend 领域内部需要的中性 DTO 不放在 `middle`；
- 能移到 `contract/dto` 的共享形状先迁移，再删除旧别名；
- 事件序号、revision 和 snapshot 语义不变。

具体迁移项：

1. backend 使用的 `middle/protocol/model` 类型；
2. `application/prompt` 中被 backend 使用的纯策略类型；
3. limit/policy 中的权限 DTO；
4. workunit / worktree / node 的共享 Scene 与 Outcome 形状；
5. `middle/switch` 当前暴露给 application 的用例类型。

## 6. Middle 目标

### 6.1 `middle/protocol`

- 只保留版本化 Snapshot、Event、Message、Plan、WorkItem、Session 等 wire 形状；
- 不持有业务状态机；
- 不 import backend 或 application。

### 6.2 `middle/view`

拆为只读查询与投影：

- Session catalog / history view；
- Worktable view；
- Subagent detail view；
- Team board / WorkItem execution view；
- Snapshot / event view。

要求：

- 输入是查询端口或 DTO，不是 `*assemble.Runtime`、`*subagent.Sessions`；
- 投影函数不持有写锁；
- 不包含 Session switch、resume、accept、dispatch 等命令判断。

### 6.3 `middle/switch`

当前 `sessionusecase`、`workspace`、`history` 属于应用用例或查询编排，
不应长期作为 middle 的领域逻辑。迁移方向：

- Session 切换 / 恢复 → `application/session`；
- Workspace 创建 / 绑定 → `application/workspace`；
- History 分页 → `application/conversation/query` 或 `application/session/query`；
- middle 只保留纯展示投影和协议转换。

## 7. Backend 目标

### 7.1 `assemble` 退出 backend 领域层

`backend/engine/assemble` 是 composition root，不是领域实现。目标迁移到：

```text
wiring/runtime/
```

迁移完成后：

- domain 包不能 import `wiring/runtime`；
- `wiring/runtime` 只负责构造、依赖注入、启动/关闭顺序；
- 新应用流程通过 Application/domain ports 接入；
- `backend/engine/assemble` 只保留一个迁移期的兼容 shim，最终删除。

### 7.2 Engine 域

`session`、`context`、`task`、`prompt`、`react` 等按状态归属拆清：

- Session 只管理会话身份与持久化边界；
- Context 消费会话快照，不反向管理 Task；
- Task 使用 execution ports，不直接读取 Session 实现；
- Prompt 只依赖 prompt DTO / policy，不依赖 Application；
- React 是执行引擎适配，不是应用用例。

跨域流程（例如恢复时同时读 Session、Context、Task）应由 `wiring/runtime` 或
Application process manager 编排，而不是 engine 子域互相 import。

### 7.3 Tool 域

`backend/tool` 是 capability adapter 集合，不是单一领域：

- `tool/io/*`：IO capability；
- `tool/ssh`：远端 transport；
- `tool/computer`：桌面能力；
- `tool/reference` / `review`：读模型工具；
- `tool/node`：节点执行编排，需要进一步拆出 runner 与查询；
- `tool/registry`：工具注册与安全门，依赖 policy/gate，不直接认识具体 fs/ssh 实现。

### 7.4 Multiagent 域

保留：

- `teamwork`：TeamPlan、WorkItem、派发、验收、收口；
- `subagent`：SubagentRun、树、恢复；
- `team`：角色装配；
- `agentsession`：独立持久会话构造；
- `resume`：恢复模板；
- `goal`：Goal 聚合。

要求：

- 不直接调用 UI/Application；
- 通过 Runner、Workspace、Session、Jobs、Events 端口执行；
- `teamwork` 只持有 WorkItem 生命周期，不持有 git 实现；
- `subagent` 只持有运行注册表与恢复，不持有工具 UI 投影。

### 7.5 WorkUnit / Worktree

- `workunit` 是执行单元生命周期与分类的唯一实现；
- `worktree` 只做 git 机械动作；
- `teamwork`、`node`、`subagent` 通过 workunit 端口协作；
- 不允许各层复制“未提交/合并失败/回收”判据。

## 8. 兼容层退场

沿用 Application 的 D0–D4 规则：

| 阶段 | 状态 |
|---|---|
| D0 | 旧路径保留，新路径可并行使用 |
| D1 | 旧路径只做一行转发 |
| D2 | 生产调用方归零 |
| D3 | 标记 deprecated |
| D4 | 删除 |

首批退场项：

| 旧面 | 新面 | 目标里程碑 |
|---|---|---|
| `backend/engine/assemble` | `wiring/runtime` | BM4 |
| backend → `middle/protocol/model` | contract/runtime DTO | BM1 |
| backend → `application/prompt` | backend prompt policy port | BM1 |
| middle/view → backend concrete | 只读 query port | BM2 |
| `middle/switch` 会话用例 | Application session/workspace/history 用例 | BM6 |
| `middle/view/subagent` 详情装配 | `middle/view` + `contract.SessionQueryPort` | BM2/BM6 |
| backend 内的跨域实现 import | consumer-owned narrow port | BM3/BM5 |

兼容 shim 不得持有状态、缓存或业务分支。

## 9. 里程碑与改动周期

与 Application M0–M9 并行，BM0–BM8 的建议周期如下。默认每个批次独立合入，
高冲突文件只在集成窗口修改。

| 里程碑 | 建议周期 | 目标 | 退出条件 |
|---|---|---|---|
| BM0 基线与边界冻结 | 2026-10-26 至 2026-10-30 | 建立依赖预算、跨域白名单、兼容台账 | backend/middle 跨层边和文件规模可视化门禁可运行 |
| BM1 共享模型契约化 | 2026-11-02 至 2026-11-13 | 把 backend 使用的 middle/application DTO 下沉到 contract | backend 不 import middle/protocol 或 application/prompt；旧别名保留 |
| BM2 Middle 去 backend 依赖 | 2026-11-16 至 2026-11-20 | `middle/view/*` 改用只读查询端口和 DTO | middle → backend 生产依赖为 0 |
| BM3 Backend 领域边界 | 2026-11-23 至 2026-12-04 | 收紧 Session/Context/Task/Prompt/React 边界 | 无 application 反向依赖；跨域边进入白名单 |
| BM4 Assemble 退出领域层 | 2026-12-07 至 2026-12-18 | `backend/engine/assemble` → `wiring/runtime` | 领域包不再 import assemble；旧 shim 只转发 |
| BM5 Multiagent/Tool 收口 | 2026-12-21 至 2027-01-08 | team/subagent/node/fork/registry 端口化 | 重要跨域实现依赖下降；无重复生命周期判据 |
| BM6 Middle switch/view 收口 | 2027-01-11 至 2027-01-22 | 会话用例迁入 Application，middle 只留投影 | `middle/switch` 不再承担业务状态机 |
| BM7 兼容退场 | 2027-01-25 至 2027-02-05 | 删除旧路径、别名、直接实现依赖 | 旧路径零生产调用；D3/D4 完成 |
| BM8 终验 | 2027-02-08 至 2027-02-19 | 目标依赖图、包规模、文档、全量验证 | backend↔middle 反向依赖为 0；全仓门禁通过 |

## 10. 任务看板

### BM0：基线

- 建立 backend → middle、middle → backend、application ← backend 的边表；
- 建立 backend 内跨域白名单；
- 为跨域边和包大小增加预算测试；
- 登记所有兼容 shim。

### BM1：共享模型契约化

- 抽取 backend 实际需要的 `protocol/model` 形状；
- 将 `application/prompt` 纯策略迁到 contract 或 backend 可依赖的 prompt policy；
- 统一 permission、scene、outcome、workitem execution 的共享 DTO；
- 为旧类型保留 D1 别名。

### BM2：Middle 去 backend 依赖

- `view/state` 使用事件/快照 DTO，不 import engine/react；
- `view/subagent` 使用 subagent query port，不 import subagent 实现；
- `view/board` 使用 effective limits DTO 或 limits query port；
- `view/sessionaccess` 保持只依赖 contract/dto；
- 增加 `middle` 禁止 import backend 的依赖门禁。

### BM3：Backend 领域边界

- 拆分 `task_context_state.go`；
- 拆分 `engine/context/coordinator.go`；
- 明确 Session/Context/Task 之间的流程归属；
- 删除 backend → application；
- 将 prompt 策略改为纯 port/DTO。

### BM4：Assemble 迁出

- 将 `runtime.go`、`runtime_*.go` 迁入 `wiring/runtime`；
- 将 `runtime_teamwork.go` 的编排放到 Application/domain process；
- 将 `backend/engine/assemble` 保留成过渡别名包；
- 验证无领域包反向 import。

### BM5：Multiagent / Tool

- `tool/node` 拆出 runner、记录、query 与 worktree adapter；
- `tool/fork` 只通过 multiagent runner port；
- `multiagent/subagent` 与 `tool/fork` 解耦；
- `teamwork`、`workunit`、`worktree` 的单一路径保持；
- `tool/registry` 通过 capability port 接 fs/ssh；
- 拆分 `items.go`、`worktree_manager.go`、`async_exec.go`。

### BM6：Middle 收口

- `sessionusecase` 迁入 `application/session`；
- `workspace` 迁入 `application/workspace`；
- `history` 迁入 `application/conversation/query`；
- `middle/view` 只接收稳定 query DTO；
- `middle/protocol` 只保留 wire 形状。

### BM7：兼容退场

- 删除 `backend/engine/assemble` 旧路径；
- 删除 middle 对 backend 的 shim；
- 删除 backend 对 middle 的 shim；
- 删除 Application API 中 backend DTO 别名；
- 删除兼容测试，保留核心回归测试。

### BM8：终验

- 依赖图符合目标矩阵；
- 跨层反向边为 0；
- 所有领域 README 与函数索引同步；
- 全仓 build/test/vet、race 目标包、GUI E2E 通过。

## 11. 并行车道

| 车道 | 改动 | 并行条件 |
|---|---|---|
| CT 契约 | contract/dto、协议模型、端口 | 全局串行，先冻结后迁移 |
| MD Middle | view/switch 去 backend、投影端口 | 不与 CT 同时改协议 DTO |
| BE Core | Session/Context/Task/Prompt | 独立包可并行，统一在 wiring 验证 |
| MA Tool | Multiagent / Tool / WorkUnit | 与 BE 可并行，不共享高冲突文件 |
| RT Runtime | assemble → wiring/runtime | 最后集成，只能单车道 |
| CP Compat | shim、调用方迁移、删除 | 各迁移完成后统一处理 |

高冲突文件：

- `middle/protocol/model/state.go`；
- `middle/view/state/coordinator.go`；
- `middle/view/subagent/coordinator.go`；
- `backend/engine/assemble/runtime*.go`；
- `backend/multiagent/teamwork/items.go`；
- `backend/engine/task/task_context_state.go`；
- `backend/engine/context/coordinator.go`；
- `backend/tool/registry/*.go`。

这些文件不得并行修改；每次只允许一个集成批次触碰。

## 12. 门禁与指标

### 12.1 结构指标

- middle → backend 生产依赖：6 → 0。
- backend → middle/protocol 生产依赖：16 → 0。
- backend → application 生产依赖：2 → 0。
- `backend/engine/assemble` 领域包依赖：清理后由 `wiring/runtime` 独占。
- backend 内跨域实现依赖：只允许白名单，数量不得上升。
- `middle/switch` 从业务用例降为兼容 shim，最终删除。
- 重要生产文件原则上不超过 500 行；超过需拆分或记录 ADR。
- `middle/protocol/model/state.go` 拆分后，单文件不超过 500 行。

### 12.2 行为指标

- Session resume/switch 前后快照、事件和 revision 序列 parity；
- Context 压缩、Task checkpoint、WorkItem lifecycle 的恢复行为不变；
- Worktree typed error、accept/close、dispatch 两阶段语义不变；
- Subagent fork/merge/resume 行为不变；
- Team board、WorkItem execution、teammate inspection DTO 不变；
- middle 查询读取不阻塞 backend 运行流。

## 13. 跨计划依赖

| Application 里程碑 | 依赖的 Backend/Middle 里程碑 |
|---|---|
| A2 契约净化 | BM1 必须先冻结共享 DTO |
| A3 Session/Conversation Query | BM2 + BM6 必须先提供只读查询端口 |
| A4 Conversation 聚合 | BM3 的 Session/Context/Task 边界需稳定 |
| A6 Goal/Team/Subagent | BM5 的多代理端口需稳定 |
| A8 兼容退场 | BM7 的旧路径删除清单需完成 |
| A9 终验 | BM8 的依赖矩阵与全仓门禁需通过 |

推荐启动顺序：

```text
BM0 + A0
  ↓
BM1 共享 DTO
  ├─ A1 API 接口
  ├─ BM2 middle 去 backend
  └─ BM3 backend 域边界
        ↓
      A3 查询模型
        ↓
      A4/A5 会话用例
        ↓
BM4 runtime 装配
  ↓
BM5 multiagent/tool
  ↓
BM6 middle 收口
  ↓
BM7/A8 兼容退场
  ↓
BM8/A9 终验
```

## 14. 首批执行包

### BM-001：依赖预算

- 生成 backend/middle 包图；
- 固化 16 条 backend → middle、6 条 middle → backend 边；
- 新增跨域白名单和文件规模预算测试。

### BM-002：协议模型分层

- 清点 backend 引用的 `middle/protocol/model` 类型；
- 将中性类型迁入 `contract/dto`；
- middle 保留 wire alias；
- 不改前端 JSON 字段。

### BM-003：Middle 只读端口

- 先改 `middle/view/subagent`：依赖 subagent 查询端口；
- 再改 `middle/view/state`：依赖 snapshot/event DTO；
- 最后改 `middle/view/board`：依赖 limits 查询 DTO；
- 增加 middle → backend 零依赖测试。

### BM-004：Backend 反向依赖清理

- 删除 `engine/prompt -> application/prompt`；
- 删除 `engine/task -> application/prompt`；
- 改为 backend prompt policy 端口；
- 保证 prompt 字节和缓存前缀不变。

### BM-005：Runtime 装配归属

- 先生成 `wiring/runtime` 迁移清单；
- 再移动装配入口和关闭顺序；
- `backend/engine/assemble` 保留一行 shim；
- 禁止 domain 包 import 新 runtime 包。

首批不做：

- 不先重命名整个 `backend/engine`；
- 不先拆 `multiagent/teamwork` 的所有生命周期；
- 不先改 worktree 或 WorkUnit 行为；
- 不先大改 `middle/protocol` 的 wire 字段。

先清理依赖方向和共享模型，再拆领域文件，最后删除兼容路径。
