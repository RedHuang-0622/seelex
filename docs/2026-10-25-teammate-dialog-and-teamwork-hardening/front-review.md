# 前置审查报告：Teammate 对话、快照复用与 Teamwork 收口硬化

日期：2026-10-25

状态：**审查完成，待用户确认方案**。本文与同目录 `plan.md` 是实施前依据；尚未修改生产代码。

## 需求摘要

为 mainagent、subagent、teammate 的会话提供统一的 ConversationSession 桥接能力：查看历史、发起 chat、持久快照与恢复；main 以主 Session 为窗口，subagent 以子代理 Session 为窗口，teammate 以 WorkItem Session 为窗口，不合并各自的 Session 管理生命周期；保留 `max_teammates` 的“最多创建 6 个 role”语义并新增独立 `max_jobs = 6`；teammate/subagent 并发路数与 WorkItem 工具失败返回支持可选的全局策略，默认不额外限制/不提前返回；禁止已收口里程碑重新派发，并在保留现有里程碑查看能力的前提下整合 WorkItem 四字段条目视图；补齐 teammate 的当前 WorkItem、system prompt、插件装配与能力、WorkItem 进度与完整执行记录、limit 和权限向量查看；worktree 创建失败时停止派发并经用户批准后才允许回退主工作区；修正 `team_accept` / `team_close` 生命周期；消除“作业已启动但计划/绑定/审计未落”的派发事务窗口。

## 结论先行

### 1. 三类会话目前没有统一的能力接口

当前事实需要按“能力”和“管理机制”分开看：

| 对象 | 当前实现 | 是否复用 mainagent 快照机制 |
|---|---|---|
| mainagent | 有持久化历史、驻留/冷读、内容 LRU、草稿周期快照；会话查看走 `SnapshotOf` | 无统一跨类型接口 |
| teammate | 角色执行面句柄 → 直接 `engine.History()` → `teammateSessionLiveProject`；角色会话刻意不接 DurableHistory | 查看、chat、恢复均不统一 |
| subagent 树 | 非阻塞 `Projection()`：树锁只拍浅快照，运行中不读子会话，上下文由计数 + telemetry 派生 | 独立摘要策略 |
| subagent 详情 | `NodeSessionConversation` 直接读子会话历史 + `NodeContextSnapshot` | 独立详情链路 |
| subagent 实时流 | `LiveDispatcher` 有界历史窗口 + 分页 | 独立实时链路 |

因此不能简单断言“三者都在用同一套会话管理”。目标应是统一**ConversationSession 能力接口**，由三个 adapter 分别接入 main、subagent、teammate，不合并目录、切换、审批、任务、团队计划等管理机制。

会话窗口的边界也必须钉死：

| 主体 | 对话窗口 |
|---|---|
| mainagent | 主 Session |
| subagent | 子代理自己的 Session |
| teammate | WorkItem 自己的 Session；role 只是归属，不是对外对话窗口 |

### 2. mainagent 的恢复能力没有同步到 teammate

mainagent 已有：

- 驻留/冷读分路；
- `SessionSnapshot` 会话粒度快照；
- 内容 LRU；
- 草稿周期快照；
- 冷加载 compact 后缀读；
- 读面之外采样慢端口，避免持 `ViewMu` 做文件 I/O；
- 历史分页与整份快照分离。

teammate/subagent 的当前安全策略不同：

- teammate 直接读角色引擎历史，角色引擎不接 DurableHistory，进程重启后没有可恢复 transcript；
- subagent 树为避免长流阻塞，运行中根本不读完整历史；
- subagent 详情读取单独路径；
- 实时流另有独立窗口。

subagent 已经使用与主会话同构的框架 Session + DurableHistory；真正缺失的是统一的对外能力面和 teammate 的持久恢复。

结论：**应统一 ConversationSession 的查看、chat、恢复策略；保留各自的 Session 目录、切换、生命周期管理实现。**

建议统一到：

```text
ConversationSession 能力接口
  ├─ MainConversationAdapter
  ├─ SubagentConversationAdapter
  └─ TeammateConversationAdapter
```

统一的是：

- 查看历史；
- 发起一轮 chat；
- 会话恢复状态机；
- offset/limit/total/has_more；
- 单条裁剪与 UTF-8 安全；
- running/live/interrupted/ended 状态词；
- 读取期间不长期持有外部大锁；
- 不可读时显式状态；
- 分页判据只有一份。

保留差异的是：

- main 的目录、切换、审批和任务管理；
- teammate 的团队编排与角色生命周期；
- subagent 的 fork 树与 merge-back；
- 三类会话各自的底层数据源。

teammate 为达到同样的快照保存与恢复策略，需要为每个 WorkItem Session 新增**独立的 durable transcript**；不能把角色历史写进主会话存储，也不能继续只依赖进程内 `engine.History()`。`team_close` 的显式 Session 清理仍按产品目标执行。

### 3. teammate 详情当前缺少多项必要读面

当前 teammate 详情页已有基本信息、负责的 Work Item、插件装配摘要和动态，但缺少：

- 当前 Work Item 的统一进度组件；
- base/effective system prompt；
- 插件名下的实际能力明细；
- Work Item 执行进度与完整会话记录；
- teammate 的 effective limit；
- 权限向量的原始 bits 与可读能力名。

当前 `PluginAssemblyView` 只有插件名、技能计数、工具计数；`Plugin` 本体虽然含 include/exclude/skills/MCP，但没有安全的详情投影。

### 4. WorkItem 进度目前被拆成两份读面

现有 Work Item 进度信息分散在：

- `TeamworkBoardView.WorkItems[]`：状态、归属、worktree、gate；
- `TeammateSessionLive`：只投影 user/assistant 对话；
- `workunit.Progress`：阶段、进度摘要和 in-flight 状态。

用户要求的是“同一组件、同一接口、同一链路”返回 WorkItem 进度与完整执行记录。因此需要合并为一个 `WorkItemExecutionView`，而不是前端同时调用三个接口再拼接。

### 5. 当前 `team_accept` 与用户目标不一致

当前代码 `AcceptItem` 只把 Work Item 置为 done 并销作业行，**不释放 worktree，也不清 Session**；测试还明确钉住了“验收不是回收点”。

用户目标是：

- `team_accept`：只释放该已完成 Work Item 的 worktree；
- `team_accept`：不清 Session；
- `team_close`：统一清 Session，并释放残余 worktree。

因此这是行为规格变更，不是措辞修正，必须同步代码、测试和文档。

### 4. 已有可直接使用的两阶段作业接口

Seele `jobs.Manager` 已提供：

```go
Declare(ctx, spec) (Handle, error)
Start(ctx, handle) error
```

这使派发可以采用：

```text
创建/复用 worktree
  → Declare（登记作业，不执行）
  → 写计划、绑定、审计
  → Start（最后一步启动）
```

从而消除当前“`jobs.Dispatch` 先启动执行体，后续持久化失败”的高风险窗口。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `backend/multiagent/teamwork/teamwork.go` | 修改 | `WorkerRequest`、`Options`、端口 | 增加对话请求、workspace 策略、`max_jobs` 语义 |
| `backend/multiagent/teamwork/coordinator.go` | 修改 | `Dispatch`、`ensureCapacity`、`Close` | 作业数上限、两阶段派发、收口保留 Session 规则 |
| `backend/multiagent/teamwork/items.go` | 修改 | `PlanMilestone`、`DispatchItem`、`AcceptItem`、`Recover`、release 路径 | 已收口里程碑闸门、两阶段派发、验收释放 worktree、恢复判据 |
| `backend/multiagent/teamwork/items_gate.go` | 修改 | `ItemGateOf` / `Next` | 已关闭里程碑与 workspace 状态展示 |
| `backend/multiagent/teamwork/tool_shapes.go` | 修改 | 12 组工具签名 | 增加 `team_message`，扩展 `team_dispatch` workspace 重试参数 |
| `backend/multiagent/teamwork/tool_requests.go` | 修改 | `DispatchRequest`、新增 `MessageRequest` | 对话与主工作区批准重试的入参 |
| `backend/multiagent/teamwork/worker_round.go` | 修改 | `JobRecordCeiling` | 从“人数”改为“作业数” |
| `backend/multiagent/teamwork/tool_failure.go` | 新增 | - | WorkItem 工具失败计数与可选返回策略 |
| `backend/multiagent/teamwork/tool_failure_test.go` | 新增 | - | 阈值、连续性、关闭时零行为、错误回执 |
| `backend/multiagent/teamwork/teammate_live.go` | 修改 | 历史读取、分页、状态 | 接入统一 ConversationSession |
| `backend/multiagent/teamwork/README.md` | 修改 | 工具表、生命周期、限制、测试 | 文档与实现同步 |
| `backend/engine/assemble/runtime_teamwork.go` | 修改 | 工具注册、handler、worker 执行 | 接入对话工具、结构化错误与两阶段派发 |
| `backend/engine/assemble/runtime_role_turn.go` | 修改 | role session handle / role round | 提供会话快照读面，避免 UI 直接 `History()` |
| `backend/engine/assemble/runtime_teammate_session_live.go` | 修改 | 宿主读数 | 改为统一会话读源 |
| `backend/engine/assemble/runtime_teamwork_items.go` | 修改 | `BindWorkspace` / `ReleaseWorkspaceItem` | worktree 创建失败显式返回错误；只释放 worktree |
| `backend/tool/worktree/worktree_manager.go` | 修改 | `BeginNamed` / `beginNamed` | 增加 error-returning strict 路径与可分类错误 |
| `backend/limit/limits.go` | 修改 | `TeamLimits` | 保留 `max_teammates` 的 role 上限语义，新增 `max_jobs` 与可选并发策略 |
| `backend/limit/README.md` | 修改 | 配置说明 | 作业上限语义 |
| `config/seelex.yaml` | 修改 | `limits.team` / `limits.concurrency` | 分离 role 上限、job 上限、teammate/subagent 可选并发限制 |
| `config/README.md` | 修改 | 团队限制说明 | 同步配置契约 |
| `middle/view/subagent/coordinator.go` | 修改 | `SubagentDetail` | 接入统一会话读取/分页 |
| `backend/multiagent/subagent/live_dispatch.go` | 修改 | `HistoryPage` | 复用统一分页/裁剪判据 |
| `middle/view/sessionaccess/` | 新增 | 新包 | 统一查看、chat、恢复能力接口与公共分页 |
| `backend/multiagent/teamwork/teammate_inspector.go` | 新增 | - | teammate 详情、插件能力、limit、权限向量 |
| `backend/multiagent/teamwork/workitem_execution.go` | 新增 | - | WorkItem 进度 + 完整会话记录唯一读面 |
| `backend/multiagent/agentsession/factory.go` | 新增/提取 | - | main/subagent/teammate 共用会话构造与 DurableHistory 装配；各自 registry 仍独立 |
| `application/core/session_scope.go` | 修改 | `snapshotOfResident` | 复用统一对话窗口投影 |
| `application/core/session_cold_read.go` | 修改 | `snapshotOfCold` | 复用统一对话分页口径 |
| `application/core/service_snapshot.go` | 修改 | 会话状态投影 | 保持主快照与子快照状态词一致 |
| `gui/frontend/dist/team-board-view.js` | 修改 | 里程碑详情 / teammate 详情 | 整合 WorkItem 条目视图与 teammate 详情子页 |
| `gui/frontend/dist/team-board-view.test.mjs` | 修改 | 里程碑 / teammate 页测试 | WorkItem 四字段条目、详情整合与对话分页 |
| `gui/frontend/dist/app.js` | 修改 | 团队页动作与数据加载 | 统一会话读取、WorkItem 进度与条目的下钻 |
| `plugins/default/teamwork/SKILL.md` | 修改 | 工具表 / 节奏 / 铁律 | 新工具、上限、accept/close 语义 |
| `plugins/default/goal/SKILL.md` | 修改 | 编制说明 | `max_jobs` 口径 |
| `test/teamwork_headless_smoke_test.go` | 修改 | 团队冒烟 | 对话、回退批准、accept 生命周期 |
| `test/README-teamwork.md` | 修改 | 覆盖说明 | 新用例登记 |

## 依赖分析

### 上游依赖

- `jobs.Manager.Declare/Start`：派发事务屏障。
- `backend/tool/worktree.WorktreeManager`：严格 worktree 创建错误。
- `backend/limit/limits.go`：作业上限配置。
- `middle/protocol/model`：快照与状态 DTO。
- `store`：团队计划、绑定账本与 workunit 记录。

### 下游影响

- `wiring/registry`：工具名、参数 schema、工具组登记。
- `wiring/bootstrap`：`MaxJobs` 注入。
- GUI 团队看板：里程碑详情、teammate 详情、工作项下钻。
- headless：工具与快照读数。
- goal/teamwork prompt：模型必须按新工具和错误恢复路径行动。
- 现有测试：所有 `max_teammates`、验收不释放 worktree、worktree 静默降级、teammate 无持久历史的假设都会变。

## 已确认的当前风险

| 风险 | 证据 | 严重度 |
|---|---|---|
| 作业先启动，后落计划/绑定/审计 | `items.go` 的 `DispatchItem` | 高 |
| worktree 创建失败静默落主工作区 | `runtime_teamwork_items.go` 的 `BindWorkspace` | 高 |
| `team_accept` 生命周期与目标不一致 | `items.go` 的 `AcceptItem` 与测试 | 高 |
| 上限按 job 计数却仍叫/解释成 teammate 人数 | `coordinator.go` 的 `ensureCapacity` | 高 |
| 已 `done` 里程碑仍可加工作项或派发遗留 pending item | `PlanMilestone` / `DispatchItem` | 高 |
| 会话查看/chat/恢复没有统一能力面 | main/subagent/teammate 各走一套 | 高 |
| teammate 会话重启不可恢复 | role engine 不接 DurableHistory | 高 |
| teammate 详情读面不完整 | 缺 prompt/plugin capabilities/limit/权限/完整执行记录 | 中高 |
| WorkItem 进度与完整会话记录分裂 | board + live + workunit 三处拼接 | 中高 |
| 里程碑已有查看能力完整 | 当前五个页签：basic/items/members/deps/events；本轮必须保留 | 不能回退 |
| `max_teammates` 与 `max_jobs` 语义被错误合并 | 一个是角色数，一个是作业数 | 高 |
| teammate 缺少 WorkItem 失败提前返回策略 | 只能等回合自然结束或手工 kill | 中 |
| teammate/subagent 并发只能硬编码 | 无法按部署规模选择是否限制 | 中 |

## 循环依赖检查

- [x] `middle/view/sessionaccess` 只依赖 `contract`/`middle/protocol/model`，不依赖 backend/application。
- [x] `backend/multiagent/teamwork` 继续只通过端口使用 engine/application。
- [x] `headless`/`gui` 不直接依赖 store 或 worktree manager。
- [x] `backend/tool/worktree` 不反向依赖 teamwork。
- [x] 配置适配只在 `wiring`，业务包不 import `wiring`。

## 风险预估

| 风险 | 概率 | 严重程度 | 缓解 |
|---|---|---|---|
| 会话统一端口误变成 Session 管理器 | 中 | 高 | 端口只含 View/Chat/Recover；目录、切换、审批、任务等仍归原模块 |
| 会话恢复统一后阻塞实时长流 | 中 | 高 | adapter/source strategy；保留 actor/终态快照；禁止统一后直接读活 Session |
| teammate durable history 污染主会话 | 中 | 高 | 每个 WorkItem Session 使用独立键与独立工厂，不复用 main 的上下文组件 |
| WorkItem 失败策略误删产出 | 中 | 高 | 默认关闭；“返回”只交还控制与原因，不自动 git 回滚/删除 worktree |
| 双重并发限制口径冲突 | 中 | 高 | `max_jobs` 是团队作业硬上限；teammate/subagent 设置只是可选更窄限制 |
| `team_accept` 先删 worktree 后写状态导致丢现场 | 中 | 高 | 先写 accepted/done，后释放；释放失败可幂等重试 |
| worktree 失败批准后仍误用主工作区 | 低 | 高 | 只有显式 `workspace=main` 才降级，账本记录 mode |
| 两阶段派发在 Start 前崩溃留下“运行中”计划 | 中 | 中 | 恢复时无作业即 interrupted；未执行不产生副作用 |
| 作业上限迁移破坏旧配置 | 中 | 中 | `max_jobs` 新键 + `max_teammates` 兼容读取与启动告警 |
| 对话工具绕过 Work Item 状态机 | 中 | 高 | 对话不复用 work item settle，不改变 milestone 状态；只能使用显式 role/item session |
| WorkItem 条目视图扩展为无关敏捷系统 | 中 | 中 | 只展示依赖、工作内容、负责 teammate、状态，保留现有页签，不引入 sprint、点数、日期 |

## 基线验证

审查时已运行：

```text
go test ./backend/multiagent/teamwork -count=1
go test ./backend/limit -count=1
go test ./middle/view/subagent -count=1
node --test gui/frontend/dist/team-board-view.test.mjs
```

全部通过。当前测试没有覆盖上述目标行为，因此不能把绿灯当作需求已完成。

## 建议方案

实施顺序：

1. 先统一 session read snapshot 与分页；
2. 再实现 teammate 对话 domain/tool/history；
3. 同一串行车道修复 teamwork 事务、worktree、accept/close、closed milestone；
4. `max_jobs` 与前端看板并行；
5. 最后做 prompt、README、E2E 与性能回归。

详细方案见同目录 `plan.md`。
