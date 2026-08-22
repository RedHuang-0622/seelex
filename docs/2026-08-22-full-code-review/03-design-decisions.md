# 技术设计决策解读

> 对应工作包：[README.md](README.md)。本节逐条解读当前代码中已落地的
> 关键技术决策：动机、实现位置、权衡与代价。长期架构事实以
> [`docs/arch/`](../arch/README.md) 为准，本文侧重“为什么这样做”的解读。

## 1. Hexagonal：Ports and Adapters + Anti-Corruption Layer

- 决策：`application/contract` 由消费方定义端口；根目录 adapter 与
  `seelebridge` 实现端口；TUI/GUI 只消费 DTO/Snapshot/Event。
- 实现位置：`application/contract/ports.go`、
  `internal/adapters/engine_port.go`、`seelebridge/runtime.go`。
- 动机：把“模型如何执行”（Seele 可演进）与“产品如何解释执行结果”
  （Task/Plan/审批/Workspace/前端协议稳定）分开；上游类型变化被限制在
  防腐层内。
- 权衡：增加了转换层数量（types.Message ↔ EngineMessage、DTO 映射），
  但换来前端/应用层对上游 Seele 的零直接依赖。审阅确认：application
  不 import Seele 内部包，仅消费自身 DTO 与 seelexctx/snapshot 等
  轻量类型。

## 2. Application Snapshot 是权威状态，Event 只做增量

- 决策：聊天、Plan、Session、Interaction、Runtime 状态的权威事实在
  Application；客户端启动读全量 Snapshot，随后消费带 seq/revision 的
  Event；缺口/未知/不兼容 → 重拉 Snapshot。
- 实现位置：`application/model/state.go`、`application/event/hub.go`、
  `gui/frontend/dist/protocol.js`。
- 动机：避免 TUI 和 GUI 各自维护一套业务状态机；流式 token、工具事件、
  Plan 节点状态不会因丢包永久错位。
- 权衡：事件发布方必须保证“先改状态再发事件”的顺序；慢订阅者以
  resync 兜底（牺牲增量效率换发布者不阻塞）。当前 seq 是进程内全局
  单调，跨进程传输时需按 scope 重构（v2 规划）。

## 3. ReAct 是默认执行路径，DAG Plan 是按需能力

- 决策：普通请求直接进主 ReAct Loop；只有复杂任务才经 `plan_load`/
  `plan_run` 加载 WorkPlan DAG；Effort 只约束 DAG，不做强制前置规划。
- 实现位置：`seelebridge/plan/`（Executor/ToolProvider/Preflight/
  ReplanGuard）、`application/core/plan_tools.go`、
  `internal/promptassets/assets/plan/`。
- 动机：避免简单任务承担规划延迟与 token 成本；Plan 节点获得独立
  Session/NodeScope/账号/token budget，天然并行隔离。
- 权衡：Medium/High/Max 不再强制 preflight（README 记录为演进），
  plan 工具族仅在 goal skill 激活时对主代理可见，防止自由层递归规划。

## 4. Context Engineering：预算驱动的可组合管线

- 决策：`Assembler → ToolResultProcessor → Compressor → ContextController`
  作为 seelectx 原子策略注入会话；软阈值窗口外压缩、硬阈值超大结果
  归档、压缩可读回。
- 实现位置：`seelexctx/`（assembler/processor/compressor/controller/
  window/gap/history_safety）与 `application/core/context_runtime/`。
- 动机：在成本、可审计性、任务连续性之间保持确定边界，不制造“无限
  上下文”错觉；provider history 必须保持 assistant/tool 配对。
- 权衡：token 估算使用脚本感知的本地估算（`seelexctx/tokens`）+ 事后
  provider usage 校准；估算与真实计数存在偏差，但阈值带（75/90/60%）
  留有余量。

## 5. 双层安全：ProjectScope 物理边界 + PathGate/PermissionGate 策略边界

- 决策：文件/Shell 工具先过 ProjectScope（canonical + EvalSymlinks
  containment，无绑定 root 拒绝访问），再在合法范围内过 PathGate
  zone 策略与 PermissionGate（manual/full_access），最后经
  ApprovalBroker 人工审批。
- 实现位置：`seelebridge/security/`、`seelebridge/tools/router.go`、
  `main.go setupPermissionGate`。
- 动机：Prompt 不是安全边界；“能否逃出项目”与“项目内哪些操作需审批”
  是两件独立的事，不能互相替代。
- 权衡：Windows shell 用显式系统 PowerShell 绝对路径 +
  `-NoProfile -NonInteractive`（避免 WSL shim/profile 注入）；代价是
  模型常用 Bash 语义在无 Git Bash 的机器上需要转换。

## 6. 会话持久化：Immutable Generation + 原子 manifest 切换

- 决策：按 `(project_id, session_id)` 分区；history 拆成固定大小
  immutable shards；manifest 只在完整写入后原子切换；JSON/SQLite/
  PostgreSQL/Redis 保持同一逻辑语义。
- 实现位置：`sessionstore/sessionstore.go`（Repository/Router/四后端）、
  `sessionstore/durable_history.go`、`sessionstore/project_record.go`。
- 动机：避免“为了恢复模型上下文而覆盖用户可见事实”；Plan/标题/工具
  来源/压缩 checkpoint 可独立演进；失败 generation 永不可见。
- 权衡：写放大（每次全量/大块重写 + shard 管理），换来崩溃安全与
  恢复确定性；Redis 用 project hash tag 保证同 slot 原子性。

## 7. 插件切换是事务，不是改一个 current 字段

- 决策：`prepare（attach 新 MCP）→ switch（tools+skills）→ cleanup
  （detach 旧 MCP）`；任一步失败按逆序恢复；`plugins_reload` 对磁盘
  diff 后事务式应用并支持快照回滚。
- 实现位置：`plugin/manager.go`、`plugin/apply.go`、
  `plugin/loader.go`。
- 动机：一个插件同时影响 Tool include/exclude、System Prompt、Skill
  可见性与 MCP Server；半激活状态会造成工具面撕裂。
- 权衡：Reload 回滚是全量快照重建（best-effort），插件数量多时较重；
  当前单选插件模型简化了叠加语义。

## 8. 账号池：按角色/分支路由 + 流式租约

- 决策：账号按 agent/subagent/goalplan 等 role 注册进 P2C 池；同步与
  流式共享路由规则；流式请求把租约保持到 EOF/错误/Close；Plan 分支用
  role + branchID 确定性 hash，显式 AccountID 可 pin。
- 实现位置：`seelebridge/account/`、`seelebridge/runtime_account.go`、
  `seelebridge/internal/stream/`。
- 动机：减少并发 Subagent 争用同一模型额度；相同 DAG 路由可复现；
  避免响应中途被切换账号。
- 权衡：租约持有时间变长（整条流），池规模较小时并发度受限。

## 9. 并发模型：actor/CSP mailbox 优先

- 决策：task 注册表、子代理上下文（父证据/merge-back）、子代理会话、
  定时任务等有状态组件收敛为“单消费者 goroutine + 有界 channel”；
  Application 侧的生命周期信号（plan 节点事件/task 变更/子代理树）走
  CSP channel 消费，取代同步回调嵌套。
- 实现位置：`seelebridge/internal/actor/actor.go`、
  `seelebridge/session/subagent_context.go`、
  `seelebridge/task/task.go`、`seelebridge/scheduler/scheduler.go`、
  `application/core/service_assembler.go`（startLifecycleConsumers）。
- 动机：避免多把业务锁互相嵌套造成死锁与锁序事故；mailbox 满时以
  丢弃 + 计数（merge-back）或 Snapshot resync（plan 事件）兜底，绝不
  阻塞执行者。
- 权衡：跨组件状态一致性依赖消息顺序与投影时机；主会话仍保留一把
  大锁（Service.Mu）保证 Snapshot 原子性（见
  [04-tech-leader-qa.md](04-tech-leader-qa.md) Q5）。

## 10. 双轨事件与统一事件库

- 决策：执行事实（plan/节点，A 类）经 `event.Sink` 全量落
  sessionstore 事件库；llm/tool 意图-效果（B 类）以有界脱敏摘要追加
  同库；前端增量仍走内存 EventHub（快照轨）。
- 实现位置：`seelebridge/events.go`、`seelebridge/events_unified.go`、
  `main.go`（eventStore 装配）。
- 动机：审计/恢复需要持久化事实轨，前端低延迟需要内存增量轨；B 类
  摘要避免把完整遥测双写进存储。
- 权衡：同一事实存在两个投影路径，需保证顺序一致；长期形态是统一
  事件库 + 分层投影（[docs/2026-08-14-decoupling/06-unified-event-store-decision.md](../2026-08-14-decoupling/06-unified-event-store-decision.md)）。

## 11. MCP 冷启动 + trace 中间件

- 决策：启动只登记不连接（RegisterLazyMCP，零进程开销）；首次使用时
  `mcp_load` spawn + initialize + tools/list；所有 MCP 调用经 mcpstack
  中间件记录不可变 trace（含熔断事件）。
- 实现位置：`main.go registerMCPServers`、`seelebridge/mcp/mcp.go`、
  `mcpstack/`。
- 权衡：首次调用有 30s 超时的握手延迟；换来启动确定性（不因某个 MCP
  服务器不可用而阻塞整个应用）。

## 12. 测试 Harness 与生产共享公开契约

- 决策：e2e 用 scripted engine + fixture + event recorder 走
  `application.Dependencies` 公开端口，不依赖真实 LLM/网络/秘密配置；
  生产与测试共用同一 Application contract。
- 实现位置：`e2e/scenario/`（harness/scripted_engine/runner/recorder）。
- 动机：Tool Calling、Plan projection、审批与前端协议可离线重复验证；
  真实模型 smoke 显式启用，避免把 API 可用性误当成代码正确性。

## 13. 构建与发布纪律

- 决策：版本经 `internal/buildinfo` + ldflags 注入；多实例会话目录用
  `.lock` + PID 探测自动递增；存储配置切换原子（Router swap）；
  dist 清理遵守 MEMORY.md 铁律（先查进程/备份/中文预警）。
- 实现位置：`main.go`（resolveStorePath/tryAcquireLock）、
  `internal/buildinfo/version.go`、`sessionstore/router_storage.go`。
- 动机：2026-08-12 的 `make release` 误删 dist 会话事故直接催生了
  MEMORY.md 铁律与多实例隔离。

## 14. 前端状态最小化

- 决策：TUI/GUI 只保存 viewport、光标、输入框、布局等纯 UI 状态；
  业务状态一律来自 Snapshot 与增量事件。
- 实现位置：`tui/state.go`、`gui/frontend/dist/protocol.js`。
- 动机：双前端共享同一状态机语义，杜绝“前端状态与后端事实分叉”；
  也是 protocol v2（多 SessionActor）的前置条件。
