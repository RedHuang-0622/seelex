# 会话生态位重划（Session Niche Redesign）

> 日期: 2026-09-01
> 状态: 设计稿（重构目标；尚未开始实施）
> 前置: [design-model.md](./design-model.md)（六元组与不变量）、
>       [session-async-chain.md](./session-async-chain.md)（当前异步链路）、
>       [plan.md](./plan.md)（P/R 清单）

## 0. 一句话结论

**把「会话」扶正为一等资源**：`session/` 从空壳适配层升级为会话域（容器 + 生命周期 +
可观察性 + 持久化编排）的唯一所有者；`application/core` 收窄为执行内核（chat/tool/task/plan
算法），只经会话域读写状态与发布事件。当前两个用户可见症状（运行中会话视图难切换、
task 跨会话）都是「生态位倒挂」的症状，而不是可以单独打补丁的缺陷。

## 1. 现状：生态位倒挂（证据）

### 1.1 `session/` 是空壳，会话逻辑全在 core

- [session/manager.go](../../session/manager.go) 自称「Application 与会话存储之间的用例适配层」，
  实际只有 legacy store 桥（Save/Load callback、workspace-scoped 读写、Router 装配）。
- 真正的会话用例全部在 `application/core`：
  - 生命周期: [session_history.go](../../application/core/session_history.go)（cold_load）、
    [session_lifecycle.go](../../application/core/session_lifecycle.go)（hot_attach/unload）、
    [session_scope.go](../../application/core/session_scope.go)（Activate/SnapshotOf/SubscribeSession）、
    [session_fork.go](../../application/core/session_fork.go)、[session_draft.go](../../application/core/session_draft.go)
  - 会话视图投影: [view_state/coordinator.go](../../application/core/view_state/coordinator.go)
    （SessionViews map + mirrorActiveView + AppendMessageLockedFor）
  - 会话运行态: `state.Core` 的 `sessionChat` map、`inputQueue`、`streamOutput`、
    `Snapshot`（全局活动会话镜像）——见 [internal/state/state.go](../../application/core/internal/state/state.go)
  - 持久化编排与目录: [session_runtime/coordinator.go](../../application/core/session_runtime/coordinator.go)
  - 会话级执行状态: [task_context](../../application/core/task_context)（transcript/plan stack/checkpoint）

**结论**: 名叫 `session` 的包没有会话逻辑，承载会话逻辑的包叫 `core`——命名与实体倒挂。

### 1.2 `application/core` 是上帝模块

`Service` 同时承担: 执行内核（chat 循环、工具分发、task 执行）+ 会话生命周期机 +
视图投影机 + 持久化编排 + 工作区绑定 + 事件路由 + 订阅管理。违反
`MEMORY.md` 的「上帝类/上帝模块/上帝包」红线，也导致跨域协调散落各处。

### 1.3 会话六元组散落六个包，无单一所有者

design-model 的 `S_i := (I, R, M, X, B, C)` 当前分布:

| 域 | 位置 |
|---|---|
| I 身份/标题 | `session_runtime`（sessionTitles）+ `Snapshot.Session` |
| R 持久记录 | `sessionstore`（Router/DurableHistory/events） |
| M 工作内存 | `state.Core.SessionViews` + `seelebridge` engines map + `task_context.sessionStates` |
| X 执行 | `state.Core` 的 sessionChat + `seelebridge` plan executor（全局） |
| B 绑定 | `workspace/` + `session_runtime`（LocateSession） |
| C 上下文 | `task_context`（四栈）+ `context_runtime` |

没有任何模块拥有「会话」的整体；切换/收尾/事件路由必须跨包协调，串写是必然结果。

## 2. 症状即证据

### 2.1 运行中会话视图难切换

- 桥接层把**所有**会话的事件原样转发给前端（[bridge.go](../../gui/bridge.go) `Start()`）；
- 前端 [protocol.js](../../gui/frontend/dist/protocol.js) 的 `applyEvent` 忽略
  `event.session_id`，后台会话的 `message.added`/`tool.*`/`worktable.changed`/`task.changed`
  直接改写当前会话快照；
- 后端 `Snapshot.Runtime.Plan`/`SubAgentTree` 仍是全局槽（design-model 已知限制 #3），
  [dto.PlanNodeEvent](../../application/contract/dto/plan.go) 甚至没有 session_id。

根因: 会话的「可观察性」（谁在看、看哪个会话、事件流按会话路由）没有成为一等公民。

### 2.2 task 跨会话

- [seelebridge/ports.go](../../seelebridge/ports.go) 的 `r.tasks` 是唯一全局注册表，
  `TaskAdd`/`TaskSetStatus`/`ResolveTaskByKey` 无会话路由；切换时 `SwitchSessionTasks`
  整体 `ReplaceAll` 换血；
- [work_table.go](../../application/core/work_table.go) 的 `syncTasksFromSources` 在
  plan/subagent 生命周期里无 sid 直写全局注册表。

根因: 执行产物（task/plan）没有随会话域收口，仍以「全局槽 + 切换换血」模拟会话隔离。

## 3. 目标生态位

### 3.1 `session/` = 会话域（升级为中枢模块）

拥有（唯一所有者）:

- **容器**: 会话六元组的内存形态（SessionState：identity/title/status、SessionView、
  ChatRuntime、workspace binding、上下文栈引用、per-session task/plan scope）。
- **生命周期**: COLD → PREPARED → LIVE(FG/BG) → COLD 状态机；
  `ColdLoad(sid)` / `HotAttach(sid)` / `Unload(sid)` / `SwitchTo(sid)`。
- **可观察性**: per-session 事件路由与订阅（bus 已是 session-aware，补全发件侧
  session_id 语义与前端过滤）。
- **持久化编排**: 三读一写（record/history/transcript → sessionstore），目录与标题。
- **执行句柄**: 向 core 暴露 `session.Runtime`：`AppendMessageFor(sid, ...)`、
  `PublishEventFor(sid, ...)`、`SnapshotOf(sid)`、`Submit(sid, text)` 路由、
  `CurrentView()`/`CurrentSessionID()`。

依赖: `sessionstore`（持久化）、`workspace`（binding 查询）、`seelebridge` 的
per-session 能力实例。**禁止**: 实现 chat/tool/task/plan 执行算法。

### 3.2 `application/core` = 执行内核（收窄）

拥有: chat 循环（startChat/runChat/stream/工具分发）、task 执行、plan 执行、
上下文装配（context_runtime/prompt_layer）。所有写回都经 `session.Runtime`
（AppendMessage/PublishEvent/Snapshot），不再直接持有 SessionViews/sessionChat/
inputQueue 等会话容器。Service 收敛为 composition root。

**禁止**: cold_load/hot_attach/切换/目录/持久化编排。

### 3.3 其余包收口

- `sessionstore`: 不变（原子、项目作用域持久化）。
- `workspace`: 不变（项目/session binding 权威源）。
- `seelebridge`: 收敛为能力层；plan executor / task registry / subagent tree
  收进 per-session scope（由会话域持有实例），从根上消灭全局槽。
- 前端: 订阅按会话过滤（bridge 或 protocol 层）；快照与增量事件只在 `V == i`
  时应用；运行中会话视图 = 其自身事件流。

### 3.4 依赖方向

```text
gui/tui → application.Service（装配）
application/core（执行内核）→ session.Runtime（接口，只经它写状态/发事件）
session（会话域）→ sessionstore / workspace / seelebridge(per-session 实例)
application/core 与 session 之间无循环依赖（core 依赖接口，session 依赖契约类型）
```

## 4. 迁移阶段（每阶段可独立验证）

### A. 契约抽取（纯重构，行为不变）
- 定义 `session.Runtime` 接口：从 Service 现有会话方法签名提取
  （resume/hot_attach/cold_load/unload/AppendMessageFor/PublishEventFor/
  SnapshotOf/SubscribeSession/TransitionLock）。
- `application/core` 先实现该接口（搬家不换行为），`session/` 定义接口与类型；
  编译期依赖方向：core → session（接口）。
- 验收: 全量测试 + race 全绿，零行为变化。

### B. 会话容器上移
- 把 `state.Core.SessionViews`、sessionChat map、inputQueue、streamOutput 移入
  会话域状态容器；core 只经接口读写。
- 所有会话写收口到会话域单一入口（编译期杜绝「G.V 字段当 M_i 用」）。

### C. seelebridge 收口
- plan executor / task registry / subagent tree 改 per-session 实例；
  `TaskAdd`/`TaskSetStatus` 带 sid 路由；`PlanNodeEvent` 带 session_id；
  事件发件侧补全 session_id。

### D. 前端收口
- bridge/protocol 按 `event.session_id` 过滤；快照只服务当前会话；
  运行中会话实时视图 = 其自身事件流（修掉「切不过去/串消息」）。

### E. 删除遗留
- 清理 `session/` legacy Manager 桥（或明确降级为迁移辅助）；
- 删除 `Snapshot` 全局槽字段（Conversation/Plan/WorkTable/Task 收进 SessionView）；
- core 只保留执行态。

## 5. 验证与不变量落地

- 沿用 1.3 四条不变量，并落地为测试期断言:
  - 域不相交: 任何会话写必须经会话域入口（阶段 B 后编译期保证）；
  - 视图不写执行: hot_attach 只换 `V`，不触碰 `X/M/R`（既有 TC-A3-01/02）；
  - 写自有域: `persist(i)` 每路来源带 sid（既有 TC-A4-01/02/03）；
  - 深拷贝边界: fork/冷加载引用集不相交。
- 新增契约测试:
  - 后台会话事件不得污染当前会话快照（前端 protocol + bridge）；
  - `TaskAddFor(sid)` 写自身 scope，`TaskSnapshotFor(sid)` 实时一致；
  - plan 节点事件携带 session_id 且只更新自身投影。
- 每阶段跑: `go test ./... -p 1`、`-race` 相关包、前端 `node --test`。

## 6. 风险与决策点

- **范围风险**: 这是结构性重构，建议按 A→E 推进、每阶段独立提交，不做一次性大爆炸。
- **决策点 1**: `session/` 是留在仓库根还是下沉为 `application/core/session/`？
  建议留在根（与会话域的地位匹配，且不改变 core 的包路径）。
- **决策点 2**: seelebridge 的 per-session plan/task 实例由谁持有——
  会话域组装并注入执行内核，还是 seelebridge 内部按 sid 索引？建议前者（单一所有者）。
- **决策点 3**: legacy `session.Manager` 在阶段 E 前保留兼容，还是本系列直接删除？
- **决策点 4**: 前端过滤放在 bridge 还是 protocol.js？建议 bridge 过滤 + protocol
  防御（双保险，避免后端再次出现跨会话事件时静默串写）。
