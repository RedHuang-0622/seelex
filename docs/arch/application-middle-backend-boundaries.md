# Application / Middle / Backend 依赖与状态边界

日期：2026-10-27

状态：**已接受的迁移期契约**。本文区分当前实现、迁移期例外和最终目标；代码与
结构测试仍是当前事实来源。

## 1. 决策摘要

1. 先固定层间依赖，再做 Session 聚合裁决。
2. Session 聚合最终归 `application/session`。Middle 只保留纯策略、纯窗口和读投影；
   Backend Engine 继续拥有执行运行时。
3. 最终目标是整个 `application/**` 不直接依赖具体 Backend/Middle/Store 类型。
   迁移期只允许登记过的 identity facade 暂时承担类型身份转换。
4. `TeamworkPlan` 系列走完整 DTO 化，不退化成只服务看板的一张窄投影。

## 2. 分层与所有权

| 层 | 拥有的状态/规则 | 明确不拥有 |
|---|---|---|
| `contract` | DTO、协议 wire 形状、跨层端口、纯词表 | 状态机、锁、I/O、具体实现 |
| Application use case | 用户意图、跨领域顺序、会话聚合、视图投影与回执 | Engine 执行循环、Store 原子写、Middle 纯策略副本 |
| Middle | 窗口、驻留、排序、切换选择、看板/表格等无状态投影 | SessionUnit 状态、Engine 运行态、持久化事务 |
| Backend Engine / domain | Agent loop、工具、Task、Context、执行会话、权限执行门 | 前端状态、Application 缓存、Middle 投影 |
| Store | SessionRecord、Transcript、ToolResult、Queue、Board 的原子持久化 | 业务状态机、UI 投影、Engine 生命周期 |
| Wiring | 具体实现选择、适配器注入、启动与关闭顺序 | 业务规则和第二份状态 |
| GUI/TUI/headless | 渲染、用户输入、Application DTO/Event 消费 | 业务状态机、Store/Engine 直接访问 |

## 3. 依赖方向

### 3.1 最终允许矩阵

| 来源 \ 目标 | Contract | Application | Middle | Backend | Store | Wiring |
|---|---:|---:|---:|---:|---:|---:|
| Contract | — | 否 | 否 | 否 | 否 | 否 |
| Application API/use case | 是 | 同层窄接口 | 否 | 否 | 否 | 否 |
| Middle | 是 | 否 | 同层纯投影 | 否 | 否 | 否 |
| Backend domain | 是 | 否 | 否 | 同层领域端口 | 是，仅持久化基座 | 否 |
| Store | 是 | 否 | 否 | 否 | 同层实现 | 否 |
| Wiring | 是 | 是 | 是 | 是 | 是 | 组合根 |
| Frontends | 是 | 是 | 否 | 否 | 否 | 否 |

补充规则：

- `Backend → Middle`、`Middle → Backend` 均不是稳定边。当前
  `middle → backend = 0`，`backend → middle` 剩余 6 条由 BM-005 清零。
- `Backend → Application = 0`、`Middle → Application = 0`，不得新增。
- Backend 对 Store 的现有依赖属于持久化基座，不在 Application 本轮越界处理；
  若后续要求 Backend 也完全端口化，另开 Backend 工作包。
- Application 只能在 composition root 获得具体实现；业务代码只持 Contract 或
  消费方窄端口。

### 3.2 迁移期 Application 例外

最终目标仍是 `application/**` 具体依赖为零。迁移期允许两类例外，但必须登记：

1. identity facade：只做同身份 alias 或逐项转发，不持状态、不加锁、不发事件；
2. host adapter：只把 Application 回调装配给 Middle/Backend 实现，不复制状态机。

迁移期准入条件：

- 包内没有第二份状态机、词表、阈值或恢复判据；
- 依赖方向和唯一调用方可列清；
- 有包级预算，只许下降；
- 有删除里程碑和回退方式；
- wire、revision/seq 与持久化格式保持不变。

当前 `application/core` 直接内部依赖为 0；SessionState/ViewState 已迁入
`application/session`。
整个 Application 的具体依赖包预算另行统计，不能再用 core 数字替代全局结论。

## 4. Session 聚合归属

### 4.1 裁决

Session 聚合迁入 `application/session`。

`application/session` 将拥有：

- `SessionDescriptor`：SessionID、标题、meta、workspace binding、draft、archived；
- 当前会话单元的 Chat、Queue、Approval reference 与 View window pointer；
- `ChatTurn` 的受理顺序、单活动回合约束和 queue promotion；
- restoring/draft 的进程内意图；
- Session switch/resume/restore/fork/release 的应用编排；
- 会话本地 revision 与释放门。

Middle 保留：

- `BusyState`、`SessionChoice`、`ProtectedInput` 等纯判定；
- history window、residency order/eviction、read projections；
- 不持有 `SessionUnit`、不启动 goroutine、不做持久化。

Backend Engine 保留：

- ReAct loop、provider stream、tool execution、Task/Context 运行态；
- Engine session bundle 与实时执行资源。

Store 保留：

- SessionRecord、transcript、tool result、queue recovery、board 载荷的原子持久化。

Wiring 负责把 Engine、Store、Middle 纯策略装配成 Application 端口。

### 4.2 锁与事件

- 锁只保护状态转换；Provider、工具、磁盘和事件发布均在锁外。
- 建议锁序固定为：

```text
Application session command lock
  -> session aggregate mutex
  -> chat/queue/view local mutex
```

- Middle 不得回调 Application 获取锁。
- revision/seq 的写入点在 Application 聚合内；事件在释放锁后发布。
- 查询使用不可变快照或只读投影，不得进入命令写锁。

### 4.3 聚合不变量

1. 一个 Session 同时最多有一个活动 `ChatTurn`。
2. queue 顺序在受理后稳定；召回、调序、promotion 必须 CAS 同一 revision。
3. draft 可以没有 Engine，但已有真实 SessionID；首次提交只物化一次。
4. restoring 是过渡态，不得同时成为可执行活动视图。
5. release 只能删除空闲、非视图、无审批、无队列的会话内容。
6. 所有会话路由以 SessionID 为键；project/session 名称只用于展示。

## 5. `TeamworkPlan` 契约策略

选择完整 DTO 化：

- `TeamworkPlan`、`TeamworkMember`、`TeamworkMilestone`、
  `TeamworkWorkItem`、`TeamworkState` 迁入 `contract/dto` 或专用
  `contract/protocol`；
- Store 保留同身份 alias，JSON 字段逐字不变；
- 类型方法改为自由函数，例如 `TeamworkPlanIsEmpty`，保持 DTO 纯数据；
- Middle 看板与 Backend teamwork 改用 DTO 和自己的窄端口；
- `application/execution.TeamworkPlan` 当前 alias 是迁移桥，最终删除。

不采用“只给看板做窄投影”作为终态，因为 Store、Backend teamwork、Middle board 与
Application project-board 都需要同一计划的完整字段；只做窄投影会把同一事实拆成
多套转换和漂移风险。

## 6. 验收口径

AP-003 分开两条指标：

1. 入站 API 纯度：`application/faces.go`、`application/*_api*.go` 与公开 DTO
   只能使用 `contract`/`contract/dto`/`contract/protocol`。
2. 内部实现依赖：单独统计整个 `application/**` 的具体 Backend/Middle/Store
   import；迁移期预算只许下降，最终为 0。

不能再用“`application/core` 依赖为零”替代“整个 Application 依赖为零”。

## 7. 下游影响

| 消费面 | 要求 |
|---|---|
| GUI/TUI/headless | 行为与 wire 不变；只消费 Application/Contract |
| Runtime/EventHub | revision/seq 写入点不变，发布仍在锁外 |
| SessionStore | 恢复载荷和 JSON 字段不变，alias 可迁移 |
| Snapshot reducer | resync 与增量语义不变 |
| 测试 | 每批先红后绿；adapter parity；race；依赖预算 |

## 8. 删除里程碑

| 迁移项 | 删除条件 |
|---|---|
| `application/core -> sessionusecase` | 已完成：SessionState/ViewState 已迁入 `application/session` |
| Application identity facade | 业务消费方改用 Contract 类型，预算归零 |
| `execution.TeamworkPlan` alias | TeamworkPlan 完整 DTO 化完成 |
| `backend -> middle` 6 条 | BM-005 runtime 装配迁移完成 |
| `middle -> store` board 边 | BM-005 看板 runtime 端口化完成 |

## 9. 非目标

- 不把 Engine loop 或 Store 事务搬进 Application；
- 不为减少 import 数创建万能 Session/Engine/Repository 接口；
- 不在迁移中修改 GUI/TUI wire 或持久化 JSON；
- 不把 Backend 的全部 Store 依赖在本轮一并 DTO 化。
